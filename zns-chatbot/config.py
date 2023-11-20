import yaml

from pydantic import BaseSettings, Field, SecretStr 

class TelegramSettings(BaseSettings):
    token: SecretStr = Field(env="TELEGRAM_TOKEN")

class OpenAI(BaseSettings):
    api_key: SecretStr = Field(env="OPENAI_API_KEY")
    model: str = Field("gpt-3.5-turbo-1106")
    reply_token_cap: int = Field(4000)
    message_token_cap: int = Field(8000)

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

class Config(BaseSettings):
    telegram: TelegramSettings
    logging: LoggingSettings
    localization: LocalizationSettings
    mongo_db: MongoDB
    openai: OpenAI

    def __init__(self, filename:str="config/config.yaml"):
        # Load a YAML configuration file
        with open(filename, 'r') as f:
            conf = yaml.safe_load(f)
        
        super().__init__(**conf)
