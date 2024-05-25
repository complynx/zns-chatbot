from motor.core import AgnosticCollection
from telegram import (
    Bot,
    Update,
    Message,
    CallbackQuery,
)
from telegram.ext import (
    CallbackContext,
    ApplicationBuilder,
    CommandHandler,
    MessageHandler,
    ConversationHandler,
    CallbackQueryHandler,
    filters,
    Application,
)
from telegram.constants import ChatAction, ParseMode
import logging
from .config import Config

logger = logging.getLogger(__name__)

class TGState:
    state: dict
    user_db: AgnosticCollection
    bot: Bot
    config: Config
    _user = None
    chat_id = None
    message_id = None
    inline_message_id = None
    update: Update|None = None
    context: CallbackContext|None = None
    message: Message|None = None
    callback_query: CallbackQuery|None = None

    def __init__(self, user: int, app, update: Update|None = None, context: CallbackContext|None = None):
        self.state = {}
        self.update = update
        self.context = context
        if update is not None:
            self.callback_query = update.callback_query
            if update.message is not None:
                self.chat_id = update.message.chat_id
                self.message_id = update.message.message_id
                self.message = update.message
            elif update.callback_query is not None:
                self.message = update.callback_query.message
                if isinstance(update.callback_query.message, Message):
                    self.chat_id = update.callback_query.message.chat_id
                    self.message_id = update.callback_query.message.message_id
            elif update.effective_user is not None:
                self.chat_id = update.effective_user.id
            elif update.effective_chat is not None:
                self.chat_id = update.effective_chat.id

            if update.callback_query is not None:
                if update.callback_query.inline_message_id:
                    self.inline_message_id = update.callback_query.inline_message_id

        self.app = app
        self.config = app.config
        self.bot = app.bot.bot
        self.user_db = app.users_collection
        self.user = user
        self.language_code = "en"
    
    def maybe_get_user(self):
        return self._user

    def l(self, s, **kwargs):
        return self.app.localization(s, args=kwargs, locale=self.language_code)
    
    async def update_user(self, request, upsert=False):
        if self.user_db is not None:
            await self.user_db.update_one({
                "user_id": self.user,
                "bot_id": self.bot.id,
            }, request, upsert=upsert)

    async def load_user(self):
        self._user = await self.user_db.find_one({
            "user_id": self.user,
            "bot_id": self.bot.id,
        })
        return self._user

    async def get_user(self):
        if self._user is None:
            return await self.load_user()
        return self._user

    async def get_state(self):
        if self.user_db is not None:
            user = await self.load_user()
            if user is None:
                self.state = {
                    "state": ""
                }
                return
            if "language_code" in user:
                self.language_code = user["language_code"]
            if "state" in user:
                self.state = user["state"]
            else:
                self.state = {
                    "state": ""
                }
    
    async def save_state(self):
        return await self.update_user({
            "$set": {
                "state": self.state,
            }
        })
    
    async def require_input(self, plugin_name: str, plugin_callback_name: str, data):
        logger.debug(f"requested text input from user {self.user}")
        self.state["state"] = "waiting_text"
        self.state["plugin"] = plugin_name
        self.state["plugin_callback"] = plugin_callback_name
        self.state["plugin_data"] = data
        await self.save_state()

    async def require_anything(self, plugin_name: str, plugin_callback_name: str, data):
        logger.debug(f"requested everything from user {self.user}")
        self.state["state"] = "waiting_everything"
        self.state["plugin"] = plugin_name
        self.state["plugin_callback"] = plugin_callback_name
        self.state["plugin_data"] = data
        await self.save_state()
    
    async def send_chat_action(self, chat_id=None, action=ChatAction.TYPING):
        if chat_id is None:
            chat_id = self.chat_id
        await self.bot.send_chat_action(
            chat_id=chat_id,
            action=action,
        )
        
    async def reply(self, text, chat_id=None, parse_mode=ParseMode.MARKDOWN_V2, *args, **kwargs):
        if chat_id is None:
            chat_id = self.chat_id
        if chat_id is None:
            chat_id = self.user

        return await self.bot.send_message(
            chat_id=chat_id,
            text=text,
            parse_mode=parse_mode,
            *args,
            **kwargs
        )
    
    async def edit_or_reply(
            self, text,
            parse_mode=ParseMode.MARKDOWN_V2,
            *args, **kwargs
        ):
        if self.callback_query is not None:
            return await self.edit_message_text(text, parse_mode=parse_mode, *args, **kwargs)

        return await self.reply(text, parse_mode=parse_mode, *args, **kwargs)

    async def delete_message(
            self,
            chat_id=None, message_id=None,
            *args, **kwargs):
        if chat_id is None:
            chat_id = self.chat_id
        if chat_id is None:
            chat_id = self.user
        if message_id is None:
            message_id = self.message_id

        return await self.bot.delete_message(
            chat_id=chat_id,
            message_id=message_id,
            *args, **kwargs
        )

    async def edit_message_text(
            self, text,
            inline_message_id=None,
            chat_id=None, message_id=None,
            *args, **kwargs):
        from telegram.error import BadRequest
        try:
            if inline_message_id is None:
                inline_message_id = self.inline_message_id
            if inline_message_id is not None:
                return await self.bot.edit_message_text(
                    text,
                    inline_message_id=self.inline_message_id,
                    *args, **kwargs
                )
            if chat_id is None:
                chat_id = self.chat_id
            if chat_id is None:
                chat_id = self.user
            if message_id is None:
                message_id = self.message_id

            return await self.bot.edit_message_text(
                text,
                chat_id=chat_id,
                message_id=message_id,
                *args, **kwargs
            )
        except BadRequest as e:
            if e.message.find("Message is not modified: ") < 0:
                raise e

    async def edit_reply_markup(
            self, reply_markup,
            inline_message_id=None,
            chat_id=None, message_id=None,
            *args, **kwargs):
        if inline_message_id is None:
            inline_message_id = self.inline_message_id
        if inline_message_id is not None:
            return await self.bot.edit_message_reply_markup(
                reply_markup=reply_markup,
                inline_message_id=self.inline_message_id,
                *args, **kwargs
            )
        if chat_id is None:
            chat_id = self.chat_id
        if chat_id is None:
            chat_id = self.user
        if message_id is None:
            message_id = self.message_id

        return await self.bot.edit_message_reply_markup(
            reply_markup=reply_markup,
            chat_id=chat_id,
            message_id=message_id,
            *args, **kwargs
        )
    
    async def forward_message(self,
            chat_id=None,
            from_chat_id=None,
            message_id=None,
            *args, **kwargs):
        if from_chat_id is None:
            from_chat_id = self.chat_id
        if from_chat_id is None:
            from_chat_id = self.user
        if message_id is None:
            message_id = self.message_id
        return await self.bot.forward_message(
            chat_id=chat_id,
            from_chat_id=self.chat_id,
            message_id=self.message_id,
            *args, **kwargs
        )
