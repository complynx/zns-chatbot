import yaml
from pydantic import BaseModel, PrivateAttr, root_validator, validator
from datetime import timedelta, datetime
import asyncio
from .aioflielock import AIOMutableFileLock
from .photo_task import async_thread
from .config import Config
import pytz
import logging

logger = logging.getLogger(__name__)

def timedelta_representer(dumper, data: timedelta):
    sign = "-" if data.total_seconds() < 0 else ""
    data = abs(data)
    days = data.days
    hours, rem = divmod(data.seconds, 3600)
    minutes, seconds = divmod(rem, 60)
    result = "{:02d}:{:02d}:{:02d}".format(hours, minutes, seconds)
    if days:
        result = "{} {:02d}:{:02d}:{:02d}".format(days, hours, minutes, seconds)
    elif hours:
        result = "{:d}:{:02d}:{:02d}".format(hours, minutes, seconds)
    elif minutes:
        result = "{:d}:{:02d}".format(minutes, seconds)
    else:
        result = "{:d}".format(seconds)
    if data.microseconds:
        result += ".{:06d}".format(data.microseconds)
    return dumper.represent_str(sign + result)
yaml.add_representer(timedelta, timedelta_representer, Dumper=yaml.SafeDumper)

FILE_LOCK_TIMEOUT = 300 # 5 min
FILE_LOCK_GRANULARITY = 0.2

ctime = datetime.now()
ctime_msk = datetime.now(tz=pytz.timezone("Europe/Moscow")).replace(tzinfo=None)
MSK_OFFSET = timedelta(seconds=round((ctime_msk-ctime).total_seconds()))

def now_msk() -> datetime:
    return datetime.now() + MSK_OFFSET

class Masseur(BaseModel):
    name: str
    last_name: str = ""
    icon: str = "💆‍♀️"
    username: str = ""
    update_notifications: bool = True
    before_massage_notifications: bool = True
    _id: int = PrivateAttr(-1)

    def link_url(self) -> str:
        return f"tg://user?id={self._id}"
    
    def link_html(self) -> str:
        return f"<a href=\"{self.link_url()}\">{self.name}</a>"

    @classmethod
    def __get_validators__(cls):
        # one or more validators may be yielded which will be called in the
        # order to validate the input, each validator will receive as an input
        # the value returned from the previous validator
        yield cls.validate
        return BaseModel.__get_validators__()
    
    @classmethod
    def validate(cls, v):
        if isinstance(v, str):
            v = v.split(" ")
        if isinstance(v, list):
            d = {"name": v[0]}
            if len(v) > 1:
                d["last_name"] = v[1]
            if len(v) > 2:
                d["icon"] = v[2]
            v = d
        if isinstance(v, dict):
            return cls(**v)
        raise TypeError("wrong type")

class MassageType(BaseModel):
    name: str
    price: int
    duration: timedelta

class Massage(BaseModel):
    massage_type_index: int
    masseur_id: int
    client_id: int
    client_name: str
    client_username: str|None = None
    start: datetime
    _id: int = PrivateAttr(-1)
    _client_notified: bool = PrivateAttr(False)
    _masseur_notified: bool = PrivateAttr(False)

    def client_link_url(self) -> str:
        return f"tg://user?id={self.client_id}"
    
    def client_link_html(self) -> str:
        return f"<a href=\"{self.client_link_url()}\">{self.client_name}</a>"

    def massage_client_repr(self) -> str:
        dow = ""
        match self.start.weekday():
            case 4:
                dow = "Пт"
            case 5:
                dow = "Сб"
            case _:
                dow = "Вс"
        return dow + " " + (self.start).strftime('%H:%M')

class WorkingHour(BaseModel):
    start: datetime
    end: datetime
    masseur_id: int

    @validator('end')
    def check_time_consistency(cls, end, values, **kwargs):
        start = values.get('start')
        if start >= end:
            raise ValueError('start should preceed end')
        return end

class TimeSlot:
    start: datetime
    end: datetime
    masseur_id: int

