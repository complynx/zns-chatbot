import yaml

from pydantic import BaseSettings, Field, SecretStr 

class TelegramSettings(BaseSettings):
    token: SecretStr = Field(env="TELEGRAM_TOKEN")

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

class Photo(BaseSettings):
    frame_size: int = Field(1000)
    frame_file: str = Field("frame/ZNS2024.png")
    quality: int = Field(90)

class Config(BaseSettings):
    telegram: TelegramSettings
    logging: LoggingSettings = LoggingSettings()
    localization: LocalizationSettings = LocalizationSettings()
    mongo_db: MongoDB = MongoDB()
    openai: OpenAI
    photo: Photo = Photo()

    def __init__(self, filename:str="config/config.yaml"):
        # Load a YAML configuration file
        with open(filename, 'r') as f:
            conf = yaml.safe_load(f)
        
        super().__init__(**conf)
