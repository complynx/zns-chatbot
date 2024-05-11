import asyncio
from contextlib import asynccontextmanager
import json
import re
import os
import mimetypes
import tempfile
from telegram import (
    InlineKeyboardMarkup,
    Update,
    ReplyKeyboardMarkup,
    ReplyKeyboardRemove,
    KeyboardButton,
    WebAppInfo,
    MenuButtonCommands,
    BotCommand,
    User
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
from .plugins.base_plugin import PRIORITY_NOT_ACCEPTING
from telegram.constants import ChatAction, ParseMode
import logging
from .config import Config
import datetime

logger = logging.getLogger(__name__)

CROPPER = 1

def user_print_name(user: User) -> str:
    if user.full_name != "":
        return user.full_name
    return user.name

async def start(update: Update, context: CallbackContext):
    """Send a welcome message when the /start command is issued."""
    logger.info(f"start called: {update.effective_user}")
    l = lambda s: context.application.base_app.localization(s, locale=update.effective_user.language_code)

    if context.application.base_app.users_collection is not None:
        try:
            await context.application.base_app.users_collection.update_one({
                "user_id": update.effective_user.id,
                "bot_id": context.bot.id,
            }, {
                "$set": {
                    "username": update.effective_user.username,
                    "first_name": update.effective_user.first_name,
                    "last_name": update.effective_user.last_name,
                    "language_code": update.effective_user.language_code,
                    "print_name": user_print_name(update.effective_user),
                },
                "$inc": {
                    "starts_called": 1,
                },
                "$setOnInsert": {
                    "user_id": update.effective_user.id,
                    "bot_id": context.bot.id,
                    "bot_username": context.bot.username,
                    "first_seen": datetime.datetime.now(),
                    "state": {"state":""},
                }
            }, upsert=True)
        except Exception as e:
            logger.error(f"mongodb update error: {e}", exc_info=1)

    await update.message.reply_html(l("start-message"))

async def error_handler(update, context):
    logger.error(msg="Exception while handling an update:", exc_info=context.error)

class TGUpdate():
    state: dict
    web_app_data = None
    def __init__(self, update: Update, context: CallbackContext) -> None:
        self.update = update
        self.user = update.effective_user.id
        self.context = context
        self.app = context.application.base_app
        self.state = {}
    
    def l(self, s, **kwargs):
        return self.app.localization(s, args=kwargs, locale=self.update.effective_user.language_code)
    
    async def get_state(self):
        col = self.app.users_collection
        if col is not None:
            user = await col.find_one({
                "user_id": self.update.effective_user.id,
                "bot_id": self.context.bot.id,
            })
            if user is None:
                await self.app.users_collection.update_one({
                    "user_id": self.update.effective_user.id,
                    "bot_id": self.context.bot.id,
                }, {
                    "$set": {
                        "username": self.update.effective_user.username,
                        "first_name": self.update.effective_user.first_name,
                        "last_name": self.update.effective_user.last_name,
                        "language_code": self.update.effective_user.language_code,
                        "print_name": user_print_name(self.update.effective_user.name),
                    },
                    "$inc": {
                        "starts_called": 1,
                    },
                    "$setOnInsert": {
                        "user_id": self.update.effective_user.id,
                        "bot_id": self.context.bot.id,
                        "bot_username": self.context.bot.username,
                        "first_seen": datetime.datetime.now(),
                        "state": {"state":""},
                    }
                }, upsert=True)
                self.state = {
                    "state": ""
                }
                return
            if "state" in user:
                self.state = user["state"]
            else:
                self.state = {
                    "state": ""
                }
    
    async def save_state(self):
        col = self.app.users_collection
        if col is not None:
            await col.update_one({
                "user_id": self.update.effective_user.id,
                "bot_id": self.context.bot.id,
            }, {
                "$set": {
                    "state": self.state,
                }
            })

    async def state_waiting_text(self):
        state = self.state
        self.state = {
            "state": ""
        }
        await self.save_state()
        if (filters.TEXT & ~filters.COMMAND).check_update(self.update):
            logger.debug(f"received awaited text input from user {self.user}")
            plugin = self.context.application.plugins[state["plugin"]]
            cb = getattr(plugin, state["plugin_callback"], None)
            await cb(self, state["plugin_data"])
        else:
            logger.debug(f"was waiting text input from user {self.user}, got something else, falling back")
            await self.state_empty(self)

    async def state_waiting_everything(self):
        state = self.state
        self.state = {
            "state": ""
        }
        await self.save_state()
        logger.debug(f"received awaited input from user {self.user}")
        plugin = self.context.application.plugins[state["plugin"]]
        cb = getattr(plugin, state["plugin_callback"], None)
        await cb(self, state["plugin_data"])

    async def state_cq_waiting_text(self):
        return await self.state_cq_empty()

    async def state_cq_waiting_everything(self):
        return await self.state_cq_empty()
    
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
    
    async def state_empty(self):
        chosen_handle = None
        chosen_priority = PRIORITY_NOT_ACCEPTING
        chosen_plugin = None
        accepted_plugins = []
        hits = 0
        # TODO: for several messages chosen at the same priority, if they have priority title, send a selector message
        for _, plugin in self.context.application.plugins.items():
            priority, handle = plugin.test_message(self.update, self.state, self.web_app_data)
            if priority > PRIORITY_NOT_ACCEPTING:
                hits += 1
                accepted_plugins.append((plugin, handle))
            if priority > chosen_priority:
                chosen_priority = priority
                chosen_handle = handle
                chosen_plugin = plugin
        if chosen_plugin is not None:
            logger.info(f"from {hits} accepting plugins selected plugin {chosen_plugin.name} based on priority {chosen_priority}")
            if chosen_handle is not None:
                await chosen_handle(self)
            else:
                await chosen_plugin.handle_message(self)
        else:
            await self.update.message.reply_markdown(self.l("unsupported-message-error"))

    async def state_undefined(self):
        self.state["state"] = ""
        await self.save_state()
        await self.update.message.reply_markdown(self.l("undefined-state-error"))
    
    async def state_cq_empty(self):
        chosen_handle = None
        chosen_priority = PRIORITY_NOT_ACCEPTING
        chosen_plugin = None
        accepted_plugins = []
        hits = 0
        # TODO: for several messages chosen at the same priority, if they have priority title, send a selector message
        for _, plugin in self.context.application.plugins.items():
            priority, handle = plugin.test_callback_query(self.update, self.state)
            if priority > PRIORITY_NOT_ACCEPTING:
                hits += 1
                accepted_plugins.append((plugin, handle))
            if priority > chosen_priority:
                chosen_priority = priority
                chosen_handle = handle
                chosen_plugin = plugin
        if chosen_plugin is not None:
            logger.info(f"from {hits} accepting plugins selected plugin {chosen_plugin.name} based on priority {chosen_priority}")
            if chosen_handle is not None:
                await chosen_handle(self)
            else:
                await chosen_plugin.handle_callback_query(self)
        else:
            await self.update.callback_query.edit_message_text(
                self.l("unsupported-message-error"),
                reply_markup=InlineKeyboardMarkup([]),
                parse_mode=ParseMode.MARKDOWN
            )

    async def state_cq_undefined(self):
        self.state["state"] = ""
        await self.save_state()
        await self.update.callback_query.edit_message_text(
            self.l("undefined-state-error"),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.MARKDOWN
        )

    async def reply(self, text, chat_id=None, parse_mode=ParseMode.MARKDOWN_V2, *args, **kwargs):
        if chat_id is None:
            if self.update.message is not None:
                chat_id = self.update.message.chat_id
            elif self.update.effective_user is not None:
                chat_id = self.update.effective_user.id
            elif self.update.effective_chat is not None:
                chat_id = self.update.effective_chat.id

        return await self.context.bot.send_message(
            chat_id=chat_id,
            text=text,
            parse_mode=parse_mode,
            *args,
            **kwargs
        )
    
    async def send_chat_action(self, chat_id=None, action=ChatAction.TYPING):
        if chat_id is None:
            chat_id = self.update.message.chat_id
        await self.context.bot.send_chat_action(
            chat_id=chat_id,
            action=action,
        )

    async def run_state(self):
        state = self.state
        fn = "state_" + (state["state"] if state["state"] != "" else "empty")
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            if callable(attr):
                return await attr()
        return await self.state_undefined()

    async def run_callback_query(self):
        state = self.state
        fn = "state_cq_" + (state["state"] if state["state"] != "" else "empty")
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            if callable(attr):
                return await attr()
        return await self.state_cq_undefined()

    def parse_web_app_data(self):
        try:
            self.web_app_data = json.loads(self.update.effective_message.web_app_data.data)
        except Exception as e:
            logger.error("Failed to parse json: %s", e, exc_info=1)

    async def parse(self):
        try:
            logger.debug(f"received message {self.update.effective_message}")
            await self.get_state()
            if filters.StatusUpdate.WEB_APP_DATA.check_update(self.update):
                logger.debug(f"data {self.update.effective_message.web_app_data.data}")
                self.parse_web_app_data()
            await self.run_state()
        except Exception as e:
            logger.error("Failed to parse message: %s", e, exc_info=1)
            await self.reply(self.l("something-went-wrong"), parse_mode=ParseMode.MARKDOWN)

    async def parse_callback_query(self):
        try:
            await self.get_state()
            await self.run_callback_query()
        except Exception as e:
            logger.error("Failed to parse callback query: %s", e, exc_info=1)

class TGApplication(Application):
    base_app = None
    config: Config

    def __init__(self, base_app, base_config: Config, chat_plugins, **kwargs):
        super().__init__(**kwargs)
        self.base_app = base_app
        self.config = base_config
        self.plugins = chat_plugins
        
async def check_startup_actions(app):
    if not "usernames_inserted" in app.storage:
        logger.info("inserting usernames")
        file_path = os.path.join(os.path.dirname(__file__), 'accounts.csv')
        with open(file_path, encoding='utf-8') as csvfile:
            import csv
            reader = csv.reader(csvfile)
            for row in reader:
                uid = int(row[0])
                logger.info(f"inserting username {uid} - {row[1]}")
                await app.users_collection.update_one({
                    "user_id": uid,
                    "bot_id": app.bot.bot.id,
                }, {
                    "$set": {
                        "known_names": [row[1]]
                    },
                    "$setOnInsert": {
                        "user_id": uid,
                        "bot_id": app.bot.bot.id,
                        "bot_username": app.bot.bot.username,
                        "first_seen": datetime.datetime.now(),
                        "state": {"state":""},
                    }
                }, upsert=True)
        await app.storage.set("usernames_inserted", True)
        logger.info("inserted usernames")
    # if not "menu_version" in app.storage or app.storage["menu_version"] != 1:
    #     await app.bot.bot.set_chat_menu_button(menu_button=MenuButtonCommands())
    #     for lc in ["ru", "en"]:
    #         def l(s, **kwargs):
    #             return app.localization(s, args=kwargs, locale=lc)
    #         commands = [
    #             BotCommand("meal", description=l("food-command-description")),
    #         ]
    #         if lc != "en":
    #             await app.bot.bot.set_my_commands(commands,language_code=lc)
    #         else:
    #             await app.bot.bot.set_my_commands(commands)
    #     await app.storage.set("menu_version", 1)


async def parse_message(update: Update, context: CallbackContext):
    update = TGUpdate(update, context)
    await update.parse()
    # await context.application.base_app.assistant.reply_to(update.message.text_markdown_v2, update.effective_user.id, update.effective_message.chat_id)

async def parse_callback_query(update: Update, context: CallbackContext):
    await update.callback_query.answer()
    update = TGUpdate(update, context)
    await update.parse_callback_query()
    # await context.application.base_app.assistant.reply_to(update.message.text_markdown_v2, update.effective_user.id, update.effective_message.chat_id)

@asynccontextmanager
async def create_telegram_bot(config: Config, app, plugins) -> TGApplication:
    global web_app_base
    application = ApplicationBuilder().application_class(TGApplication, kwargs={
        "base_app": app,
        "base_config": config,
        "chat_plugins": plugins,
    }).token(token=config.telegram.token.get_secret_value()).build()

    application.add_handler(CommandHandler("start", start))
    application.add_handler(MessageHandler(filters.ALL, parse_message))
    application.add_handler(MessageHandler(filters.StatusUpdate.WEB_APP_DATA, parse_message))
    application.add_handler(CallbackQueryHandler(parse_callback_query, pattern=f".*"))
    application.add_error_handler(error_handler)
#log: ["[Telegram.WebView] > postEvent","web_app_data_send",{"data":"{\"avatar_result\":\"BQACAgIAAxkBAAIEaWX4mn7_AvMKjlPV5_NlehFRlCnbAAKWSQACYmrBS1Z671_bqZQLNAQ.jpg0.20488621038131738\"}"}]
    try:
        await application.initialize()
        await application.start()
        await application.updater.start_polling()

        app.bot = application

        await app.storage.refresh(application.bot.id)
        asyncio.create_task(check_startup_actions(app))
        yield application
    finally:
        app.bot = None
        await application.stop()
        await application.updater.stop()
        await application.shutdown()