class MassageSystem(BaseModel):
    masseurs: dict[int, Masseur]
    buffer_time: timedelta = timedelta(minutes=5)
    massage_types: list[MassageType]
    working_hours: list[WorkingHour]
    massages: dict[int, Massage] = {}
    admins: set[int]
    # The next option sets the hour after which all times are attributed to the next day, and before
    # which they are attributed to the previous day. For example, if `previous_or_next_day_hour` is
    # set to 7, any time before 7am will be considered part of the previous day, and any time after
    # 7am will be considered part of the next day. This is used for calculating day of week and
    # generating datetime from (dow+time)
    previous_or_next_day_hour: int = 4
    remove_masseurs_self_massage: bool = True
    latest_possible_massage_set: timedelta = timedelta(minutes=15)
    latest_possible_massage_set_buffer: timedelta = timedelta(minutes=5)
    split_if_longer_than: timedelta = timedelta(hours=4)
    split_chunk: timedelta = timedelta(hours=2, minutes=10)

    _config: Config = PrivateAttr()
    _app = PrivateAttr()
    _filename: str = PrivateAttr()
    _lock: asyncio.Lock = PrivateAttr()
    _massage_id_cap: int = PrivateAttr(0)
    _first_day: datetime = PrivateAttr()

    def __init__(self, config: Config, app):
        filename = config.massage.data_path
        with open(filename, "r", encoding="utf-8", newline="\n") as f:
            data = yaml.safe_load(f)
        super().__init__(**data)
        self._config = config
        self._app = app
        self._filename = filename
        self._lock = asyncio.Lock()
       
        self._after_load()

        app.massage_system = self

    def _after_load(self):
        for id, massage in self.massages.items():
            massage._id = id
        for id, masseur in self.masseurs.items():
            masseur._id = id
        self._massage_id_cap = max(self.massages.keys()) if len(self.massages) > 0 else 0
        self.massage_types.sort(key=lambda type: type.name)

        self.working_hours.sort(key=lambda wh: wh.start)
        self._first_day = self.working_hours[0].start
    
    async def reload(self):
        async with self._lock:
            with open(self._filename, "r", encoding="utf-8", newline="\n") as f:
                data = yaml.safe_load(f)
            super().__init__(**data)
            self._after_load()

    def get_massage_full(self, massage_id):
        massage = self.massages[massage_id]
        masseur = self.masseurs[massage.masseur_id]
        m_type = self.massage_types[massage.massage_type_index]
        return massage, masseur, m_type
    
    async def get_new_id(self):
        async with self._lock:
            return self._get_new_id_insecure()

    async def remove_massage(self, massage_id):
        async with self._lock:
            del self.massages[massage_id]
            self._save_unsafe_sync()
    
    def _get_new_id_insecure(self):
        self._massage_id_cap += 1
        return self._massage_id_cap
    
    def dow_to_day_start(self, dow: int) -> datetime:
        # calculating the desired day
        desired_day = self._first_day + timedelta(days=(dow - self._first_day.weekday() + 7) % 7)

        # setting the boundaries
        start = datetime(desired_day.year, desired_day.month, desired_day.day, self.previous_or_next_day_hour, 0, 0)
        # end = start + timedelta(days=1)
        return start
    
    async def try_add_massage(self, new_massage: Massage) -> bool:
        new_massage_start = new_massage.start
        new_massage_end = new_massage_start + self.massage_types[new_massage.massage_type_index].duration
        masseur_id = new_massage.masseur_id
        if new_massage_start < now_msk() + self.latest_possible_massage_set:
            return -1

        # Check if there's any overlap with existing massages
        async with self._lock:
            for massage in self.massages.values():
                if massage.client_id == new_massage.client_id:
                    massage_start = massage.start
                    massage_end = massage_start + self.massage_types[massage.massage_type_index].duration
                    if max(new_massage_start, massage_start) < min(new_massage_end, massage_end):
                        return -1
                
                if massage.masseur_id == masseur_id:
                    massage_start = massage.start - self.buffer_time
                    massage_end = massage_start + self.massage_types[massage.massage_type_index].duration + (2*self.buffer_time)
                    if max(new_massage_start, massage_start) < min(new_massage_end, massage_end):
                        return -1

            # Check if the new massage fits into the masseur's working hours
            for wh in self.working_hours:
                if wh.masseur_id == masseur_id:
                    if new_massage_start >= wh.start and new_massage_end <= wh.end:
                        # Massage fits into working hours, so we add it
                        new_id = self._get_new_id_insecure()
                        new_massage._id = new_id
                        self.massages[new_id] = new_massage
                        self._save_unsafe_sync()
                        return new_id
        return -1

    async def filter_available_slots(self, slots: dict, dow: int, duration: timedelta, client_id: int):
        day_start = self.dow_to_day_start(dow)
        day_end = day_start + timedelta(days=1)
        filtered_slots = {}
        async with self._lock:
            client_massages = [m for m in self.massages.values() if m.client_id == client_id and m.start < day_end and m.start >= day_start]
            client_massages.sort(key=lambda x: x.start)

        first_possible_time_msk = now_msk() + self.latest_possible_massage_set_buffer + self.latest_possible_massage_set
        for masseur_id, masseur_slots in slots.items():
            filtered_slots[masseur_id] = []
            for slot_start, slot_end in masseur_slots:
                if slot_end < first_possible_time_msk:
                    continue
                if slot_start < first_possible_time_msk:
                    slot_start = first_possible_time_msk
                temp_slot_start = slot_start
                temp_slot_end = slot_end
                for massage in client_massages:
                    massage_end = massage.start + self.massage_types[massage.massage_type_index].duration
                    if temp_slot_start < massage.start < temp_slot_end:
                        if massage.start - temp_slot_start >= duration:
                            filtered_slots[masseur_id].append((temp_slot_start, massage.start))
                        temp_slot_start = massage_end
                    if temp_slot_start < massage_end < temp_slot_end:
                        temp_slot_end = massage.start
                if temp_slot_end - temp_slot_start >= duration:
                    filtered_slots[masseur_id].append((temp_slot_start, temp_slot_end))
        return filtered_slots

    async def get_available_slots(self, dow: int, masseur_ids: list[int], duration: timedelta) -> dict[int, tuple[datetime, datetime]]:
        slots = {}
        day_start = self.dow_to_day_start(dow)
        day_end = day_start + timedelta(days=1)
        for masseur_id in masseur_ids:
            async with self._lock:
                working_hours = sorted(
                    [x for x in self.working_hours if x.masseur_id == masseur_id and x.start < day_end and x.end > day_start],
                    key=lambda x: x.start
                )
                massages = sorted(
                    [x for x in self.massages.values() if x.masseur_id == masseur_id and x.start < day_end and x.start >= day_start],
                    key=lambda x: x.start
                )

            if not working_hours:
                continue

            slots[masseur_id] = []

            # Loop over all working hours
            for wh in working_hours:
                start = max(wh.start, day_start)
                end = min(wh.end, day_end)

                # Check for availability before first massage
                if massages:
                    first_massage_start = massages[0].start
                    if first_massage_start - start >= duration + self.buffer_time:
                        slots[masseur_id].append((start, first_massage_start - self.buffer_time))

                else:
                    # If there are no massages, the whole working period is available
                    if end - start >= duration:
                        slots[masseur_id].append((start, end))

                # Check for availability between massages
                for i in range(1, len(massages)):
                    prev_massage_end = massages[i-1].start + self.massage_types[massages[i-1].massage_type_index].duration
                    curr_massage_start = massages[i].start

                    if curr_massage_start - prev_massage_end >= duration + 2*self.buffer_time:
                        slots[masseur_id].append((prev_massage_end + self.buffer_time, curr_massage_start - self.buffer_time))

                # Check for availability after last massage
                if massages:
                    last_massage = massages[-1]
                    last_massage_end = last_massage.start + self.massage_types[last_massage.massage_type_index].duration
                    if end - last_massage_end >= duration + self.buffer_time:
                        slots[masseur_id].append((last_massage_end + self.buffer_time, end))

        return slots

    async def get_available_slots_for_client(self, client_id: int, dow: int, masseur_ids: list[int], duration: timedelta) -> list[TimeSlot]:
        slots = await self.get_available_slots(
            dow,
            masseur_ids,
            duration,
        )
        slots = await self.filter_available_slots(
            slots,
            dow,
            duration,
            client_id,
        )

        slots_by_time:list[tuple[datetime,datetime,int]] = []
        for masseur_id, m_slots in slots.items():
            for slot in m_slots:
                start, end = slot
                slots_by_time.append((start,end,masseur_id))
        slots_by_time.sort(key=lambda x: x[0])

        ret = []
        for slot in slots_by_time:
            start, end, masseur_id = slot
            if end-start > self.split_if_longer_than:
                while end-start > self.split_if_longer_than:
                    ts = TimeSlot()
                    ts.start = start
                    ts.end = start + self.split_chunk
                    ts.masseur_id = masseur_id
                    ret.append(ts)
                    start = ts.end
            if end-start > duration:
                ts = TimeSlot()
                ts.start = start
                ts.end = start + duration
                ts.masseur_id = masseur_id
                ret.append(ts)

                ts = TimeSlot()
                ts.start = end - duration
                ts.end = end
                ts.masseur_id = masseur_id
                ret.append(ts)
        
        ret.sort(key=lambda x: (x.masseur_id, x.start))

        filtered_slots = [ret[0]]  # We always include the first slot
        last_slot = ret[0]
        half_duration = duration / 2

        for i in range(1, len(ret)):
            # If the current slot and previous slot have the same masseur,
            # and their starts are closer than duration/2, skip the current slot

            if ret[i].masseur_id == last_slot.masseur_id and abs(ret[i].start - last_slot.start) < half_duration:
                continue
            else:
                filtered_slots.append(ret[i])
                last_slot = ret[i]

        filtered_slots.sort(key=lambda x: x.start)
        return filtered_slots

    @validator('masseurs')
    def check_masseurs_len(cls, masseurs):
        if len(masseurs) > 5:
            raise ValueError("currently only up to 5 masseurs are supported")
        return masseurs

    @validator('working_hours')
    def check_working_hours(cls, working_hours):
        masseur_dict = {}
        for wh in working_hours:
            if wh.masseur_id not in masseur_dict:
                masseur_dict[wh.masseur_id] = []
            masseur_dict[wh.masseur_id].append(wh)

        for wh in masseur_dict.values():
            wh.sort(key=lambda h: h.start)

            for i in range(len(wh) - 1):
                if wh[i].end > wh[i + 1].start:
                    raise ValueError("working hours can't overlap")

        return working_hours
    
    def get_client_massages(self, client_id: int) -> list[Massage]:
        return sorted([massage for massage in self.massages.values()
                if massage.client_id == client_id
                and massage.start + self.massage_types[massage.massage_type_index].duration > now_msk()],
                key=lambda x: x.start)
    
    def get_masseur_massages(self, masseur_id: int) -> list[Massage]:
        return sorted([massage for massage in self.massages.values()
                if massage.masseur_id == masseur_id
                and massage.start + self.massage_types[massage.massage_type_index].duration > now_msk()],
                key=lambda x: x.start)

    def _save_unsafe_sync(self):
        with open(self._filename, "w", encoding="utf-8", newline="\n") as f:
            yaml.safe_dump(self.dict(), stream=f, allow_unicode=True)
    
    @async_thread
    def _save_unsafe_async(self):
        self._save_unsafe_sync()
    
    async def save(self):
        async with self._lock:
            self._save_unsafe_sync()
            # await self._save_async()
    
    async def notificator(self):
        import asyncio
        from telegram.constants import ParseMode
        while True:
            await asyncio.sleep(self._config.massage.notificator_loop_frequency.total_seconds())
            msk_time = now_msk()
            for massage in self.massages.values():
                masseur = self.masseurs[massage.masseur_id]
                m_type = self.massage_types[massage.massage_type_index]
                total_minutes = m_type.duration.total_seconds() // 60

                if not massage._client_notified and \
                    massage.start - self._config.massage.notify_client_in_prior < msk_time and \
                    massage.start > msk_time:

                    await self._app.bot.bot.send_message(
                        chat_id=massage.client_id,
                        text=
                        "Привет зуконавт!\nНапоминаю, что у тебя есть запись на массаж через "+
                        f"{self._config.massage.notify_client_in_prior.total_seconds()//60} минут:\n"+
                        f"Тип массажа: {m_type.name} — {m_type.price} ₽ / {total_minutes} минут.\n"+
                        f"Массажист: {masseur.link_html()}\nВремя: {massage.massage_client_repr()}\n"+
                        "Приходи <u>вовремя</u> ведь после тебя будет кто-то ещё. А если не можешь прийти — лучше заранее отменить.\n"+
                        "Приятного погружения!",
                        parse_mode=ParseMode.HTML
                    )
                    massage._client_notified = True
                if not massage._masseur_notified and \
                    massage.start - self.buffer_time < msk_time and \
                    massage.start > msk_time:
                    
                    masseur = self.masseurs[massage.masseur_id]
                    if masseur.before_massage_notifications:
                        await self._app.bot.bot.send_message(
                            chat_id=masseur._id,
                            text=
                            "Напоминаю, что у тебя есть запись через "+
                            f"{self.buffer_time.total_seconds()//60} минут:\n"+
                            f"Тип массажа: {m_type.name} — {m_type.price} ₽ / {total_minutes} минут.\n"+
                            f"Клиент: <i>{massage.client_link_html()}</i>\nВремя: {massage.massage_client_repr()}",
                            parse_mode=ParseMode.HTML
                        )
                        pass
                    massage._masseur_notified = True
                
