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

from .config import Config
from .food import FoodStorage
from .photo_task import get_by_user, PhotoTask
from .tg_constants import (
    IC_FOOD_PAYMENT_PAYED,
    IC_FOOD_ADMIN_CONFIRM,
    IC_FOOD_ADMIN_DECLINE,
    IC_FOOD_PAYMENT_CANCEL,
    IC_FOOD_PROMPT_WILL_PAY,
)

logger = logging.getLogger(__name__)

PHOTO, CROPPER = range(2)
NAME, WAITING_PAYMENT_PROOF = range(2)

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

cover = "static/cover.jpg"

async def start(update: Update, context: CallbackContext):
    """Send a welcome message when the /start command is issued."""
    logger.info(f"start called: {update.effective_user}")
    conf: Config = context.application.config
    menu = [
        ("/avatar", "Создать аватар."),
    ]
    if conf.food.hard_deadline > datetime.datetime.now():
        menu.append(("/food", "Заказать еду."))
    await context.bot.set_my_commands(menu)
    await update.message.reply_text(
        "Здравствуй, зуконавт! Меня зовут ЗиНуСя, твой виртуальный помощник 🤗\n\n"+
        (
            "🟢 Я могу сделать тебе красивую аватарку! Для этого выбери команду:\n"
            if conf.food.hard_deadline < datetime.datetime.now()
            else
            "🟢 Я могу помочь заказать тебе горячее питание и сделать красивую аватарку! Для этого выбери команду:\n"+
            "/food - заказ горячего питания\n"
        )+
        "/avatar - создать аватарку"
    )

# region AVATAR SECTION

async def avatar_cmd(update: Update, context: CallbackContext):
    """Handle the /avatar command, requesting a photo."""
    logger.info(f"Received /avatar command from {update.effective_user}")
    await avatar_cancel_inflow(update, context)
    buttons = [["Отмена"]]
    await update.message.reply_text(
        "📸 Отправь мне своё лучшее фото.\n\nP.S. Если в процессе покажется, что я "+
        "уснула, то просто разбуди меня, снова выбрав команду\n/avatar",
        reply_markup = ReplyKeyboardMarkup(
            buttons,
            resize_keyboard=True,
            one_time_keyboard=True
        )
    )
    return PHOTO

async def avatar_received_image(update: Update, context: CallbackContext):
    """Handle the photo submission as photo"""
    logger.info(f"Received avatar photo from {update.effective_user}")

    photo_file = await update.message.photo[-1].get_file()
    file_name = f"{photo_file.file_id}.jpg"
    file_path = os.path.join(tempfile.gettempdir(), file_name)
    
    await photo_file.download_to_drive(file_path)
    return await avatar_received_stage2(update, context, file_path, "jpg")

async def avatar_received_document_image(update: Update, context: CallbackContext):
    """Handle the photo submission as document"""
    logger.info(f"Received avatar document from {update.effective_user}")

    document = update.message.document

    # Download the document
    document_file = await document.get_file()
    file_ext = mimetypes.guess_extension(document.mime_type)
    file_path = os.path.join(tempfile.gettempdir(), f"{document.file_id}.{file_ext}")
    await document_file.download_to_drive(file_path)
    return await avatar_received_stage2(update, context, file_path, file_ext)

async def avatar_received_stage2(update: Update, context: CallbackContext, file_path:str, file_ext:str):
    await avatar_cancel_inner(update)
    task = PhotoTask(update.effective_chat, update.effective_user)
    task.add_file(file_path, file_ext)
    buttons = [
        [
            KeyboardButton(
                "Выбрать расположение",
                web_app=WebAppInfo(full_link(context.application, f"/fit_frame?id={task.id.hex}"))
            )
        ],
        ["Так сойдёт"],["Отмена"]
    ]


    markup = ReplyKeyboardMarkup(
        buttons,
        resize_keyboard=True,
        one_time_keyboard=True
    )
    logger.debug(f"url: " + full_link(context.application, f"/fit_frame?id={task.id.hex}"))
    await update.message.reply_text(
        "Фото загружено. Выберите как оно будет располагаться внутри рамки.",
        reply_markup=markup
    )
    return CROPPER

