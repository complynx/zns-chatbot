import os
import yaml

class Config:
    def __init__(self, config_path:str="config/config.prod.yaml"):
        with open(config_path, "r") as config_file:
            self._config_data = yaml.safe_load(config_file)

    @property
    def telegram_admins(self):
        return self._config_data["telegram"]["admins"]

    @property
    def telegram_token(self)->str:
        return os.environ.get("TELEGRAM_TOKEN", self._config_data["telegram"]["token"])

    @property
    def server_port(self)->str:
        return int(os.environ.get("SERVER_PORT", self._config_data["server"]["port"]))

    @property
    def server_base(self)->str:
        return os.environ.get("SERVER_BASE", self._config_data["server"]["base"])

    @property
    def logging_level(self)->str:
        return os.environ.get("LOGGING_LEVEL", self._config_data["logging"]["level"])

    @property
    def tasker_cpu_threads(self)->int:
        return int(os.environ.get("CPU_THREADS", str(self._config_data["photo_tasker"]["cpu_threads"])))
