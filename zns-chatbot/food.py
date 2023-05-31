from datetime import datetime
import os
import uuid
from bson import BSON
import logging
import aiofiles
from .aioflielock import AIOMutableFileLock, LockException
from aiofiles.os import remove
from .config import Config
from .tg_constants import (
    IC_FOOD_PROMPT_WILL_PAY,
    IC_FOOD_PAYMENT_PAYED,
    IC_FOOD_PAYMENT_CANCEL,
)

logger = logging.getLogger(__name__)

LOCK_TIMEOUT_DEFAULT = 300 # 5 min
GRANULARITY_DEFAULT = 0.2 # 1/5 seconds

class AsyncMealContextConstructor(object):
    def __init__(
            self,
            meal_cls: "MealContext",
            path: str,
            lock_timeout: float,
            lock_granularity: float,
        ) -> None:
        self.path = path
        self.meal_cls = meal_cls
        self.lock_timeout = lock_timeout
        self.lock_granularity = lock_granularity
    async def __aenter__(self) -> "MealContext":
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
    id: str = ""
    filename: str = ""
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

        assert self.id != ""
        assert self.filename != ""

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

    async def __aenter__(self) -> "MealContext":
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
        ) -> AsyncMealContextConstructor:
        return AsyncMealContextConstructor(cls, path, lock_timeout=lock_timeout, lock_granularity=lock_granularity)
    
    @property
    def link(self):
        return f"/menu?id={self.id}"

