from motor.core import AgnosticCollection
from ..tg_state import TGState
from telegram import Update
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler
from telegram.constants import ParseMode
from ..telegram_links import client_user_link_html

class UserEcho(BasePlugin):
    name = "user_echo"
    admins: set[int]
    user_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.admins = self.config.telegram.admins
        self.user_db = base_app.users_collection
        self._checker = CommandHandler(self.name, self.handle_message)
        self._checker_gf = CommandHandler("get_file", self.handle_get_file)

    def test_message(self, message: Update, state, web_app_data):
        if self._checker.check_update(message) and message.effective_user.id in self.admins:
            return PRIORITY_BASIC, self.handle_message
        if self._checker_gf.check_update(message) and message.effective_user.id in self.admins:
            return PRIORITY_BASIC, self.handle_get_file
        return PRIORITY_NOT_ACCEPTING, None
    
    async def handle_get_file(self, update: TGState):
        data = update.message.text.split(" ", maxsplit=1)[1]
        abs_path = await self.base_app.avatar.get_file(data)
        await update.message.reply_document(
            abs_path,
            filename=data,
        )

    async def handle_message(self, update: TGState):
        data = update.message.text.split(" ", maxsplit=1)[1]
        uid = int(data)
        user = await self.user_db.find_one({
            "user_id": uid,
            "bot_id": update.bot.id,
        })
        if user is not None:
            await update.reply(client_user_link_html(user), parse_mode=ParseMode.HTML)
        else:
            await update.reply(f"<a href=\"tg://user?id={uid}\">Unknown</a>", parse_mode=ParseMode.HTML)
            