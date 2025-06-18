from typing import List
from motor.core import AgnosticCollection
import pytz
from ..tg_state import TGState
from ..telegram_links import client_user_link_html, client_user_name
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler
from telegram.constants import ParseMode
import logging
from bson.objectid import ObjectId
from ..config import Party, full_link
from datetime import datetime, timedelta
from asyncio import Lock, create_task, gather
from math import floor, ceil
from .pass_keys import PASS_KEY

logger = logging.getLogger(__name__)

SLOT_LENGTH=20
SLOT_BUFFER=5
SLOT_DURATION=timedelta(minutes=SLOT_LENGTH)
EARLY_COMER_TOLERANCE=timedelta(hours=2)
SPECIALIST_SLOT_FLYOVER=timedelta(minutes=5)
CLIENT_SLOT_RESTRICTION=timedelta(minutes=15)
NOTIFICATOR_LOOP=30

ctime = datetime.now()
ctime_msk = datetime.now(tz=pytz.timezone("Europe/Moscow")).replace(tzinfo=None)
MSK_OFFSET = timedelta(seconds=round((ctime_msk-ctime).total_seconds()))

def now_msk() -> datetime:
    return datetime.now() + MSK_OFFSET

logger.debug(f"ctime {ctime}, ctime msk {ctime_msk}, msk offset {MSK_OFFSET}")
logger.debug(f"now msk {now_msk()}")

def price_from_length(length:int=1)->int:
    if length == 1:
        return 900
    if length == 2:
        return 1400
    if length == 3:
        return 1800
    if length == 4:
        return 2400
    if length == 5:
        return 2700
    if length == 6:
        return 3000
    raise ValueError(f"Unsupported length {length} for price calculation")

def min_from_length(length:int =1)->int:
    return (length*SLOT_LENGTH)-SLOT_BUFFER

def length_icon(length:int =1)->str:
    return ("🕐🕑🕒🕓🕔🕕🕖🕗🕘🕙🕚")[length]

def async_cache(ttl: int):
    """
    Decorator to cache the result of an async function for a specific amount of time.
    
    Args:
        ttl (int): Time-to-live for the cache in seconds.
    """
    import functools
    from time import time
    from typing import Any, Callable, Dict, Tuple
    def decorator(func: Callable):
        cache: Dict[Tuple[Any, ...], Tuple[Any, float]] = {}

        @functools.wraps(func)
        async def wrapper(*args, nocache=False, **kwargs):
            nonlocal cache
            key = (args, frozenset(kwargs.items()))
            current_time = time()
            
            if key in cache and not nocache:
                result, timestamp = cache[key]
                if current_time - timestamp < ttl:
                    return result
            
            result = await func(*args, **kwargs)
            cache[key] = (result, current_time)
            return result

        return wrapper

    return decorator

class Specialist:
    user = None
    def __init__(self, base: 'MassagePlugin', masseur_id: int, user = None):
        self.base = base
        self.id = int(masseur_id)
        self.language_code = "en"
        self._set_user(user)

    def _set_user(self, user):
        if user is not None and not "massage_specialist" in user:
            raise Exception("user not massage specialist")
        if user is not None:
            self.user = user
            self.specialist = user["massage_specialist"]
            if "language_code" in user:
                self.language_code = user["language_code"]
    @property
    def icon(self):
        if "icon" in self.specialist:
            return self.specialist["icon"]
        return "💆‍♂️"

    @property
    def name(self):
        if "name" in self.specialist:
            return self.specialist["name"]
        return client_user_name(self.user)
    
    @property
    def min_duration(self):
        if "min_duration" in self.specialist:
            return self.specialist["min_duration"]
        return 1
    
    @property
    def max_duration(self):
        if "max_duration" in self.specialist:
            return self.specialist["max_duration"]
        return 1000
    
    @property
    def table_required(self):
        if "table_not_required" in self.specialist:
            return self.specialist["table_not_required"]
    
    @property
    def notify_bookings(self) -> bool:
        return "notify_bookings" not in self.specialist \
            or self.specialist["notify_bookings"]
        return True
    
    @property
    def notify_next(self) -> bool:
        return "notify_next" not in self.specialist \
            or self.specialist["notify_next"]
        return True

    def l(self, s, **kwargs):
        return self.base.base_app.localization(s, args=kwargs, locale=self.language_code)

    async def init(self):
        user = await self.base.user_db.find_one({
            "user_id": self.id,
            "bot_id": self.base.bot.id,
        })
        self._set_user(user)

    def get_my_slots(self, day) -> set[int]:
        party = self.base.parties_by_day[day]
        work_hours = self.specialist["work_hours"]
        slots = set[int]()
        for work_span in work_hours:
            if party.start-EARLY_COMER_TOLERANCE <= work_span["start"] <= party.end:
                first_slot = ceil((work_span["start"] - party.start).total_seconds()/60/SLOT_LENGTH)
                last_slot = floor((work_span["end"] - party.start).total_seconds()/60/SLOT_LENGTH)
                slots = slots.union(range(first_slot, last_slot))
        logger.debug(f"specialist {self.name} slots for day {day}: {slots}")
        return slots
    
    async def get_both_slots(self, day) -> tuple[set[int], set[int]]:
        slots = self.get_my_slots(day)
        occupied = await self.get_slots_occupied(day)
        logger.debug(f"slots for {day} for {self.name}: {occupied}, {slots.difference(occupied)}")
        return occupied, slots.difference(occupied)
    
    async def get_slots_available(self, day) -> set[int]:
        _, avail = await self.get_both_slots(day)
        return avail
    
    async def get_nearest_massage(self) -> 'MassageRecord|None':
        ret = await self.base.massage_db.find({
            "specialist": self.id,
            "deleted": {"$exists":False},
            "start": {"$gt": now_msk()},
            "pass_key": PASS_KEY,
        }).sort('start', 1).to_list(length=1)
        if len(ret) == 0:
            return None
        ret = MassageRecord(ret[0], self.base, specialist=self)
        await ret.init()
        return ret
    
    async def current_massage(self) -> 'MassageRecord|None':
        now = now_msk()
        ret = await self.base.massage_db.find_one({
            "specialist": self.id,
            "deleted": {"$exists":False},
            "start": {"$lte": now},
            "end": {"$gte": now},
            "pass_key": PASS_KEY,
        })
        if ret is None:
            return None
        ret = MassageRecord(ret, self.base, specialist=self)
        await ret.init()
        return ret
    
    async def get_slots_occupied(self, day) -> set[int]:
        massages = await self.get_massages(day)
        slots = set[int]()
        for massage in massages:
            slot_start = massage.slot_index
            slots = slots.union(range(slot_start, slot_start+massage.record["length"]))
        return slots

    async def get_massages(self, day) -> list['MassageRecord']:
        cursor = self.base.massage_db.find({
            "specialist": self.id,
            "deleted": {"$exists":False},
            "party": day,
            "pass_key": PASS_KEY
        }).sort('start', 1)
        ret = []
        async for massage in cursor:
            mas = MassageRecord(massage, self.base, specialist=self)
            await mas.init()
            ret.append(mas)
        return ret
    
    async def write(self, msg, *args, **kwargs):
        return await self.base.bot.send_message(self.id, msg, *args, **kwargs)
    
    def about(self, lc="en"):
        about = self.specialist["about"]
        if "about_" + lc in self.specialist:
            about = self.specialist["about_" + lc]
        return self.icon + " <b>" + client_user_link_html(self.user, self.name) + "</b> \n" + about + "\n"
    
    def massage_edit_buttons(self, massage: 'MassageRecord'):
        return [[
            InlineKeyboardButton(
                self.l("massage-specialist-to-start-button"),
                callback_data=f"{self.base.name}|start"
            ),
            InlineKeyboardButton(
                self.l("massage-specialist-clientlist-button"),
                callback_data=f"{self.base.name}|clientlist|{massage.day}"
            ),
            InlineKeyboardButton(
                self.l("massage-specialist-timetable-button"),
                web_app=WebAppInfo(full_link(self.base.base_app, f"/massage_timetable"))
            ),
        ]]

