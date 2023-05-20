import datetime
import uuid

class MealContext(object):
    def __init__(self, user, for_who) -> None:
        self.user = user
        self.for_who = for_who
        self.started = datetime.datetime.now()
        self._id = uuid.uuid4()

    @property
    def id(self):
        return self._id.hex
    
    @property
    def link(self):
        return f"/menu?id={self.id}"
    
    def is_old(self):
        return self.started < (datetime.datetime.now() + datetime.timedelta(days=-1))
    
