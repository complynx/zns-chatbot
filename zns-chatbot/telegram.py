from contextlib import asynccontextmanager
import json
import re
import os
import mimetypes
import tempfile
from telegram import Update, ReplyKeyboardMarkup, ReplyKeyboardRemove, KeyboardButton, WebAppInfo
from telegram.ext import (
    CallbackContext,
    ApplicationBuilder,
    CommandHandler,
    MessageHandler,
    ConversationHandler,
    filters,
    Application,
)
import logging
from .config import Config
import datetime

logger = logging.getLogger(__name__)

CROPPER = 1

def full_link(app: "TGApplication", link: str) -> str:
    link = f"{app.config.server.base}{link}"
    match = re.match(r"http://localhost(:(\d+))?/", link)
    if match:
        port = match.group(2)
        if port is None:
            port = "80"
        # Replace the localhost part with your custom URL and port
        link = re.sub(r"http://localhost(:\d+)?/", f"https://complynx.net/testbot/{port}/", link)
    return link

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
                },
                "$inc": {
                    "starts_called": 1,
                },
                "$setOnInsert": {
                    "user_id": update.effective_user.id,
                    "bot_id": context.bot.id,
                    "bot_username": context.bot.username,
                    "first_seen": datetime.datetime.now(),
                }
            }, upsert=True)
        except Exception as e:
            logger.error(f"mongodb update error: {e}", exc_info=1)

    await update.message.reply_html(l("start-message"))

async def log_msg(update: Update, context: CallbackContext):
    logger.info(f"got unparsed update {update}")

async def error_handler(update, context):
    logger.error(msg="Exception while handling an update:", exc_info=context.error)

class TGApplication(Application):
    base_app = None
    config: Config

    def __init__(self, base_app, base_config: Config, **kwargs):
        super().__init__(**kwargs)
        self.base_app = base_app
        self.config = base_config

@asynccontextmanager
async def create_telegram_bot(config: Config, app) -> TGApplication:
    global web_app_base
    application = ApplicationBuilder().application_class(TGApplication, kwargs={
        "base_app": app,
        "base_config": config
    }).token(token=config.telegram.token.get_secret_value()).build()

    application.add_handler(CommandHandler("start", start))
    application.add_handler(MessageHandler(filters.ALL, log_msg))
    application.add_error_handler(error_handler)

    try:
        await application.initialize()
        await application.start()
        await application.updater.start_polling()

        app.bot = application
        yield application
    finally:
        app.bot = None
        await application.stop()
        await application.updater.stop()
        await application.shutdown()
