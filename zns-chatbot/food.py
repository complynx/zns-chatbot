from datetime import datetime
import uuid
from bson import BSON
import logging
import aiofiles
from aiofilelock import AIOMutableFileLock
from aiofiles.os import remove
from .photo_task import async_thread

logger = logging.getLogger(__name__)

class AsyncMealContextConstructor(object):
    def __init__(self, meal_cls, path) -> None:
        self.path = path
        self.meal_cls = meal_cls
    async def __aenter__(self):
        logger.debug(f"from_file trying read: {self.path}")
        async with aiofiles.open(self.path, 'rb') as f:
            try:
                async with AIOMutableFileLock(f):
                    logger.debug(f"from_file locked: {self.path}")
                    data = BSON(await f.read()).decode()
                    
                    self.context = self.meal_cls(**data, filename=self.path)
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

    _cancelled = False
    _non_cacheable = {
        "_lock",
        "_file",
        "filename",
        "_non_cacheable",
        "_cancelled"
    }

    def __init__(
            self,
            id="",
            filename=None,
            lock=None,
            file=None,
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
        assert self._file is not None or self._lock is None

    @staticmethod
    def id_path(id):
        return f"/menu/{id}.bson"

    async def __aenter__(self):
        logger.debug(f"aenter locking id: {self.id}")
        if self._file is None:
            self._file = await aiofiles.open(self.filename, "wb+")
        if self._lock is None:
            self._lock = AIOMutableFileLock(self._file)
        await self._lock.acquire()
        logger.debug(f"aenter locked id: {self.id}")
        return self

    async def __aexit__(self, exc_type, exc_value, traceback):
        if self._cancelled:
            return
        await self.save_to_file()
        await self._lock.close()
        await self._file.close()
        logger.info(f"aexit unlocked id: {self.id}")

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
    
    @classmethod
    def from_file(cls, path):
        return AsyncMealContextConstructor(cls, path)
    
    @classmethod
    def from_id(cls, id):
        filename = cls.id_path(id)
        return cls.from_file(filename)
    
    @property
    def link(self):
        return f"/menu?id={self.id}"

@async_thread
def get_csv(csv_filename):
    import glob
    from fcntl import flock, LOCK_EX, LOCK_NB, LOCK_UN
    import time
    import csv

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

    logger.info(f"Collectiong data for CSV")
    while len(files)>0 and trials>0:
        trials -= 1
        files_working = files
        files = []
        for file in files_working:
            logger.info(f"trying to parse file {file}")
            with open(file, 'rb') as f:
                logger.info(f"opened {file}")
                try:
                    flock(f, LOCK_EX | LOCK_NB)
                    locked = True
                    logger.info(f"locked {file}")

                    data = BSON(f.read()).decode()
                    logger.info(f"parsed BSON {file}")
                except BlockingIOError:
                    logger.info(f"file is already locked, skipping for now {file}")
                    files.append(file)
                    continue
                finally:
                    if locked:
                        flock(f, LOCK_UN)
                        logger.info(f"unlocked {file}")
                
                arr = [
                    data.get("id",""),
                    data.get("created",""),
                    data.get("tg_user_id",""),
                    data.get("tg_username",""),
                    data.get("tg_user_first_name",""),
                    data.get("tg_user_last_name",""),
                    data.get("for_who",""),
                ]

                if "choice" in data and isinstance(data["choice"], dict):
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
        writer.writerow(ret)
    logger.info(f"CSV {csv_filename} created")
    

