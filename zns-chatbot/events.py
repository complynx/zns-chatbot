from __future__ import annotations

from asyncio import CancelledError, Lock, Task, create_task, sleep
from contextlib import suppress
from dataclasses import dataclass
from datetime import datetime
import logging
from typing import Protocol, cast

from motor.motor_asyncio import AsyncIOMotorCollection, AsyncIOMotorDatabase
from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings, SettingsConfigDict

from .config import Config

logger = logging.getLogger(__name__)

EVENTS_REFRESH_INTERVAL_SECONDS: int = 3600


class EventPassTypeSettings(BaseModel):
    model_config = SettingsConfigDict(extra="ignore")

    amount: int = Field(0, ge=0)
    price: int = Field(..., gt=0)
    start: datetime = Field(datetime.max)
    promo: bool = Field(False)


class EventSettings(BaseSettings):
    model_config = SettingsConfigDict(extra="ignore")

    amount_cap_per_role: int = Field(80)
    payment_admin: list[int] | int | None = Field(None)
    hidden_payment_admins: list[int] | int | None = Field(None)
    pass_assignment_rule: str = Field("distributed")
    finish_date: datetime | None = Field(None)
    title_long: dict[str, str] | str | None = Field(None)
    title_short: dict[str, str] | str | None = Field(None)
    country_emoji: str = Field("")
    thread_channel: int | str = Field("")
    thread_id: int | None = Field(None)
    thread_locale: str = Field("ru")
    require_passport: bool = Field(False)
    price: int | None = Field(None)
    pass_types: list[EventPassTypeSettings] = Field(default_factory=list)


@dataclass(frozen=True, slots=True)
class EventPassType:
    amount: int
    price: int
    start: datetime
    promo: bool = False


def _normalize_admins(raw: list[int] | int | None) -> list[int]:
    if raw is None:
        return []
    if isinstance(raw, int):
        return [raw]
    return [int(admin_id) for admin_id in raw]


def _normalize_locale(locale: str) -> str:
    return locale.strip().lower().replace("_", "-")


def _normalize_localized_text(
    raw: dict[str, str] | str | None,
    fallback: str,
) -> dict[str, str]:
    if isinstance(raw, str):
        value = raw.strip()
        if value:
            return {"default": value}
        return {"default": fallback}
    if isinstance(raw, dict):
        localized: dict[str, str] = {}
        for locale, text in raw.items():
            if not isinstance(locale, str) or not isinstance(text, str):
                continue
            locale_key = _normalize_locale(locale)
            text_value = text.strip()
            if locale_key and text_value:
                localized[locale_key] = text_value
        if localized:
            return localized
    return {"default": fallback}


def _pick_localized_text(
    localized: dict[str, str],
    locale: str | None,
    fallback: str,
) -> str:
    candidates: list[str] = []
    base_language: str | None = None
    if isinstance(locale, str) and locale.strip():
        normalized = _normalize_locale(locale)
        candidates.append(normalized)
        if "-" in normalized:
            base_language = normalized.split("-", maxsplit=1)[0]
            candidates.append(base_language)
        else:
            base_language = normalized

    # Prefer same-language regional variants if exact/base key is not present.
    # This covers cases like locale "en" with stored key "en-us".
    if base_language:
        regional_keys = sorted(
            key for key in localized if key.startswith(f"{base_language}-")
        )
        candidates.extend(regional_keys)

    candidates.extend(["en", "ru", "default"])
    for candidate in candidates:
        value = localized.get(candidate)
        if isinstance(value, str) and value:
            return value
    if localized:
        return next(iter(localized.values()))
    return fallback


@dataclass(frozen=True, slots=True)
class EventInfo:
    key: str
    amount_cap_per_role: int
    payment_admin: list[int]
    hidden_payment_admins: list[int]
    finish_date: datetime | None
    title_long: dict[str, str]
    title_short: dict[str, str]
    country_emoji: str
    thread_channel: int | str
    thread_id: int | None
    thread_locale: str
    require_passport: bool
    price: int | None
    pass_types: tuple[EventPassType, ...]
    pass_assignment_rule: str

    @property
    def sell_start(self) -> datetime:
        if not self.pass_types:
            return datetime.max
        return min(pass_type.start for pass_type in self.pass_types)

    @classmethod
    def from_settings(cls, key: str, settings: EventSettings) -> "EventInfo":
        pass_types: tuple[EventPassType, ...] = tuple(
            EventPassType(
                amount=pass_type.amount,
                price=pass_type.price,
                start=pass_type.start,
                promo=pass_type.promo,
            )
            for pass_type in settings.pass_types
        )
        pass_assignment_rule = settings.pass_assignment_rule.strip().lower()
        if pass_assignment_rule not in {"paired", "distributed"}:
            logger.warning(
                "Event %s has unsupported pass_assignment_rule=%s, using 'paired'",
                key,
                settings.pass_assignment_rule,
            )
            pass_assignment_rule = "paired"
        return cls(
            key=key,
            amount_cap_per_role=settings.amount_cap_per_role,
            payment_admin=_normalize_admins(settings.payment_admin),
            hidden_payment_admins=_normalize_admins(settings.hidden_payment_admins),
            finish_date=settings.finish_date,
            title_long=_normalize_localized_text(settings.title_long, key),
            title_short=_normalize_localized_text(settings.title_short, key),
            country_emoji=settings.country_emoji,
            thread_channel=settings.thread_channel,
            thread_id=settings.thread_id,
            thread_locale=settings.thread_locale,
            require_passport=settings.require_passport,
            price=settings.price,
            pass_types=pass_types,
            pass_assignment_rule=pass_assignment_rule,
        )

    def title_long_for_locale(self, locale: str | None) -> str:
        return _pick_localized_text(self.title_long, locale, self.key)

    def title_short_for_locale(self, locale: str | None) -> str:
        fallback = self.title_long_for_locale(locale)
        return _pick_localized_text(self.title_short, locale, fallback)

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
        self._collection_name: str = app.config.mongo_db.events_collection
        db: AsyncIOMotorDatabase | None = app.mongodb
        self._collection: AsyncIOMotorCollection | None = (
            db[self._collection_name] if db is not None else None
        )
        self._events: dict[str, EventInfo] = {}
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
        if self._collection is None:
            if self._events:
                logger.warning("MongoDB is unavailable. Event cache has been cleared.")
            self._events = {}
            return
        async with self._refresh_lock:
            docs = cast(list[dict[str, object]], await self._collection.find({}).to_list(None))
            loaded: dict[str, EventInfo] = self._parse_event_docs(docs)
            if loaded:
                self._events = loaded
                return
            logger.warning("No events loaded from collection %s.", self._collection_name)
            self._events = {}

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
