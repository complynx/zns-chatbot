import re
from datetime import datetime, date, time, timedelta

from pydantic import Field, SecretStr, AliasChoices
from typing import Tuple, Type

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

class PassesEventSettings(BaseSettings):
    amount_cap_per_role: int = Field(80)
    payment_admin: list[int]|int|None = Field(None)
    sell_start: datetime = Field(datetime(2025, 1, 24, 18, 45))
    thread_channel: int|str = Field("")
    thread_id: int|None = Field(None)
    thread_locale: str = Field("ru")

class PassesSettings(BaseSettings):
    events: dict[str,PassesEventSettings] = Field({
        "pass_2025_1": PassesEventSettings(
            amount_cap_per_role=80,
            sell_start=datetime(2025, 1, 24, 18, 45),
        ),
        "pass_2025_2": PassesEventSettings(
            amount_cap_per_role=6,
            sell_start=datetime(2025, 4, 7, 18, 45),
        ),
    })

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
    max_messages_per_user_per_day: int = Field(15)

class LoggingSettings(BaseSettings):
    level: str = Field("WARNING")

class LocalizationSettings(BaseSettings):
    path: str = Field("i18n/{locale}", validation_alias="LOCALIZATION_PATH")
    fallbacks: list[str] = Field(["en-US", "en"])
    file: str = Field("bot.ftl")

class MongoDB(BaseSettings):
    address: str = Field("", validation_alias=AliasChoices("address","BOT_MONGODB_ADDRESS"))
    users_collection: str = Field("zns_bot_users", validation_alias="ZNS_BOT_MONGODB_USERS_COLLECTION")
    messages_collection: str = Field("zns_bot_messages", validation_alias="ZNS_BOT_MONGODB_MESSAGES_COLLECTION")
    bots_storage: str = Field("bots_storage", validation_alias="BOTS_STORAGE_COLLECTION")
    food_collection: str = Field("zns_bot_food", validation_alias="ZNS_BOT_FOOD_COLLECTION")
    massage_collection: str = Field("zns_bot_massage", validation_alias="ZNS_BOT_MASSAGE_COLLECTION")

class ServerSettings(BaseSettings):
    base: str = Field("http://localhost:8080")
    port: int = Field(8080)
    auth_timeout: float = Field(1, description="days till auth expire")

class Photo(BaseSettings):
    frame_size: int = Field(1000)
    face_expand: float = Field(5)
    face_offset_y: float = Field(-0.2)
    face_offset_x: float = Field(0.2)
    frame_file: str = Field("frame/frame_vibes.png")
    # flare_file: str = Field("frame/flare.png")
    quality: int = Field(90)
    # cover_file: str = Field("cover/ZNS2024_2.jpg")
    cover_file: str = Field("")

class Orders(BaseSettings):
    deadline: date = Field(date(2024, 6, 7))
    payment_admin_ru: int = Field(-1)
    admins: set[int] = Field({379278985})
    event_number: int = Field(8)

class Food(BaseSettings):
    deadline: date = Field(date(2024, 6, 7))
    start_day: date = Field(date(2024, 6, 12))
    lunch_time: time = Field(time(17,0,0))
    dinner_time: time = Field(time(22,0,0))
    payment_admin: int = Field(-1)
    out_of_stock_admin: int = Field(379278985)
    admins: set[int] = Field({379278985})

class Massages(BaseSettings):
    max_massages_a_day: int = Field(3)
    notify_client_prior_long: timedelta = Field(timedelta(hours=1))
    notify_client_prior: timedelta = Field(timedelta(minutes=10))

class Config(BaseSettings):
    model_config = SettingsConfigDict(env_prefix='zns_', yaml_file="config/config.yaml", env_nested_delimiter='__')
    telegram: TelegramSettings
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
            start=datetime(2024, 9, 26, 20, 30),
            end=datetime(2024, 9, 27, 1, 30),
        ),
        Party(
            start=datetime(2024, 9, 27, 20),
            end=datetime(2024, 9, 28, 5),
            massage_tables=3
        ),
        Party(
            start=datetime(2024, 9, 28, 16),
            end=datetime(2024, 9, 29, 5),
            massage_tables=3
        ),
        Party(
            start=datetime(2024, 9, 29, 16),
            end=datetime(2024, 9, 30, 4),
            massage_tables=3
        )
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
        return env_settings, YamlConfigSettingsSource(settings_cls, yaml_file_encoding="utf-8"), init_settings, dotenv_settings, file_secret_settings

def full_link(app, link):
    link = f"{app.config.server.base}{link}"
    match = re.match(r"http://localhost(:(\d+))?/", link)
    if match:
        port = match.group(2)
        if port is None:
            port = "80"
        # Replace the localhost part with your custom URL and port
        link = re.sub(r"http://localhost(:\d+)?/", f"https://complynx.net/testbot/{port}/", link)
    return link
