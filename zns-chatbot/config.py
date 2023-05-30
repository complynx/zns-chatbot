import yaml

from pydantic import BaseSettings, Field, SecretStr 

class TelegramSettings(BaseSettings):
    token: SecretStr = Field(env="TELEGRAM_TOKEN")

class LoggingSettings(BaseSettings):
    level: str = Field("WARNING", env="LOGGING_LEVEL")

class ServerSettings(BaseSettings):
    base: str
    port: int = Field(8080, env="SERVER_PORT")

class FoodSettings(BaseSettings):
    admins: list[int] = []
    proover: int
    storage_path: str = Field("menu")

class PhotoSettings(BaseSettings):
    cpu_threads: int = Field(8)
    storage_path: str = Field("photos")

class Config(BaseSettings):
    telegram: TelegramSettings
    logging: LoggingSettings
    server: ServerSettings
    food: FoodSettings
    photo: PhotoSettings


def config(filename:str="config/config.yaml") -> Config:
    # Load a YAML configuration file
    with open(filename, 'r') as f:
        conf = yaml.safe_load(f)
    
    return Config(**conf)
