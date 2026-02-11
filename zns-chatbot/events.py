from __future__ import annotations

from asyncio import CancelledError, Lock, Task, create_task, sleep
from contextlib import suppress
from dataclasses import dataclass
from datetime import datetime
import logging
from typing import Protocol, cast

from motor.motor_asyncio import AsyncIOMotorCollection, AsyncIOMotorDatabase

from .config import Config, EventSettings

logger = logging.getLogger(__name__)

EVENTS_REFRESH_INTERVAL_SECONDS: int = 3600


def _normalize_admins(raw: list[int] | int | None) -> list[int]:
    if raw is None:
        return []
    if isinstance(raw, int):
        return [raw]
    return [int(admin_id) for admin_id in raw]


@dataclass(frozen=True, slots=True)
class EventInfo:
    key: str
    amount_cap_per_role: int
    payment_admin: list[int]
    hidden_payment_admins: list[int]
    sell_start: datetime
    finish_date: datetime | None
    thread_channel: int | str
    thread_id: int | None
    thread_locale: str
    require_passport: bool
    price: int | None

    @classmethod
    def from_settings(cls, key: str, settings: EventSettings) -> "EventInfo":
        return cls(
            key=key,
            amount_cap_per_role=settings.amount_cap_per_role,
            payment_admin=_normalize_admins(settings.payment_admin),
            hidden_payment_admins=_normalize_admins(settings.hidden_payment_admins),
            sell_start=settings.sell_start,
            finish_date=settings.finish_date,
            thread_channel=settings.thread_channel,
            thread_id=settings.thread_id,
            thread_locale=settings.thread_locale,
            require_passport=settings.require_passport,
            price=settings.price,
        )

    def is_active(self, reference_time: datetime) -> bool:
        if self.finish_date is None:
            return True
        return reference_time < self.finish_date


class Events:
    # Couple notes: 1. There is no need in several instances of app race prevention as only one can run
    # 2. If no events are active it is ok to fail certain actions

    class _AppProtocol(Protocol):
        config: Config
        mongodb: AsyncIOMotorDatabase | None

    def __init__(self, app: _AppProtocol) -> None:
        self._db: AsyncIOMotorDatabase | None = app.mongodb
        self._collection_name: str = app.config.mongo_db.events_collection
        self._collection: AsyncIOMotorCollection | None = (
            self._db[self._collection_name] if self._db is not None else None
        )
        self._bootstrap_events: dict[str, EventSettings] = dict(app.config.passes.events)
        self._events: dict[str, EventInfo] = self._from_bootstrap_settings()
        self._refresh_lock: Lock = Lock()
        self._refresh_task: Task[None] | None = None

    async def start(self) -> None:
        await self.refresh()
        if self._refresh_task is None:
            self._refresh_task = create_task(self._refresh_loop())

    async def stop(self) -> None:
        if self._refresh_task is None:
            return
        self._refresh_task.cancel()
        with suppress(CancelledError):
            await self._refresh_task
        self._refresh_task = None

    async def _refresh_loop(self) -> None:
        while True:
            await sleep(EVENTS_REFRESH_INTERVAL_SECONDS)
            try:
                await self.refresh()
            except Exception as exc:
                logger.error(
                    "Events refresh failed. Keeping the previous in-memory cache: %s",
                    exc,
                    exc_info=True,
                )

    async def refresh(self) -> None:
        if self._collection is None or self._db is None:
            self._events = self._from_bootstrap_settings()
            return
        async with self._refresh_lock:
            await self._bootstrap_collection_if_needed()
            docs = cast(list[dict[str, object]], await self._collection.find({}).to_list(None))
            loaded: dict[str, EventInfo] = self._parse_event_docs(docs)
            if loaded:
                self._events = loaded
                return
            logger.warning(
                "No events loaded from collection %s. Falling back to config bootstrap.",
                self._collection_name,
            )
            self._events = self._from_bootstrap_settings()

    def all_events(self) -> tuple[EventInfo, ...]:
        return tuple(sorted(self._events.values(), key=lambda item: item.sell_start))

    def active_events(self, reference_time: datetime | None = None) -> tuple[EventInfo, ...]:
        now: datetime = reference_time or datetime.now()
        return tuple(event for event in self.all_events() if event.is_active(now))

    def all_pass_keys(self) -> list[str]:
        return [event.key for event in self.all_events()]

    def active_pass_keys(self, reference_time: datetime | None = None) -> list[str]:
        return [event.key for event in self.active_events(reference_time)]

    def get_event(self, pass_key: str) -> EventInfo | None:
        return self._events.get(pass_key)

    def closest_active_event(self, reference_time: datetime | None = None) -> EventInfo | None:
        now: datetime = reference_time or datetime.now()
        active: list[EventInfo] = list(self.active_events(now))
        if not active:
            return None
        started: list[EventInfo] = [event for event in active if event.sell_start <= now]
        if started:
            return max(started, key=lambda event: event.sell_start)
        return min(active, key=lambda event: event.sell_start)

    def closest_active_pass_key(self, reference_time: datetime | None = None) -> str:
        event: EventInfo | None = self.closest_active_event(reference_time)
        if event is None:
            raise RuntimeError("No events configured")
        return event.key

    def _from_bootstrap_settings(self) -> dict[str, EventInfo]:
        return {
            key: EventInfo.from_settings(key, settings)
            for key, settings in self._bootstrap_events.items()
        }

    async def _bootstrap_collection_if_needed(self) -> None:
        assert self._db is not None
        assert self._collection is not None
        names: list[str] = await self._db.list_collection_names()
        if self._collection_name in names:
            return
        docs: list[dict[str, object]] = [
            self._event_doc_from_settings(key, settings)
            for key, settings in self._bootstrap_events.items()
        ]
        if docs:
            await self._collection.insert_many(docs)

    def _event_doc_from_settings(self, key: str, settings: EventSettings) -> dict[str, object]:
        return {
            "key": key,
            "amount_cap_per_role": settings.amount_cap_per_role,
            "payment_admin": settings.payment_admin,
            "hidden_payment_admins": settings.hidden_payment_admins,
            "sell_start": settings.sell_start,
            "finish_date": settings.finish_date,
            "thread_channel": settings.thread_channel,
            "thread_id": settings.thread_id,
            "thread_locale": settings.thread_locale,
            "require_passport": settings.require_passport,
            "price": settings.price,
        }

    def _parse_event_docs(self, docs: list[dict[str, object]]) -> dict[str, EventInfo]:
        parsed: dict[str, EventInfo] = {}
        for doc in docs:
            raw_key = doc.get("key")
            if not isinstance(raw_key, str) or raw_key == "":
                logger.warning("Skipping event without valid key: %s", doc)
                continue
            settings_payload: dict[str, object] = {
                field_name: doc[field_name]
                for field_name in EventSettings.model_fields
                if field_name in doc
            }
            try:
                settings: EventSettings = EventSettings.model_validate(settings_payload)
            except Exception as exc:
                logger.error("Skipping event %s due to invalid payload: %s", raw_key, exc)
                continue
            parsed[raw_key] = EventInfo.from_settings(raw_key, settings)
        return parsed
