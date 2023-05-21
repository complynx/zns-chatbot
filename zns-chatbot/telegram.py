from contextlib import asynccontextmanager
import json
import re
import os
import mimetypes
import tempfile
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, ReplyKeyboardMarkup, ReplyKeyboardRemove, KeyboardButton, WebAppInfo
from telegram.ext import (
    CallbackContext,
    ApplicationBuilder,
    CommandHandler,
    MessageHandler,
    ConversationHandler,
    filters,
    Application,
    CallbackQueryHandler,
)
import logging

from .food import MealContext
from .photo_task import get_by_user, PhotoTask

logger = logging.getLogger(__name__)

PHOTO, CROPPER, UPSCALE, FINISH = range(4)
NAME, WAITING_PAYMENT = range(2)

web_app_base = ""
cover = "static/cover.jpg"

async def avatar_error(update: Update, context: CallbackContext):
    reply_markup = ReplyKeyboardRemove()
    try:
        get_by_user(update.effective_user.id).delete()
    except KeyError:
        pass
    except Exception as e:
        logger.error("Exception in avatar_error: %s", e, exc_info=1)
    await update.message.reply_text(
        "Ошибка обработки фото, попробуйте ещё раз.\n/avatar",
        reply_markup=reply_markup
    )
    return ConversationHandler.END

async def start(update: Update, context: CallbackContext):
    """Send a welcome message when the /start command is issued."""
    logger.info(f"start called: {update.effective_user}")
    await context.bot.set_my_commands([("/avatar", "Создать аватар.")])
    await update.message.reply_text(
        "Здравствуй, зуконавт! Меня зовут ЗиНуСя, твой виртуальный помощник 🤗\n\n"+
        "🟢 Я могу помочь сделать красивую аватарку! Для этого выбери команду\n"+
        "/avatar"
    )

async def avatar(update: Update, context: CallbackContext):
    """Handle the /avatar command, requesting a photo."""
    logger.info(f"Received /avatar command from {update.effective_user}")
    _ = PhotoTask(update.effective_chat, update.effective_user)
    markup = ReplyKeyboardMarkup([["Отмена"]], resize_keyboard=True, one_time_keyboard=True)
    await update.message.reply_text(
        "📸 Теперь отправь своё лучшее фото.\n\nP.S. Если в процессе покажется, что я "+
        "уснула, то просто разбуди меня, снова выбрав команду\n/avatar",
        reply_markup=markup
    )
    return PHOTO

async def reavatar(update: Update, context: CallbackContext):
    logger.info(f"Avatar submission for {update.effective_user} canceled")
    try:
        get_by_user(update.effective_user.id).delete()
    except KeyError:
        pass
    except Exception as e:
        logger.error("Exception in cancel: %s", e, exc_info=1)
    await update.message.reply_text("Предыдущая обработка отменена.")
    return await avatar(update, context)

async def photo_stage2(update: Update, context: CallbackContext, file_path:str, file_ext:str):
    try:
        task = get_by_user(update.effective_user.id)
    except KeyError:
        return await avatar_error(update, context)
    except Exception as e:
        logger.error("Exception in photo_stage2: %s", e, exc_info=1)
        return await avatar_error(update, context)
    task.add_file(file_path, file_ext)
    buttons = [
        [KeyboardButton("Выбрать расположение", web_app=WebAppInfo(f"{web_app_base}/fit_frame?id={task.id.hex}"))],
        ["Так сойдёт"],["Отмена"]
    ]


    markup = ReplyKeyboardMarkup(
        buttons,
        resize_keyboard=True,
        one_time_keyboard=True
    )
    await update.message.reply_text(
        "Фото загружено. Выберите как оно будет располагаться внутри рамки.",
        reply_markup=markup
    )
    return CROPPER

async def autocrop(update: Update, context: CallbackContext):
    try:
        task = get_by_user(update.effective_user.id)
    except KeyError:
        return await avatar_error(update, context)
    except Exception as e:
        logger.error("Exception in autocrop: %s", e, exc_info=1)
        return await avatar_error(update, context)
    await update.message.reply_text(f"Аватар обрабатывается... 🔄", reply_markup=ReplyKeyboardRemove())
    
    try:
        await task.resize_avatar()
    except Exception as e:
        logger.error("Exception in autocrop: %s", e, exc_info=1)
        return await avatar_error(update, context)
    return await cropped_st2(task, update, context)

