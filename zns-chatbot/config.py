import yaml

from pydantic import BaseSettings, Field, SecretStr 

class TelegramSettings(BaseSettings):
    token: SecretStr = Field(env="TELEGRAM_TOKEN")

class LoggingSettings(BaseSettings):
    level: str = Field("WARNING", env="LOGGING_LEVEL")

class LocalizationSettings(BaseSettings):
    path: str = Field("i18n/{locale}", env="LOCALIZATION_PATH")
    fallbacks: list[str] = Field(["en-US", "en"])
    file: str = Field("bot.ftl")

class MongoDB(BaseSettings):
    address: str = Field("", env="BOT_MONGODB_ADDRESS")
    collection: str = Field("bot_users", env="BOT_MONGODB_COLLECTION")

class Config(BaseSettings):
    telegram: TelegramSettings
    logging: LoggingSettings
    localization: LocalizationSettings
    mongo_db: MongoDB

    def __init__(self, filename:str="config/config.yaml"):
        # Load a YAML configuration file
        with open(filename, 'r') as f:
            conf = yaml.safe_load(f)
        
        super().__init__(**conf)
