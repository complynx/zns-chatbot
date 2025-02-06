from ..config import Config
from telegram import Bot


PRIORITY_NOT_ACCEPTING = -1000
PRIORITY_BASIC = 0

class BasePlugin():
    name = "_BasePlugin"
    base_app = None

    def __init__(self, app) -> None:
        self.base_app = app
        self.config: Config = app.config
    
    @property
    def bot(self) -> Bot:
        return self.base_app.bot.bot

    def test_message(self, update, state, web_app_data):
        return PRIORITY_NOT_ACCEPTING, None

    def test_callback_query(self, query, state):
        return PRIORITY_NOT_ACCEPTING, None
    
    async def handle_callback_query(self, updater):
        raise Exception("handle_callback_query unimplemented")

    async def handle_message(self, updater):
        raise Exception("handle_message unimplemented")
