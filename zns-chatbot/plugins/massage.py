from motor.core import AgnosticCollection
from ..tg_state import TGState
from telegram import Update
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler
from telegram.constants import ParseMode
import logging

logger = logging.getLogger(__name__)

class UserMassage:
    def __init__(self, massage: 'Massage', update: TGState) -> None:
        self.update = update
        self.plugin = massage
        self.user = None
        
    async def get_user(self):
        self.user = await self.update.get_user()
        return self.user

    async def handle_callback_query(self):
        q = self.update.callback_query
        await q.answer()
        logger.info(f"Received callback_query from {self.user}, data: {q.data}")
        data = q.data.split("|")
        fn = "handle_cq_" + data[1]
        logger.debug(f"fn: {fn}")
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            logger.debug(f"fn: {attr}")
            if callable(attr):
                return await attr(*data[2:])
        logger.error(f"unknown callback {data[1]}: {data[2:]}")

    async def handle_start(self):
        pass

class Massage(BasePlugin):
    name = "massage"
    user_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.admins = self.config.telegram.admins
        self.user_db = base_app.users_collection
        self._cmd_checker = CommandHandler(self.name, self.handle_start)
        self._cq_checker = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")

    def test_message(self, message: Update, state, web_app_data):
        if self._cmd_checker.check_update(message) and message.effective_user.id in self.admins:
            return PRIORITY_BASIC, self.handle_start
        return PRIORITY_NOT_ACCEPTING, None
    
    def test_callback_query(self, query: Update, state):
        if self._cq_checker.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None
    
    async def create_user_massage(self, update) -> UserMassage:
        ret = UserMassage(self, update)
        await ret.get_user()
        return ret

    async def handle_start(self, update: TGState):
        u = await self.create_user_massage(update)
        return await u.handle_start()
    async def handle_callback_query(self, updater):
        u = await self.create_user_massage(updater)
        return await u.handle_callback_query()