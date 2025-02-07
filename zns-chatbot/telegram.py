import asyncio
from contextlib import asynccontextmanager
from .tg_state import TGState
import json
import re
import os
import mimetypes
import tempfile
from telegram import (
    InlineKeyboardMarkup,
    Update,
    ReplyKeyboardMarkup,
    Message,
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
    ExtBot
)
from .plugins.base_plugin import PRIORITY_NOT_ACCEPTING
from telegram.constants import ChatAction, ParseMode
import logging
from .config import Config
import datetime

logger = logging.getLogger(__name__)

CROPPER = 1
DEADLINE_TICK = 200

def user_print_name(user: User) -> str:
    if user.full_name != "":
        return user.full_name
    return user.name

async def start(update: Update, context: CallbackContext):
    asyncio.create_task(start_task(update, context))

async def start_task(update: Update, context: CallbackContext):
    """Send a welcome message when the /start command is issued."""
    try:
        logger.info(f"start called: {update.effective_user=}")
        def l(s):  # noqa: E743
            return context.application.base_app.localization(s, locale=update.effective_user.language_code)

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
                logger.error(f"mongodb update error {update.effective_user=}: {e}", exc_info=1)

        await update.message.reply_html(l("start-message"))
    except Exception as e:
        logger.error(f"Exception in start_task {update=}: {e}", exc_info=1)

async def error_handler(update, context):
    logger.error(f"Exception while handling an update: {update=}", exc_info=context.error)

class TGUpdate(TGState):
    state: dict
    web_app_data = None
    def __init__(self, update: Update, context: CallbackContext) -> None:
        super().__init__(
            update.effective_user.id,
            context.application.base_app,
            update=update,
            context=context,
        )
    
    def l(self, s, **kwargs):
        return self.app.localization(s, args=kwargs, locale=self.update.effective_user.language_code)
    
    async def get_state(self):
        user = await self.load_user()
        if user is None and self.user_db is not None:
            await self.update_user({
                "$set": {
                    "username": self.update.effective_user.username,
                    "first_name": self.update.effective_user.first_name,
                    "last_name": self.update.effective_user.last_name,
                    "language_code": self.update.effective_user.language_code,
                    "print_name": user_print_name(self.update.effective_user),
                },
                "$inc": {
                    "starts_called": 1,
                },
                "$setOnInsert": {
                    "user_id": self.user,
                    "bot_id": self.bot.id,
                    "bot_username": self.bot.username,
                    "first_seen": datetime.datetime.now(),
                    "state": {"state":""},
                }
            }, upsert=True)
        else:
            await self.update_user({
                "$set": {
                    "username": self.update.effective_user.username,
                    "first_name": self.update.effective_user.first_name,
                    "last_name": self.update.effective_user.last_name,
                    "language_code": self.update.effective_user.language_code,
                    "print_name": user_print_name(self.update.effective_user),
                }
            }, upsert=True)
        self.language_code = self.update.effective_user.language_code
        if user is not None and "state" in user:
            self.state = user["state"]
        else:
            self.state = {
                "state": ""
            }

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
            await self.state_empty()

    async def state_waiting_everything(self):
        state = self.state
        self.state = {
            "state": ""
        }
        await self.save_state()
        logger.debug(f"received awaited input {self.user=}")
        plugin = self.context.application.plugins[state["plugin"]]
        cb = getattr(plugin, state["plugin_callback"], None)
        await cb(self, state["plugin_data"])

    async def state_cq_waiting_text(self):
        return await self.state_cq_empty()

    async def state_cq_waiting_everything(self):
        return await self.state_cq_empty()
    
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
            logger.info(f"from {hits} accepting plugins for user {self.user} selected plugin {chosen_plugin.name} based on priority {chosen_priority}")
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
            logger.info(f"from {hits} accepting plugins for user {self.user} selected plugin {chosen_plugin.name} based on priority {chosen_priority}")
            if chosen_handle is not None:
                await chosen_handle(self)
            else:
                await chosen_plugin.handle_callback_query(self)
        else:
            logger.debug(f"unsupported message: {self.update=}")
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
            logger.error(f"Failed to parse json {self.user=}: {e}", exc_info=1)

    async def parse(self):
        try:
            logger.debug(f"received message {self.update.effective_message}")
            await self.get_state()
            if filters.StatusUpdate.WEB_APP_DATA.check_update(self.update):
                logger.debug(f"data {self.update.effective_message.web_app_data.data}")
                self.parse_web_app_data()
            await self.run_state()
        except Exception as e:
            logger.error(f"Failed to parse message {self.user=}: {e}", exc_info=1)
            await self.reply(self.l("something-went-wrong"), parse_mode=ParseMode.MARKDOWN)

    async def parse_callback_query(self):
        try:
            await self.get_state()
            await self.run_callback_query()
        except Exception as e:
            logger.error(f"Failed to parse callback query {self.user=}: {e}", exc_info=1)

