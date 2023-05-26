from datetime import datetime, timedelta
import uuid
from bson import BSON
import logging
import aiofiles
from .aioflielock import AIOMutableFileLock
from aiofiles.os import remove
from .photo_task import async_thread

logger = logging.getLogger(__name__)

LOCK_TIMEOUT_DEFAULT = 300 # 5 min
GRANULARITY_DEFAULT = 0.2 # 1/5 seconds

class AsyncMealContextConstructor(object):
    def __init__(
            self,
            meal_cls,
            path,
            lock_timeout,
            lock_granularity
        ) -> None:
        self.path = path
        self.meal_cls = meal_cls
        self.lock_timeout = lock_timeout
        self.lock_granularity = lock_granularity
    async def __aenter__(self):
        logger.debug(f"from_file trying read: {self.path}")
        async with aiofiles.open(self.path, 'rb') as f:
            try:
                async with AIOMutableFileLock(f, granularity=self.lock_granularity, timeout=self.lock_timeout):
                    logger.debug(f"from_file locked: {self.path}")
                    data = BSON(await f.read()).decode()
                    
                    self.context = self.meal_cls(
                        **data,
                        filename=self.path,
                        lock_timeout=self.lock_timeout,
                        lock_granularity=self.lock_granularity
                    )
                logger.debug(f"from_file unlocked: {self.path}")
            except Exception as e:
                logger.debug(f"from_file unlocked: {self.path} while propagating exception {e}")
                raise
        return await self.context.__aenter__()
    async def __aexit__(self, exc_type, exc_value, traceback):
        return await self.context.__aexit__(exc_type, exc_value, traceback)

class MealContext(object):
    tg_user_id = None
    tg_user_first_name = None
    tg_user_last_name = None
    tg_username = None
    started = None
    for_who = None
    id = None
    filename = None
    choice = None
    total = None
    choice_date = None
    marked_payed = None
    proof_received = None
    payment_confirmed = False
    payment_confirmed_date = None
    payment_declined = False
    payment_declined_date = None
    prompt_sent = False
    proof_prompt_sent = False

    _cancelled = False
    _non_cacheable = {
        "_lock",
        "_file",
        "filename",
        "_non_cacheable",
        "_cancelled",
        "_lock_timeout",
        "_lock_granularity"
    }

    def __init__(
            self,
            id="",
            filename=None,
            lock=None,
            file=None,
            lock_timeout=LOCK_TIMEOUT_DEFAULT,
            lock_granularity=GRANULARITY_DEFAULT,
            **kwargs
        ) -> None:
        for key, value in kwargs.items():
            setattr(self, key, value)
        if not hasattr(self, "created"):
            self.created = datetime.now()
        self.id = id if id!="" else uuid.uuid4().hex
        self.filename = filename if filename is not None else self.id_path(self.id)
        self._lock = lock
        self._file = file
        self._lock_timeout = lock_timeout
        self._lock_granularity = lock_granularity
        assert self._file is not None or self._lock is None

    def tg_user_repr(self):
        return f"User({self.tg_user_id}" + \
            (f" {self.tg_username}" if self.tg_username is not None else "") + \
            f" {self.tg_user_first_name}" + \
            (f" {self.tg_user_last_name}" if self.tg_user_last_name is not None else "") + ")"

    @staticmethod
    def id_path(id):
        return f"/menu/{id}.bson"

    async def __aenter__(self):
        logger.debug(f"aenter locking id: {self.id}")
        if self._file is None:
            self._file = await aiofiles.open(self.filename, "wb+")
        if self._lock is None:
            self._lock = AIOMutableFileLock(self._file, granularity=self._lock_granularity, timeout=self._lock_timeout)
        await self._lock.acquire()
        logger.debug(f"aenter locked id: {self.id}")
        return self

    async def __aexit__(self, exc_type, exc_value, traceback):
        if self._cancelled:
            return
        await self.save_to_file()
        await self._lock.close()
        await self._file.close()
        logger.debug(f"aexit unlocked id: {self.id}")

    async def save_to_file(self):
        logger.debug(f"saving to file {self.filename}, id: {self.id}")
        data = {key: value for key, value in self.__dict__.items() if key not in self._non_cacheable}
        await self._file.write(BSON.encode(data))

    async def cancel(self):
        self._cancelled = True
        await self._lock.close()
        await self._file.close()
        logger.debug(f"cancel unlocked id: {self.id}")
        await remove(self.filename)

    def format_choice(self):
        if self.choice is not None:
            day_ru = {
                "friday": "Пятница",
                "saturday": "Суббота",
                "sunday": "Воскресенье"
            }
            meal_ru = {
                "dinner": "ужин",
                "lunch": "обед"
            }

            formatted_choice = ""
            for choice_dict in self.choice:
                formatted_choice += f"\n\t<b>{day_ru[choice_dict['day']]}, {meal_ru[choice_dict['meal']]}</b> — "
                if choice_dict["cost"] == 0:
                    formatted_choice += "не буду есть."
                else:
                    formatted_choice += f"за <b>{choice_dict['cost']}</b> ₽ из ресторана <i>"
                    formatted_choice += choice_dict["restaurant"] + "</i>\n"
                    formatted_choice += choice_dict["choice"]
                formatted_choice += "\n"
            formatted_choice += f"\n\t\tИтого, общая сумма: <b>{self.total}</b> ₽."
            return formatted_choice
        return "Не выбрано"
    
    @classmethod
    def from_file(
        cls,
        path,
        lock_timeout=LOCK_TIMEOUT_DEFAULT,
        lock_granularity=GRANULARITY_DEFAULT
        ):
        return AsyncMealContextConstructor(cls, path, lock_timeout=lock_timeout, lock_granularity=lock_granularity)
    
    @classmethod
    def from_id(cls,
        id,
        lock_timeout=LOCK_TIMEOUT_DEFAULT,
        lock_granularity=GRANULARITY_DEFAULT
        ):
        
        filename = cls.id_path(id)
        return cls.from_file(filename, lock_timeout, lock_granularity)
    
    @property
    def link(self):
        return f"/menu?id={self.id}"

