from contextlib import asynccontextmanager
import json
import re
import os
import asyncio
import mimetypes
import tempfile
from telegram import Update, ReplyKeyboardMarkup, ReplyKeyboardRemove, KeyboardButton, WebAppInfo
from telegram.ext import (
    CallbackContext,
    ApplicationBuilder,
    CommandHandler,
    MessageHandler,
    ConversationHandler,
    filters
)
import logging
from .photo_task import get_by_user, PhotoTask, real_frame_size
from PIL import Image

logger = logging.getLogger(__name__)

PHOTO, CROPPER, FINISH = range(3)

web_app_base = ""

async def avatar_error(update: Update, context: CallbackContext):
    reply_markup = ReplyKeyboardRemove()
    await update.message.reply_text("Ошибка обработки фото, попробуйте ещё раз.", reply_markup=reply_markup)
    return ConversationHandler.END

async def start(update: Update, context: CallbackContext):
    """Send a welcome message when the /start command is issued."""
    logger.info(f"start called: {update.effective_user}")
    await context.bot.set_my_commands([("/avatar", "Создать аватар.")])
    await update.message.reply_text("Добро пожаловать в ZNS бот. Пока что мы умеем только делать аватарки. Для этого надо ввести команду /avatar")

async def avatar(update: Update, context: CallbackContext):
    """Handle the /avatar command, requesting a photo."""
    logger.info(f"Received /avatar command from {update.effective_user}")
    _ = PhotoTask(update.effective_chat, update.effective_user)
    markup = ReplyKeyboardMarkup([["Отмена"]], resize_keyboard=True, one_time_keyboard=True)
    await update.message.reply_text("Начнём с простого. Пришлите вашу фотографию.", reply_markup=markup)
    return PHOTO

async def photo_stage2(update: Update, context: CallbackContext, file_path:str, file_ext:str):
    try:
        task = get_by_user(update.effective_user.id)
    except KeyError:
        return await avatar_error(update, context)
    task.add_file(file_path, file_ext)

    markup = ReplyKeyboardMarkup(
        [
            # [KeyboardButton("Выбрать расположение", web_app=WebAppInfo(f"{web_app_base}/fit_frame?id={task.id.hex}"))],
            [KeyboardButton("Выбрать расположение", web_app=WebAppInfo(f"https://zouknonstop.com/itworks1.html#id={task.id.hex}"))],
            ["Так сойдёт"],
            ["Отмена"]
        ],
        resize_keyboard=True,
        one_time_keyboard=True
    )
    await update.message.reply_text("Фото загружено, теперь стоит выбрать, как оно расположится внутри рамки.", reply_markup=markup)
    return CROPPER

async def autocrop(update: Update, context: CallbackContext):
    try:
        task = get_by_user(update.effective_user.id)
    except KeyError:
        return await avatar_error(update, context)
    
    task.resize_avatar()
    return await cropped_st2(task, update, context)

async def cropped_st2(task: PhotoTask, update: Update, context: CallbackContext):
    markup = ReplyKeyboardMarkup(
        [
            ["Ок"]
        ],
        resize_keyboard=True,
        one_time_keyboard=True
    )
    await update.message.reply_text(f"Аватар вырезан и вставляется...", reply_markup=markup)
    return FINISH

async def image_crop_matrix(update: Update, context):
    try:
        task = get_by_user(update.effective_user.id)
    except KeyError:
        return await avatar_error(update, context)
    data = json.loads(update.effective_message.web_app_data.data)
    id_str = data['id']
    a = float(data['a'])
    b = float(data['b'])
    c = float(data['c'])
    d = float(data['d'])
    e = float(data['e'])
    f = float(data['f'])
    if task.id.hex != id_str:
        return await avatar_error(update, context)
    
    task.transform_avatar(a,b,c,d,e,f)

    return await cropped_st2(task, update, context)

async def photo(update: Update, context: CallbackContext):
    """Handle the photo submission as photo"""
    logger.info(f"Received avatar photo from {update.effective_user}")

    photo_file = await update.message.photo[-1].get_file()
    file_name = f"{photo_file.file_id}.jpg"
    file_path = os.path.join(tempfile.gettempdir(), file_name)
    
    await photo_file.download_to_drive(file_path)
    return await photo_stage2(update, context, file_path, "jpg")

async def photo_doc(update: Update, context: CallbackContext):
    """Handle the photo submission as document"""
    logger.info(f"Received avatar document from {update.effective_user}")

    document = update.message.document

    # Download the document
    document_file = await document.get_file()
    file_ext = mimetypes.guess_extension(document.mime_type)
    file_path = os.path.join(tempfile.gettempdir(), f"{document.file_id}.{file_ext}")
    await document_file.download_to_drive(file_path)
    return await photo_stage2(update, context, file_path, file_ext)

async def cancel(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    logger.info(f"Avatar submission for {update.effective_user} canceled")
    try:
        get_by_user(update.effective_user.id).delete()
    except KeyError:
        pass
    reply_markup = ReplyKeyboardRemove()
    await update.message.reply_text("Обработка фотографии отменена.", reply_markup=reply_markup)
    return ConversationHandler.END


@asynccontextmanager
async def create_telegram_bot(config):
    global web_app_base
    application = ApplicationBuilder().token(token=config.telegram_token).build()

    web_app_base = config.server_base
    # Conversation handler for /аватар command
    ava_handler = ConversationHandler(
        entry_points=[CommandHandler("avatar", avatar)],
        states={
            PHOTO: [MessageHandler(filters.PHOTO, photo), MessageHandler(filters.Document.IMAGE, photo_doc)],
            CROPPER: [MessageHandler(filters.StatusUpdate.WEB_APP_DATA, image_crop_matrix),MessageHandler(filters.Regex(re.compile("^(Так сойдёт)$", re.I)), cancel)],
            FINISH: [MessageHandler(filters.Regex(".*"), cancel)],
        },
        fallbacks=[CommandHandler("cancel", cancel), MessageHandler(filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)), cancel)],
    )

    application.add_handler(CommandHandler("start", start))
    application.add_handler(ava_handler)
    
    await application.initialize()
    await application.start()
    await application.updater.start_polling()
    try:
        yield application
    except Exception as ex:
        logger.exception("got while running server", ex)
    finally:
        await application.stop()
        await application.updater.stop()
        await application.shutdown()

    