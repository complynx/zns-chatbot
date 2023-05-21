from datetime import datetime
import uuid
from bson import BSON
import logging
import aiofiles
from aiofilelock import AIOMutableFileLock

logger = logging.getLogger(__name__)

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

    _non_cacheable = {
        "_lock",
        "_file",
        "filename",
        "_non_cacheable"
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
        logger.info(f"aenter locking id: {self.id}")
        if self._file is None:
            self._file = open(self.filename, "wb+")
        if self._lock is None:
            self._lock = AIOMutableFileLock(self._file)
        await self._lock.acquire()
        logger.info(f"aenter locked id: {self.id}")
        return self

    async def __aexit__(self, exc_type, exc_value, traceback):
        self.save_to_file()
        self._lock.close()
        self._file.close()
        logger.info(f"aexit unlocked id: {self.id}")

    def save_to_file(self):
        logger.info(f"saving to file {self.filename}, id: {self.id}")
        data = {key: value for key, value in self.__dict__.items() if key not in self._non_cacheable}
        self._file.write(BSON.encode(data))
    
    @classmethod
    async def from_file(cls, path):
        logger.info(f"from_file reading: {path}")
        try:
            async with aiofiles.open(path, 'rb') as f:
                async with AIOMutableFileLock(f):
                    logger.info(f"from_file locked: {path}")
                    data = BSON(f.read()).decode()
                    return cls(**data, filename=path)
        finally:
            logger.info(f"from_file unlocked if was locked: {path}")
    
    @classmethod
    def from_id(cls, id):
        filename = cls.id_path(id)
        return cls.from_file(filename)
    
    @property
    def link(self):
        return f"/menu?id={self.id}"
    