DELETE_EMPTY_AFTER = timedelta(days=1)
SEND_PROMPT_AFTER = timedelta(days=1)
SEND_PROOF_PROMPT_AFTER = timedelta(hours=1)

async def checker(app):
    import glob
    import asyncio
    from telegram.constants import ParseMode
    from telegram import InlineKeyboardButton, InlineKeyboardMarkup

    while True:
        await asyncio.sleep(600) # every 5 minutes
        files = glob.glob('/menu/*.bson')

        for file in files:
            async with MealContext.from_file(file, lock_timeout=3) as meal_context:

                if meal_context.choice_date is None and meal_context.created < datetime.now() - DELETE_EMPTY_AFTER:
                    await meal_context.cancel()
                    logger.info(f"deleted empty stale meal context {meal_context.id}")
                    continue

                if meal_context.choice_date is not None and \
                    meal_context.choice_date < datetime.now() - SEND_PROMPT_AFTER and \
                    meal_context.marked_payed is None and not meal_context.prompt_sent:

                    logger.info(f"prompting for payment user {meal_context.tg_user_repr()} of {meal_context.id} for {meal_context.for_who}")

                    keyboard = [
                        [
                            InlineKeyboardButton("👌 Оплачу попозже", callback_data=f"FoodChoiceReplWillPay|{meal_context.id}"),
                            InlineKeyboardButton("❌ Отменить заказ", callback_data=f"FoodChoiceReplCanc|{meal_context.id}"),
                        ]
                    ]

                    await app.bot.bot.send_message(
                        chat_id=meal_context.tg_user_id,
                        text=
                        f"Зуконавт, я вижу твой заказ для <i>{meal_context.for_who}</i> "+
                        f"на сумму {meal_context.total}, который пока не оплачен.\n\n"+
                        "Если у тебя есть вопросы — их можно в напрямую задать ответственной по горячему"+
                        " питанию — <a href=\"https://t.me/capricorndarrel\">Даше</a>."+
                        "Изменить заказ можно через копку \"❌ Отменить заказ\" и новое создание заказа.\n\n"+
                        "Нужно оплатить питание <u>до 1 июня</u> включительно чтобы ZNS смог привезти "+
                        "его для тебя на площадку горячим.",
                        parse_mode=ParseMode.HTML,
                        reply_markup=InlineKeyboardMarkup(keyboard),
                    )
                    meal_context.prompt_sent = True
                    continue

                if meal_context.marked_payed is not None and \
                    meal_context.marked_payed < datetime.now() - SEND_PROOF_PROMPT_AFTER and \
                    meal_context.proof_received is None and not meal_context.proof_prompt_sent:

                    logger.info(f"prompting for proof user {meal_context.tg_user_repr()} of {meal_context.id} for {meal_context.for_who}")

                    keyboard = [
                        [
                            InlineKeyboardButton("👌 Сейчас пришлю", callback_data=f"FoodChoiceReplPaym|{meal_context.id}"),
                            InlineKeyboardButton("❌ Отменить заказ", callback_data=f"FoodChoiceReplCanc|{meal_context.id}"),
                        ]
                    ]

                    await app.bot.bot.send_message(
                        chat_id=meal_context.tg_user_id,
                        text=
                        f"Я вижу твой заказ для зуконавта по имени <i>{meal_context.for_who}</i> "+
                        f"на сумму {meal_context.total}. Указано, что он оплачен, но не получено подтверждения.\n\n"+
                        "Если у тебя есть вопросы — их можно в напрямую задать ответственной по горячему"+
                        " питанию — <a href=\"https://t.me/capricorndarrel\">Даше</a>."+
                        "Изменить заказ можно через копку \"❌ Отменить заказ\" и новое создание заказа.\n\n"+
                        "Подтверждение оплаты заказа надо прислать в форме <u><b>квитанции (чека)</b></u> об "+
                        "оплате <u>до 1 июня</u> включительно чтобы ZNS смог привезти его для тебя на площадку горячим.",
                        parse_mode=ParseMode.HTML,
                        reply_markup=InlineKeyboardMarkup(keyboard),
                    )
                    meal_context.proof_prompt_sent = True
                    continue
        