class TGDeadline(TGState):
    def __init__(self, user: int, app, plugins, deadline_state: dict) -> None:
        super().__init__(
            user,
            app,
        )
        self._deadline_state = deadline_state
        self._plugins = plugins
    
    async def process(self):
        try:
            user = await self.get_user()
            if user is not None and "language_code" in user:
                self.language_code = user["language_code"]

            logger.debug(f"state timeout for user {self.user}")
            plugin = self._plugins[self._deadline_state["plugin"]]
            cb = getattr(plugin, self._deadline_state["timeout_callback"], None)
            await cb(self, self._deadline_state["plugin_data"])
        except Exception as e:
            logger.error(f"Failed to process TGDeadline  {self.user=}: {e}", exc_info=1)

class TGApplication(Application):
    base_app = None
    config: Config

    def __init__(self, base_app, base_config: Config, chat_plugins, **kwargs):
        super().__init__(**kwargs)
        self.base_app = base_app
        self.config = base_config
        self.plugins = chat_plugins

async def deadline_cleaner_task(app, plugins):
    from asyncio import sleep
    from motor.core import AgnosticCollection
    user_db: AgnosticCollection = app.users_collection
    while True:
        try:
            await sleep(DEADLINE_TICK)
            now = datetime.datetime.now()
            async for user in user_db.find({
                "bot_id": app.bot.bot.id,
                "state.deadline": {"$lt": now},
            }):
                result = await user_db.update_one({
                    "bot_id": app.bot.bot.id,
                    "user_id": user["user_id"],
                    "state.state": user["state"]["state"],
                    "state.deadline": {
                        "$lte": user["state"]["deadline"] + datetime.timedelta(seconds=1),
                        "$gte": user["state"]["deadline"] - datetime.timedelta(seconds=1),
                    }
                },
                {
                    "$set": {
                        "state": {
                            "state": ""
                        }
                    }
                })
                if result.modified_count > 0:
                    d = TGDeadline(user["user_id"], app, plugins, user["state"])
                    asyncio.create_task(d.process())
        except Exception as e:
            logger.error(f"error in deadline_cleaner_task: {e}", exc_info=1)
        
async def check_startup_actions(app):
#     from asyncio import sleep
#     if not "sent_announcement" in app.storage:
#         async for user in app.users_collection.find({}):
#             try:
#                 await app.bot.bot.send_message(
#                     chat_id=user["user_id"],
#                     text="""<b>Внимание!

# ПРОПУСК</b>
# В первый день марафона 12/06 необходимо получить пропуск.
# ПАСПОРТ обязателен. Только лично.
# <b>Выдача пропусков до 23:00.</b>
# <u>Если вы опоздаете, то не сможете попасть на марафон в этот день.</u>
# Если вы приезжаете в другой день, выдача пропусков тоже до 23:00.

# <b>ВХОД</b>
# Все дни ВХОД на территорию марафона <b>строго <u>до 00:00</u>!</b>
# <u>Опоздаете на 1 сек - останетесь без марафона в эту ночь.</u>
# После 00:00 проходная работает только на выход. 

