
from datetime import timedelta
import logging
from bson import ObjectId
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler, MessageHandler, filters
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, ReplyKeyboardMarkup, Update, ReplyKeyboardRemove
from ..tg_state import TGState
from motor.core import AgnosticCollection
from telegram.constants import ParseMode
from .massage import MSK_OFFSET, now_msk, split_list
from asyncio import Lock, create_task, sleep
from ..telegram_links import client_user_link_html

logger = logging.getLogger(__name__)

CANCEL_CHR = chr(0xE007F) # Tag cancel
PASS_KEY = "pass_2025_1"
CURRENT_PRICE=7900
TIMEOUT_PROCESSOR_TICK = 3600
PAYMENT_TIMEOUT=timedelta(days=8)
PAYMENT_TIMEOUT_NOTIFY=timedelta(days=7)

class PassUpdate:
    base: 'Passes'
    tgUpdate: Update
    bot: int
    update: TGState

    def __init__(self, base, update: TGState) -> None:
        self.base = base
        self.update = update
        self.l = update.l
        self.tgUpdate = update.update
        self.bot = self.update.bot.id
        self.user = None
    
    async def handle_callback_query(self):
        q = self.update.callback_query
        await q.answer()
        logger.info(f"Received callback_query from {self.update.user}, data: {q.data}")
        data = q.data.split("|")
        fn = "handle_cq_" + data[1]
        logger.debug(f"fn: {fn}")
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            logger.debug(f"fn: {attr}")
            if callable(attr):
                return await attr(*data[2:])
        logger.error(f"unknown callback {data[1]}: {data[2:]}")
    
    async def show_pass_edit(self, user, u_pass, text_key: str|None = None):
        keys = dict(u_pass)
        keys["name"] = user["legal_name"]
        if text_key is None:
            text_key = f"passes-pass-edit-{u_pass['state']}"

        buttons = [
            InlineKeyboardButton(
                self.l("passes-button-change-name"),
                callback_data=f"{self.base.name}|name"
            ),
        ]
        if u_pass["state"] == "assigned":
            buttons.append(InlineKeyboardButton(
                self.l("passes-button-pay"),
                callback_data=f"{self.base.name}|pay"
            ))
        if u_pass["state"] != "payed":
            buttons.append(InlineKeyboardButton(
                self.l("passes-button-cancel"),
                callback_data=f"{self.base.name}|cancel"
            ))
        buttons.append(InlineKeyboardButton(
            self.l("passes-button-exit"),
            callback_data=f"{self.base.name}|exit"
        ))

        await self.update.edit_or_reply(
            self.l(
                text_key,
                **keys,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(split_list(buttons, 2)),
        )

    async def handle_cq_pass_exit(self):
        await self.update.edit_or_reply(
            self.l("passes-pass-create-cancel"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_cq_exit(self):
        await self.update.edit_or_reply(
            self.l("passes-pass-exit"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )
    
    async def handle_cq_cancel(self):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": {"$ne": "payed"},
        }, {
            "$unset": {
                PASS_KEY: "",
            }
        })
        if result.modified_count > 0:
            await self.update.edit_or_reply(
                self.l("passes-pass-cancelled"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
            await self.base.recalculate_queues()
        else:
            await self.update.edit_or_reply(
                self.l("passes-pass-cancel-failed"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        
    async def handle_cq_start(self):
        user = await self.get_user()
        if PASS_KEY in user:
            return await self.show_pass_edit(user, user[PASS_KEY])
        else:
            return await self.new_pass()
    
    async def handle_cq_pay(self):
        user = await self.get_user()
        if PASS_KEY not in user or user[PASS_KEY]["state"] != "assigned":
            return await self.handle_cq_exit()
        u_pass = user[PASS_KEY]
        keys = dict(u_pass)
        keys["name"] = user["legal_name"]
        await self.update.edit_or_reply(
            self.l(
                "passes-payment-request-callback-message",
                **keys,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )
        await self.update.reply(
            self.l(
                "passes-payment-request-waiting-message",
                **keys,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup([[CANCEL_CHR+self.l("cancel-command")]], resize_keyboard=True),
        )
        await self.update.require_anything(self.base.name, "handle_payment_proof_input", "", "handle_payment_proof_timeout")
    
    async def handle_cq_adm_acc(self, key: str, user_id_s: str):
        if key != PASS_KEY or self.update.user != self.base.config.passes.payment_admin:
            return
        user_id = int(user_id_s)
        result = await self.base.user_db.update_one({
            "user_id": user_id,
            "bot_id": self.bot,
            PASS_KEY+".state": "payed",
        }, {
            "$set": {
                PASS_KEY+".proof_accepted": now_msk(),
            }
        })
        if result.modified_count <= 0:
            return
        user = await self.base.user_db.find_one({
            "user_id": user_id,
            "bot_id": self.bot,
        })
        keys = dict(user[PASS_KEY])
        keys["name"] = user["legal_name"]
        ls = 'en'
        if "language_code" in user:
            ls=user["language_code"]
        def l(s, **kwargs):  # noqa: E743
            return self.base.base_app.localization(s, args=kwargs, locale=ls)
        await self.update.reply(
            l(
                "passes-payment-proof-accepted",
                **keys,
            ),
            user_id,
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "passes-adm-payment-proof-accepted",
                link=client_user_link_html(user),
                **keys,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )

    async def handle_cq_adm_rej(self, key: str, user_id_s: str):
        if key != PASS_KEY or self.update.user != self.base.config.passes.payment_admin:
            return
        user_id = int(user_id_s)
        result = await self.base.user_db.update_one({
            "user_id": user_id,
            "bot_id": self.bot,
            PASS_KEY+".state": "payed",
        }, {
            "$set": {
                PASS_KEY+".state": "assigned",
                PASS_KEY+".proof_rejected": now_msk(),
            }
        })
        if result.modified_count <= 0:
            return
        user = await self.base.user_db.find_one({
            "user_id": user_id,
            "bot_id": self.bot,
        })
        keys = dict(user[PASS_KEY])
        keys["name"] = user["legal_name"]
        ls = 'en'
        if "language_code" in user:
            ls=user["language_code"]
        def l(s, **kwargs):  # noqa: E743
            return self.base.base_app.localization(s, args=kwargs, locale=ls)
        await self.update.reply(
            l(
                "passes-payment-proof-rejected",
                **keys,
            ),
            user_id,
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "passes-adm-payment-proof-rejected",
                link=client_user_link_html(user),
                **keys,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )

    async def handle_payment_proof_timeout(self, data):
        return await self.update.reply(
            self.l("passes-payment-proof-timeout"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardRemove(),
        )

    async def handle_legal_name_timeout(self, data):
        return await self.update.reply(
            self.l("passes-name-timeout"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardRemove(),
        )

    async def handle_payment_proof_input(self, data):
        if filters.TEXT.check_update(self.tgUpdate) and self.update.message.text[0] == CANCEL_CHR:
            return await self.update.reply(
                self.l("passes-payment-proof-cancelled"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        if not (filters.PHOTO | filters.Document.ALL).check_update(self.tgUpdate):
            return await self.update.reply(
                self.l(
                    "passes-payment-proof-wrong-data",
                ),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        if filters.PHOTO.check_update(self.tgUpdate):
            doc = self.update.message.photo[-1]
            file_ext = ".jpg"
        else:
            doc = self.update.message.document
            import mimetypes
            file_ext = mimetypes.guess_extension(doc.mime_type)
        user = await self.get_user()
        if PASS_KEY not in user or user[PASS_KEY]["state"] != "assigned":
            return await self.handle_cq_exit()
        u_pass = user[PASS_KEY]
        keys = dict(u_pass)
        keys["name"] = user["legal_name"]
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "assigned",
        }, {
            "$set": {
                PASS_KEY+".state": "payed",
                PASS_KEY+".proof_received": now_msk(),
                PASS_KEY+".proof_file": f"{doc.file_id}{file_ext}",
                PASS_KEY+".proof_admin": self.base.config.passes.payment_admin,
            }
        })
        if result.modified_count <= 0:
            return
        if self.base.config.passes.payment_admin>0:
            admin = await self.base.user_db.find_one({
                "user_id": self.base.config.passes.payment_admin,
                "bot_id": self.bot,
            })
            lc = "ru"
            if admin is not None and "language_code" in admin:
                lc = admin["language_code"]
            def l(s, **kwargs):  # noqa: E743
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.update.forward_message(self.base.config.passes.payment_admin)
            await self.update.reply(
                l(
                    "passes-adm-payment-proof-received",
                    link=client_user_link_html(user),
                    **keys,
                ),
                chat_id=self.base.config.passes.payment_admin,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton(l("passes-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{PASS_KEY}|{self.update.user}"),
                        InlineKeyboardButton(l("passes-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{PASS_KEY}|{self.update.user}"),
                    ]
                ])
            )
        await self.update.reply(
            self.l("passes-payment-proof-forwarded"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardRemove()
        )

    async def new_pass(self):
        if now_msk() < self.base.config.passes.sell_start:
            return await self.update.edit_or_reply(self.l("passes-sell-not-started"), reply_markup=InlineKeyboardMarkup([]), parse_mode=ParseMode.HTML)
        await self.update.edit_or_reply(self.l("passes-pass-create-start-message"), reply_markup=InlineKeyboardMarkup([]), parse_mode=ParseMode.HTML)
        return await self.handle_name_request("pass")

    async def handle_start(self):
        logger.debug(f"starting passes for: {self.update.user}")
        return await self.handle_cq_start()
    
    async def handle_cq_name(self):
        logger.debug(f"command to change legal name for: {self.update.user}")
        return await self.handle_name_request("cmd")
    
    async def handle_name_request(self, data):
        user = await self.get_user()
        if "legal_name_frozen" in user:
            pass
        btns = []
        if "legal_name" in user:
            btns.append([user["legal_name"]])
        btns.append([CANCEL_CHR+self.l("cancel-command")])
        markup = ReplyKeyboardMarkup(
            btns,
            resize_keyboard=True
        )
        await self.update.reply(self.l("passes-legal-name-request-message"), reply_markup=markup, parse_mode=ParseMode.HTML)
        await self.update.require_input(self.base.name, "handle_legal_name_input", data, "handle_legal_name_timeout")
    
    async def handle_legal_name_input(self, data):
        user = await self.get_user()
        name = self.update.message.text
        if name[0] == CANCEL_CHR:
            return await self.update.reply(
                self.l("passes-pass-create-cancel"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        old_name = user["legal_name"] if "legal_name" in user else ""
        logger.debug(f"changing legal name for: {self.update.user} from {old_name} to {name}")
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot
        }, {
            "$set": {"legal_name": name}
        })
        await self.update.reply(
            self.l(
                "passes-legal-name-changed-message",
                name=name,
            ),
            reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML,
        )
        if data == "pass":
            await self.update.reply(
                self.l("passes-pass-role-select"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[
                    InlineKeyboardButton(
                        self.l("passes-role-button-leader"),
                        callback_data=f"{self.base.name}|pass_role|l"
                    ),
                    InlineKeyboardButton(
                        self.l("passes-role-button-follower"),
                        callback_data=f"{self.base.name}|pass_role|f"
                    ),
                ],[
                    InlineKeyboardButton(
                        self.l("passes-role-button-cancel"),
                        callback_data=f"{self.base.name}|pass_exit"
                    ),
                ]]),
            )

    async def handle_cq_pass_role(self, role: str):
        new_role = "leader" if role.startswith("l") else "follower"
        u_pass = {
            "role": new_role,
            "state": "waitlist",
            "date_created": now_msk(),
        }
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot
        }, {
            "$set": {
                PASS_KEY: u_pass,
                "role": new_role,
            }
        })
        user = await self.get_user()
        keys = dict(u_pass)
        keys["name"] = user["legal_name"]
        await self.update.edit_or_reply(
            self.l(
                "passes-pass-role-saved",
                role=new_role,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )
        if self.base.config.passes.thread_channel != "":
            try:
                ch = self.base.config.passes.thread_channel
                if isinstance(ch, str):
                    ch = "@" + ch
                logger.debug(f"chat id: {ch}, type {type(ch)}")
                await self.base.bot.send_message(
                    chat_id=ch,
                    message_thread_id=self.base.config.passes.thread_id,
                    text=self.base.base_app.localization(
                        "passes-announce-user-registered",
                        args=keys,
                        locale=self.base.config.passes.thread_locale,
                    ),
                    parse_mode=ParseMode.HTML,
                )
            except Exception as e:
                logger.error(f"Exception while sending message to chat: {e}", exc_info=1)
        
        await self.base.recalculate_queues()

    async def handle_cq_role(self, role: str):
        new_role = "leader" if role.startswith("l") else "follower"
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot
        }, {
            "$set": {"role": new_role}
        })
        
        await self.update.edit_or_reply(
            self.l(
                "passes-role-saved",
                role=new_role,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )
    async def handle_cq_role_exit(self):
        await self.update.edit_or_reply(
            self.l("passes-role-exit"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def get_user(self):
        if self.user is None:
            self.user = await self.update.get_user()
        return self.user

    async def assign_pass(self):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "waitlist",
        }, {
            "$set": {
                PASS_KEY+".state": "assigned",
                PASS_KEY+".price": CURRENT_PRICE,
                PASS_KEY+".date_assignment": now_msk(),
            }
        })
        if result.modified_count > 0:
            user = await self.update.load_user()
            logger.info(f"assigned pass to {self.update.user}, role {user[PASS_KEY]['role']}, name {user['legal_name']}")
            await self.show_pass_edit(user, user[PASS_KEY], "passes-pass-assigned")
    
    async def handle_role_cmd(self):
        logger.debug(f"setting role for: {self.update.user}")
        user = await self.get_user()
        text = self.l("passes-role-select")
        if "role" in user:
            text = self.l(
                "passes-role-change-select",
                role=user["role"],
            )
        
        await self.update.edit_or_reply(
            text,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[
                InlineKeyboardButton(
                    self.l("passes-role-button-leader"),
                    callback_data=f"{self.base.name}|role|l"
                ),
                InlineKeyboardButton(
                    self.l("passes-role-button-follower"),
                    callback_data=f"{self.base.name}|role|f"
                ),
            ],[
                InlineKeyboardButton(
                    self.l("passes-role-button-cancel"),
                    callback_data=f"{self.base.name}|role_exit"
                ),
            ]]),
        )

    async def notify_no_more_passes(self):
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "waitlist",
        }, {
            "$set": {
                PASS_KEY+".no_more_passes_notification_sent": now_msk(),
            }
        })
        user = await self.get_user()
        await self.show_pass_edit(user, user[PASS_KEY], "passes-added-to-waitlist")

    async def notify_deadline_close(self):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "assigned",
            PASS_KEY + ".notified_deadline_close": {"$exists": False},
        }, {
            "$set": {
                PASS_KEY + ".notified_deadline_close": now_msk(),
            }
        })
        if result.modified_count > 0:
            await self.update.reply(
                self.l("passes-payment-deadline-close"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
    
    async def cancel_due_deadline(self):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "assigned",
        }, {
            "$unset": {
                PASS_KEY: "",
            }
        })
        if result.modified_count > 0:
            await self.update.reply(
                self.l("passes-payment-deadline-exceeded"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )

class Passes(BasePlugin):
    name = "passes"

    def __init__(self, base_app):
        super().__init__(base_app)
        self.base_app.passes = self
        self.user_db: AgnosticCollection = base_app.users_collection
        self._checker = CommandHandler(self.name, self.handle_start)
        self._name_checker = CommandHandler("legal_name", self.handle_name_cmd)
        self._role_checker = CommandHandler("role", self.handle_role_cmd)
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
        self._queue_lock = Lock()
        create_task(self._timeout_processor())
    
    async def _timeout_processor(self) -> None:
        while True:
            try:
                await sleep(TIMEOUT_PROCESSOR_TICK)
                async for user in self.user_db.find({
                        "bot_id": self.base_app.bot.bot.id,
                        PASS_KEY + ".state": "assigned",
                        PASS_KEY + ".date_assignment": {
                            "$lt": now_msk() - PAYMENT_TIMEOUT,
                        },
                    }):
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.cancel_due_deadline()
                async for user in self.user_db.find({
                        "bot_id": self.base_app.bot.bot.id,
                        PASS_KEY + ".state": "assigned",
                        PASS_KEY + ".date_assignment": {
                            "$lt": now_msk() - PAYMENT_TIMEOUT_NOTIFY,
                        },
                        PASS_KEY + ".notified_deadline_close": {"$exists": False},
                    }):
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.notify_deadline_close()
            except Exception as e:
                logger.error("Exception in Passes._timeout_processor %s", e, exc_info=1)
    
    async def recalculate_queues(self) -> None:
        await self.recalculate_queues_role("leader")
        await self.recalculate_queues_role("follower")

    async def recalculate_queues_role(self, role: str) -> None:
        async with self._queue_lock:
            have_passes = True
            while have_passes:
                assigned_passes = await self.user_db.count_documents({
                    "bot_id": self.base_app.bot.bot.id,
                    PASS_KEY + ".state": {"$ne": "waitlist"},
                    PASS_KEY + ".role": role,
                })
                have_passes = assigned_passes < self.config.passes.amount_cap_per_role
                if not have_passes:
                    logger.info(f"no more passes available for role {role}")
                    break
                available = self.config.passes.amount_cap_per_role - assigned_passes
                selected = await self.user_db.aggregate([
                    {"$match": {
                        "bot_id": self.base_app.bot.bot.id,
                        PASS_KEY + ".state": "waitlist",
                        PASS_KEY + ".role": role,
                    }},
                    {"$sort": {PASS_KEY+".date_created": 1, "user_id": 1}},
                    {"$limit": available},
                ]).to_list(available)
                if len(selected) < 1:
                    logger.info(f"{available} available passes but no candidates for role {role}")
                    break
                for user in selected:
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.assign_pass()
            have_unnotified = True
            while have_unnotified:
                selected = await self.user_db.find({
                    "bot_id": self.base_app.bot.bot.id,
                    PASS_KEY + ".state": "waitlist",
                    PASS_KEY + ".no_more_passes_notification_sent": { "$exists": False },
                    PASS_KEY + ".role": role,
                }).to_list(100)
                if len(selected) == 0:
                    logger.info(f"no unnotified candidates in the waiting list for role {role}")
                    break
                for user in selected:
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.notify_no_more_passes()

    async def create_update_from_user(self, user: int) -> PassUpdate:
        upd = TGState(user, self.base_app)
        await upd.get_state()
        return PassUpdate(self, upd)

    def test_message(self, message: Update, state, web_app_data):
        if self._checker.check_update(message):
            return PRIORITY_BASIC, self.handle_start
        if self._role_checker.check_update(message):
            return PRIORITY_BASIC, self.handle_role_cmd
        if self._name_checker.check_update(message):
            return PRIORITY_BASIC, self.handle_name_cmd
        return PRIORITY_NOT_ACCEPTING, None
    
    def test_callback_query(self, query: Update, state):
        if self._cbq_handler.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None
    
    def create_update(self, update) -> PassUpdate:
        return PassUpdate(self, update)
    
    async def handle_callback_query(self, updater):
        return await self.create_update(updater).handle_callback_query()

    async def handle_start(self, update: TGState):
        return await self.create_update(update).handle_start()

    async def handle_name_cmd(self, update: TGState):
        return await self.create_update(update).handle_cq_name()

    async def handle_role_cmd(self, update: TGState):
        return await self.create_update(update).handle_role_cmd()
    
    async def handle_legal_name_input(self, update, data):
        return await self.create_update(update).handle_legal_name_input(data)
    
    async def handle_payment_proof_input(self, update, data):
        return await self.create_update(update).handle_payment_proof_input(data)
    
    async def handle_legal_name_timeout(self, update, data):
        return await self.create_update(update).handle_legal_name_timeout(data)
    
    async def handle_payment_proof_timeout(self, update, data):
        return await self.create_update(update).handle_payment_proof_timeout(data)