async def cropped_st2(task: PhotoTask, update: Update, context: CallbackContext):
    try:
        await update.message.reply_text(
            "🪐 Уже совсем скоро твоё чудесное фото станет ещё и космическим! Процесс запущен...",
            reply_markup=ReplyKeyboardRemove()
        )
        await task.finalize_avatar()
        await update.message.reply_document(task.get_final_file(), filename="avatar.jpg")
        await update.message.reply_document(
            cover,
            caption="❗️Получившуюся аватарку рекомендуется загружать в личный профиль Вконтакте "+
            "вместе со специальной обложкой профиля."
        )
        await update.message.reply_text(
            "🔁 Если хочешь другое фото, то для этого снова используй команду\n"+
            "/avatar\n\n🛸 Всё готово! До встречи на ZNS! 🐋",
            reply_markup=ReplyKeyboardRemove()
        )
    except Exception as e:
        logger.error("Exception in cropped_st2: %s", e, exc_info=1)
        return await avatar_error(update, context)
    
    task.delete()
    return ConversationHandler.END

async def image_crop_matrix(update: Update, context):
    try:
        task = get_by_user(update.effective_user.id)
    except KeyError:
        return await avatar_error(update, context)
    except Exception as e:
        logger.error("Exception in image_crop_matrix: %s", e, exc_info=1)
        return await avatar_error(update, context)
    try:
        data = json.loads(update.effective_message.web_app_data.data)
        id_str = data['id']
        a = float(data['a'])
        b = float(data['b'])
        c = float(data['c'])
        d = float(data['d'])
        e = float(data['e'])
        f = float(data['f'])
    except Exception as e:
        logger.error("Exception in image_crop_matrix: %s", e, exc_info=1)
        return await avatar_error(update, context)
    if task.id.hex != id_str:
        return await avatar_error(update, context)
    await update.message.reply_text(f"Аватар обрабатывается...", reply_markup=ReplyKeyboardRemove())
    
    try:
        await task.transform_avatar(a,b,c,d,e,f)
    except Exception as e:
        logger.error("Exception in image_crop_matrix: %s", e, exc_info=1)
        return await avatar_error(update, context)
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

