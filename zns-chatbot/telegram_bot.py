from contextlib import asynccontextmanager
import datetime
import json
import re
import os
import mimetypes
import tempfile
from telegram import (
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    Update,
    ReplyKeyboardMarkup,
    ReplyKeyboardRemove,
    KeyboardButton,
    WebAppInfo,
)
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
from telegram.constants import ParseMode
import logging
import shutil

from .food import MealContext, get_csv
from .photo_task import get_by_user, PhotoTask

logger = logging.getLogger(__name__)

PHOTO, CROPPER, UPSCALE, FINISH = range(4)
NAME, WAITING_PAYMENT_PROOF = range(2)

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
    await context.bot.set_my_commands([
        ("/avatar", "Создать аватар."),
        # ("/food", "Заказать еду."),
    ])
    await update.message.reply_text(
        "Здравствуй, зуконавт! Меня зовут ЗиНуСя, твой виртуальный помощник 🤗\n\n"+
        "🟢 Я могу помочь заказать тебе горячее питание и сделать красивую аватарку! Для этого выбери команду:\n"+
        # "/food - заказ горячего питания\n"+
        "/avatar - создать аватарку"
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
    markup = ReplyKeyboardMarkup(
        [[update.effective_user.full_name],["Отмена"]],
        resize_keyboard=True,
        one_time_keyboard=True
    )
    await update.message.reply_html(
        "Привет, зуконавт! Наша звёздная кухня готова принять твой заказ! Чего изволите? 🖖\n\n"+
        "Напиши полные <u>имя</u> и <u>фамилию</u> зуконавта, для которого будем собирать меню.",
        reply_markup=markup
    )
    return NAME

async def food_for_who(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    name = update.message.text
    logger.info(f"Received for_who from {update.effective_user}: {name}")
    try:
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
                f"Отлично, составляем меню для зуконавта по имени <i>{name}</i>. Для выбора блюд, "+
                f"<a href=\"{web_app_base}{meal_context.link}\">жми сюда (ссылка действительна 24 часа)</a>.",
                reply_markup=reply_markup
            )
    except Exception as e:
        logger.error("Exception in food_for_who: %s", e, exc_info=1)
    return ConversationHandler.END

async def food_cancel(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    logger.info(f"Food conversation for {update.effective_user} canceled")
    reply_markup = ReplyKeyboardRemove()
    await update.message.reply_text("Составление меню отменено.", reply_markup=reply_markup)
    return ConversationHandler.END

CANCEL_FOOD_STAGE2_REPLACEMENT_TEXT = "Этот выбор меню отменён. Для нового выбора можно снова воспользоваться командой /food"

async def food_choice_reply_payment(update: Update, context: CallbackContext) -> int:
    """Handle payment answer after menu received"""
    # Get CallbackQuery from Update
    try:
        query = update.callback_query
        logger.info(f"Received food_choice_reply_payment from {update.effective_user}, query: {query}")
        # CallbackQueries need to be answered, even if no notification to the user is needed
        # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
        await query.answer()
        id = query.data.split("|")[1]
        async with MealContext.from_id(id) as meal_context:
            meal_context.marked_payed = datetime.datetime.now()
            meal_context.message_inline_id = query.inline_message_id
            meal_context.message_self_id = query.message.message_id
            meal_context.message_chat_id = query.message.chat_id
        logger.info(f"MealContext ID: {id}")
        await query.edit_message_reply_markup(reply_markup=InlineKeyboardMarkup([]))
        if context.user_data is None:
            context.user_data.update(dict())
        context.user_data["food_choice_id"] = id

        markup = ReplyKeyboardMarkup([["Отменить выбор еды"]], resize_keyboard=True, one_time_keyboard=True)
        await context.bot.send_message(
            update.effective_user.id,
            "Ок, жду скрин или документ — подтверждение оплаты.",
            reply_markup=markup
        )
        return WAITING_PAYMENT_PROOF
    except FileNotFoundError:
        logger.info("MealContext file not found in food_choice_reply_payment.")
        await query.edit_message_text(
            "Что-то непредвиденное случилось с вашим заказом. Попробуйте ещё раз: /food",
            reply_markup=InlineKeyboardMarkup([]))
        return ConversationHandler.END
    except Exception as e:
        logger.error("Exception in food_choice_reply_payment: %s", e, exc_info=1)

async def food_choice_reply_cancel(update: Update, context) -> int:
    """Handle payment answer after menu received"""
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_reply_payment from {update.effective_user}, query: {query}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()
    id = query.data.split("|")[1]
    try:
        async with MealContext.from_id(id) as meal_context:
            await meal_context.cancel()
    except FileNotFoundError:
        pass # already cancelled
    # Instead of sending a new message, edit the message that
    # originated the CallbackQuery. This gives the feeling of an
    # interactive menu.
    await query.edit_message_text(
        text=CANCEL_FOOD_STAGE2_REPLACEMENT_TEXT
    )
    return ConversationHandler.END

async def food_choice_conversation_cancel(update: Update, context: CallbackContext) -> int:
    """Cancel food choice conversation"""
    logger.info(f"Received food_choice_conversation_cancel from {update.effective_user}")
    try:
        id = context.user_data["food_choice_id"]
        async with MealContext.from_id(id) as meal_context:
            if meal_context.message_inline_id:
                await context.bot.edit_message_text(
                    inline_message_id=meal_context.message_inline_id,
                    text=CANCEL_FOOD_STAGE2_REPLACEMENT_TEXT
                )
            else:
                await context.bot.edit_message_text(
                    message_id=meal_context.message_self_id,
                    chat_id=meal_context.message_chat_id,
                    text=CANCEL_FOOD_STAGE2_REPLACEMENT_TEXT
                )
            await meal_context.cancel()
        del context.user_data["food_choice_id"]
    except FileNotFoundError or KeyError:
        pass
    except Exception as e:
        logger.error("Exception in food_choice_conversation_cancel: %s", e, exc_info=1)

    await update.message.reply_text(
        text="Выбор меню отменён. Для нового выбора можно снова воспользоваться командой /food",
        reply_markup=ReplyKeyboardRemove()
    )
    return ConversationHandler.END

async def food_choice_payment_photo(update: Update, context: CallbackContext) -> int:
    """Received payment proof as image"""
    logger.info(f"Received food_choice_payment_photo from {update.effective_user}")

    photo_file = await update.message.photo[-1].get_file()
    file_name = f"{photo_file.file_id}.jpg"
    file_path = os.path.join(tempfile.gettempdir(), file_name)
    await photo_file.download_to_drive(file_path)
    return await food_choice_payment_stage2(update, context, file_path)

async def food_choice_payment_doc(update: Update, context: CallbackContext) -> int:
    """Received payment proof as file"""
    logger.info(f"Received food_choice_payment_doc from {update.effective_user}")
    
    document = update.message.document

    document_file = await document.get_file()
    file_ext = mimetypes.guess_extension(document.mime_type)
    file_name = f"{document.file_id}.{file_ext}"
    file_path = os.path.join(tempfile.gettempdir(), file_name)
    await document_file.download_to_drive(file_path)
    return await food_choice_payment_stage2(update, context, file_path)

ADMIN_PROOVING_PAYMENT = 1012402779 # darrel
# ADMIN_PROOVING_PAYMENT = 379278985 # me
FOOD_ADMINS = [
    1012402779, # darrel
    379278985, # me
    20538574, # love_zelensky
    249413857, # vbutman
]

async def food_choice_admin_proof_confirmed(update: Update, context: CallbackContext):
    """Handle admin proof"""
    if update.effective_user.id != ADMIN_PROOVING_PAYMENT:
        return # check admin
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_admin_proof_confirmed from {update.effective_user}, query: {query}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()
    id = query.data.split("|")[1]
    async with MealContext.from_id(id) as meal_context:
        meal_context.payment_confirmed = True
        meal_context.payment_confirmed_date = datetime.datetime.now()
    
        await query.edit_message_text(
            f"Питание от пользователя <i>{meal_context.tg_user_first_name} {meal_context.tg_user_last_name}</i>"+
            f" для зуконавта по имени <i>{meal_context.for_who}</i> подтверждено.",
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )
        await context.bot.send_message(
            meal_context.tg_user_id,
            f"Админ подтвердил заказ на питание"+
            f" для зуконавта по имени <i>{meal_context.for_who}</i>.\nЖдём тебя на ZNS.",
            parse_mode=ParseMode.HTML,
        )

async def food_choice_admin_proof_declined(update: Update, context: CallbackContext):
    """Handle admin proof"""
    if update.effective_user.id != ADMIN_PROOVING_PAYMENT:
        return # check admin
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_admin_proof_declined from {update.effective_user}, query: {query}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()
    id = query.data.split("|")[1]
    async with MealContext.from_id(id) as meal_context:
        meal_context.payment_declined = True
        meal_context.payment_declined_date = datetime.datetime.now()
    
        await query.edit_message_text(
            f"Питание от пользователя <i>{meal_context.tg_user_first_name} {meal_context.tg_user_last_name}</i>"+
            f" для зуконавта по имени <i>{meal_context.for_who}</i> было отклонено.",
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )
        await context.bot.send_message(
            meal_context.tg_user_id,
            f"Админ отменил заказ на питание"+
            f" для зуконавта по имени <i>{meal_context.for_who}</i>.\n"+
            "Если нужно сделать другой заказ, можно снова вызвать команду /food",
            parse_mode=ParseMode.HTML,
        )

async def food_choice_payment_stage2(update: Update, context: CallbackContext, received_file) -> int:
    await update.message.reply_text(
        text="Я переслала подтверждение админам. Они проверят и я вернусь с результатом.",
        reply_markup=ReplyKeyboardRemove()
    )
    
    try:
        id = context.user_data["food_choice_id"]
        del context.user_data["food_choice_id"]
    except KeyError:
        logger.error("food_choice_id not found in food_choice_payment_stage2")
        await update.message.reply_text(
            text="Возникла какая-то непредвиденная проблема с вашим заказом,"+
            " возможно придётся попробовать ещё раз: /food",
            reply_markup=ReplyKeyboardRemove()
        )
        return ConversationHandler.END
    try:
        async with MealContext.from_id(id) as meal_context:
            proof_file_name = os.path.splitext(meal_context.filename)[0] + ".proof" + os.path.splitext(received_file)[1]
            shutil.move(received_file, proof_file_name)
            meal_context.proof_file = proof_file_name
            meal_context.proof_received = datetime.datetime.now()

            keyboard = [
                [
                    InlineKeyboardButton("✅ Подтверждено", callback_data=f"FoodChoiceAdmConf|{meal_context.id}"),
                    InlineKeyboardButton("❌ Отказ", callback_data=f"FoodChoiceAdmDecl|{meal_context.id}"),
                ]
            ]
            await update.message.forward(ADMIN_PROOVING_PAYMENT)
            await context.bot.send_message(
                ADMIN_PROOVING_PAYMENT,
                f"Пользователь <i>{update.effective_user.full_name}</i> прислал подтверждение оплаты еды"+
                f" для зуконавта по имени <i>{meal_context.for_who}</i>. Необходимо подтверждение.\n"+
                "<b>Внимание</b>, не стоит помечать отсутствие оплаты раньше времени, лучше сначала удостовериться.",
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(keyboard),
            )
    except FileNotFoundError:
        logger.error(f"MealContext by ID not found in food_choice_payment_stage2: {id}")
        await update.message.reply_text(
            text="Возникла какая-то непредвиденная проблема с вашим заказом, он потерялся.\n"+
            " Придётся попробовать ещё раз: /food",
            reply_markup=ReplyKeyboardRemove()
        )
        return ConversationHandler.END

    return ConversationHandler.END

async def food_admin_get_csv(update: Update, context: CallbackContext):
    """Handle the /food_adm_csv command, requesting a photo."""
    if update.effective_user.id not in FOOD_ADMINS:
        return
    logger.info(f"Received /food_adm_csv command from {update.effective_user}")

    await update.message.reply_text(
        "Создаю CSV, подожди..."
    )
    try:
        filename = tempfile.mktemp()
        await get_csv(filename)
        await update.message.reply_document(
            filename,
            caption="Вот обещанный файлик CSV",
            filename="menu.csv"
        )
        os.remove(filename)
    except:
        await update.message.reply_text("Возникла ошибка, попробуй ещё раз.")
        raise

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
            CallbackQueryHandler(food_choice_reply_payment, pattern="^FoodChoiceReplPaym|[a-zA-Z_\\-0-9]$"),
            CallbackQueryHandler(food_choice_reply_cancel, pattern="^FoodChoiceReplCanc|[a-zA-Z_\\-0-9]$"),
        ],
        states={
            WAITING_PAYMENT_PROOF: [
                MessageHandler(filters.PHOTO, food_choice_payment_photo),
                MessageHandler(filters.Document.ALL, food_choice_payment_doc)
            ],
        },
        fallbacks=[
            CommandHandler("cancel", food_choice_conversation_cancel),
            MessageHandler(
                filters.Regex(re.compile("^(Cancel|Отмена|Отменить выбор еды)$", re.I|re.U)),
                food_choice_conversation_cancel
            )
        ],
    )
    application.add_handler(CallbackQueryHandler(food_choice_admin_proof_confirmed, pattern="^FoodChoiceAdmConf|[a-zA-Z_\\-0-9]$"))
    application.add_handler(CallbackQueryHandler(food_choice_admin_proof_declined, pattern="^FoodChoiceAdmDecl|[a-zA-Z_\\-0-9]$"))

    application.add_handler(CommandHandler("food_adm_csv", food_admin_get_csv))
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
