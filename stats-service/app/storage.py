import asyncio
import json
import os
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any
from uuid import UUID

from redis.asyncio import Redis
from sqlalchemy import DateTime, String, Uuid, func, select
from sqlalchemy.dialects.postgresql import insert as postgresql_insert
from sqlalchemy.dialects.sqlite import insert as sqlite_insert
from sqlalchemy.ext.asyncio import (
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class SettingsError(RuntimeError):
    pass


@dataclass(frozen=True)
class KafkaSettings:
    kafka_brokers: str
    kafka_topic: str
    kafka_group_id: str


def _required_env(name: str) -> str:
    value = os.getenv(name)
    if not value:
        raise SettingsError(f"Missing required configuration: {name}")
    return value


def kafka_settings_from_env() -> KafkaSettings:
    return KafkaSettings(
        kafka_brokers=_required_env("KAFKA_BROKERS"),
        kafka_topic=_required_env("KAFKA_TOPIC_LINK_CLICKED"),
        kafka_group_id=_required_env("KAFKA_GROUP_ID"),
    )


class Base(DeclarativeBase):
    pass


class Click(Base):
    __tablename__ = "click_events"

    event_id: Mapped[UUID] = mapped_column(Uuid(as_uuid=True), primary_key=True)
    link_id: Mapped[UUID] = mapped_column(Uuid(as_uuid=True), index=True)
    slug: Mapped[str] = mapped_column(String(255))
    clicked_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), index=True)


class ClickRepository:
    def __init__(self) -> None:
        self._engine: Any = None
        self._sessions: async_sessionmaker[AsyncSession] | None = None
        self._initialized = False
        self._init_lock = asyncio.Lock()

    async def _ensure_initialized(self) -> None:
        if self._initialized:
            return
        async with self._init_lock:
            if self._initialized:
                return
            url = _required_env("DATABASE_URL")
            if url.startswith("postgres://"):
                url = "postgresql+asyncpg://" + url[len("postgres://") :]
            elif url.startswith("postgresql://"):
                url = "postgresql+asyncpg://" + url[len("postgresql://") :]
            engine = create_async_engine(url, pool_pre_ping=True)
            try:
                async with engine.begin() as connection:
                    await connection.run_sync(Base.metadata.create_all)
            except Exception:
                await engine.dispose()
                raise
            self._engine = engine
            self._sessions = async_sessionmaker(engine, expire_on_commit=False)
            self._initialized = True

    async def record_click(self, event: Any) -> None:
        await self._ensure_initialized()
        dialect = self._engine.dialect.name
        if dialect == "postgresql":
            statement = postgresql_insert(Click).values(
                event_id=event.eventId,
                link_id=event.linkId,
                slug=event.slug,
                clicked_at=event.clickedAt,
            )
        elif dialect == "sqlite":
            statement = sqlite_insert(Click).values(
                event_id=event.eventId,
                link_id=event.linkId,
                slug=event.slug,
                clicked_at=event.clickedAt,
            )
        else:
            raise RuntimeError(f"Unsupported database dialect for idempotent inserts: {dialect}")
        statement = statement.on_conflict_do_nothing(index_elements=[Click.event_id])
        async with self._sessions() as session:
            async with session.begin():
                await session.execute(statement)

    async def total_clicks(self, link_id: UUID) -> int:
        await self._ensure_initialized()
        async with self._sessions() as session:
            result = await session.execute(
                select(func.count()).select_from(Click).where(Click.link_id == link_id)
            )
            return result.scalar_one()

    async def clicks_per_day(self, link_id: UUID) -> list[dict[str, Any]]:
        await self._ensure_initialized()
        async with self._sessions() as session:
            result = await session.execute(
                select(Click.clicked_at).where(Click.link_id == link_id)
            )
            counts: Counter[str] = Counter()
            for clicked_at in result.scalars():
                if clicked_at.tzinfo is None:
                    clicked_at = clicked_at.replace(tzinfo=timezone.utc)
                date = clicked_at.astimezone(timezone.utc).date().isoformat()
                counts[date] += 1
            return [{"date": date, "clicks": counts[date]} for date in sorted(counts)]

    async def close(self) -> None:
        if self._engine is not None:
            await self._engine.dispose()
            self._engine = None
            self._sessions = None
            self._initialized = False


class RedisSummaryCache:
    def __init__(self) -> None:
        self._client: Redis | None = None

    def _get_client(self) -> Redis:
        if self._client is None:
            self._client = Redis.from_url(
                _required_env("REDIS_URL"),
                decode_responses=True,
            )
        return self._client

    @staticmethod
    def key(link_id: UUID) -> str:
        return f"stats:summary:{link_id}"

    async def get(self, link_id: UUID) -> dict[str, Any] | None:
        value = await self._get_client().get(self.key(link_id))
        if value is None:
            return None
        parsed = json.loads(value)
        if (
            not isinstance(parsed, dict)
            or set(parsed) != {"linkId", "totalClicks"}
            or parsed["linkId"] != str(link_id)
            or not isinstance(parsed["totalClicks"], int)
            or parsed["totalClicks"] < 0
        ):
            raise ValueError(f"Invalid cached summary for link {link_id}")
        return parsed

    async def set(self, link_id: UUID, value: dict[str, Any], *, ttl: int) -> None:
        await self._get_client().set(self.key(link_id), json.dumps(value), ex=ttl)

    async def invalidate(self, link_id: UUID) -> None:
        await self._get_client().delete(self.key(link_id))

    async def close(self) -> None:
        if self._client is not None:
            await self._client.aclose()
            self._client = None