class FoodStorage():
    def __init__(self, config: Config, app):
        self.config: Config = config
        self.app = app
        
        if not os.path.exists(self.config.food.storage_path):
            os.makedirs(self.config.food.storage_path)

        app.food_storage = self
    
    def path_from_id(self, id) -> str:
        from os.path import join
        return join(self.config.food.storage_path, f'{id}.bson')

    def from_id(self, id):
        filename = self.path_from_id(id)
        return MealContext.from_file(filename)

    def new_meal(self, id="", filename=None, **kwargs):
        if id == "":
            id = uuid.uuid4().hex
        return MealContext(
            id = id,
            filename = filename if filename is not None else self.path_from_id(id),
            **kwargs
        )

    async def checker(self):
        import glob
        import asyncio
        from telegram.constants import ParseMode
        from telegram import InlineKeyboardButton, InlineKeyboardMarkup
        from telegram.ext import ExtBot
        from os.path import join
        bot: ExtBot = self.app.bot.bot

        while True:
            await asyncio.sleep(self.config.food.checker_loop_frequency.total_seconds())
            files = glob.glob(join(self.config.food.storage_path, '*.bson'))

            for file in files:
                try:
                    async with MealContext.from_file(file, lock_timeout=3) as meal_context:

                        if meal_context.choice_date is None and \
                            meal_context.created < datetime.now() - self.config.food.remove_empty_orders:

                            await meal_context.cancel()
                            logger.info(f"deleted empty stale meal context {meal_context.id}")
                            continue

                        if meal_context.choice_date is not None and \
                            meal_context.choice_date < datetime.now() - self.config.food.send_prompt_after and \
                            meal_context.marked_payed is None and not meal_context.prompt_sent:

                            logger.info(f"prompting for payment user {meal_context.tg_user_repr()} of"+
                                        " {meal_context.id} for {meal_context.for_who}")

                            keyboard = [
                                [
                                    InlineKeyboardButton(
                                        "👌 Оплачу попозже",
                                        callback_data=f"{IC_FOOD_PROMPT_WILL_PAY}|{meal_context.id}"
                                    ),
                                    InlineKeyboardButton(
                                        "❌ Отменить заказ",
                                        callback_data=f"{IC_FOOD_PAYMENT_CANCEL}|{meal_context.id}"
                                    ),
                                ]
                            ]

                            await bot.send_message(
                                chat_id=meal_context.tg_user_id,
                                text=
                                f"Зуконавт, я вижу твой заказ для <i>{meal_context.for_who}</i> "+
                                f"на сумму {meal_context.total}, который пока не оплачен.\n\n"+
                                "Если у тебя есть вопросы — их можно в напрямую задать ответственной по горячему"+
                                " питанию — <a href=\"https://t.me/capricorndarrel\">Даше</a>."+
                                " Если заказ не актуален или требуется что-то поменять, жми \"❌ Отменить заказ\" "+
                                "и потом создай новый через команду /food.\n\n"+
                                "Нужно оплатить питание <u>до 1 июня</u> включительно чтобы ZNS смог привезти "+
                                "его для тебя на площадку горячим.",
                                parse_mode=ParseMode.HTML,
                                reply_markup=InlineKeyboardMarkup(keyboard),
                            )
                            meal_context.prompt_sent = True
                            continue

                        if meal_context.marked_payed is not None and \
                            meal_context.marked_payed < datetime.now() - self.config.food.send_proof_prompt_after and \
                            meal_context.proof_received is None and not meal_context.proof_prompt_sent:

                            logger.info(f"prompting for proof user {meal_context.tg_user_repr()} of "+
                                        "{meal_context.id} for {meal_context.for_who}")

                            keyboard = [
                                [
                                    InlineKeyboardButton(
                                        "👌 Сейчас пришлю",
                                        callback_data=f"{IC_FOOD_PAYMENT_PAYED}|{meal_context.id}"
                                    ),
                                    InlineKeyboardButton(
                                        "❌ Отменить заказ",
                                        callback_data=f"{IC_FOOD_PAYMENT_CANCEL}|{meal_context.id}"
                                    ),
                                ]
                            ]

                            await bot.send_message(
                                chat_id=meal_context.tg_user_id,
                                text=
                                f"Я вижу твой заказ для зуконавта по имени <i>{meal_context.for_who}</i> "+
                                f"на сумму {meal_context.total}. Указано, что он оплачен, но не получено подтверждения.\n\n"+
                                "Если у тебя есть вопросы — их можно в напрямую задать ответственной по горячему"+
                                " питанию — <a href=\"https://t.me/capricorndarrel\">Даше</a>."+
                                " Если заказ не актуален или требуется что-то поменять, жми \"❌ Отменить заказ\" "+
                                "и потом создай новый через команду /food.\n\n"+
                                "Подтверждение оплаты заказа надо прислать в форме <u><b>квитанции (чека)</b></u> об "+
                                "оплате <u>до 1 июня</u> включительно чтобы ZNS смог привезти его для тебя на площадку горячим.",
                                parse_mode=ParseMode.HTML,
                                reply_markup=InlineKeyboardMarkup(keyboard),
                            )
                            meal_context.proof_prompt_sent = True
                            continue
                
                except FileNotFoundError:
                    pass # file was deleted in the meantime by other process (user?)
                except LockException:
                    logger.warning(f"Failed to lock {file}, will check other time")
                except Exception as e:
                    logger.error(f"Exception while trying to check file {file}, {e}", exc_info=1)

    async def get_csv(self, csv_filename):
        import glob
        import asyncio
        import csv
        from collections.abc import Iterable
        from os.path import join

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
            "напоминание об оплате",
            "напоминание о подтверждении оплаты",
        ]]
        files = glob.glob(join(self.config.food.storage_path, '*.bson'))

        trials = 10

        def get_date(date):
            return date.strftime("%m/%d/%Y %H:%M:%S") if isinstance(date, datetime) else ""

        logger.info(f"Collecting data for CSV")
        while len(files)>0 and trials>0:
            trials -= 1
            files_working = files
            files = []
            for file in files_working:
                logger.debug(f"trying to parse file {file}")
                try:
                    async with MealContext.from_file(file, lock_timeout=-1) as meal:
                        logger.debug(f"opened {file}")
                        arr = [
                            meal.id,
                            get_date(meal.created),
                            meal.tg_user_id,
                            meal.tg_username,
                            meal.tg_user_first_name,
                            meal.tg_user_last_name,
                            meal.for_who,
                        ]

                        if isinstance(meal.choice, Iterable):
                            for choice_dict in meal.choice:
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
                        
                        arr.extend([
                            meal.total,
                            get_date(meal.choice_date),
                            get_date(meal.marked_payed),
                            get_date(meal.proof_received),
                            meal.payment_confirmed,
                            get_date(meal.payment_confirmed_date) if meal.payment_confirmed else \
                                get_date(meal.payment_declined_date),
                            meal.prompt_sent,
                            meal.proof_prompt_sent,
                        ])
                        ret.append(arr)
                        
                except LockException:
                    logger.debug(f"file is locked, skipping for now {file}")
                    files.append(file)
                    continue
                    
            if len(files)>0:
                logger.info(f"there are {len(files)} files to try parse again")
                await asyncio.sleep(1)
        if len(files) > 0:
            logger.warn(f"there are {len(files)} files that couldn't be parsed: {files}")

        logger.info(f"saving CSV to {csv_filename}")
        with open(csv_filename, 'w', encoding="utf-8", newline="\n") as f:
            writer = csv.writer(f)
            writer.writerows(ret)
        logger.info(f"CSV {csv_filename} created")


