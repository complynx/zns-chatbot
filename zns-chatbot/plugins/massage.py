from typing import List
from motor.core import AgnosticCollection
from ..tg_state import TGState
from ..telegram_links import client_user_link_html, client_user_name
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo, ReplyKeyboardMarkup, User, ReplyKeyboardRemove, Message
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler
from telegram.constants import ParseMode
import logging
from bson.objectid import ObjectId
from ..config import Party

logger = logging.getLogger(__name__)

def price_from_length(length:int=1)->int:
    price = 500*length
    if length<3:
        price += 100 * (3 - length)
    return price

def min_from_length(length:int =1)->int:
    return (length*20)-5

def length_icon(length:int =1)->str:
    return ("🕐🕑🕒🕓🕔🕕🕖🕗🕘🕙🕚")[length]

class Specialist:
    user = None
    def __init__(self, base: 'MassagePlugin', masseur_id: int, user = None):
        self.base = base
        self.id = masseur_id
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

    def l(self, s, **kwargs):
        return self.base.base_app.localization(s, args=kwargs, locale=self.language_code)

    async def init(self):
        user = await self.base.user_db.find_one({
            "user_id": self.id,
            "bot_id": self.base.bot.id,
        })
        self._set_user(user)
    
    def about(self, lc="en"):
        about = self.specialist["about"]
        if "about_" + lc in self.specialist:
            about = self.specialist["about_" + lc]
        return self.icon + " " + client_user_link_html(self.user) + " " + about

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
    
    async def init(self):
        if not "_id" in self.record:
            rec = await self.plugin.massage_db.insert_one(self.record)
            self.record["_id"] = rec.inserted_id
        if self.client is None:
            client = await self.plugin.user_db.find_one({
                "user_id": self.record["user_id"],
            })
            self._set_client(client)
        if self.specialist is None and "specialist" in self.record:
            specialist = await self.plugin.user_db.find_one({
                "user_id": self.record["specialist"],
            })
            if specialist is not None:
                self._set_specialist(Specialist(self.plugin, specialist["user_id"], specialist))
    
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
        if "day" in self.record:
            return self.plugin.parties_by_day[self.record["day"]]
        return None

    @property
    def is_finished(self):
        return "start" in self.record
    
    @property
    def start(self):
        if "start" in self.record:
            return self.record["start"]
        return None
    
    @property
    def day(self):
        if "party" in self.record:
            return self.record["party"]
        return None
    
    @property
    def id(self):
        return str(self.record["_id"])
    
    @property
    def client_repr(self):
        if not self.is_finished:
            return self.cl("massage-unfinished")
        else:
            return self.cl("dow-short", dow=self.party.start.weekday()) + \
                "-" + self.cl("dow-short", dow=self.party.end.weekday()) + \
                " " + self.start.strftime('%H:%M') + \
                " " + self.specialist.icon