async def avatar_crop_auto(update: Update, context: CallbackContext):
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
    return await avatar_crop_stage2(task, update, context)

async def avatar_crop_matrix(update: Update, context):
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
    return await avatar_crop_stage2(task, update, context)

async def avatar_crop_stage2(task: PhotoTask, update: Update, context: CallbackContext):
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

async def avatar_cancel_inner(update: Update):
    try:
        get_by_user(update.effective_user.id).delete()
        return True
    except KeyError:
        pass
    except Exception as e:
        logger.error("Exception in cancel: %s", e, exc_info=1)
    return False

async def avatar_cancel_inflow(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    logger.info(f"Avatar submission for {update.effective_user} canceled")
    if await avatar_cancel_inner(update):
        await update.message.reply_text(
            "Уже активированная обработка фотографии отменена.",
            reply_markup=ReplyKeyboardRemove()
        )
    return ConversationHandler.END

async def avatar_cancel_command(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    logger.info(f"Avatar submission for {update.effective_user} canceled")
    await avatar_cancel_inner(update)
    await update.message.reply_text(
        "Oбработка фотографии отменена.",
        reply_markup=ReplyKeyboardRemove()
    )
    return ConversationHandler.END

async def avatar_error(update: Update, context: CallbackContext):
    await avatar_cancel_inner(update)
    await update.message.reply_text(
        "Ошибка обработки фото, попробуйте ещё раз.\n/avatar",
        reply_markup=ReplyKeyboardRemove()
    )
    return ConversationHandler.END

async def avatar_timeout(update: Update, context: CallbackContext):
    await avatar_cancel_inner(update)
    await update.message.reply_text(
        "Обработка фото отменена, так как долго не было активных действий от пользователя.\n/avatar",
        reply_markup=ReplyKeyboardRemove()
    )
    return ConversationHandler.END

# endregion AVATAR SECTION

# region FOOD SECTION

CANCEL_FOOD_STAGE2_REPLACEMENT_TEXT = "Этот выбор меню отменён. Для нового выбора можно снова воспользоваться командой /food"

async def food_cmd(update: Update, context: CallbackContext):
    """Handle the /food command, requesting a photo."""
    logger.info(f"Received /food command from {update.effective_user}")
    conf: Config = context.application.config
    if conf.food.hard_deadline < datetime.datetime.now():
        await update.message.reply_text("Увы, поест ушёл...")
        return ConversationHandler.END
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

async def food_received_name(update: Update, context: CallbackContext):
    """Handle the cancel command during the avatar submission."""
    name = update.message.text
    logger.info(f"Received for_who from {update.effective_user}: {name}")
    food_storage: FoodStorage = context.application.base_app.food_storage
    try:
        async with food_storage.new_meal(
            tg_user_id=update.effective_user.id,
            tg_username=update.effective_user.username,
            tg_user_first_name=update.effective_user.first_name,
            tg_user_last_name=update.effective_user.last_name,
            for_who=name,
        ) as meal_context:
            # f"{web_app_base}/fit_frame?id={task.id.hex}"
            reply_markup = ReplyKeyboardRemove()
            link = full_link(context.application, meal_context.link)
            await update.message.reply_html(
                f"Отлично, составляем меню для зуконавта по имени <i>{name}</i>. Для выбора блюд, "+
                f"<a href=\"{link}\">жми сюда (ссылка действительна 24 часа)</a>.",
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

async def food_timeout(update: Update, context: CallbackContext):
    """Handle the timeout command during the avatar submission."""
    logger.info(f"Food conversation for {update.effective_user} timed out")
    reply_markup = ReplyKeyboardRemove()
    await update.message.reply_text(
        "Составление меню отменено из-за отсутствия действий пользователя.",
        reply_markup=reply_markup
    )
    return ConversationHandler.END

async def food_payment_payed(update: Update, context: CallbackContext) -> int:
    """Handle payment answer after menu received"""
    # Get CallbackQuery from Update
    try:
        query = update.callback_query
        logger.info(f"Received food_choice_reply_payment from {update.effective_user}, data: {query.data}")
        # CallbackQueries need to be answered, even if no notification to the user is needed
        # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
        await query.answer()
        id = query.data.split("|")[1]
        food_storage: FoodStorage = context.application.base_app.food_storage
        async with food_storage.from_id(id) as meal_context:
            if meal_context.proof_received is not None:
                await query.edit_message_text(
                    "Да, я уже отправила подтверждение админинам.",
                    reply_markup=InlineKeyboardMarkup([])
                )
                return ConversationHandler.END
            if meal_context.marked_payed is None:
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
            "Ок, жду скрин или документ с квитанцией/чеком об оплате.",
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

async def food_prompt_will_pay(update: Update, context: CallbackContext):
    """Handle will pay answer for prompt"""
    # Get CallbackQuery from Update
    try:
        query = update.callback_query
        logger.info(f"Received food_choice_reply_will_pay from {update.effective_user}, data: {query.data}")
        # CallbackQueries need to be answered, even if no notification to the user is needed
        # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
        await query.answer()
        id = query.data.split("|")[1]
        food_storage: FoodStorage = context.application.base_app.food_storage
        async with food_storage.from_id(id) as meal_context:
            meal_context.marked_will_pay = datetime.datetime.now()
        logger.info(f"MealContext ID: {id}")
        await query.edit_message_text(
            "Принято. Тогда жду оплаты. Подтверждение оплаты заказа надо прислать в форме "+
            "<u><b>квитанции (чека)</b></u> об оплате <u>до 1 числа</u>, иначе заказ будет аннулирован. "+
            "На всякий случай, продублирую кнопку оплаты сюда. "+
            "<u><b>Обязательно</b></u> нажми на неё, как будешь готов(а) предоставить доказательство.\n\n"+
            "Если есть какие-то вопросы, не стесняйся обращаться к <a href=\"https://t.me/vbutman\">Вове</a>"+
            " или <a href=\"https://t.me/complynx\">Дане</a>, или <a href=\"https://t.me/capricorndarrel\">Даше</a>.",
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[
                InlineKeyboardButton("💸 Оплачено", callback_data=f"{IC_FOOD_PAYMENT_PAYED}|{id}"),
                InlineKeyboardButton("❌ Отменить", callback_data=f"{IC_FOOD_PAYMENT_CANCEL}|{id}"),
            ]])
        )
    except FileNotFoundError:
        logger.info("MealContext file not found in food_choice_reply_will_pay.")
        await query.edit_message_text(
            "Что-то непредвиденное случилось с вашим заказом. Попробуйте ещё раз: /food",
            reply_markup=InlineKeyboardMarkup([]))
    except Exception as e:
        logger.error("Exception in food_choice_reply_will_pay: %s", e, exc_info=1)

async def food_payment_cancel_inline(update: Update, context) -> int:
    """Handle payment answer after menu received"""
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_reply_payment from {update.effective_user}, data: {query.data}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()
    id = query.data.split("|")[1]
    try:
        food_storage: FoodStorage = context.application.base_app.food_storage
        async with food_storage.from_id(id) as meal_context:
            await meal_context.cancel()
    except FileNotFoundError:
        pass # already cancelled
    # Instead of sending a new message, edit the message that
    # originated the CallbackQuery. This gives the feeling of an
    # interactive menu.
    await query.edit_message_text(
        text=CANCEL_FOOD_STAGE2_REPLACEMENT_TEXT,
        reply_markup=InlineKeyboardMarkup([])
    )
    return ConversationHandler.END

async def food_payment_cancel_message(update: Update, context: CallbackContext) -> int:
    """Cancel food choice conversation"""
    logger.info(f"Received food_choice_conversation_cancel from {update.effective_user}")
    try:
        id = context.user_data["food_choice_id"]
        food_storage: FoodStorage = context.application.base_app.food_storage
        async with food_storage.from_id(id) as meal_context:
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

async def food_payment_proof_photo(update: Update, context: CallbackContext) -> int:
    """Received payment proof as image"""
    logger.info(f"Received food_choice_payment_photo from {update.effective_user}")

    photo_file = await update.message.photo[-1].get_file()
    file_name = f"{photo_file.file_id}.jpg"
    file_path = os.path.join(tempfile.gettempdir(), file_name)
    await photo_file.download_to_drive(file_path)
    return await food_payment_proof_stage2(update, context, file_path)

async def food_payment_proof_doc(update: Update, context: CallbackContext) -> int:
    """Received payment proof as file"""
    logger.info(f"Received food_choice_payment_doc from {update.effective_user}")
    
    document = update.message.document

    document_file = await document.get_file()
    file_ext = mimetypes.guess_extension(document.mime_type)
    file_name = f"{document.file_id}.{file_ext}"
    file_path = os.path.join(tempfile.gettempdir(), file_name)
    await document_file.download_to_drive(file_path)
    return await food_payment_proof_stage2(update, context, file_path)

async def food_payment_proof_stage2(update: Update, context: CallbackContext, received_file) -> int:
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
        food_storage: FoodStorage = context.application.base_app.food_storage
        async with food_storage.from_id(id) as meal_context:
            proof_file_name = os.path.splitext(meal_context.filename)[0] + ".proof" + os.path.splitext(received_file)[1]
            shutil.move(received_file, proof_file_name)
            meal_context.proof_file = proof_file_name
            meal_context.proof_received = datetime.datetime.now()

            keyboard = [
                [
                    InlineKeyboardButton("✅ Подтверждено", callback_data=f"{IC_FOOD_ADMIN_CONFIRM}|{meal_context.id}"),
                    InlineKeyboardButton("❌ Отказ", callback_data=f"{IC_FOOD_ADMIN_DECLINE}|{meal_context.id}"),
                ]
            ]
            conf: Config = context.application.config
            await update.message.forward(conf.food.proover)
            await context.bot.send_message(
                conf.food.proover,
                f"Пользователь <i>{update.effective_user.full_name}</i> прислал подтверждение оплаты еды"+
                f" на сумму {meal_context.total} ₽"+
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

async def food_admin_proof_confirmed(update: Update, context: CallbackContext):
    """Handle admin proof"""
    conf: Config = context.application.config
    if update.effective_user.id != conf.food.proover:
        return # check admin
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_admin_proof_confirmed from {update.effective_user}, data: {query.data}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()
    id = query.data.split("|")[1]
    food_storage: FoodStorage = context.application.base_app.food_storage
    async with food_storage.from_id(id) as meal_context:
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

async def food_admin_proof_declined(update: Update, context: CallbackContext):
    """Handle admin proof"""
    conf: Config = context.application.config
    if update.effective_user.id != conf.food.proover:
        return # check admin
    # Get CallbackQuery from Update
    query = update.callback_query
    logger.info(f"Received food_choice_admin_proof_declined from {update.effective_user}, data: {query.data}")
    # CallbackQueries need to be answered, even if no notification to the user is needed
    # Some clients may have trouble otherwise. See https://core.telegram.org/bots/api#callbackquery
    await query.answer()
    id = query.data.split("|")[1]
    
    food_storage: FoodStorage = context.application.base_app.food_storage
    async with food_storage.from_id(id) as meal_context:
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

async def food_admin_get_csv(update: Update, context: CallbackContext):
    """Handle the /food_adm_csv command, requesting a photo."""
    conf: Config = context.application.config
    if update.effective_user.id not in conf.food.admins:
        return
    logger.info(f"Received /food_adm_csv command from {update.effective_user}")

    await update.message.reply_text(
        "Создаю CSV, подожди..."
    )
    try:
        filename = tempfile.mktemp()
        food_storage: FoodStorage = context.application.base_app.food_storage
        await food_storage.get_csv(filename)
        await update.message.reply_document(
            filename,
            caption="Вот обещанный файлик CSV",
            filename="menu.csv"
        )
        os.remove(filename)
    except:
        await update.message.reply_text("Возникла ошибка, попробуй ещё раз.")
        raise

# endregion FOOD SECTION

# region MASSAGE SECTION

# flow: /massage -> ?create booking -> select type <-> de/select masseurs <-> select day <-> select time -> END
#       /massage -> ?select booking -> cancel
#                  \_ cancel conversation | conversation timeout

async def massage_cmd(update: Update, context: CallbackContext):
    """Handle the /massage command."""
    logger.info(f"Received /massage command from {update.effective_user}")
    if True: # have no bookings
        return await massage_create_stage2(update, context)
    keyboard = [[InlineKeyboardButton("📝 Записаться", callback_data="MassageCreate")]]
    # add all existing bookings
    keyboard.append([InlineKeyboardButton("❌ Отмена", callback_data="MassageCancel")])
    await update.message.reply_text(
        "Выбери запись, которую надо отменить или нажми \"Записаться\", чтобы создать новую:",
        reply_markup=InlineKeyboardMarkup(keyboard)
    )

async def massage_create_stage2(update: Update, context: CallbackContext):
    pass

# endregion MASSAGE SECTION

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
    application = ApplicationBuilder().application_class(TGApplication, kwargs={
        "base_app": app,
        "base_config": config
    }).token(token=config.telegram.token.get_secret_value()).build()

    avatar_conversation = ConversationHandler(
        entry_points=[
            CommandHandler("avatar", avatar_cmd),
        ],
        states={
            PHOTO: [
                MessageHandler(filters.PHOTO, avatar_received_image),
                MessageHandler(filters.Document.IMAGE, avatar_received_document_image),
            ],
            CROPPER: [
                MessageHandler(filters.StatusUpdate.WEB_APP_DATA, avatar_crop_matrix),
                MessageHandler(filters.Regex(re.compile("^(Так сойдёт)$", re.I)), avatar_crop_auto),
            ],
            ConversationHandler.TIMEOUT: [
                MessageHandler(filters.ALL, avatar_timeout)
            ],
        },
        fallbacks=[
            CommandHandler("cancel", avatar_cancel_command),
            CommandHandler("avatar", avatar_cancel_command),
            MessageHandler(filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)), avatar_cancel_command)
        ],
        conversation_timeout=datetime.timedelta(hours=2)
    )
    food_start_conversation = ConversationHandler(
        entry_points=[CommandHandler("food", food_cmd)],
        states={
            NAME: [
                MessageHandler(
                    filters.TEXT & ~filters.COMMAND & ~filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)),
                    food_received_name
                )
            ],
            ConversationHandler.TIMEOUT: [
                MessageHandler(filters.ALL, food_timeout)
            ]
        },
        fallbacks=[
            CommandHandler("cancel", food_cancel),
            MessageHandler(filters.Regex(re.compile("^(Cancel|Отмена)$", re.I|re.U)), food_cancel)
        ],
        conversation_timeout=datetime.timedelta(hours=1)
    )
    food_proof_conversation = ConversationHandler(
        entry_points=[
            CallbackQueryHandler(food_payment_payed, pattern=f"^{IC_FOOD_PAYMENT_PAYED}|[a-zA-Z_\\-0-9]$"),
        ],
        states={
            WAITING_PAYMENT_PROOF: [
                MessageHandler(filters.PHOTO, food_payment_proof_photo),
                MessageHandler(filters.Document.ALL, food_payment_proof_doc)
            ],
        },
        fallbacks=[
            CommandHandler("cancel", food_payment_cancel_message),
            CallbackQueryHandler(food_payment_cancel_inline, pattern=f"^{IC_FOOD_PAYMENT_CANCEL}|[a-zA-Z_\\-0-9]$"),
            MessageHandler(
                filters.Regex(re.compile("^(Cancel|Отмена|Отменить выбор еды)$", re.I|re.U)),
                food_payment_cancel_message
            )
        ],
    )
    application.add_handler(CallbackQueryHandler(food_payment_cancel_inline, pattern=f"^{IC_FOOD_PAYMENT_CANCEL}|[a-zA-Z_\\-0-9]$"))
    application.add_handler(CallbackQueryHandler(food_prompt_will_pay, pattern=f"^{IC_FOOD_PROMPT_WILL_PAY}|[a-zA-Z_\\-0-9]$"))
    application.add_handler(CallbackQueryHandler(food_admin_proof_confirmed, pattern=f"^{IC_FOOD_ADMIN_CONFIRM}|[a-zA-Z_\\-0-9]$"))
    application.add_handler(CallbackQueryHandler(food_admin_proof_declined, pattern=f"^{IC_FOOD_ADMIN_DECLINE}|[a-zA-Z_\\-0-9]$"))

    application.add_handler(CommandHandler("food_adm_csv", food_admin_get_csv))
    application.add_handler(CommandHandler("start", start))
    application.add_handler(food_start_conversation)
    application.add_handler(food_proof_conversation)
    application.add_handler(avatar_conversation)

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
