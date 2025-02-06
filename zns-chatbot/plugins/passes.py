
from datetime import timedelta
import logging
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler, filters
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, ReplyKeyboardMarkup, Update, ReplyKeyboardRemove, MessageOriginUser
from ..tg_state import TGState
from motor.core import AgnosticCollection
from telegram.constants import ParseMode
from .massage import now_msk, split_list
from asyncio import Lock, create_task, sleep
from ..telegram_links import client_user_link_html, client_user_name
from random import choice

logger = logging.getLogger(__name__)

CANCEL_CHR = chr(0xE007F) # Tag cancel
PASS_KEY = "pass_2025_1"
MAX_CONCURRENT_ASSIGNMENTS = 5
CURRENT_PRICE=9500
TIMEOUT_PROCESSOR_TICK = 3600
INVITATION_TIMEOUT=timedelta(days=2, hours=10)
PAYMENT_TIMEOUT=timedelta(days=8)
PAYMENT_TIMEOUT_NOTIFY2=timedelta(days=7)
PAYMENT_TIMEOUT_NOTIFY=timedelta(days=6)

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

    async def format_message(
            self,
            message_id: str,
            user: int|dict|None = None,
            for_user: int|None = None,
            u_pass: dict|None = None,
            couple: int|dict|None = None,
            admin: int|dict|None = None,
            **additional_keys,
        ) -> str:
        if user is None:
            user = self.update.user
        if not isinstance(user, dict):
            user = await self.base.user_db.find_one({
                "user_id": user,
                "bot_id": self.bot,
            })
        if not isinstance(user, dict):
            logger.error(f"format_message.user is not a dict: {user=}, {type(user)=}", stack_info=True)
            return message_id
        l = self.l  # noqa: E741
        lc = self.update.language_code
        if for_user is not None:
            upd = await self.base.create_update_from_user(for_user)
            l = upd.l  # noqa: E741
            lc = upd.update.language_code
        if not isinstance(u_pass, dict):
            if PASS_KEY in user:
                u_pass = user[PASS_KEY]
            else:
                u_pass = {}
        keys = dict(u_pass)
        keys["name"] = user.get("legal_name", "")
        keys["link"] = client_user_link_html(user, language_code=lc)
        if couple is None:
            if "couple" in u_pass:
                couple = u_pass["couple"]
        if isinstance(couple, int):
            couple = await self.base.user_db.find_one({
                "user_id": couple,
                "bot_id": self.bot,
            })
        if isinstance(couple, dict):
            keys["coupleName"] = couple.get("legal_name")
            keys["coupleRole"] = couple.get(PASS_KEY, dict()).get("role")
            keys["coupleLink"] = client_user_link_html(couple, language_code=lc)
            if "type" not in keys:
                keys["type"] = "couple"
        elif "type" not in keys:
            keys["type"] = "solo"
        if admin is None:
            if "proof_admin" in u_pass:
                admin = u_pass["proof_admin"]
        if isinstance(admin, int):
            admin = await self.base.user_db.find_one({
                "user_id": admin,
                "bot_id": self.bot,
            })
        if isinstance(admin, dict):
            keys["adminLink"] = client_user_link_html(admin, language_code=lc)
            keys["phoneSBP"] = admin.get("phone_sbp", None)
            keys["banksRu"] = admin.get("banks_ru", None)
            keys["banksEn"] = admin.get("banks_en", None)
        keys.update(additional_keys)
        return l(message_id, **keys)

    async def show_pass_edit(self, user, u_pass, text_key: str|None = None):
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
            if len(self.base.payment_admins) > 1:
                buttons.append(InlineKeyboardButton(
                    self.l("passes-button-change-admin"),
                    callback_data=f"{self.base.name}|change_admin"
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
            await self.format_message(
                text_key,
                user=user,
                u_pass=u_pass,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(split_list(buttons, 2)),
        )
    
    async def handle_cq_change_admin(self):
        payment_admins = await self.base.user_db.find({
            "bot_id": self.bot,
            "user_id": {"$in": self.base.payment_admins},
        }).to_list(None)
        btns = []
        for admin in payment_admins:
            btns.append(InlineKeyboardButton(
                f"{admin['emoji']} {client_user_name(admin, language_code=self.update.language_code)}",
                callback_data=f"{self.base.name}|cha2|{admin['user_id']}",
            ))
        btns.append(InlineKeyboardButton(
            self.l("passes-button-exit"),
            callback_data=f"{self.base.name}|exit",
        ))
        await self.update.edit_or_reply(
            self.l("passes-choose-admin"),
            reply_markup=InlineKeyboardMarkup(split_list(btns, 2)),
            parse_mode=ParseMode.HTML,
        )
    
    async def handle_cq_cha2(self, admin_id_str):
        admin_id = int(admin_id_str)
        assert admin_id in self.base.payment_admins
        updated = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.update.bot.id,
            PASS_KEY+".state": "assigned",
        }, {
            "$set": {
                PASS_KEY+".proof_admin": admin_id,
            }
        })
        assert updated.matched_count > 0
        user = await self.base.user_db.find_one({
            "user_id": self.update.user,
            "bot_id": self.update.bot.id,
        })
        await self.show_pass_edit(user, user[PASS_KEY])

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
        success = await self.cancel_pass()
        if success:
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
    
    async def show_couple_invitation(self, inviter) -> None:
        await self.update.reply(
            await self.format_message(
                "passes-couple-invitation",
                couple=inviter,
            ),
            reply_markup=InlineKeyboardMarkup([[
                InlineKeyboardButton(self.l("passes-button-couple-decline"), callback_data=f"{self.base.name}|cp_decl|{inviter['user_id']}"),
                InlineKeyboardButton(self.l("passes-button-couple-accept"), callback_data=f"{self.base.name}|cp_acc|{inviter['user_id']}"),
            ]]),
            parse_mode=ParseMode.HTML,
        )
    
    async def handle_cq_cp_acc(self, inviter_id):
        inviter_id = int(inviter_id)
        await self.update.edit_message_text(self.l("passes-accept-pass-request-name"),
            reply_markup=InlineKeyboardMarkup([]), parse_mode=ParseMode.HTML)
        await self.handle_name_request(inviter_id)
    
    async def accept_invitation(self, inviter_id):
        inviter_id = int(inviter_id)
        user = await self.get_user()
        updated = await self.base.user_db.update_one({
            "user_id": inviter_id,
            "bot_id": self.update.bot.id,
            PASS_KEY+".couple": self.update.user,
        }, {
            "$set": {
                PASS_KEY+".state": "waitlist",
            }
        })
        if updated.matched_count > 0:
            inviter = await self.base.user_db.find_one({
                "user_id": inviter_id,
                "bot_id": self.update.bot.id,
                PASS_KEY+".couple": self.update.user,
            })
            if inviter is not None:
                u_pass = dict(inviter[PASS_KEY])
                u_pass["role"] = "leader" if u_pass["role"].startswith("f") else "follower"
                u_pass["couple"] = inviter_id
                await self.base.user_db.update_one({
                    "user_id": self.update.user,
                    "bot_id": self.update.bot.id,
                }, {
                    "$set": {
                        PASS_KEY: u_pass,
                    }
                })
                user[PASS_KEY] = u_pass
                inv_update = await self.base.create_update_from_user(inviter_id)
                await inv_update.handle_invitation_accepted(user)
                await self.update.reply(self.l("passes-invitation-successfully-accepted"), parse_mode=ParseMode.HTML)

                return await self.base.recalculate_queues()
        await self.update.reply(self.l("passes-invitation-accept-failed"), parse_mode=ParseMode.HTML)
    
    async def handle_cq_cp_decl(self, inviter_id):
        inviter_id = int(inviter_id)
        user = await self.get_user()
        inviter = await self.base.user_db.find_one({
            "user_id": inviter_id,
            "bot_id": self.update.bot.id,
            PASS_KEY+".couple": self.update.user,
        })
        if inviter is not None:
            inv_update = await self.base.create_update_from_user(inviter_id)
            await inv_update.handle_invitation_declined(user)
        await self.update.edit_or_reply(self.l("passes-invitation-successfully-declined"), 
            reply_markup=InlineKeyboardMarkup([]), parse_mode=ParseMode.HTML)
    
    async def handle_invitation_accepted(self, invitee):
        await self.update.edit_or_reply(
            await self.format_message(
                "passes-invitation-was-accepted",
                couple=invitee
            ),
            parse_mode=ParseMode.HTML,
        )

    async def handle_invitation_declined(self, invitee):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.update.bot.id,
            PASS_KEY+".couple": invitee["user_id"],
        }, {
            "$unset": {PASS_KEY+".couple": ""},
        })
        if result.modified_count > 0:
            await self.update.reply(
                await self.format_message(
                    "passes-invitation-was-declined",
                    couple=invitee
                ),
                reply_markup=InlineKeyboardMarkup([[
                    InlineKeyboardButton(
                        self.l("passes-button-solo"),
                        callback_data=f"{self.base.name}|solo"
                    ),
                    InlineKeyboardButton(
                        self.l("passes-button-couple"),
                        callback_data=f"{self.base.name}|couple"
                    ),
                ],[
                    InlineKeyboardButton(
                        self.l("passes-button-cancel"),
                        callback_data=f"{self.base.name}|pass_exit"
                    ),
                ]]),
                parse_mode=ParseMode.HTML,
            )

    async def handle_cq_start(self):
        user = await self.get_user()
        inviter = await self.base.user_db.find_one({
            "user_id": {"$ne": self.update.user},
            "bot_id": self.update.bot.id,
            PASS_KEY+".couple": self.update.user,
        })
        if inviter is not None:
            if PASS_KEY not in user or user[PASS_KEY]["state"] != "payed" and user[PASS_KEY].get("couple", 0) != inviter["user_id"]:
                return await self.show_couple_invitation(inviter)
            else:
                inv_update = await self.base.create_update_from_user(inviter["user_id"])
                await inv_update.handle_invitation_declined(user)
        if PASS_KEY in user:
            return await self.show_pass_edit(user, user[PASS_KEY])
        else:
            return await self.new_pass()
    
    async def handle_cq_pay(self):
        user = await self.get_user()
        if PASS_KEY not in user or user[PASS_KEY]["state"] != "assigned":
            return await self.handle_cq_exit()
        await self.update.edit_or_reply(
            await self.format_message("passes-payment-request-callback-message"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )
        await self.update.reply(
            await self.format_message("passes-payment-request-waiting-message"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup([[CANCEL_CHR+self.l("cancel-command")]], resize_keyboard=True),
        )
        await self.update.require_anything(self.base.name, "handle_payment_proof_input", "", "handle_payment_proof_timeout")
    
    async def handle_cq_adm_acc(self, key: str, user_id_s: str):
        if key != PASS_KEY or self.update.user not in self.base.payment_admins:
            return
        user_id = int(user_id_s)
        user = await self.base.user_db.find_one({
            "user_id": user_id,
            "bot_id": self.bot,
            PASS_KEY+".state": "payed",
        })
        assert user is not None
        u_pass = user[PASS_KEY]
        uids = [user_id]
        if "couple" in user[PASS_KEY]:
            uids.append(user[PASS_KEY]["couple"])
        result = await self.base.user_db.update_many({
            "user_id": {"$in": uids},
            "bot_id": self.bot,
            PASS_KEY: {"$exists": True},
        }, {
            "$set": {
                PASS_KEY+".state": "payed",
                PASS_KEY+".proof_received": u_pass["proof_received"],
                PASS_KEY+".proof_file": u_pass["proof_file"],
                PASS_KEY+".proof_admin": u_pass["proof_admin"],
                PASS_KEY+".proof_accepted": now_msk(),
            }
        })
        if result.modified_count <= 0:
            return
        await self.update.reply(
            await self.format_message(
                "passes-payment-proof-accepted",
                user=user_id,
                for_user=user_id,
            ),
            user_id,
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            await self.format_message(
                "passes-adm-payment-proof-accepted",
                user=user_id,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )

    async def handle_cq_adm_rej(self, key: str, user_id_s: str):
        if key != PASS_KEY or self.update.user not in self.base.payment_admins:
            return
        user_id = int(user_id_s)
        user = await self.base.user_db.update_one({
            "user_id": user_id,
            "bot_id": self.bot,
            PASS_KEY+".state": "payed",
        })
        assert user is not None
        u_pass = user[PASS_KEY]
        uids = [user_id]
        if "couple" in user[PASS_KEY]:
            uids.append(user[PASS_KEY]["couple"])
        result = await self.base.user_db.update_one({
            "user_id": {"$in": uids},
            "bot_id": self.bot,
            PASS_KEY: {"$exists": True},
        }, {
            "$set": {
                PASS_KEY+".state": "assigned",
                PASS_KEY+".proof_received": u_pass["proof_received"],
                PASS_KEY+".proof_file": u_pass["proof_file"],
                PASS_KEY+".proof_admin": u_pass["proof_admin"],
                PASS_KEY+".proof_rejected": now_msk(),
            }
        })
        if result.modified_count <= 0:
            return
        await self.update.reply(
            await self.format_message(
                "passes-payment-proof-rejected",
                user=user_id,
                for_user=user_id,
            ),
            user_id,
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            await self.format_message(
                "passes-adm-payment-proof-rejected",
                user=user_id,
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
        uids = [user["user_id"]]
        req = {
            "bot_id": self.bot,
            PASS_KEY+".state": "assigned",
            PASS_KEY+".type":user[PASS_KEY]["type"],
        }
        if "couple" in user[PASS_KEY]:
            uids.append(user[PASS_KEY]["couple"])
            req[PASS_KEY+".couple"] = {"$in":uids}
        req["user_id"] = {"$in": uids}
        result = await self.base.user_db.update_many(req, {
            "$set": {
                PASS_KEY+".state": "payed",
                PASS_KEY+".proof_received": now_msk(),
                PASS_KEY+".proof_file": f"{doc.file_id}{file_ext}",
                PASS_KEY+".proof_admin": user[PASS_KEY]["proof_admin"],
            }
        })
        if result.modified_count <= 0:
            return
        if user[PASS_KEY]["proof_admin"] in self.base.payment_admins:
            admin = await self.base.user_db.find_one({
                "user_id": user[PASS_KEY]["proof_admin"],
                "bot_id": self.bot,
            })
            lc = "ru"
            if admin is not None and "language_code" in admin:
                lc = admin["language_code"]
            def l(s, **kwargs):  # noqa: E743
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.update.forward_message(admin["user_id"])
            await self.update.reply(
                await self.format_message(
                    "passes-adm-payment-proof-received",
                    user=user,
                    for_user=admin["user_id"],
                ),
                chat_id=admin["user_id"],
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
    
    async def handle_cq_couple(self):
        markup = ReplyKeyboardMarkup(
            [[CANCEL_CHR+self.l("cancel-command")]],
            resize_keyboard=True
        )
        try:
            await self.update.edit_message_text(self.l("passes-couple-request-edit"), reply_markup=InlineKeyboardMarkup([]), parse_mode=ParseMode.HTML)
        except Exception as e:
            logger.error(f"Exception in handle_cq_couple: {e}", exc_info=1)
        await self.update.reply(self.l("passes-couple-request-message"), reply_markup=markup, parse_mode=ParseMode.HTML)
        await self.update.require_anything(self.base.name, "handle_couple_input", None, "handle_couple_timeout")
    
    async def handle_couple_timeout(self, _data):
        await self.update.reply(
            self.l(
                "passes-couple-request-timeout",
            ),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_solo(self):
        user = await self.get_user()
        u_pass = {
            "role": user["role"],
            "state": "waitlist",
            "date_created": now_msk(),
            "type": "solo",
        }
        if PASS_KEY in user:
            u_pass["date_created"] = user[PASS_KEY]["date_created"]
            u_pass["role"] = user[PASS_KEY]["role"]
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot
        }, {
            "$set": {
                PASS_KEY: u_pass,
            }
        })
        keys = dict(u_pass)
        keys["name"] = user["legal_name"]
        await self.update.edit_or_reply(self.l("passes-solo-saved"), reply_markup=InlineKeyboardMarkup([]), parse_mode=ParseMode.HTML)
        
        await self.base.recalculate_queues()
    
    async def handle_couple_input(self, _data):
        if filters.TEXT.check_update(self.tgUpdate) and self.update.message.text[0] == CANCEL_CHR:
            return await self.update.reply(
                self.l("passes-couple-request-cancelled"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        msg = self.update.update.message
        if (msg is None or msg.forward_origin is None or
            not isinstance(msg.forward_origin, MessageOriginUser) or
            msg.forward_origin.sender_user.id == self.update.user):
            return await self.update.reply(self.l("passes-couple-request-wrong-data"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
        other_user_id = msg.forward_origin.sender_user.id
        
        invitee = await self.base.user_db.find_one({
            "user_id": other_user_id,
            "bot_id": self.update.bot.id,
        })
        if invitee is not None and PASS_KEY in invitee and invitee[PASS_KEY]["state"]=="payed":
            return await self.update.reply(self.l("passes-couple-request-invitee-payed"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
        
        user = await self.get_user()
        u_pass = {
            "role": user["role"],
            "state": "waiting-for-couple",
            "date_created": now_msk(),
            "type": "couple",
            "couple": other_user_id,
        }
        if PASS_KEY in user:
            u_pass["date_created"] = user[PASS_KEY]["date_created"]
            u_pass["role"] = user[PASS_KEY]["role"]
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot
        }, {
            "$set": {
                PASS_KEY: u_pass,
            }
        })
        user[PASS_KEY] = u_pass
        keys = dict(u_pass)
        keys["name"] = user["legal_name"]
        if invitee is not None:
            inv_update = await self.base.create_update_from_user(invitee["user_id"])
            await inv_update.show_couple_invitation(user)
            await self.update.reply(self.l("passes-couple-saved-sent"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
        else:
            await self.update.reply(self.l("passes-couple-saved"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)

    async def handle_name_request(self, data):
        user = await self.get_user()
        if "legal_name_frozen" in user:
            pass # TODO: skip the step
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
        elif data != "cmd":
            await self.accept_invitation(data)

    async def handle_cq_pass_role(self, role: str):
        new_role = "leader" if role.startswith("l") else "follower"
        await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot
        }, {
            "$set": {
                "role": new_role,
            }
        })
        await self.update.edit_or_reply(
            self.l(
                "passes-pass-role-saved",
                role=new_role,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[
                InlineKeyboardButton(
                    self.l("passes-button-solo"),
                    callback_data=f"{self.base.name}|solo"
                ),
                InlineKeyboardButton(
                    self.l("passes-button-couple"),
                    callback_data=f"{self.base.name}|couple"
                ),
            ],[
                InlineKeyboardButton(
                    self.l("passes-button-cancel"),
                    callback_data=f"{self.base.name}|pass_exit"
                ),
            ]]),
        )

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
        user = await self.base.user_db.find_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "waitlist",
        })
        if user is None:
            return False
        
        uids = [self.update.user]
        if "couple" in user[PASS_KEY]:
            uids.append(user[PASS_KEY]["couple"])
        in_q = {"$in": uids}
        matcher = {
            "user_id": in_q,
            "bot_id": self.bot,
            PASS_KEY+".state": "waitlist",
        }
        if "couple" in user[PASS_KEY]:
            matcher[PASS_KEY+".couple"] = in_q
        else:
            matcher[PASS_KEY+".couple"] = {"$exists": False}

        admin_id = choice(self.base.payment_admins)
        result = await self.base.user_db.update_many(matcher, {
            "$set": {
                PASS_KEY+".state": "assigned",
                PASS_KEY+".price": CURRENT_PRICE * len(uids),
                PASS_KEY+".date_assignment": now_msk(),
                PASS_KEY+".proof_admin": admin_id,
            }
        })
        have_changes = result.modified_count > 0
        if result.matched_count < len(uids):
            r2 = await self.base.user_db.update_many({
                "user_id": in_q,
                "bot_id": self.bot,
            }, {
                "$unset": {PASS_KEY: ""}
            })
            have_changes = have_changes or r2.modified_count > 0
        del matcher[PASS_KEY+".state"]
        users = await self.base.user_db.find(matcher).to_list(None)
        for user in users:
            if result.matched_count < len(uids):
                if len(uids) > 1:
                    upd = await self.base.create_update_from_user(user["user_id"])
                    await upd.update.reply(upd.l("passes-error-couple-not-found"), parse_mode=ParseMode.HTML)
            else:
                logger.info(f"assigned pass to {user['user_id']}, role {user[PASS_KEY]['role']}, name {user['legal_name']}")
                upd = await self.base.create_update_from_user(user["user_id"])
                await upd.show_pass_edit(user, user[PASS_KEY], "passes-pass-assigned")
        return have_changes
    
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

    async def inform_pass_cancelled(self):
        await self.update.reply(await self.format_message("passes-pass-cancelled-by-other"), parse_mode=ParseMode.HTML)

    async def cancel_pass(self) -> bool:
        user = await self.base.user_db.find_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": {"$ne": "payed"},
        })
        uids = [self.update.user]
        if user is not None and "couple" in user[PASS_KEY] and user[PASS_KEY]["state"] != "waiting-for-couple":
            uids.append(user[PASS_KEY]["couple"])
            upd = await self.base.create_update_from_user(user[PASS_KEY]["couple"])
            await upd.inform_pass_cancelled()
        result = await self.base.user_db.update_many({
            "user_id": {"$in": uids},
            "bot_id": self.bot,
            PASS_KEY+".state": {"$ne": "payed"},
        }, {
            "$unset": {
                PASS_KEY: "",
            }
        })
        return result.modified_count > 0

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

    async def notify_deadline_close(self, suffix:str = ""):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.bot,
            PASS_KEY+".state": "assigned",
            PASS_KEY + ".notified_deadline_close"+suffix: {"$exists": False},
        }, {
            "$set": {
                PASS_KEY + ".notified_deadline_close"+suffix: now_msk(),
            }
        })
        if result.modified_count > 0:
            await self.update.reply(
                self.l("passes-payment-deadline-close"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
    
    async def decline_due_deadline(self):
        result = await self.base.user_db.update_one({
            "user_id": self.update.user,
            "bot_id": self.update.bot.id,
            PASS_KEY+".couple": {"$exists": True},
        }, {
            "$unset": {PASS_KEY+".couple": ""},
        })
        if result.modified_count > 0:
            await self.update.reply(
                await self.format_message(
                    "passes-couple-didnt-answer"
                ),
                reply_markup=InlineKeyboardMarkup([[
                    InlineKeyboardButton(
                        self.l("passes-button-solo"),
                        callback_data=f"{self.base.name}|solo"
                    ),
                    InlineKeyboardButton(
                        self.l("passes-button-couple"),
                        callback_data=f"{self.base.name}|couple"
                    ),
                ],[
                    InlineKeyboardButton(
                        self.l("passes-button-cancel"),
                        callback_data=f"{self.base.name}|pass_exit"
                    ),
                ]]),
                parse_mode=ParseMode.HTML,
            )
    
    async def decline_invitation_due_deadline(self):
        await self.update.reply(
            await self.format_message(
                "passes-invitation-timeout"
            ),
            parse_mode=ParseMode.HTML,
        )

    async def cancel_due_deadline(self):
        success = await self.cancel_pass()
        if success:
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
        self.payment_admins: list[int] = []
        if isinstance(self.config.passes.payment_admin, int):
            self.payment_admins.append(self.config.passes.payment_admin)
        elif isinstance(self.config.passes.payment_admin, list):
            self.payment_admins.extend(self.config.passes.payment_admin)
        self.user_db: AgnosticCollection = base_app.users_collection
        self._checker = CommandHandler(self.name, self.handle_start)
        self._name_checker = CommandHandler("legal_name", self.handle_name_cmd)
        self._role_checker = CommandHandler("role", self.handle_role_cmd)
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
        self._queue_lock = Lock()
        create_task(self._timeout_processor())
    
    async def _timeout_processor(self) -> None:
        await sleep(1)
        while True:
            try:
                await sleep(TIMEOUT_PROCESSOR_TICK)
                async for user in self.user_db.find({
                        "bot_id": self.bot.id,
                        PASS_KEY + ".state": "assigned",
                        PASS_KEY + ".notified_deadline_close": {
                            "$lt": now_msk() - PAYMENT_TIMEOUT + PAYMENT_TIMEOUT_NOTIFY,
                        },
                    }):
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.cancel_due_deadline()
                async for user in self.user_db.find({
                        "bot_id": self.bot.id,
                        PASS_KEY + ".state": "waiting-for-couple",
                        PASS_KEY + ".date_created": {
                            "$lt": now_msk() - INVITATION_TIMEOUT,
                        },
                    }):
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.decline_due_deadline()
                    upd = await self.create_update_from_user(user[PASS_KEY]["couple"])
                    await upd.decline_invitation_due_deadline()
                async for user in self.user_db.find({
                        "bot_id": self.bot.id,
                        PASS_KEY + ".state": "assigned",
                        PASS_KEY + ".date_assignment": {
                            "$lt": now_msk() - PAYMENT_TIMEOUT_NOTIFY,
                        },
                        PASS_KEY + ".notified_deadline_close": {"$exists": False},
                    }):
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.notify_deadline_close()
                async for user in self.user_db.find({
                        "bot_id": self.bot.id,
                        PASS_KEY + ".state": "assigned",
                        PASS_KEY + ".notified_deadline_close": {
                            "$lt": now_msk() - PAYMENT_TIMEOUT_NOTIFY2 + PAYMENT_TIMEOUT_NOTIFY,
                        },
                        PASS_KEY + ".notified_deadline_close2": {"$exists": False},
                    }):
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.notify_deadline_close("2")
            except Exception as e:
                logger.error("Exception in Passes._timeout_processor %s", e, exc_info=1)
    
    async def recalculate_queues(self) -> None:
        try:
            while True:
                aggregation = await self.user_db.aggregate([{
                    "$match": {
                        PASS_KEY: {"$exists": True},
                        "bot_id": self.bot.id,
                    },
                },{
                    "$group": {
                        "_id": {
                            "state": f"${PASS_KEY}.state",
                            "role": f"${PASS_KEY}.role",
                        },
                        "count": { "$count": {} },
                    }
                }]).to_list(None)
                couples = {group["_id"]:group["count"] for group in await self.user_db.aggregate([{
                    "$match": {
                        PASS_KEY+".couple": {"$exists": True},
                        PASS_KEY+".role": "leader",
                        "bot_id": self.bot.id,
                    },
                },{
                    "$group": {
                        "_id": f"${PASS_KEY}.state",
                        "count": { "$count": {} },
                    }
                }]).to_list(None)}
                counts = {
                    "leader": dict(),
                    "follower": dict(),
                }
                max_assigned = 0
                for group in aggregation:
                    if len(group["_id"]) == 0:
                        continue
                    counts[group["_id"]["role"]][group["_id"]["state"]] = group["count"]
                    if group["_id"]["state"] == "assigned" and max_assigned < group["count"]:
                        max_assigned = group["count"]
                counts["leader"]["RA"] = counts["leader"].get("payed", 0) + counts["leader"].get("assigned", 0)
                counts["follower"]["RA"] = counts["follower"].get("payed", 0) + counts["follower"].get("assigned", 0)
                max_assigned -= couples.get("assigned", 0)
                if counts["leader"]["RA"] != counts["follower"]["RA"]:
                    target_group = "follower" if counts["leader"]["RA"] > counts["follower"]["RA"] else "leader"
                    success = await self.assign_pass(target_group)
                    if not success:
                        break
                    continue
                else:
                    if max_assigned > MAX_CONCURRENT_ASSIGNMENTS:
                        break
                    target_group = "follower" if counts["leader"].get("waitlist", 0) > counts["follower"].get("waitlist", 0) else "leader"
                    success = await self.assign_pass(target_group)
                    if not success:
                        break
                    continue
            while True:
                aggregation = await self.user_db.aggregate([{
                    "$match": {
                        PASS_KEY: {"$exists": True},
                        "bot_id": self.bot.id,
                    },
                },{
                    "$group": {
                        "_id": {
                            "state": f"${PASS_KEY}.state",
                            "role": f"${PASS_KEY}.role",
                        },
                        "count": { "$count": {} },
                    }
                }]).to_list(None)
                counts = {
                    "leader": dict(),
                    "follower": dict(),
                }
                for group in aggregation:
                    if len(group["_id"]) == 0:
                        continue
                    counts[group["_id"]["role"]][group["_id"]["state"]] = group["count"]
                counts["leader"]["RA"] = counts["leader"].get("payed", 0) + counts["leader"].get("assigned", 0)
                counts["follower"]["RA"] = counts["follower"].get("payed", 0) + counts["follower"].get("assigned", 0)
                ra = counts["leader"]["RA"]  if counts["leader"]["RA"] >= counts["follower"]["RA"] else counts["follower"]["RA"]
                if ra > self.config.passes.amount_cap_per_role:
                    break
                success = await self.assign_pass("couple")
                if not success:
                    break
                continue
            
            have_unnotified = True
            while have_unnotified:
                selected = await self.user_db.find({
                    "bot_id": self.bot.id,
                    PASS_KEY + ".state": "waitlist",
                    PASS_KEY + ".no_more_passes_notification_sent": { "$exists": False },
                }).to_list(100)
                if len(selected) == 0:
                    logger.info("no unnotified candidates in the waiting list")
                    break
                for user in selected:
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.notify_no_more_passes()
        except Exception as e:
            logger.error(f"Exception in recalculate_queues: {e}", exc_info=1)
            
        try:
            async for user in self.user_db.find({
                    "bot_id": self.bot.id,
                    PASS_KEY: {"$exists": True},
                    PASS_KEY + ".sent_to_hype_thread": {"$exists": False},
                }):
                await self.user_db.update_one({
                    "user_id": user["user_id"],
                    "bot_id": self.bot.id,
                    PASS_KEY: {"$exists": True},
                }, {"$set":{
                    PASS_KEY + ".sent_to_hype_thread": now_msk(),
                }})
                if self.config.passes.thread_channel != "":
                    try:
                        ch = self.config.passes.thread_channel
                        if isinstance(ch, str):
                            ch = "@" + ch
                        logger.debug(f"chat id: {ch}, type {type(ch)}")
                        args = {
                            "name": client_user_name(user),
                            "role": user[PASS_KEY]["role"],
                        }
                        await self.bot.send_message(
                            chat_id=ch,
                            message_thread_id=self.config.passes.thread_id,
                            text=self.base_app.localization(
                                "passes-announce-user-registered",
                                args=args,
                                locale=self.config.passes.thread_locale,
                            ),
                            parse_mode=ParseMode.HTML,
                        )
                    except Exception as e:
                        logger.error(f"Exception in recalculate_queues: {e}", exc_info=1)
        except Exception as e:
            logger.error(f"Exception in recalculate_queues: {e}", exc_info=1)
    
    async def assign_pass(self, role:str) -> bool:
        match = {
            "bot_id": self.bot.id,
            PASS_KEY + ".state": "waitlist",
        }
        if role == "couple":
            match[PASS_KEY + ".role"] = "leader"
            match[PASS_KEY + ".couple"] = {"$exists": True}
        else:
            match[PASS_KEY + ".role"] = role
        selected = await self.user_db.aggregate([
            {"$match": match},
            {"$sort": {PASS_KEY+".date_created": 1, "user_id": 1}},
            {"$limit": 1},
        ]).to_list(1)

        if len(selected) == 0:
            return False
        upd = await self.create_update_from_user(selected[0]["user_id"])
        return await upd.assign_pass()

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
    
    async def handle_couple_input(self, update, data):
        return await self.create_update(update).handle_couple_input(data)
    
    async def handle_couple_timeout(self, update, data):
        return await self.create_update(update).handle_couple_timeout(data)
    
    async def handle_legal_name_timeout(self, update, data):
        return await self.create_update(update).handle_legal_name_timeout(data)
    
    async def handle_payment_proof_timeout(self, update, data):
        return await self.create_update(update).handle_payment_proof_timeout(data)