@async_thread
def get_csv(csv_filename):
    import glob
    from fcntl import flock, LOCK_EX, LOCK_NB, LOCK_UN
    import time
    import csv
    from collections.abc import Iterable

    ret = [[
        "ID заказа",
        "Заказ создан",
        "TG ID Пользователя",
        "TG Пользователь",
        "TG Имя",
        "TG Фамилия",
        "Для кого",
        "пятница, ужин, ресторан",
        "пятница, ужин, выбор",
        "пятница, ужин, сумма",
        "суббота, обед, ресторан",
        "суббота, обед, выбор",
        "суббота, обед, сумма",
        "суббота, ужин, ресторан",
        "суббота, ужин, выбор",
        "суббота, ужин, сумма",
        "воскресенье, обед, ресторан",
        "воскресенье, обед, выбор",
        "воскресенье, обед, сумма",
        "воскресенье, ужин, ресторан",
        "воскресенье, ужин, выбор",
        "воскресенье, ужин, сумма",
        "всего",
        "дата выбора",
        "дата оплаты",
        "дата получения пруфа",
        "подтверждено",
        "дата подтверждения",
    ]]
    files = glob.glob('/menu/*.bson')

    trials = 10

    logger.info(f"Collecting data for CSV")
    while len(files)>0 and trials>0:
        trials -= 1
        files_working = files
        files = []
        for file in files_working:
            logger.debug(f"trying to parse file {file}")
            with open(file, 'rb') as f:
                logger.debug(f"opened {file}")
                try:
                    flock(f, LOCK_EX | LOCK_NB)
                    locked = True
                    logger.debug(f"locked {file}")

                    data = BSON(f.read()).decode()
                    logger.debug(f"parsed BSON {file}")
                except BlockingIOError:
                    logger.debug(f"file is already locked, skipping for now {file}")
                    files.append(file)
                    continue
                finally:
                    if locked:
                        flock(f, LOCK_UN)
                        logger.debug(f"unlocked {file}")
                
                arr = [
                    data.get("id",""),
                    data.get("created",""),
                    data.get("tg_user_id",""),
                    data.get("tg_username",""),
                    data.get("tg_user_first_name",""),
                    data.get("tg_user_last_name",""),
                    data.get("for_who",""),
                ]

                if "choice" in data and isinstance(data["choice"], Iterable):
                    for choice_dict in data["choice"]:
                        if choice_dict["cost"]>0:
                            arr.append(choice_dict["restaurant"])
                            arr.append(choice_dict["choice"])
                            arr.append(choice_dict["cost"])
                        else:
                            arr.append("нет")
                            arr.append(choice_dict["choice"])
                            arr.append(choice_dict["cost"])
                else:
                    for _ in range(15):
                        arr.append("")

                def get_date(data, name):
                    return data[name].strftime("%m/%d/%Y %H:%M:%S") if name in data and isinstance(data[name], datetime) else ""
                
                arr.extend([
                    data.get("total", 0),
                    get_date(data, "choice_date"),
                    get_date(data, "marked_payed"),
                    get_date(data, "proof_received"),
                    data.get("payment_confirmed", False),
                    get_date(data, "payment_confirmed_date") if data.get("payment_confirmed", False) else get_date(data, "payment_declined_date") ,
                ])
                ret.append(arr)
        if len(files)>0:
            logger.info(f"there are {len(files)} files to try parse again")
            time.sleep(1)
    if len(files) > 0:
        logger.warn(f"there are {len(files)} files that couldn't be parsed: {files}")

    logger.info(f"saving CSV to {csv_filename}")
    with open(csv_filename, 'w') as f:
        writer = csv.writer(f)
        writer.writerows(ret)
    logger.info(f"CSV {csv_filename} created")
    

