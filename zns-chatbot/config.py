import re
import yaml
from datetime import datetime, date, time, timedelta

from pydantic import BaseSettings, Field, SecretStr

class Party(BaseSettings):
    start: datetime
    end: datetime
    is_open: bool = Field(False)
    massage_tables: int = Field(0)

class TelegramSettings(BaseSettings):
    token: SecretStr = Field(env="TELEGRAM_TOKEN")
    admins: set[int] = Field({379278985})

class OpenAI(BaseSettings):
    api_key: SecretStr = Field(env="OPENAI_API_KEY")
    model: str = Field("gpt-3.5-turbo-0125")
    reply_token_cap: int = Field(3000)
    message_token_cap: int = Field(4000)
    temperature: float = Field(1)
    max_messages_per_user_per_day: int = Field(5)

class LoggingSettings(BaseSettings):
    level: str = Field("WARNING", env="LOGGING_LEVEL")

class LocalizationSettings(BaseSettings):
    path: str = Field("i18n/{locale}", env="LOCALIZATION_PATH")
    fallbacks: list[str] = Field(["en-US", "en"])
    file: str = Field("bot.ftl")

class MongoDB(BaseSettings):
    address: str = Field("", env="BOT_MONGODB_ADDRESS")
    users_collection: str = Field("zns_bot_users", env="ZNS_BOT_MONGODB_USERS_COLLECTION")
    messages_collection: str = Field("zns_bot_messages", env="ZNS_BOT_MONGODB_MESSAGES_COLLECTION")
    bots_storage: str = Field("bots_storage", env="BOTS_STORAGE_COLLECTION")
    food_collection: str = Field("zns_bot_food", env="ZNS_BOT_FOOD_COLLECTION")
    massage_collection: str = Field("zns_bot_massage", env="ZNS_BOT_MASSAGE_COLLECTION")

class ServerSettings(BaseSettings):
    base: str = Field("http://localhost:8080")
    port: int = Field(8080, env="SERVER_PORT")
    auth_timeout: float = Field(1, description="days till auth expire")

class Photo(BaseSettings):
    frame_size: int = Field(1000)
    face_expand: float = Field(5)
    face_offset_y: float = Field(0.2)
    face_offset_x: float = Field(0)
    frame_file: str = Field("frame/ZNS2024.png")
    quality: int = Field(90)
    cover_file: str = Field("cover/ZNS2024.jpg")

class Food(BaseSettings):
    deadline: date = Field(date(2024, 6, 6))
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
    telegram: TelegramSettings
    logging: LoggingSettings = LoggingSettings()
    localization: LocalizationSettings = LocalizationSettings()
    mongo_db: MongoDB = MongoDB()
    openai: OpenAI
    server: ServerSettings = ServerSettings()
    photo: Photo = Photo()
    food: Food = Food()
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

    def __init__(self, filename:str="config/config.yaml"):
        # Load a YAML configuration file
        with open(filename, 'r') as f:
            conf = yaml.safe_load(f)
        
        super().__init__(**conf)

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
