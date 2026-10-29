import asyncio
import json
from collections import Counter
from datetime import datetime, timezone
from types import SimpleNamespace
from uuid import UUID, uuid4

import pytest
from fastapi.testclient import TestClient

from app.main import ClickEvent, Runtime, StatsService, create_app
from app.storage import ClickRepository


class MemoryRepository:
    def __init__(self) -> None:
        self.events: dict[UUID, ClickEvent] = {}

    async def record_click(self, event: ClickEvent) -> None:
        self.events.setdefault(event.eventId, event)

    async def total_clicks(self, link_id: UUID) -> int:
        return sum(event.linkId == link_id for event in self.events.values())

    async def clicks_per_day(self, link_id: UUID) -> list[dict[str, object]]:
        counts: Counter[str] = Counter()
        for event in self.events.values():
            if event.linkId == link_id:
                counts[event.clickedAt.date().isoformat()] += 1
        return [{"date": date, "clicks": counts[date]} for date in sorted(counts)]


class MemoryCache:
    def __init__(self) -> None:
        self.values: dict[UUID, dict[str, object]] = {}

    async def get(self, link_id: UUID) -> dict[str, object] | None:
        return self.values.get(link_id)

    async def set(self, link_id: UUID, value: dict[str, object], *, ttl: int) -> None:
        assert ttl == 30
        self.values[link_id] = value

    async def invalidate(self, link_id: UUID) -> None:
        self.values.pop(link_id, None)


@pytest.fixture
def api():
    repository = MemoryRepository()
    cache = MemoryCache()
    client = TestClient(create_app(StatsService(repository, cache), start_consumer=False))
    with client:
        yield client, repository, cache


def _event(
    *,
    event_id: UUID | None = None,
    link_id: UUID | None = None,
    clicked_at: str = "2026-10-28T12:00:00Z",
) -> ClickEvent:
    return ClickEvent.model_validate(
        {
            "eventId": str(event_id or uuid4()),
            "linkId": str(link_id or uuid4()),
            "slug": "short-link",
            "clickedAt": clicked_at,
        }
    )


def test_unknown_link_returns_zero_and_empty_daily_stats(api):
    client, _, _ = api
    link_id = uuid4()

    response = client.get(f"/stats/{link_id}")

    assert response.status_code == 200
    assert response.json() == {
        "linkId": str(link_id),
        "totalClicks": 0,
        "clicksPerDay": [],
    }
    assert response.headers["x-cache"] == "MISS"


def test_summary_endpoint_returns_requested_links_and_cache_status(api):
    client, repository, _ = api
    first_link, second_link = uuid4(), uuid4()
    repository.events[uuid4()] = _event(link_id=first_link)

    response = client.get(f"/stats?linkIds={first_link},{second_link}")

    assert response.status_code == 200
    assert response.json() == [
        {"linkId": str(first_link), "totalClicks": 1},
        {"linkId": str(second_link), "totalClicks": 0},
    ]
    assert response.headers["x-cache"] == "MISS"

    cached = client.get(f"/stats?linkIds={first_link},{second_link}")
    assert cached.headers["x-cache"] == "HIT"


def test_detail_includes_utc_daily_counts_and_cache_hit(api):
    client, repository, _ = api
    link_id = uuid4()
    repository.events[uuid4()] = _event(link_id=link_id)
    repository.events[uuid4()] = _event(
        link_id=link_id, clicked_at="2026-10-27T23:30:00-01:00"
    )

    response = client.get(f"/stats/{link_id}")

    assert response.json() == {
        "linkId": str(link_id),
        "totalClicks": 2,
        "clicksPerDay": [{"date": "2026-10-28", "clicks": 2}],
    }
    assert response.headers["x-cache"] == "MISS"
    assert client.get(f"/stats/{link_id}").headers["x-cache"] == "HIT"


def test_invalid_link_id_is_rejected(api):
    client, _, _ = api
    assert client.get("/stats/not-a-uuid").status_code == 422
    assert client.get("/stats?linkIds=not-a-uuid").status_code == 422
    assert client.get("/stats?linkIds=").status_code == 422
    assert client.get(f"/stats?linkIds={uuid4()},").status_code == 422


def test_kafka_offset_is_committed_after_database_and_cache():
    class OrderedRepository:
        async def record_click(self, event: ClickEvent) -> None:
            order.append("database")

    class OrderedCache:
        async def invalidate(self, link_id: UUID) -> None:
            order.append("cache")

    class OrderedConsumer:
        async def commit(self, offsets: dict) -> None:
            offset = next(iter(offsets.values()))
            assert offset.offset == 8
            assert offset.metadata == ""
            order.append("commit")

    async def verify() -> None:
        runtime = Runtime()
        runtime.repository = OrderedRepository()
        runtime.cache = OrderedCache()
        runtime.consumer = OrderedConsumer()
        message = SimpleNamespace(
            value=json.dumps(
                {
                    "eventId": str(uuid4()),
                    "linkId": str(uuid4()),
                    "slug": "slug",
                    "clickedAt": "2026-10-28T12:00:00Z",
                }
            ).encode(),
            topic="link-clicked",
            partition=0,
            offset=7,
        )
        await runtime._process_once(message)

    order: list[str] = []
    asyncio.run(verify())
    assert order == ["database", "cache", "commit"]


def test_sqlite_repository_makes_event_idempotent_and_counts_days(monkeypatch):
    monkeypatch.setenv("DATABASE_URL", "sqlite+aiosqlite:///:memory:")

    async def verify() -> None:
        repository = ClickRepository()
        link_id = uuid4()
        event = ClickEvent(
            eventId=uuid4(),
            linkId=link_id,
            slug="slug",
            clickedAt=datetime(2026, 10, 28, 12, tzinfo=timezone.utc),
        )
        try:
            await repository.record_click(event)
            await repository.record_click(event)
            assert await repository.total_clicks(link_id) == 1
            assert await repository.clicks_per_day(link_id) == [
                {"date": "2026-10-28", "clicks": 1}
            ]
        finally:
            await repository.close()

    asyncio.run(verify())
