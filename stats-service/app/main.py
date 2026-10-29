import asyncio
import logging
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Annotated, Any
from uuid import UUID

from aiokafka import AIOKafkaConsumer
from aiokafka.structs import OffsetAndMetadata, TopicPartition
from fastapi import FastAPI, HTTPException, Query, Request, Response
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, field_validator

from app.storage import (
    ClickRepository,
    RedisSummaryCache,
    SettingsError,
    kafka_settings_from_env,
)

SERVICE_NAME = "stats-service"
logger = logging.getLogger(__name__)


class ClickEvent(BaseModel):
    model_config = ConfigDict(extra="ignore")

    eventId: UUID
    linkId: UUID
    slug: str
    clickedAt: datetime

    @field_validator("clickedAt")
    @classmethod
    def require_utc_timestamp(cls, value: datetime) -> datetime:
        if value.tzinfo is None or value.utcoffset() != timezone.utc.utcoffset(value):
            raise ValueError("clickedAt must be an ISO timestamp in UTC")
        return value.astimezone(timezone.utc)


class StatsService:
    def __init__(self, repository: Any, cache: Any) -> None:
        self.repository = repository
        self.cache = cache

    async def summary(self, link_id: UUID) -> tuple[dict[str, Any], str]:
        cached = await self.cache.get(link_id)
        if cached is not None:
            return cached, "HIT"

        result = {
            "linkId": str(link_id),
            "totalClicks": await self.repository.total_clicks(link_id),
        }
        await self.cache.set(link_id, result, ttl=30)
        return result, "MISS"

    async def detail(self, link_id: UUID) -> tuple[dict[str, Any], str]:
        summary, cache_status = await self.summary(link_id)
        return {
            **summary,
            "clicksPerDay": await self.repository.clicks_per_day(link_id),
        }, cache_status

    async def summaries(self, link_ids: list[UUID]) -> tuple[list[dict[str, Any]], str]:
        results = []
        statuses = []
        for link_id in link_ids:
            result, status = await self.summary(link_id)
            results.append(result)
            statuses.append(status)
        cache_status = (
            "HIT" if statuses and all(status == "HIT" for status in statuses) else "MISS"
        )
        return results, cache_status


class Runtime:
    def __init__(self) -> None:
        self.repository = ClickRepository()
        self.cache = RedisSummaryCache()
        self.service = StatsService(self.repository, self.cache)
        self.consumer: AIOKafkaConsumer | None = None
        self.consumer_task: asyncio.Task[None] | None = None

    async def start(self) -> None:
        self.consumer_task = asyncio.create_task(self._consume_forever())

    async def close(self) -> None:
        errors: list[Exception] = []
        if self.consumer_task is not None:
            self.consumer_task.cancel()
            try:
                await self.consumer_task
            except asyncio.CancelledError:
                pass
            except Exception as exc:
                logger.exception("Kafka consumer task stopped with an error")
                errors.append(exc)
        if self.consumer is not None:
            try:
                await self.consumer.stop()
            except Exception as exc:
                logger.exception("Could not stop Kafka consumer cleanly")
                errors.append(exc)
        for resource in (self.cache, self.repository):
            try:
                await resource.close()
            except Exception as exc:
                logger.exception("Could not close stats-service resource")
                errors.append(exc)
        if errors:
            raise ExceptionGroup("Errors while shutting down stats-service", errors)

    async def _consume_forever(self) -> None:
        delay = 1
        while True:
            try:
                config = kafka_settings_from_env()
                consumer = AIOKafkaConsumer(
                    config.kafka_topic,
                    bootstrap_servers=config.kafka_brokers,
                    group_id=config.kafka_group_id,
                    enable_auto_commit=False,
                    auto_offset_reset="earliest",
                )
                self.consumer = consumer
                await consumer.start()
                delay = 1
                async for message in consumer:
                    await self._process_message(message)
            except asyncio.CancelledError:
                raise
            except Exception:
                logger.exception("Kafka consumer failed; reconnecting without acknowledging events")
            finally:
                if self.consumer is not None:
                    try:
                        await self.consumer.stop()
                    except Exception:
                        logger.exception("Could not stop Kafka consumer cleanly")
                    self.consumer = None
            await asyncio.sleep(delay)
            delay = min(delay * 2, 30)

    async def _process_message(self, message: Any) -> None:
        delay = 1
        while True:
            try:
                await self._process_once(message)
                return
            except asyncio.CancelledError:
                raise
            except Exception:
                logger.exception(
                    "Could not persist/process Kafka click at %s[%s] offset %s; retrying",
                    message.topic,
                    message.partition,
                    message.offset,
                )
                await asyncio.sleep(delay)
                delay = min(delay * 2, 30)

    async def _process_once(self, message: Any) -> None:
        event = ClickEvent.model_validate_json(message.value)
        await self.repository.record_click(event)
        await self.cache.invalidate(event.linkId)
        await self.consumer.commit(
            {
                TopicPartition(message.topic, message.partition): OffsetAndMetadata(
                    message.offset + 1,
                    "",
                )
            }
        )


def create_app(
    service: StatsService | None = None, *, start_consumer: bool = True
) -> FastAPI:
    runtime = Runtime() if service is None else None
    selected_service = service if service is not None else runtime.service

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        if runtime is not None and start_consumer:
            await runtime.start()
        try:
            yield
        finally:
            if runtime is not None:
                await runtime.close()

    application = FastAPI(title=SERVICE_NAME, lifespan=lifespan)
    application.state.stats_service = selected_service

    @application.exception_handler(SettingsError)
    async def settings_error_handler(_: Request, exc: SettingsError) -> JSONResponse:
        return JSONResponse(status_code=503, content={"detail": str(exc)})

    @application.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok", "service": SERVICE_NAME}

    @application.get("/stats")
    async def stats(
        link_ids: Annotated[str, Query(alias="linkIds")],
        request: Request,
        response: Response,
    ) -> list[dict[str, Any]]:
        try:
            values = [value.strip() for value in link_ids.split(",")]
            if not values or any(not value for value in values):
                raise ValueError("linkIds must contain at least one UUID")
            ids = [UUID(value) for value in values]
        except ValueError as exc:
            raise HTTPException(
                status_code=422,
                detail="linkIds must be comma-separated UUIDs",
            ) from exc
        results, cache_status = await request.app.state.stats_service.summaries(ids)
        response.headers["X-Cache"] = cache_status
        return results

    @application.get("/stats/{link_id}")
    async def link_stats(
        link_id: UUID,
        request: Request,
        response: Response,
    ) -> dict[str, Any]:
        result, cache_status = await request.app.state.stats_service.detail(link_id)
        response.headers["X-Cache"] = cache_status
        return result

    return application


app = create_app()
