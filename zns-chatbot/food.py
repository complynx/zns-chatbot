from datetime import datetime
import uuid
from bson import BSON
import logging
import aiofiles
from aiofilelock import AIOMutableFileLock
from aiofiles.os import remove

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
    for_who = None
    id = None
    filename = None
    choice = None
    total = None
    choice_date = None
    marked_payed = None

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
    
