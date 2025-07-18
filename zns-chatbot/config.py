import re
from datetime import date, datetime, time, timedelta
from typing import Tuple, Type

from pydantic import AliasChoices, Field, SecretStr
from pydantic_settings import (
    BaseSettings,
    PydanticBaseSettingsSource,
    SettingsConfigDict,
    YamlConfigSettingsSource,
)


class Party(BaseSettings):
    start: datetime
    end: datetime
    is_open: bool = Field(False)
    massage_tables: int = Field(0)


class GoogleSettings(BaseSettings):
    credentials: SecretStr = Field("")
    about_doc_id: str = Field("1Jfed4yZ-Kv_W_S1e1qMUFZPdh0nsdEEP5J0EKiqZB_Q")


class PassesEventSettings(BaseSettings):
    amount_cap_per_role: int = Field(80)
    payment_admin: list[int] | int | None = Field(None)
    sell_start: datetime = Field(datetime(2025, 1, 24, 18, 45))
    thread_channel: int | str = Field("")
    thread_id: int | None = Field(None)
    thread_locale: str = Field("ru")


class PassesSettings(BaseSettings):
    events: dict[str, PassesEventSettings] = Field(
        {
            "pass_2025_1": PassesEventSettings(
                amount_cap_per_role=80,
                sell_start=datetime(2025, 1, 24, 18, 45),
            ),
            "pass_2025_2": PassesEventSettings(
                amount_cap_per_role=6,
                sell_start=datetime(2025, 4, 7, 18, 45),
            ),
        }
    )


class TelegramSettings(BaseSettings):
    token: SecretStr = Field()
    admins: set[int] = Field({379278985})


class LanguageModel(BaseSettings):
    openai_api_key: SecretStr = Field("")
    google_api_key: SecretStr = Field("")
    model: str = Field("gemini-2.0-flash")
    simple_model: str = Field("gemini-2.0-flash-lite")
    tokenizer_model: str = Field("gpt-4o-mini")
    reply_token_cap: int = Field(3000)
    message_token_cap: int = Field(5000)
    temperature: float = Field(0.7)
    max_messages_per_user_per_day: int = Field(50)


class LoggingSettings(BaseSettings):
    level: str = Field("WARNING")


class LocalizationSettings(BaseSettings):
    path: str = Field("i18n/{locale}", validation_alias="LOCALIZATION_PATH")
    fallbacks: list[str] = Field(["en-US", "en"])
    file: str = Field("bot.ftl")


class MongoDB(BaseSettings):
    address: str = Field(
        "", validation_alias=AliasChoices("address", "BOT_MONGODB_ADDRESS")
    )
    users_collection: str = Field(
        "zns_bot_users", validation_alias="ZNS_BOT_MONGODB_USERS_COLLECTION"
    )
    messages_collection: str = Field(
        "zns_bot_messages", validation_alias="ZNS_BOT_MONGODB_MESSAGES_COLLECTION"
    )
    bots_storage: str = Field(
        "bots_storage", validation_alias="BOTS_STORAGE_COLLECTION"
    )
    food_collection: str = Field(
        "zns_bot_food", validation_alias="ZNS_BOT_FOOD_COLLECTION"
    )
    files_collection: str = Field(
        "zns_bot_files", validation_alias="ZNS_BOT_FILES_COLLECTION"
    )
    massage_collection: str = Field(
        "zns_bot_massage", validation_alias="ZNS_BOT_MASSAGE_COLLECTION"
    )


class ServerSettings(BaseSettings):
    base: str = Field("http://localhost:8080")
    port: int = Field(8080)
    auth_timeout: float = Field(1, description="days till auth expire")


class Photo(BaseSettings):
    frame_size: int = Field(1000)
    face_expand: float = Field(5)
    face_offset_y: float = Field(-0.2)
    face_offset_x: float = Field(0.2)
    quality: int = Field(95)
    cover_file: str = Field("cover/ZNS2025_1.jpg")
    frame_file: str = Field("frame/zns_2025_simple.png")
    # cover_file: str = Field("")


class Orders(BaseSettings):
    deadline: date = Field(date(2024, 6, 7))
    payment_admin_ru: int = Field(-1)
    admins: set[int] = Field({379278985})
    event_number: int = Field(8)


class Food(BaseSettings):
    activities_day: date = Field(date(2025, 6, 12))
    start_day: date = Field(date(2025, 6, 13))
    lunch_time: time = Field(time(17, 0, 0))
    dinner_time: time = Field(time(21, 0, 0))
    payment_admins: set[int] = Field(set())
    payment_admins_old: set[int] = Field(set())
    out_of_stock_admin: int = Field(379278985)
    admins: set[int] = Field({379278985})
    deadline: datetime = Field(datetime(2025, 6, 5, 1, 0, 0))
    notification_last_time: timedelta = Field(timedelta(days=1))
    notification_first_time: timedelta = Field(timedelta(days=4))
    notify_after: timedelta = Field(timedelta(hours=3))


class Massages(BaseSettings):
    max_massages_a_day: int = Field(3)
    notify_client_prior_long: timedelta = Field(timedelta(hours=1))
    notify_client_prior: timedelta = Field(timedelta(minutes=10))


class Config(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="zns_", yaml_file="config/config.yaml", env_nested_delimiter="__"
    )
    line_up: str = Field("static/line-up.csv")
    telegram: TelegramSettings
    google: GoogleSettings = GoogleSettings()
    logging: LoggingSettings = LoggingSettings()
    localization: LocalizationSettings = LocalizationSettings()
    mongo_db: MongoDB = MongoDB()
    language_model: LanguageModel = LanguageModel()
    passes: PassesSettings = PassesSettings()
    server: ServerSettings = ServerSettings()
    photo: Photo = Photo()
    food: Food = Food()
    orders: Orders = Orders()
    massages: Massages = Massages()
    parties: list[Party] = [
        Party(
            start=datetime(2025, 6, 11, 21),
            end=datetime(2025, 6, 12, 5),
            massage_tables=2,
        ),
        Party(
            start=datetime(2025, 6, 13, 14),
            end=datetime(2025, 6, 14, 5),
            massage_tables=2,
        ),
        Party(
            start=datetime(2025, 6, 14, 12),
            end=datetime(2025, 6, 15, 6),
            massage_tables=2,
        ),
        Party(
            start=datetime(2025, 6, 15, 13),
            end=datetime(2025, 6, 16, 5),
            massage_tables=2,
        ),
    ]

    @classmethod
    def settings_customise_sources(
        cls,
        settings_cls: Type[BaseSettings],
        init_settings: PydanticBaseSettingsSource,
        env_settings: PydanticBaseSettingsSource,
        dotenv_settings: PydanticBaseSettingsSource,
        file_secret_settings: PydanticBaseSettingsSource,
    ) -> Tuple[PydanticBaseSettingsSource, ...]:
        return (
            env_settings,
            YamlConfigSettingsSource(settings_cls, yaml_file_encoding="utf-8"),
            init_settings,
            dotenv_settings,
            file_secret_settings,
        )


def full_link(app, link):
    link = f"{app.config.server.base}{link}"
    match = re.match(r"http://localhost(:(\d+))?/", link)
    if match:
        port = match.group(2)
        if port is None:
            port = "80"
        # Replace the localhost part with your custom URL and port
        link = re.sub(
            r"http://localhost(:\d+)?/", f"https://complynx.net/testbot/{port}/", link
        )
    return link