class MassageRecord:
    client = None
    client_language_code = "en"
    specialist: Specialist|None = None
    sl = None
    def __init__(self, massage, plugin: 'MassagePlugin', client = None, specialist: Specialist|None = None):
        self.db = plugin.massage_db
        self.plugin = plugin
        self.record = massage
        self._set_client(client)
        self._set_specialist(specialist)
    
    async def init(self, clean=False):
        if not "_id" in self.record:
            self.record["pass_key"] = PASS_KEY
            rec = await self.plugin.massage_db.insert_one(self.record)
            self.record["_id"] = rec.inserted_id
        if self.client is None:
            client = await self.plugin.user_db.find_one({
                "user_id": self.record["user_id"],
            })
            self._set_client(client)
        if self.specialist is None and "specialist" in self.record:
            specialist = await self.plugin.get_specialist(self.record["specialist"])
            self._set_specialist(specialist)
    
    async def notify_client_prior_long(self):
        await self.plugin.massage_db.update_one(
            {"_id":self.record["_id"]},
            {"$set":{"client_notified_prior_long":True}}
        )
        await self.init()
        await self.plugin.bot.send_message(
            self.record["user_id"],
            self.client_repr(
                "massage-notification-prior-long",
                inminutes=floor(self.plugin.config.massages.notify_client_prior_long.total_seconds()/60)
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(self.client_buttons()),
            disable_web_page_preview=True,
        )
    
    async def notify_client_addidtional(self):
        await self.plugin.massage_db.update_one(
            {"_id":self.record["_id"]},
            {"$unset":{"notify":True}}
        )
        await self.init()
        await self.plugin.bot.send_message(
            self.record["user_id"],
            self.client_repr(
                "massage-notification-additional"
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(self.client_buttons()),
            disable_web_page_preview=True,
        )
    
    async def notify_client(self):
        await self.plugin.massage_db.update_one(
            {"_id":self.record["_id"]},
            {"$set":{"client_notified":True}}
        )
        await self.init()
        await self.plugin.bot.send_message(
            self.record["user_id"],
            self.client_repr(
                "massage-notification-prior",
                inminutes=floor(self.plugin.config.massages.notify_client_prior.total_seconds()/60)
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(self.client_buttons()),
            disable_web_page_preview=True,
        )

    def specialist_repr(self, key, **kwargs):
        return self.specialist.l(
            key,
            duration=self.duration,
            price=self.price,
            durationicon=length_icon(self.length),
            client=client_user_link_html(self.client),
            party=self.specialist.l("dow-long", dow=self.party.start.weekday())+
            "-"+self.specialist.l("dow-long", dow=self.party.end.weekday()),
            time=self.start_str, **kwargs
        )
    
    def client_buttons(self, cl=None):
        if cl is None:
            cl = self.cl
        return [[
            InlineKeyboardButton(cl("massage-booking-cancel-button"), callback_data=f"{self.plugin.name}|ed|{self.id}|1")
        ],[
            InlineKeyboardButton(cl("massage-edit-back-button"), callback_data=f"{self.plugin.name}|start"),
            InlineKeyboardButton(cl("massage-exit-button"), callback_data=f"{self.plugin.name}|exit"),
        ]]

    def client_repr(self, key, client_language_code=None, **kwargs):
        if client_language_code is not None:
            ocl = self.client_language_code
            self.client_language_code = client_language_code
        ret = self.cl(
            key,
            duration=self.duration,
            price=self.price,
            durationicon=length_icon(self.length),
            specialist=self.specialist.about(self.client_language_code),
            party=self.cl("dow-long", dow=self.party.start.weekday())+
            "-"+self.cl("dow-long", dow=self.party.end.weekday()),
            time=self.start_str, **kwargs
        )
        if client_language_code is not None:
            self.client_language_code = ocl
        return ret
    
    async def notify_specialist(self):
        await self.plugin.massage_db.update_one(
            {"_id":self.record["_id"]},
            {"$set":{"specialist_notified":True}}
        )
        await self.init()
        if self.specialist.notify_next:
            await self.specialist.write(
                self.specialist_repr(
                    "massage-specialist-notification-soon",
                    inminutes=SLOT_BUFFER
                ),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(self.specialist.massage_edit_buttons(self)),
                disable_web_page_preview=True,
            )
    
    async def save(self):
        return await self.plugin.massage_db.replace_one(
            {"_id":self.record["_id"]},
            self.record
        )

    def cl(self, s, **kwargs):
        return self.plugin.base_app.localization(s, args=kwargs, locale=self.client_language_code)
    def _set_client(self,client):
        if client is None:
            return
        self.client = client
        if "language_code" in client:
            self.language_code = client["language_code"]
    def _set_specialist(self,specialist: Specialist|None):
        if specialist is None:
            return
        self.specialist = specialist
        self.sl = specialist.l
    
    @property
    def party(self) -> Party|None:
        day = self.day
        if day is not None:
            return self.plugin.parties_by_day[day]
        return None

    @property
    def is_finished(self):
        return "start" in self.record

    @property
    def duration(self):
        if "length" in self.record:
            return min_from_length(self.record["length"])
        return None

    @property
    def length(self):
        if "length" in self.record:
            return self.record["length"]
        return None

    @property
    def price(self):
        if "length" in self.record:
            return price_from_length(self.record["length"])
        return None
    
    @property
    def start(self) -> datetime|None:
        if "start" in self.record:
            return self.record["start"]
        return None
    
    @property
    def end(self) -> datetime|None:
        if "start" in self.record and "length" in self.record:
            return self.start + timedelta(minutes=self.duration)
        return None
    
    @property
    def start_str(self) -> str:
        if "start" in self.record:
            return self.record["start"].strftime("%H:%M")
        return "??:??"
    
    @property
    def slot_index(self):
        return floor((self.start - self.party.start).total_seconds()/60/SLOT_LENGTH)
    
    @property
    def day(self):
        if "party" in self.record:
            return self.record["party"]
        return None
    
    @property
    def id(self):
        return str(self.record["_id"])

def split_list(lst: list, chunk: int) -> list[list]:
    result = []
    for i in range(0, len(lst), chunk):
        sublist = lst[i:i+chunk]
        result.append(sublist)
    return result

SLOT_PAGE_SPLIT=4
SLOT_PAGE_SIZE=SLOT_PAGE_SPLIT*6
MY_MASSAGES_SPLIT=2
SPECIALIST_MASSAGES_SPLIT=3
class UserMassages:
    def __init__(self, massage: 'MassagePlugin', update: TGState) -> None:
        self.update = update
        self.plugin = massage
        self.user = None
        self.specialist = None
        self.l = update.l
        
    async def get_user(self):
        if self.user is None:
            self.user = await self.update.get_user()
            if "massage_specialist" in self.user:
                self.specialist = Specialist(self.plugin, self.user["user_id"], self.user)
        return self.user
    
    async def get_massages(self) -> List[MassageRecord]:
        ret = []
        async for massage in self.plugin.massage_db.find({
            "user_id": self.user["user_id"],
            "deleted": {"$exists":False},
            "pass_key": PASS_KEY,
            "start": {
                "$gte": self.plugin.parties_begin,
                "$lte": self.plugin.parties_end
            },
        }).sort("start", 1):
            rec = MassageRecord(massage, self.plugin, self.user)
            await rec.init()
            ret.append(rec)
        return ret
    
    def get_occupied_slots(self, day: int, massages: List[MassageRecord]) -> set[int]:
        ret = set[int]()
        for massage in massages:
            if massage.day == day:
                sid = massage.slot_index
                ret = ret.union(range(sid, sid + massage.length))
        return ret
    
    async def get_massage(self, id) -> MassageRecord:
        massage = await self.plugin.massage_db.find_one({
            "_id": ObjectId(id),
        })
        if massage is None:
            return None
        rec = MassageRecord(massage, self.plugin)
        await rec.init()
        return rec
    
    async def handle_cq_create(self):
        user = await self.get_user()
        rec = MassageRecord({
            "user_id": self.update.user,
            "created_at": datetime.now(),
            "pass_key": PASS_KEY,
        }, self.plugin, user)
        await rec.init()
        await self.edit_massage(rec)
    
    async def handle_cq_ed(self, massage_id, choice="0"):
        rec = await self.get_massage(massage_id)
        await self.edit_massage(rec, int(choice))
    
    async def handle_cq_back(self, massage_id):
        rec = await self.get_massage(massage_id)
        await self.edit_massage(rec, -1)
    
    async def handle_cq_clientlist(self, day=None):
        if self.specialist is None:
            raise Exception("forbidden")
        if day is None:
            day = self.plugin.config.parties[0].start.day
        else:
            day = int(day)
        massages = await self.specialist.get_massages(day)
        parties = [party for party in self.plugin.config.parties if not party.is_open]
        keyboard = [[
            InlineKeyboardButton(
                ("" if party.start.day != day else "🔘 ")+
                self.l("massage-edit-choose-party-button",
                    party=self.l("dow-short", dow=party.start.weekday()) + \
                        "-" + self.l("dow-short", dow=party.end.weekday())
                ),
                callback_data=f"{self.plugin.name}|clientlist|{party.start.day}"
            ) for party in parties
        ]]
        buttons = [InlineKeyboardButton(
            massage.start_str + f" {massage.duration}' " + client_user_name(massage.client),
            callback_data=f"{self.plugin.name}|sped|{massage.id}"
        ) for massage in massages]
        keyboard.extend(split_list(buttons, SPECIALIST_MASSAGES_SPLIT))
        keyboard.append([
            InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|start"),
            InlineKeyboardButton(
                self.l("massage-specialist-timetable-button"),
                web_app=WebAppInfo(full_link(self.plugin.base_app, f"/massage_timetable"))
            ),
            InlineKeyboardButton(self.l("massage-exit-button"), callback_data=f"{self.plugin.name}|exit"),
        ])
        await self.update.edit_or_reply(
            self.l("massage-specialist-clientlist") + "\n\n" + "\n".join([
                massage.start_str + f" {massage.duration}' " + client_user_link_html(massage.client) for massage in massages
            ]),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )

    async def handle_cq_sped(self, massage_id):
        if self.specialist is None:
            raise Exception("forbidden")
        massage = await self.get_massage(massage_id)
        await massage.specialist.write(
            massage.specialist_repr("massage-specialist-view"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(self.specialist.massage_edit_buttons(massage)),
            disable_web_page_preview=True,
        )
    
    # Cancel edit
    async def handle_cq_xed(self, massage_id):
        await self.plugin.massage_db.delete_one({
            "_id": ObjectId(massage_id)
        })
        await self.handle_cq_exit()
    
    async def delete_massage(self, massage: MassageRecord):
        await self.plugin.massage_db.update_one({
            "_id": ObjectId(massage.id)
        }, {
            "$set": {"deleted": True}
        })
        if massage.specialist.notify_bookings:
            await massage.specialist.write(
                massage.specialist.l(
                    "massage-specialist-booking-cancelled",
                    duration=massage.duration,
                    price=massage.price,
                    durationicon=length_icon(massage.length),
                    client=client_user_link_html(massage.client),
                    party=self.l("dow-long", dow=massage.party.start.weekday())+"-"+self.l("dow-long", dow=massage.party.end.weekday()),
                    time=massage.start_str
                ),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
                disable_web_page_preview=True,
            )
        await self.update.edit_or_reply(
            self.l("massage-deleted"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
            disable_web_page_preview=True,
        )

    async def edit_massage(self, massage: MassageRecord, choice = 0):
        just_finalized=False
        if not "length" in massage.record:
            logger.info(f"massage edit length {massage.id}, choice {choice}")
            if choice == 0:
                return await self.render_choose_length(massage)
            if choice < 0:
                await self.plugin.massage_db.delete_one({
                    "_id": ObjectId(massage.id)
                })
                return await self.handle_start()
            massage.record["length"] = choice
            await massage.save()
            return await self.render_select_slot(massage)
        if not "start" in massage.record:
            logger.info(f"massage edit select slot {massage.id}, choice {choice}")
            if choice < 0:
                del massage.record["length"]
                del massage.record["specialists_choices"]
                del massage.record["page"]
                await massage.save()
                return await self.render_choose_length(massage)
            if "choices" in massage.record and str(choice) in massage.record["choices"]:
                ch = massage.record["choices"][str(choice)]
                logger.info(f"massage edit select slot {massage.id}, choice {ch}")
                if "party" in ch:
                    massage.record["party"] = ch["party"]
                elif "page" in ch:
                    massage.record["page"] += ch["page"]
                elif "slot" in ch:
                    just_finalized=await self.finalize_massage(massage, ch["slot"], ch["specialist"])
                elif "specialist" in ch:
                    sp_id = str(ch["specialist"])
                    massage.record["specialists_choices"][sp_id] = not massage.record["specialists_choices"][sp_id]
        if not "start" in massage.record:
            return await self.render_select_slot(massage)
        if choice == 1:
            return await self.delete_massage(massage)
        keyboard = massage.client_buttons(self.l)
        msg = ""
        if just_finalized:
            msg = self.l("massage-successfully-created") + "\n\n"
        msg += massage.client_repr("massage-client-about", client_language_code=self.update.language_code)
        await self.update.edit_or_reply(
            msg,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )
    
    async def finalize_massage(self, massage: MassageRecord, slot: int, specialist_id: int, skip_notify=False):
        deadline_time = now_msk() + CLIENT_SLOT_RESTRICTION
        if self.specialist is not None:
            deadline_time = now_msk() - SPECIALIST_SLOT_FLYOVER
        if self.plugin.slot_time(massage.day, slot) < deadline_time:
                massage.record["error"] = self.l("massage-edit-error-slot-timeout")
                return False
        async with self.plugin.finalizer_lock:
            available = await self.plugin.available_slots(massage.day, massage.length)
            if self.specialist is None:
                massages = await self.get_massages()
                if len([m for m in massages if m.day == massage.day]) >= self.plugin.config.massages.max_massages_a_day:
                    massage.record["error"] = self.l(
                        "massage-edit-error-too-many-massages",
                        max=self.plugin.config.massages.max_massages_a_day
                    )
                    return False
                my_occupied = self.get_occupied_slots(massage.day, massages)
                my_occupied_extended = my_occupied.copy()
                for slot_id in my_occupied:
                    for i in range(1, massage.length+1):
                        if not slot_id-i in my_occupied:
                            my_occupied_extended.add(slot_id-i)
            else:
                my_occupied_extended = set[int]()
            if not slot in available or not specialist_id in available[slot] and not slot in my_occupied_extended:
                massage.record["error"] = self.l("massage-edit-error-slot-unavailable")
                return False
            massage.record["slot"] = slot
            massage.record["start"] = self.plugin.slot_time(massage.day, slot)
            massage.record["end"] = massage.record["start"]+timedelta(minutes=massage.duration)
            massage.record["specialist"] = specialist_id
            massage.record["finalized_at"] = datetime.now()
            if "choices" in massage.record:
                del massage.record["choices"]
            if "specialists_choices" in massage.record:
                del massage.record["specialists_choices"]
            if "page" in massage.record:
                del massage.record["page"]
            await massage.save()
        await massage.init()
        if massage.specialist.notify_bookings and not skip_notify:
            await massage.specialist.write(
                massage.specialist_repr("massage-specialist-new-booking"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(massage.specialist.massage_edit_buttons(massage)),
                disable_web_page_preview=True,
            )
        return True
    
    async def render_select_slot(self, massage: MassageRecord):
        choices = {}
        _choice_id = 0
        def choice_id():
            nonlocal _choice_id
            _choice_id += 1
            return _choice_id
        def add_choice(doc):
            nonlocal choices
            cid = choice_id()
            choices[str(cid)] = doc
            return f"{self.plugin.name}|ed|{massage.id}|{cid}"
        parties = [party for party in self.plugin.config.parties if not party.is_open and party.end + EARLY_COMER_TOLERANCE > now_msk()]
        current_party = self.plugin.current_party()
        if massage.party is None:
            if current_party is not None:
                massage.record["party"] = current_party.start.day
            else:
                massage.record["party"] = parties[-1].start.day
        keyboard = [[
            InlineKeyboardButton(
                ("" if party.start.day != massage.day else "🔘 ")+
                self.l("massage-edit-choose-party-button",
                    party=self.l("dow-short", dow=party.start.weekday()) + \
                        "-" + self.l("dow-short", dow=party.end.weekday())
                ),
                callback_data=add_choice({
                    "party": party.start.day
                })
            ) for party in parties if (current_party is None or party.start >= current_party.start) and party.end >= now_msk()
        ]]
        specialists = await self.plugin.get_specialists()
        total_specialists = len(specialists)
        specialists = [s for s in specialists if s.min_duration <= massage.length <= s.max_duration]
        hidden_specialists = total_specialists != len(specialists)
        specialists.sort(key=lambda s: s.id)
        specialists_dict = {s.id:s for s in specialists}
        if "specialists_choices" not in massage.record:
            massage.record["specialists_choices"] = {str(s.id):True for s in specialists}
        buttons = [
            InlineKeyboardButton(
                text=("✅" if massage.record["specialists_choices"][str(s.id)] else "❌") + f" {s.icon} {s.name}",
                callback_data=add_choice({
                    "specialist": str(s.id)
                })
            ) for s in specialists
        ]
        keyboard.extend(split_list(buttons, 2))

        available = await self.plugin.available_slots(massage.day, massage.length)
        cp = self.plugin.current_party()
        if cp is not None and cp.start.weekday() == massage.day:
            current_slot = self.plugin.current_slot()
            if self.specialist is None and current_slot is not None:
                current_slot += 1
        else:
            current_slot = None
        if self.specialist is None:
            my_massages = await self.get_massages()
            day_massages = [m for m in my_massages if m.day == massage.day]
            my_occupied = self.get_occupied_slots(massage.day, my_massages)
            my_occupied_extended = my_occupied.copy()
            for slot_id in my_occupied:
                for i in range(1, massage.length+1):
                    if not slot_id-i in my_occupied:
                        my_occupied_extended.add(slot_id-i)
        else:
            my_occupied_extended = set[int]()
            day_massages = []
        buttons = []
        for slot in available:
            if self.plugin.slot_time(massage.day, slot) < now_msk():
                continue
            if current_slot is not None and slot <= current_slot:
                continue
            for specialist_id in available[slot]:
                if specialist_id in specialists_dict and \
                    massage.record["specialists_choices"][str(specialist_id)] and \
                        not slot in my_occupied_extended:
                    buttons.append(InlineKeyboardButton(
                        specialists_dict[specialist_id].icon + " " + self.plugin.slot_time(massage.day, slot).strftime("%H:%M"),
                        callback_data=add_choice({
                            "slot": slot,
                            "specialist": specialist_id
                        })
                    ))
        if not "page" in massage.record or massage.record["page"] < 0:
            massage.record["page"] = 0
        buttons = split_list(buttons, SLOT_PAGE_SIZE)
        if massage.record["page"] >= len(buttons):
            massage.record["page"] = len(buttons) - 1
        
        if len(buttons) > 0 and len(day_massages) < self.plugin.config.massages.max_massages_a_day:
            keyboard.extend(split_list(buttons[massage.record["page"]], SLOT_PAGE_SPLIT))
        else:
            keyboard.append([InlineKeyboardButton(self.l("massage-edit-no-slots-available"), callback_data=f"{self.plugin.name}|ed|{massage.id}|0")])
        keyboard.extend([
            [
                InlineKeyboardButton(self.l("massage-edit-page-previous-button"), callback_data=add_choice({
                    "page":-1
                })),
                InlineKeyboardButton(self.l(
                    "massage-edit-page",
                    page=massage.record["page"]+1,
                    leng=len(buttons)
                ), callback_data=f"{self.plugin.name}|ed|{massage.id}|0"),
                InlineKeyboardButton(self.l("massage-edit-page-next-button"), callback_data=add_choice({
                    "page":1
                }))
            ],[
                InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|back|{massage.id}"),
                InlineKeyboardButton(self.l("massage-edit-cancel-button"), callback_data=f"{self.plugin.name}|xed|{massage.id}"),
            ]
        ])

        msg = self.l(
            "massage-edit-select-slot",
            duration=massage.duration,
            price=massage.price,
            durationicon=length_icon(massage.length),
            specialists="   " + "\n   ".join([s.about(self.update.language_code) for s in specialists]),
            filtered=("\n\n"+self.l("massage-edit-select-specialists-filtered")) if hidden_specialists else "",
            error=("\n\n"+massage.record["error"]) if "error" in massage.record else ""
        )
        if "error" in massage.record:
            del massage.record["error"]
        massage.record["choices"] = choices
        await massage.save()

        await self.update.edit_or_reply(
            msg,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )

    async def render_choose_length(self, massage: MassageRecord):
        def len_btn(length):
            return InlineKeyboardButton(
                self.l("massage-edit-length-button",
                        icon=length_icon(length),
                        minutes=min_from_length(length),
                        price=price_from_length(length)
                ), callback_data=f"{self.plugin.name}|ed|{massage.id}|{length}")
        keyboard = [
            [len_btn(1),len_btn(2)],
            [len_btn(3),len_btn(5)],
            # [len_btn(5),len_btn(6)],
            [
                InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|back|{massage.id}"),
                InlineKeyboardButton(self.l("massage-edit-cancel-button"), callback_data=f"{self.plugin.name}|xed|{massage.id}")
            ]
        ]
        await self.update.edit_or_reply(
            self.l(
                "massage-edit-choose-length"
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )

    async def handle_cq_start(self):
        return await self.handle_start()

    async def handle_cq_exit(self):
        await self.update.edit_message_text(
            self.l("massage-exited"),
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_callback_query(self):
        q = self.update.callback_query
        await q.answer()
        logger.info(f"Received callback_query from {self.user}, data: {q.data}")
        data = q.data.split("|")
        fn = "handle_cq_" + data[1]
        logger.debug(f"fn: {fn}")
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            logger.debug(f"fn: {attr}")
            if callable(attr):
                return await attr(*data[2:])
        logger.error(f"unknown callback {data[1]}: {data[2:]}")

    async def handle_cq_notifications(self, toggle="0"):
        toggle = int(toggle)
        buttons = []
        if self.specialist is not None:
            toggle_value = None
            if toggle == 51:
                toggle_name = "notify_bookings"
                toggle_value = not self.specialist.notify_bookings
            if toggle == 52:
                toggle_name = "notify_next"
                toggle_value = not self.specialist.notify_next
            if toggle_value is not None:
                self.specialist.specialist[toggle_name] = toggle_value
                await self.plugin.user_db.update_one({
                    "user_id": self.update.user,
                    "bot_id": self.plugin.bot.id
                }, {
                    "$set": {"massage_specialist."+toggle_name: toggle_value}
                })
            buttons.extend([
                InlineKeyboardButton(
                    self.l("massage-notification-toggle",
                        pos="y" if self.specialist.notify_bookings else "n"
                    ) + " " + self.l("massage-specialist-notification-notify-bookings"),
                    callback_data=f"{self.plugin.name}|notifications|51"
                ),
                InlineKeyboardButton(
                    self.l("massage-notification-toggle",
                        pos="y" if self.specialist.notify_next else "n"
                    ) + " " + self.l("massage-specialist-notification-notify-next"),
                    callback_data=f"{self.plugin.name}|notifications|52"
                ),
            ])
        keyboard = split_list(buttons, 1)
        keyboard.append([
            InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|start"),
            InlineKeyboardButton(self.l("massage-exit-button"), callback_data=f"{self.plugin.name}|exit"),
        ])
        return await self.update.edit_or_reply(
            self.l("massage-notifications-edit"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )
    
    async def handle_cq_instant(self, num_slots="1"):
        num_slots = int(num_slots)
        if self.specialist is None:
            raise Exception("forbidden")
        party = self.plugin.current_party()
        slot = self.plugin.current_slot()
        if party is None or slot is None:
            return await self.update.edit_or_reply(
                self.l("massage-specialist-no-party-or-slot"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[
                    InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|start"),
                    InlineKeyboardButton(self.l("massage-exit-button"), callback_data=f"{self.plugin.name}|exit"),
                ]]),
                disable_web_page_preview=True,
            )
        if self.plugin.slot_time(party.start.day, slot) < now_msk() - SPECIALIST_SLOT_FLYOVER:
            slot += 1
        massage_record={
            "user_id": self.update.user,
            "length": num_slots,
            "party": party.start.day,
            "pass_key": PASS_KEY,
        }
        user = await self.get_user()
        massage = MassageRecord(massage_record, self.plugin, user)
        await massage.init()
        finalized = await self.finalize_massage(massage, slot, self.update.user, skip_notify=True)
        if not finalized:
            await self.update.edit_or_reply(
                self.l("massage-specialist-failed-to-reserve"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[
                    InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|start"),
                    InlineKeyboardButton(self.l("massage-exit-button"), callback_data=f"{self.plugin.name}|exit"),
                ]]),
                disable_web_page_preview=True,
            )
        else:
            await self.update.edit_or_reply(
                massage.specialist_repr("massage-specialist-reserved"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(self.specialist.massage_edit_buttons(massage)),
                disable_web_page_preview=True,
            )

    async def handle_start(self):
        keyboard = []
        if self.specialist is not None:
            keyboard.append([
                InlineKeyboardButton(
                    self.l("massage-specialist-timetable-button"),
                    web_app=WebAppInfo(full_link(self.plugin.base_app, f"/massage_timetable"))
                ),
                InlineKeyboardButton(self.l("massage-specialist-clientlist-button"), callback_data=f"{self.plugin.name}|clientlist"),
            ])
            cur = await self.specialist.current_massage()
            if cur is None and self.plugin.current_party() is not None:
                nxt = await self.specialist.get_nearest_massage()
                if nxt is not None:
                    possible_slots = min(floor((nxt.start-now_msk()).total_seconds()/60/SLOT_LENGTH), 6)
                else:
                    possible_slots = 6
                keyboard.append([InlineKeyboardButton(self.l("massage-specialist-instantbook"), callback_data=f"{self.plugin.name}|start")])
                def instant_book(length):
                    return InlineKeyboardButton(
                        self.l("massage-specialist-instantbook-button",
                            icon=length_icon(length),
                            minutes=min_from_length(length),
                            price=price_from_length(length)
                        ), callback_data=f"{self.plugin.name}|instant|{length}")
                keyboard.extend(split_list([instant_book(i+1) for i in range(0, possible_slots)], 3))
            keyboard.append([InlineKeyboardButton(self.l("massage-specialist-notifications-button"),
                                                   callback_data=f"{self.plugin.name}|notifications")])
        elif self.update.user in self.plugin.config.food.admins:
            keyboard.append([
                InlineKeyboardButton(
                    self.l("massage-specialist-timetable-button"),
                    web_app=WebAppInfo(full_link(self.plugin.base_app, f"/massage_timetable"))
                ),
            ])
        massages = await self.get_massages()
        massage_buttons = [
            InlineKeyboardButton(
                self.l("dow-short", dow=massage.party.start.weekday()) + \
                "-" + self.l("dow-short", dow=massage.party.end.weekday()) + \
                " " + massage.start_str + \
                f" {massage.duration}'" + \
                " " + massage.specialist.icon,
                callback_data=f"{self.plugin.name}|ed|{massage.id}"
            ) for massage in massages if massage.end > now_msk()
        ]
        if len(massage_buttons) > 0:
            keyboard.append([InlineKeyboardButton(self.l("massage-your-boookings"), callback_data=f"{self.plugin.name}|start")])
            keyboard.extend(split_list(massage_buttons, MY_MASSAGES_SPLIT))
        keyboard.append([InlineKeyboardButton(self.l("massage-create-button"), callback_data=f"{self.plugin.name}|create")])
        keyboard.append([InlineKeyboardButton(self.l("massage-exit-button"), callback_data=f"{self.plugin.name}|exit")])

        msg = self.l("massage-start-message")
        specialists = await self.plugin.get_specialists()
        msg += "\n   "+ "\n   ".join([s.about(self.update.language_code) for s in specialists])
        await self.update.edit_or_reply(
            msg,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )

class MassagePlugin(BasePlugin):
    name = "massage"
    user_db: AgnosticCollection
    massage_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.finalizer_lock = Lock()
        self.admins = self.config.telegram.admins
        self.user_db = base_app.users_collection
        self.massage_db = base_app.mongodb[self.config.mongo_db.massage_collection]
        self.parties_by_day = dict[int,Party]()
        self.parties_begin = self.config.parties[0].start
        self.parties_end = self.config.parties[0].end
        for party in self.config.parties:
            self.parties_by_day[party.start.day] = party
            if self.parties_begin > party.start:
                self.parties_begin = party.start
            if self.parties_end < party.end:
                self.parties_end = party.end
        self.parties_begin -= EARLY_COMER_TOLERANCE
        self.parties_end += EARLY_COMER_TOLERANCE
        self._cmd_checker = CommandHandler(self.name, self.handle_start)
        self._cq_checker = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
        create_task(self._notifier())
        base_app.massages = self

    async def _notifier(self):
        from asyncio import sleep
        logger.info(f"starting MassagePlugin.notifier loop ")
        while True:
            await sleep(NOTIFICATOR_LOOP)
            try:
                now = now_msk()
                logger.debug(f"notificator tick {now}")
                async for doc in self.massage_db.find({
                    "deleted": {"$exists":False},
                    "start": {
                        "$lt": now+self.config.massages.notify_client_prior_long,
                        "$gte": self.parties_begin,
                    },
                    "pass_key": PASS_KEY,
                    "client_notified_prior_long": {"$exists":False},
                }):
                    rec = MassageRecord(doc, self)
                    create_task(rec.notify_client_prior_long())
                async for doc in self.massage_db.find({
                    "deleted": {"$exists":False},
                    "start": {
                        "$lt": now+self.config.massages.notify_client_prior,
                        "$gte": self.parties_begin
                    },
                    "pass_key": PASS_KEY,
                    "client_notified": {"$exists":False},
                }):
                    rec = MassageRecord(doc, self)
                    create_task(rec.notify_client())
                async for doc in self.massage_db.find({
                    "deleted": {"$exists":False},
                    "start": {
                        "$lt": now+timedelta(minutes=SLOT_BUFFER),
                        "$gte": self.parties_begin
                    },
                    "pass_key": PASS_KEY,
                    "specialist_notified": {"$exists":False},
                }):
                    rec = MassageRecord(doc, self)
                    create_task(rec.notify_specialist())
                async for doc in self.massage_db.find({
                    "deleted": {"$exists":False},
                    "start": {
                        "$gte": now,
                    },
                    "pass_key": PASS_KEY,
                    "notify": {"$eq":True},
                }):
                    rec = MassageRecord(doc, self)
                    create_task(rec.notify_client_addidtional())
            except Exception as e:
                logger.error("Exception in notifier: %s", e, exc_info=1)

    def current_party(self):
        now = now_msk()
        for party in self.config.parties:
            if party.start - EARLY_COMER_TOLERANCE < now < party.end + EARLY_COMER_TOLERANCE:
                return party
        return None

    def nearest_party(self):
        now = now_msk()
        for party in self.config.parties:
            if now < party.end + EARLY_COMER_TOLERANCE:
                return party
        return None
    
    def current_slot(self):
        party = self.current_party()
        if party is None:
            return None
        return floor((now_msk() - party.start).total_seconds()/60/SLOT_LENGTH)
    
    def slot_time(self, day, index) -> datetime:
        return self.parties_by_day[day].start + (SLOT_DURATION * index)

    async def available_slots(self, day: int, length: int = 1) -> dict[int,set[int]]:
        logger.debug(f"length {length}")
        if self.parties_by_day[day].end + EARLY_COMER_TOLERANCE < now_msk():
            return dict[int,set[int]]()
        available, _ = await self.all_slots(day)
        if length <= 1:
            return available
        available_filtered = dict[int,set[int]]()
        for slot in available:
            slot_set = set[int]()
            for specialist_id in available[slot]:
                for i in range(1, length):
                    next_slot = slot + i
                    if not next_slot in available or not specialist_id in available[next_slot]:
                        break
                else:
                    slot_set.add(specialist_id)
            if len(slot_set) != 0:
                available_filtered[slot] = slot_set
        logger.info(f"available slots for {day} of length {length}: {available_filtered}")
        return available_filtered
    
    async def all_slots(self, day: int) -> tuple[dict[int,set[int]], dict[int,set[int]]]:
        all_available = dict[int,set[int]]()
        all_occupied = dict[int,set[int]]()
        tables = set[int]()
        for specialist in await self.get_specialists():
            if specialist.table_required:
                tables.add(specialist.id)
            occupied, available = await specialist.get_both_slots(day)
            logger.info(f"slots for {day} for {specialist.id}: {available}, {occupied}")
            for slot in available:
                if not slot in all_available:
                    all_available[slot] = set[int]()
                all_available[slot].add(specialist.id)
            for slot in occupied:
                if not slot in all_occupied:
                    all_occupied[slot] = set[int]()
                all_occupied[slot].add(specialist.id)
        
        if self.parties_by_day[day].massage_tables == 1:
            all_available_filtered = dict[int,set[int]]()
            for slot_id in all_available:
                if slot_id in all_occupied:
                    if len(all_occupied[slot_id].intersection(tables)) > 0:
                        all_available[slot_id] = all_available[slot_id].difference(tables)
                if len(all_available[slot_id]) != 0:
                    all_available_filtered[slot_id] = all_available[slot_id]
            all_available = all_available_filtered
        elif self.parties_by_day[day].massage_tables < len(tables):
            raise Exception("unimplemented")
        logger.info(f"slots for {day}: {all_available}, {all_occupied}")
        return all_available, all_occupied
    
    @async_cache(ttl=60)
    async def get_specialists(self) -> List[Specialist]:
        ret = []
        async for user in self.user_db.find({
            "bot_id": self.bot.id,
            "massage_specialist": {"$exists": True}
        }):
            ret.append(Specialist(self, user["user_id"], user))
        return ret

    @async_cache(ttl=60)
    async def get_specialist(self, id: int) -> Specialist:
        specialist = await self.user_db.find_one({
            "user_id": id,
            "bot_id": self.bot.id,
        })
        if specialist is None or not "massage_specialist" in specialist:
            return None
        return Specialist(self, id, specialist)

    async def get_all_massages_for_web(self):
        massages = list[MassageRecord]()
        tasks = []
        speaialists: List[Specialist] = await self.get_specialists()
        async for massage in self.massage_db.find({
            "deleted": {"$exists":False},
            "pass_key": PASS_KEY,
            "start": {
                "$gte": self.parties_begin,
                "$lte": self.parties_end
            },
        }):
            rec = MassageRecord(massage, self)
            massages.append(rec)
            tasks.append(create_task(rec.init()))
            await gather(*tasks)
        ret = {
            "early_comer_tolerance": EARLY_COMER_TOLERANCE.total_seconds(),
            "parties": [{
                "start": party.start.strftime("%Y-%m-%d %H:%M:%S"),
                "end": party.end.strftime("%Y-%m-%d %H:%M:%S"),
            } for party in self.config.parties if not party.is_open],
            "massages":[{
                "start": massage.start.strftime("%Y-%m-%d %H:%M:%S"),
                "end": massage.end.strftime("%Y-%m-%d %H:%M:%S"),
                "duration": massage.duration,
                "price": massage.price,
                "user": {
                    "name": client_user_name(massage.client),
                    "id": massage.client["user_id"]
                },
                "specialist": int(massage.specialist.id)
            } for massage in massages],
            "specialists": {
                int(speaialist.id): {
                    "name": speaialist.name,
                    "icon": speaialist.icon,
                    "work_hours": [
                        {
                            "start": work_span["start"].strftime("%Y-%m-%d %H:%M:%S"),
                            "end": work_span["end"].strftime("%Y-%m-%d %H:%M:%S"),
                        }
                        for work_span in speaialist.specialist["work_hours"]
                    ]
                }
                for speaialist in speaialists
            }
        }
        return ret

    def test_message(self, message: Update, state, web_app_data):
        if self._cmd_checker.check_update(message):
            return PRIORITY_BASIC, self.handle_start
        return PRIORITY_NOT_ACCEPTING, None
    
    def test_callback_query(self, query: Update, state):
        if self._cq_checker.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None
    
    async def create_user_massage(self, update) -> UserMassages:
        ret = UserMassages(self, update)
        await ret.get_user()
        return ret

    async def handle_start(self, update: TGState):
        u = await self.create_user_massage(update)
        return await u.handle_start()
    async def handle_callback_query(self, updater):
        u = await self.create_user_massage(updater)
        return await u.handle_callback_query()