def split_list(lst: list, chunk: int):
    result = []
    for i in range(0, len(lst), chunk):
        sublist = lst[i:i+chunk]
        result.append(sublist)
    return result

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
            "start": {"$exists":True},
        }):
            rec = MassageRecord(massage, self.plugin, self.user)
            await rec.init()
            ret.append(rec)
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
            "user_id": self.update.user
        }, self.plugin, user)
        await rec.init()
        await self.edit_massage(rec)
    
    async def handle_cq_ed(self, massage_id, choice="0"):
        rec = await self.get_massage(massage_id)
        await self.edit_massage(rec, int(choice))
    
    async def handle_cq_back(self, massage_id):
        rec = await self.get_massage(massage_id)
        await self.edit_massage(rec, -1)
    
    # Cancel edit
    async def handle_cq_xed(self, massage_id):
        await self.plugin.massage_db.delete_one({
            "_id": ObjectId(massage_id)
        })
        await self.handle_cq_exit()
    
    async def edit_massage(self, massage: MassageRecord, choice = 0):
        if not "day" in massage.record:
            logger.info(f"massage edit day {massage.id}, choice {choice}")
            if choice == 0:
                return await self.render_choose_day(massage)
            if choice < 0:
                await self.plugin.massage_db.delete_one({
                    "_id": ObjectId(massage.id)
                })
                return await self.handle_start()
            massage.record["day"] = choice
            await massage.save()
            return await self.render_choose_length(massage)
        if not "length" in massage.record:
            logger.info(f"massage edit length {massage.id}, choice {choice}")
            if choice == 0:
                return await self.render_choose_length(massage)
            if choice < 0:
                del massage.record["day"]
                await massage.save()
                return await self.render_choose_day(massage)
            massage.record["length"] = choice
            await massage.save()
            return await self.render_select_specialists(massage)
        if not "specialists_choices" in massage.record and "specialists_selected" in massage.record:
            del massage.record["specialists_selected"]
            await massage.save()
        if not "specialists_selected" in massage.record:
            logger.info(f"massage edit select specialists {massage.id}, choice {choice}")
            if choice == 0:
                return await self.render_select_specialists(massage)
            if choice < 0:
                del massage.record["specialists_choices"]
                del massage.record["length"]
                await massage.save()
                return await self.render_choose_length(massage)
            if choice >= 1000:
                massage.record["specialists_selected"] = True
                await massage.save()
                return await self.render_select_slot(massage)
            if "specialists_choices" in massage.record:
                massage.record["specialists_choices"][choice-1]["selected"] = not massage.record["specialists_choices"][choice-1]["selected"]
                await massage.save()
            return await self.render_select_specialists(massage)
        if not "start" in massage.record:
            logger.info(f"massage edit select slot {massage.id}, choice {choice}")
            if choice == 0:
                return await self.render_select_slot(massage)
    
    async def render_select_slot(self, massage: MassageRecord):
        pass
    
    async def render_choose_day(self, massage: MassageRecord):
        buttons = [
            InlineKeyboardButton(
                self.l("massage-edit-choose-party-button",
                    party=self.l("dow-short", dow=party.start.weekday()) + \
                        "-" + self.l("dow-short", dow=party.end.weekday())
                ),
                callback_data=f"{self.plugin.name}|ed|{massage.id}|{party.start.day}"
            ) for party in self.plugin.config.parties if not party.is_open
        ]
        keyboard = split_list(buttons, 2)
        keyboard.append([
                InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|back|{massage.id}"),
                InlineKeyboardButton(self.l("massage-edit-cancel-button"), callback_data=f"{self.plugin.name}|xed|{massage.id}")
        ])
        await self.update.edit_or_reply(
            self.l("massage-edit-choose-day"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(keyboard),
            disable_web_page_preview=True,
        )

    async def render_select_specialists(self, massage: MassageRecord):
        specialists = await self.plugin.get_specialists()
        total_specialists = len(specialists)
        specialists = [s for s in specialists if s.min_duration <= massage.record["length"] <= s.max_duration]
        specialists.sort(key=lambda s: s.id)
        specialists_dict = {s.id:s for s in specialists}
        if not "specialists_choices" in massage.record:
            massage.record["specialists_choices"] = [{"id":s.id,"selected":True} for s in specialists]
            await massage.save()
        
        msg = self.l(
            "massage-edit-select-specialists",
            party=self.l("dow-long", dow=massage.party.start.weekday()) + \
                "-" + self.l("dow-long", dow=massage.party.end.weekday()),
            duration=min_from_length(massage.record["length"]),
            price=price_from_length(massage.record["length"]),
            durationicon=length_icon(massage.record["length"]),
            specialists="   " + "\n   ".join([s.about(self.update.language_code) for s in specialists]),
            filtered=self.l("massage-edit-select-specialists-filtered") if \
                len(massage.record["specialists_choices"]) != total_specialists else ""
        )
        buttons = []
        for i in range(0, len(massage.record["specialists_choices"])):
            choice = massage.record["specialists_choices"][i]
            specialist = specialists_dict[choice["id"]]
            text = ("✅" if choice["selected"] else "❌") + " " + specialist.name
            buttons.append(InlineKeyboardButton(
                text=text,
                callback_data=f"{self.plugin.name}|ed|{massage.id}|{i+1}"
            ))
        keyboard = split_list(buttons, 2)
        keyboard.append([
            InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|back|{massage.id}"),
            InlineKeyboardButton(self.l("massage-edit-cancel-button"), callback_data=f"{self.plugin.name}|xed|{massage.id}"),
            InlineKeyboardButton(self.l("massage-edit-next-button"), callback_data=f"{self.plugin.name}|ed|{massage.id}|1000")
        ])
        
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
            [len_btn(3),len_btn(4)],
            [len_btn(5),len_btn(6)],
            [
                InlineKeyboardButton(self.l("massage-edit-back-button"), callback_data=f"{self.plugin.name}|back|{massage.id}"),
                InlineKeyboardButton(self.l("massage-edit-cancel-button"), callback_data=f"{self.plugin.name}|xed|{massage.id}")
            ]
        ]
        await self.update.edit_or_reply(
            self.l(
                "massage-edit-choose-length",
                party=self.l("dow-long", dow=massage.party.start.weekday()) + \
                    "-" + self.l("dow-long", dow=massage.party.end.weekday()),
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

    async def handle_start(self):
        keyboard = []
        if self.specialist is not None:
            keyboard.append([
                InlineKeyboardButton(self.l("massage-specialist-timetable-button"), callback_data=f"{self.plugin.name}|timetable"),
                InlineKeyboardButton(self.l("massage-specialist-clientlist-button"), callback_data=f"{self.plugin.name}|clientlist"),
            ])
            keyboard.append([InlineKeyboardButton(self.l("massage-specialist-instantbook"), callback_data=f"{self.plugin.name}|start")])
            def instant_book(length):
                return InlineKeyboardButton(
                    self.l("massage-specialist-instantbook-button",
                           icon=length_icon(length),
                           minutes=min_from_length(length),
                           price=price_from_length(length)
                    ), callback_data=f"{self.plugin.name}|instant|{length}")
            keyboard.extend([
                [instant_book(1),instant_book(2),instant_book(3)],
                [instant_book(4),instant_book(5),instant_book(6)]
            ])
            keyboard.append([InlineKeyboardButton(self.l("massage-specialist-notifications-button"),
                                                   callback_data=f"{self.plugin.name}|notifications")])
        massages = await self.get_massages()
        massage_buttons = [
            InlineKeyboardButton(massage.client_repr, callback_data=f"{self.plugin.name}|ed|{massage.id}")
              for massage in massages
        ]
        if len(massage_buttons) > 0:
            keyboard.append([InlineKeyboardButton(self.l("massage-your-boookings"), callback_data=f"{self.plugin.name}|start")])
            keyboard.extend(split_list(massage_buttons, 3))
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
        self.admins = self.config.telegram.admins
        self.user_db = base_app.users_collection
        self.massage_db = base_app.mongodb[self.config.mongo_db.massage_collection]
        self.parties_by_day = {party.start.day:party for party in self.config.parties}
        self._cmd_checker = CommandHandler(self.name, self.handle_start)
        self._cq_checker = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
    
    async def get_specialists(self) -> List[Specialist]:
        ret = []
        async for user in self.user_db.find({
            "bot_id": self.bot.id,
            "massage_specialist": {"$exists": True}
        }):
            ret.append(Specialist(self, user["user_id"], user))
        return ret

    def test_message(self, message: Update, state, web_app_data):
        if self._cmd_checker.check_update(message) and message.effective_user.id in self.admins:
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