# <b>Вернуть</b> пропуск необходимо будет на проходной в последнюю ночь/утро марафона.
# <u>Если забудете, штраф 500 руб.</u>""",
#                     parse_mode=ParseMode.HTML,
#                 )
#                 await sleep(0.3)
#             except Exception as e:
#                 logger.error("Error in sender: %s", e, exc_info=1)
#         await app.storage.set("sent_announcement", 1)
    new_menu_version = 5
    if not "menu_version" in app.storage or app.storage["menu_version"] != new_menu_version:
        bot: ExtBot = app.bot.bot
        await bot.set_chat_menu_button(menu_button=MenuButtonCommands())
        for lc in ["ru", "en"]:
            def l(s, **kwargs):
                return app.localization(s, args=kwargs, locale=lc)
            commands = [
                BotCommand("passes", description=l("passes-command-description")),
            ]
            if lc != "en":
                await bot.set_my_commands(commands,language_code=lc)
            else:
                await bot.set_my_commands(commands)
        await app.storage.set("menu_version", new_menu_version)


async def parse_message(tgupdate: Update, context: CallbackContext):
    asyncio.create_task(parse_message_task(tgupdate, context))

async def parse_message_task(tgupdate: Update, context: CallbackContext):
    try:
        update = TGUpdate(tgupdate, context)
        user = await update.get_user()
        if user is None:
            return await update.reply(update.l("user-is-none"), parse_mode=ParseMode.HTML)
        if "banned" in user:
            return await update.reply(update.l("user-is-restricted"), parse_mode=ParseMode.HTML)
        await update.parse()
    except Exception as e:
        logger.error(f"Exception in parse_message_task: {e}, {tgupdate=}", exc_info=1)

async def parse_callback_query(tgupdate: Update, context: CallbackContext):
    await tgupdate.callback_query.answer()
    asyncio.create_task(parse_callback_query_task(tgupdate, context))

async def parse_callback_query_task(tgupdate: Update, context: CallbackContext):
    try:
        update = TGUpdate(tgupdate, context)
        user = await update.get_user()
        if "banned" in user:
            return await update.edit_or_reply(update.l("user-is-restricted"), parse_mode=ParseMode.HTML)
        await update.parse_callback_query()
    except Exception as e:
        logger.error(f"Exception in parse_callback_query_task: {e}, {tgupdate=}", exc_info=1)

@asynccontextmanager
async def create_telegram_bot(config: Config, app, plugins) -> TGApplication:
    global web_app_base
    application = ApplicationBuilder().application_class(TGApplication, kwargs={
        "base_app": app,
        "base_config": config,
        "chat_plugins": plugins,
    }).token(token=config.telegram.token.get_secret_value()).build()

    application.add_handler(CommandHandler("start", start))
    application.add_handler(MessageHandler(filters.ChatType.PRIVATE, parse_message))
    application.add_handler(MessageHandler(filters.StatusUpdate.WEB_APP_DATA & filters.ChatType.PRIVATE, parse_message))
    application.add_handler(CallbackQueryHandler(parse_callback_query, pattern=".*"))
    application.add_error_handler(error_handler)
#log: ["[Telegram.WebView] > postEvent","web_app_data_send",{"data":"{\"avatar_result\":\"BQACAgIAAxkBAAIEaWX4mn7_AvMKjlPV5_NlehFRlCnbAAKWSQACYmrBS1Z671_bqZQLNAQ.jpg0.20488621038131738\"}"}]
    try:
        await application.initialize()
        await application.start()
        await application.updater.start_polling()

        app.bot = application

        await app.storage.refresh(application.bot.id)
        asyncio.create_task(check_startup_actions(app))
        asyncio.create_task(deadline_cleaner_task(app, plugins))
        yield application
    finally:
        app.bot = None
        await application.stop()
        await application.updater.stop()
        await application.shutdown()
