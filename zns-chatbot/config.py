import re
import yaml
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

class TelegramSettings(BaseSettings):
    token: SecretStr = Field()
    admins: set[int] = Field({379278985})

class OpenAI(BaseSettings):
    api_key: SecretStr = Field("")
    model: str = Field("gpt-4o-mini")
    simple_model: str = Field("gpt-4o-mini")
    reply_token_cap: int = Field(2000)
    message_token_cap: int = Field(2000)
    temperature: float = Field(1.06)
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
    face_offset_y: float = Field(0.2)
    face_offset_x: float = Field(0)
    frame_file: str = Field("frame/frame.png")
    flare_file: str = Field("frame/flare.png")
    quality: int = Field(90)
    cover_file: str = Field("cover/ZNS2024_2.jpg")

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
    openai: OpenAI = OpenAI()
    server: ServerSettings = ServerSettings()
    photo: Photo = Photo()
    food: Food = Food()
    orders: Orders = Orders()
    massages: Massages = Massages()
    parties: list[Party] = [
        Party(
            start=datetime(2024, 6, 12, 16),
            end=datetime(2024, 6, 13, 6),
            massage_tables=1
        ),
        Party(
            start=datetime(2024, 6, 13, 18),
            end=datetime(2024, 6, 13, 23),
            is_open=True
        ),
        Party(
            start=datetime(2024, 6, 14, 20),
            end=datetime(2024, 6, 15, 6),
            massage_tables=3
        ),
        Party(
            start=datetime(2024, 6, 15, 13),
            end=datetime(2024, 6, 16, 6),
            massage_tables=3
        ),
        Party(
            start=datetime(2024, 6, 16, 13),
            end=datetime(2024, 6, 17, 6),
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
        return env_settings, YamlConfigSettingsSource(settings_cls), init_settings, dotenv_settings, file_secret_settings

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