async def cancel_avatar(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    logger.info(f"Avatar submission for {update.effective_user} canceled")
    try:
        get_by_user(update.effective_user.id).delete()
    except KeyError:
        pass
    except Exception as e:
        logger.error("Exception in cancel: %s", e, exc_info=1)
    reply_markup = ReplyKeyboardRemove()
    await update.message.reply_text("Обработка фотографии отменена.", reply_markup=reply_markup)
    return ConversationHandler.END

async def food(update: Update, context: CallbackContext):
    """Handle the /food command, requesting a photo."""
    logger.info(f"Received /food command from {update.effective_user}")
    _ = PhotoTask(update.effective_chat, update.effective_user)
    markup = ReplyKeyboardMarkup([["Отмена"]], resize_keyboard=True, one_time_keyboard=True)
    await update.message.reply_html(
        "Окей, я приму заказ на еду. Сначала напиши, для кого будет еда. "+
        "Напиши полные <b>имя</b> и <b>фамилию</b> зуконавта.",
        reply_markup=markup
    )
    return NAME

async def food_for_who(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    name = update.message.text
    logger.info(f"Received meal acceptor name from {update.effective_user}: {name}")
    async with MealContext(
        tg_user_id=update.effective_user.id,
        tg_username=update.effective_user.username,
        tg_user_first_name=update.effective_user.first_name,
        tg_user_last_name=update.effective_user.last_name,
        for_who=name,
    ) as meal_context:
        # f"{web_app_base}/fit_frame?id={task.id.hex}"
        reply_markup = ReplyKeyboardRemove()
        await update.message.reply_html(
            f"Отлично, составляем меню для зуконавта по имени {name}. Для выбора блюд, "+
            f"<a href=\"{web_app_base}{meal_context.link}\">жми сюда (ссылка действительна 24 часа)</a>.",
            reply_markup=reply_markup
        )
    return ConversationHandler.END

async def food_cancel(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    logger.info(f"Food conversation for {update.effective_user} canceled")
    reply_markup = ReplyKeyboardRemove()
    await update.message.reply_text("Составление меню отменено.", reply_markup=reply_markup)
    return ConversationHandler.END

async def food_choice_reply_payment(update: Update, context) -> int:
    """Handle payment answer after menu received"""
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_reply_payment from {update.effective_user}, query: {query}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()

    # Instead of sending a new message, edit the message that
    # originated the CallbackQuery. This gives the feeling of an
    # interactive menu.
    await query.edit_message_text(text="Пришли подтверждение в виде картинки или файла.", reply_markup=ReplyKeyboardRemove())
    # return WAITING_PAYMENT
    return ConversationHandler.END

async def food_choice_reply_cancel(update: Update, context) -> int:
    """Handle payment answer after menu received"""
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_reply_payment from {update.effective_user}, query: {query}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()

    # Instead of sending a new message, edit the message that
    # originated the CallbackQuery. This gives the feeling of an
    # interactive menu.
    await query.edit_message_text(text="Я отменила этот выбор.", reply_markup=ReplyKeyboardRemove())
    return ConversationHandler.END

# async def log_msg(update: Update, context: CallbackContext):
#     logger.info(f"got message from user {update.effective_user}: {update.message}")

async def error_handler(update, context):
    logger.error(msg="Exception while handling an update:", exc_info=context.error)


@asynccontextmanager
async def create_telegram_bot(config, app) -> Application:
    global web_app_base
    application = ApplicationBuilder().token(token=config.telegram_token).build()
    application.base_app = app

    web_app_base = config.server_base
    # Conversation handler for /аватар command
    ava_handler = ConversationHandler(
        entry_points=[CommandHandler("avatar", avatar)],
        states={
            PHOTO: [
                MessageHandler(filters.PHOTO, photo),
                MessageHandler(filters.Document.IMAGE, photo_doc)
            ],
            CROPPER: [
                MessageHandler(filters.StatusUpdate.WEB_APP_DATA, image_crop_matrix),
                MessageHandler(filters.Regex(re.compile("^(Так сойдёт)$", re.I)), autocrop),
            ],
            FINISH: [MessageHandler(filters.Regex(".*"), cancel_avatar)],
        },
        fallbacks=[
            CommandHandler("cancel", cancel_avatar),
            CommandHandler("avatar", reavatar),
            MessageHandler(filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)), cancel_avatar)
        ],
    )
    food_handler = ConversationHandler(
        entry_points=[CommandHandler("food", food)],
        states={
            NAME: [
                MessageHandler(
                    filters.TEXT & ~filters.COMMAND & ~filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)),
                    food_for_who
                )
            ],
        },
        fallbacks=[
            CommandHandler("cancel", food_cancel),
            MessageHandler(filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)), food_cancel)
        ],
    )
    food_stage2_handler = ConversationHandler(
        entry_points=[
            CallbackQueryHandler(food_choice_reply_payment, pattern="^food_choice_reply_payment|[a-zA-Z_\\-0-9]$"),
            CallbackQueryHandler(food_choice_reply_cancel, pattern="^food_choice_reply_cancel|[a-zA-Z_\\-0-9]$"),
        ],
        states={
            WAITING_PAYMENT: [],
        },
        fallbacks=[
            # CallbackQueryHandler(food_choice_reply_cancel, pattern="^food_choice_reply_cancel|[a-zA-Z_-0-9]$"),
            # CommandHandler("cancel", food_cancel),
            # MessageHandler(filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)), food_cancel)
        ],
    )

    application.add_handler(CommandHandler("start", start))
    application.add_handler(food_handler)
    application.add_handler(ava_handler)
    application.add_handler(food_stage2_handler)
    # application.add_handler(MessageHandler(filters.TEXT, log_msg))
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
