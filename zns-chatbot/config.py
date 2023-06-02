import yaml

from pydantic import BaseSettings, Field, SecretStr 
from datetime import timedelta

class MassageSettings(BaseSettings):
    data_path = Field("massage.yaml")
    conversation_timeout: timedelta = Field(timedelta(hours=2))

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
    checker_loop_frequency: timedelta = Field(timedelta(minutes=5))
    remove_empty_orders: timedelta = Field(timedelta(days=1))
    send_prompt_after: timedelta = Field(timedelta(days=1))
    send_proof_prompt_after: timedelta = Field(timedelta(hours=2))
    receive_username_conversation_timeout: timedelta = Field(timedelta(hours=1))
    receive_proof_conversation_timeout: timedelta = Field(timedelta(hours=1))

class PhotoSettings(BaseSettings):
    cpu_threads: int = Field(8)
    storage_path: str = Field("photos")
    conversation_timeout: timedelta = Field(timedelta(hours=2))

class Config(BaseSettings):
    telegram: TelegramSettings
    logging: LoggingSettings
    server: ServerSettings
    food: FoodSettings
    photo: PhotoSettings
    massage: MassageSettings


    def __init__(self, filename:str="config/config.yaml"):
        # Load a YAML configuration file
        with open(filename, 'r') as f:
            conf = yaml.safe_load(f)
        
        super().__init__(**conf)
