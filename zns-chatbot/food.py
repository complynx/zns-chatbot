from datetime import datetime
import uuid
from bson import BSON

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

    def __init__(
            self,
            id="",
            filename=None,
            **kwargs
        ) -> None:
        for key, value in kwargs.items():
            setattr(self, key, value)
        if not hasattr(self, "created"):
            self.created = datetime.now()
        self.id = id if id!="" else uuid.uuid4().hex
        self.filename = filename if filename is not None else self.id_path(self.id)

    @staticmethod
    def id_path(id):
        return f"/menu/{id}.json"

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_value, traceback):
        self.save_to_file()

    def save_to_file(self):
        with open(self.filename, 'wb') as f:
            f.write(BSON.encode(self))
    
    @classmethod
    def from_file(cls, path):
        with open(path, 'rb') as f:
            data = BSON(f.read()).decode()
            return cls(**data, filename=path)
    
    @classmethod
    def from_id(cls, id):
        filename = cls.id_path(id)
        return cls.from_file(filename)
    
    @property
    def link(self):
        return f"/menu?id={self.id}"
    
