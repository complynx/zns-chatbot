from asyncio import Event, Lock, create_task, sleep
from datetime import timedelta
from random import choice
import logging

from motor.core import AgnosticCollection
from telegram import (
    Contact,
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    MessageOriginUser,
    ReplyKeyboardMarkup,
    ReplyKeyboardRemove,
    Update,
)
from telegram.constants import ParseMode
from telegram.ext import CallbackQueryHandler, CommandHandler, filters

from ..telegram_links import client_user_link_html, client_user_name
from ..tg_state import SilentArgumentParser, TGState
from .base_plugin import PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING, BasePlugin
from .massage import now_msk, split_list
from .pass_keys import (
    PASS_RU,
    PASS_BY,
    PASS_KEYS,
    PASS_KEY,
)

logger = logging.getLogger(__name__)

CANCEL_CHR = chr(0xE007F)  # Tag cancel
MAX_CONCURRENT_ASSIGNMENTS = 6
QUEUE_LOOKAHEAD = 0  # 0 = scan the whole queue when searching for an assignable pass

CURRENT_PRICE = {
    PASS_RU: 12500,#x40=67, 13000,#x40=82, 13500,#x15=90
    # PASS_BY: 10900,
}
TIMEOUT_PROCESSOR_TICK = 3600
INVITATION_TIMEOUT = timedelta(days=2, hours=10)
PAYMENT_TIMEOUT = timedelta(days=8)
PAYMENT_TIMEOUT_NOTIFY2 = timedelta(days=7)
PAYMENT_TIMEOUT_NOTIFY = timedelta(days=6)
PASS_TYPES_ASSIGNABLE = ["solo", "couple", "sputnik"]


class PassUpdate:
    base: "Passes"
    tgUpdate: Update
    bot: int
    update: TGState

    def __init__(self, base, update: TGState) -> None:
        self.base = base
        self.update = update
        self.l = update.l
        self.tgUpdate = update.update
        self.bot = self.update.bot.id
        self.pass_key = ""
        self.user = None

    def is_passport_required(self) -> bool:
        try:
            return self.base.config.passes.events[self.pass_key].require_passport
        except Exception:
            return True

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

    def admin_to_keys(self, admin: dict, lc: str | None = None) -> dict:
        assert isinstance(admin, dict)
        if lc is None:
            lc = self.update.language_code
        keys = {}
        keys["adminEmoji"] = admin.get("emoji", "")
        keys["adminLink"] = client_user_link_html(admin, language_code=lc)
        keys["adminName"] = client_user_name(admin, language_code=lc)
        keys["phoneContact"] = admin.get("phone_contact", "nophone")
        keys["phoneSBP"] = admin.get("phone_sbp", "nosbp")
        if admin.get("paypal", "") != "" and keys["phoneSBP"] == "nosbp":
            keys["phoneSBP"] = "paypal"
        keys["banks"] = admin.get("banks_en", "")
        if lc is not None:
            keys["banks"] = admin.get("banks_" + lc, keys["banks"])
        return keys

    async def format_message(
        self,
        message_id: str,
        user: int | dict | None = None,
        for_user: int | None = None,
        u_pass: dict | None = None,
        couple: int | dict | None = None,
        admin: int | dict | None = None,
        pass_key: str | None = None,
        **additional_keys,
    ) -> str:
        if pass_key is None:
            pass_key = self.pass_key
        if user is None:
            user = self.update.user
        if not isinstance(user, dict):
            user = await self.base.user_db.find_one(
                {
                    "user_id": user,
                    "bot_id": self.bot,
                }
            )
        if not isinstance(user, dict):
            logger.error(
                f"format_message.user is not a dict: {user=}, {type(user)=}",
                stack_info=True,
            )
            return message_id
        l = self.l  # noqa: E741
        lc = self.update.language_code
        if for_user is not None:
            upd = await self.base.create_update_from_user(for_user)
            l = upd.l  # noqa: E741
            lc = upd.update.language_code
        if not isinstance(u_pass, dict):
            if pass_key in user:
                u_pass = user[pass_key]
            else:
                u_pass = {}
        keys = dict(u_pass)
        keys["passKey"] = pass_key
        keys["name"] = user.get("legal_name", "")
        keys["link"] = client_user_link_html(user, language_code=lc)
        if couple is None:
            if "couple" in u_pass:
                couple = u_pass["couple"]
        if isinstance(couple, int):
            couple = await self.base.user_db.find_one(
                {
                    "user_id": couple,
                    "bot_id": self.bot,
                }
            )
        if isinstance(couple, dict):
            keys["coupleName"] = couple.get("legal_name")
            keys["coupleRole"] = couple.get(pass_key, dict()).get("role")
            keys["coupleLink"] = client_user_link_html(couple, language_code=lc)
            if "type" not in keys:
                keys["type"] = "couple"
        elif "type" not in keys:
            keys["type"] = "solo"
        if admin is None:
            if "proof_admin" in u_pass:
                admin = u_pass["proof_admin"]
        if isinstance(admin, int):
            admin = await self.base.user_db.find_one(
                {
                    "user_id": admin,
                    "bot_id": self.bot,
                }
            )
        if isinstance(admin, dict):
            keys.update(self.admin_to_keys(admin, lc))
        keys.update(additional_keys)
        return l(message_id, **keys)

    async def show_pass_edit(self, user, u_pass, text_key: str | None = None):
        if text_key is None:
            text_key = f"passes-pass-edit-{u_pass['state']}"

        buttons = [
            InlineKeyboardButton(
                self.l("passes-button-change-name"),
                callback_data=f"{self.base.name}|name",
            ),
        ]
        if u_pass["state"] == "assigned":
            buttons.append(
                InlineKeyboardButton(
                    self.l("passes-button-pay"),
                    callback_data=f"{self.base.name}|pay|{self.pass_key}",
                )
            )
        if u_pass["state"] != "payed":
            if len(self.base.payment_admins[self.pass_key]) > 1:
                buttons.append(
                    InlineKeyboardButton(
                        self.l("passes-button-change-admin"),
                        callback_data=f"{self.base.name}|change_admin|{self.pass_key}",
                    )
                )
            buttons.append(
                InlineKeyboardButton(
                    self.l("passes-button-cancel"),
                    callback_data=f"{self.base.name}|cancel|{self.pass_key}",
                )
            )
        buttons.append(
            InlineKeyboardButton(
                self.l("passes-button-exit"), callback_data=f"{self.base.name}|exit"
            )
        )

        await self.update.edit_or_reply(
            await self.format_message(
                text_key,
                user=user,
                u_pass=u_pass,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(split_list(buttons, 2)),
        )

    async def handle_cq_change_admin(
        self, key, message_key: str = "passes-choose-admin"
    ):
        self.set_pass_key(key)
        payment_admins = await self.base.user_db.find(
            {
                "bot_id": self.bot,
                "user_id": {"$in": self.base.payment_admins[self.pass_key]},
            }
        ).to_list(None)
        btns = []
        admin_texts = []
        for admin in payment_admins:
            admin_keys = self.admin_to_keys(admin)
            btns.append(
                InlineKeyboardButton(
                    self.l(
                        "passes-payment-admin-button",
                        **admin_keys,
                    ),
                    callback_data=f"{self.base.name}|cha2|{self.pass_key}|{admin['user_id']}",
                )
            )
            admin_texts.append(
                self.l(
                    "passes-payment-admin-desc",
                    **admin_keys,
                )
            )
        btns.append(
            InlineKeyboardButton(
                self.l("passes-button-exit"),
                callback_data=f"{self.base.name}|exit",
            )
        )
        await self.update.edit_or_reply(
            self.l(
                message_key,
                adminTexts="\n".join(
                    [f"{i + 1}. {t}" for i, t in enumerate(admin_texts)]
                ),
            ),
            reply_markup=InlineKeyboardMarkup(split_list(btns, 2)),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_cha2(self, key, admin_id_str):
        self.set_pass_key(key)
        admin_id = int(admin_id_str)
        assert admin_id in self.base.payment_admins[self.pass_key]
        updated = await self.base.user_db.update_one(
            {
                "user_id": self.update.user,
                "bot_id": self.update.bot.id,
                self.pass_key: {"$exists": True},
            },
            {
                "$set": {
                    self.pass_key + ".proof_admin": admin_id,
                }
            },
        )
        if updated.matched_count > 0:  # has pass
            user = await self.base.user_db.find_one(
                {
                    "user_id": self.update.user,
                    "bot_id": self.update.bot.id,
                }
            )
            await self.show_pass_edit(user, user[self.pass_key])
        else:  # no pass yet
            updated = await self.base.user_db.update_one(
                {
                    "user_id": self.update.user,
                    "bot_id": self.update.bot.id,
                    self.pass_key: {"$exists": False},
                },
                {
                    "$set": {
                        "proof_admins." + self.pass_key: admin_id,
                    }
                },
            )
            assert updated.matched_count > 0
            await self.update.edit_or_reply(
                self.l(
                    "passes-pass-admin-saved",
                ),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-solo"),
                                callback_data=f"{self.base.name}|solo|{self.pass_key}",
                            ),
                            InlineKeyboardButton(
                                self.l("passes-button-couple"),
                                callback_data=f"{self.base.name}|couple|{self.pass_key}",
                            ),
                        ],
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-cancel"),
                                callback_data=f"{self.base.name}|pass_exit",
                            ),
                        ],
                    ]
                ),
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

    async def handle_cq_cancel(self, key):
        self.set_pass_key(key)
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
            reply_markup=InlineKeyboardMarkup(
                [
                    [
                        InlineKeyboardButton(
                            self.l("passes-button-couple-decline"),
                            callback_data=f"{self.base.name}|cp_decl|{self.pass_key}|{inviter['user_id']}",
                        ),
                        InlineKeyboardButton(
                            self.l("passes-button-couple-accept"),
                            callback_data=f"{self.base.name}|cp_acc|{self.pass_key}|{inviter['user_id']}",
                        ),
                    ]
                ]
            ),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_cp_acc(self, key, inviter_id):
        self.set_pass_key(key)
        inviter_id = int(inviter_id)
        await self.update.edit_message_text(
            self.l("passes-accept-pass-request-name"),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.HTML,
        )
        if not self.is_passport_required():
            return await self.after_legal_name_input(inviter_id, "skip")
        await self.handle_name_request(inviter_id)

    async def accept_invitation(self, inviter_id):
        inviter_id = int(inviter_id)
        user = await self.get_user()
        updated = await self.base.user_db.update_one(
            {
                "user_id": inviter_id,
                "bot_id": self.update.bot.id,
                self.pass_key + ".couple": self.update.user,
            },
            {
                "$set": {
                    self.pass_key + ".state": "waitlist",
                }
            },
        )
        if updated.matched_count > 0:
            inviter = await self.base.user_db.find_one(
                {
                    "user_id": inviter_id,
                    "bot_id": self.update.bot.id,
                    self.pass_key + ".couple": self.update.user,
                }
            )
            if inviter is not None:
                u_pass = dict(inviter[self.pass_key])
                u_pass["role"] = (
                    "leader" if u_pass["role"].startswith("f") else "follower"
                )
                u_pass["couple"] = inviter_id
                await self.base.user_db.update_one(
                    {
                        "user_id": self.update.user,
                        "bot_id": self.update.bot.id,
                    },
                    {
                        "$set": {
                            self.pass_key: u_pass,
                        }
                    },
                )
                user[self.pass_key] = u_pass
                inv_update = await self.base.create_update_from_user(inviter_id)
                inv_update.set_pass_key(self.pass_key)
                await inv_update.handle_invitation_accepted(user)
                await self.update.reply(
                    self.l(
                        "passes-invitation-successfully-accepted", passKey=self.pass_key
                    ),
                    parse_mode=ParseMode.HTML,
                )

                return await self.base.recalculate_queues()
        await self.update.reply(
            self.l("passes-invitation-accept-failed", passKey=self.pass_key),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_cp_decl(self, key, inviter_id):
        self.set_pass_key(key)
        inviter_id = int(inviter_id)
        user = await self.get_user()
        inviter = await self.base.user_db.find_one(
            {
                "user_id": inviter_id,
                "bot_id": self.update.bot.id,
                self.pass_key + ".couple": self.update.user,
            }
        )
        if inviter is not None:
            inv_update = await self.base.create_update_from_user(inviter_id)
            inv_update.set_pass_key(key)
            await inv_update.handle_invitation_declined(user)
        await self.update.edit_or_reply(
            self.l("passes-invitation-successfully-declined", passKey=self.pass_key),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.HTML,
        )

    async def handle_invitation_accepted(self, invitee):
        await self.update.edit_or_reply(
            await self.format_message("passes-invitation-was-accepted", couple=invitee),
            parse_mode=ParseMode.HTML,
        )

    async def handle_invitation_declined(self, invitee):
        result = await self.base.user_db.update_one(
            {
                "user_id": self.update.user,
                "bot_id": self.update.bot.id,
                self.pass_key + ".couple": invitee["user_id"],
            },
            {
                "$unset": {self.pass_key + ".couple": ""},
            },
        )
        if result.modified_count > 0:
            await self.update.reply(
                await self.format_message(
                    "passes-invitation-was-declined", couple=invitee
                ),
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-solo"),
                                callback_data=f"{self.base.name}|solo|{self.pass_key}",
                            ),
                            InlineKeyboardButton(
                                self.l("passes-button-couple"),
                                callback_data=f"{self.base.name}|couple|{self.pass_key}",
                            ),
                        ],
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-cancel"),
                                callback_data=f"{self.base.name}|pass_exit",
                            ),
                        ],
                    ]
                ),
                parse_mode=ParseMode.HTML,
            )

    def set_pass_key(self, pass_key: str) -> None:
        if pass_key not in PASS_KEYS:
            raise Exception(f"wrong pass key {pass_key}")
        self.pass_key = pass_key

    async def handle_cq_start(self, pass_key: str):
        self.set_pass_key(pass_key)
        user = await self.get_user()
        inviter = await self.base.user_db.find_one(
            {
                "user_id": {"$ne": self.update.user},
                "bot_id": self.update.bot.id,
                self.pass_key + ".couple": self.update.user,
            }
        )
        if inviter is not None:
            if (
                self.pass_key not in user
                or user[self.pass_key].get("couple", 0) != inviter["user_id"]
            ):
                if self.pass_key not in user or user[self.pass_key]["state"] != "payed":
                    return await self.show_couple_invitation(inviter)
                else:
                    inv_update = await self.base.create_update_from_user(
                        inviter["user_id"]
                    )
                    inv_update.set_pass_key(self.pass_key)
                    await inv_update.handle_invitation_declined(user)
        if self.pass_key in user:
            return await self.show_pass_edit(user, user[self.pass_key])
        else:
            return await self.new_pass()

    async def handle_cq_pay(self, pass_key: str):
        self.set_pass_key(pass_key)
        user = await self.get_user()
        if self.pass_key not in user or user[self.pass_key]["state"] != "assigned":
            return await self.handle_cq_exit()
        await self.update.edit_or_reply(
            await self.format_message("passes-payment-request-callback-message"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )
        await self.update.reply(
            await self.format_message("passes-payment-request-waiting-message"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup(
                [[CANCEL_CHR + self.l("cancel-command")]], resize_keyboard=True
            ),
        )
        await self.update.require_anything(
            self.base.name,
            "handle_payment_proof_input",
            self.pass_key,
            "handle_payment_proof_timeout",
        )

    async def handle_cq_adm_acc(self, key: str, user_id_s: str):
        self.set_pass_key(key)
        if not self.base.is_payment_admin(self.update.user, self.pass_key):
            return
        user_id = int(user_id_s)
        user = await self.base.user_db.find_one(
            {
                "user_id": user_id,
                "bot_id": self.bot,
                self.pass_key + ".state": "payed",
            }
        )
        assert user is not None
        u_pass = user[self.pass_key]
        uids = [user_id]
        if "couple" in user[self.pass_key]:
            uids.append(user[self.pass_key]["couple"])
        result = await self.base.user_db.update_many(
            {
                "user_id": {"$in": uids},
                "bot_id": self.bot,
                self.pass_key: {"$exists": True},
            },
            {
                "$set": {
                    self.pass_key + ".state": "payed",
                    self.pass_key + ".proof_received": u_pass["proof_received"],
                    self.pass_key + ".proof_file": u_pass["proof_file"],
                    self.pass_key + ".proof_admin": u_pass["proof_admin"],
                    self.pass_key + ".proof_admin_accepted": self.update.user,
                    self.pass_key + ".proof_accepted": now_msk(),
                }
            },
        )
        if result.modified_count <= 0:
            return
        await self.update.reply(
            await self.format_message(
                "passes-payment-proof-accepted",
                user=user_id,
                for_user=user_id,
            ),
            user_id,
            parse_mode=ParseMode.HTML,
        )
        await self.update.edit_message_text(
            await self.format_message(
                "passes-adm-payment-proof-accepted",
                user=user_id,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_cq_adm_rej(self, key: str, user_id_s: str):
        self.set_pass_key(key)
        if not self.base.is_payment_admin(self.update.user, self.pass_key):
            return
        user_id = int(user_id_s)
        user = await self.base.user_db.find_one(
            {
                "user_id": user_id,
                "bot_id": self.bot,
                self.pass_key + ".state": "payed",
            }
        )
        assert user is not None
        u_pass = user[self.pass_key]
        uids = [user_id]
        if "couple" in user[self.pass_key]:
            uids.append(user[self.pass_key]["couple"])
        result = await self.base.user_db.update_one(
            {
                "user_id": {"$in": uids},
                "bot_id": self.bot,
                self.pass_key: {"$exists": True},
            },
            {
                "$set": {
                    self.pass_key + ".state": "assigned",
                    self.pass_key + ".proof_received": u_pass["proof_received"],
                    self.pass_key + ".proof_file": u_pass["proof_file"],
                    self.pass_key + ".proof_admin": u_pass["proof_admin"],
                    self.pass_key + ".proof_rejected": now_msk(),
                }
            },
        )
        if result.modified_count <= 0:
            return
        await self.update.reply(
            await self.format_message(
                "passes-payment-proof-rejected",
                user=user_id,
                for_user=user_id,
            ),
            user_id,
            parse_mode=ParseMode.HTML,
        )
        await self.update.edit_message_text(
            await self.format_message(
                "passes-adm-payment-proof-rejected",
                user=user_id,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
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
        self.set_pass_key(data)
        if (
            filters.TEXT.check_update(self.tgUpdate)
            and self.update.message.text[0] == CANCEL_CHR
        ):
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
        if self.pass_key not in user or user[self.pass_key]["state"] != "assigned":
            return await self.handle_cq_exit()
        uids = [user["user_id"]]
        req = {
            "bot_id": self.bot,
            self.pass_key + ".state": "assigned",
            self.pass_key + ".type": user[self.pass_key]["type"]
            if "type" in user[self.pass_key]
            else "solo",
        }
        if "couple" in user[self.pass_key]:
            uids.append(user[self.pass_key]["couple"])
            req[self.pass_key + ".couple"] = {"$in": uids}
        req["user_id"] = {"$in": uids}
        result = await self.base.user_db.update_many(
            req,
            {
                "$set": {
                    self.pass_key + ".state": "payed",
                    self.pass_key + ".proof_received": now_msk(),
                    self.pass_key + ".proof_file": f"{doc.file_id}{file_ext}",
                    self.pass_key + ".proof_admin": user[self.pass_key]["proof_admin"],
                }
            },
        )
        if result.modified_count <= 0:
            return
        if user[self.pass_key]["proof_admin"] in self.base.get_all_payment_admins(
            self.pass_key
        ):
            admin = await self.base.user_db.find_one(
                {
                    "user_id": user[self.pass_key]["proof_admin"],
                    "bot_id": self.bot,
                }
            )
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
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                l("passes-adm-payment-proof-accept-button"),
                                callback_data=f"{self.base.name}|adm_acc|{self.pass_key}|{self.update.user}",
                            ),
                            InlineKeyboardButton(
                                l("passes-adm-payment-proof-reject-button"),
                                callback_data=f"{self.base.name}|adm_rej|{self.pass_key}|{self.update.user}",
                            ),
                        ]
                    ]
                ),
            )
        await self.update.reply(
            self.l("passes-payment-proof-forwarded"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardRemove(),
        )

    async def new_pass(self):
        if now_msk() < self.base.config.passes.events[self.pass_key].sell_start:
            return await self.update.edit_or_reply(
                self.l("passes-sell-not-started", passKey=self.pass_key),
                reply_markup=InlineKeyboardMarkup([]),
                parse_mode=ParseMode.HTML,
            )
        await self.update.edit_or_reply(
            self.l("passes-pass-create-start-message", passKey=self.pass_key),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.HTML,
        )
        if not self.is_passport_required():
            return await self.after_legal_name_input("pass", "skip")
        return await self.handle_name_request("pass")

    async def handle_start(self):
        logger.debug(f"starting passes for: {self.update.user}")
        buttons = []
        for pass_key in PASS_KEYS:
            buttons.append(
                InlineKeyboardButton(
                    self.l("passes-select-type-button", passKey=pass_key),
                    callback_data=f"{self.base.name}|start|{pass_key}",
                )
            )
        buttons.append(
            InlineKeyboardButton(
                self.l("passes-button-exit"), callback_data=f"{self.base.name}|exit"
            )
        )
        await self.update.reply(
            self.l("passes-select-type-message"),
            reply_markup=InlineKeyboardMarkup([buttons]),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_name(self):
        logger.debug(f"command to change legal name for: {self.update.user}")
        return await self.handle_name_request("cmd")

    async def handle_cq_couple(self, key: str):
        self.set_pass_key(key)
        markup = ReplyKeyboardMarkup(
            [[CANCEL_CHR + self.l("cancel-command")]], resize_keyboard=True
        )
        try:
            await self.update.edit_message_text(
                self.l("passes-couple-request-edit", passKey=self.pass_key),
                reply_markup=InlineKeyboardMarkup([]),
                parse_mode=ParseMode.HTML,
            )
        except Exception as err:
            logger.error(
                f"Exception in handle_cq_couple: {err=}, {self.update.user=}",
                exc_info=1,
            )
        await self.update.reply(
            self.l("passes-couple-request-message", passKey=self.pass_key),
            reply_markup=markup,
            parse_mode=ParseMode.HTML,
        )
        await self.update.require_anything(
            self.base.name,
            "handle_couple_input",
            self.pass_key,
            "handle_couple_timeout",
        )

    async def handle_couple_timeout(self, _data):
        await self.update.reply(
            self.l(
                "passes-couple-request-timeout",
            ),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_solo(self, key: str):
        self.set_pass_key(key)
        user = await self.get_user()
        u_pass = {
            "role": user["role"],
            "state": "waitlist",
            "date_created": now_msk(),
            "type": "solo",
            "proof_admin": user.get("proof_admins", {}).get(
                self.pass_key,
                choice(self.base.payment_admins[self.pass_key]),
            ),
        }
        if self.pass_key in user:
            u_pass["date_created"] = user[self.pass_key]["date_created"]
            u_pass["role"] = user[self.pass_key]["role"]
            u_pass["proof_admin"] = user[self.pass_key].get(
                "proof_admin",
                choice(self.base.payment_admins[self.pass_key]),
            )
        await self.base.user_db.update_one(
            {"user_id": self.update.user, "bot_id": self.bot},
            {
                "$set": {
                    self.pass_key: u_pass,
                }
            },
        )
        keys = dict(u_pass)
        if self.is_passport_required():
            keys["name"] = user.get("legal_name", "")
        await self.update.edit_or_reply(
            self.l("passes-solo-saved", passKey=self.pass_key),
            reply_markup=InlineKeyboardMarkup([]),
            parse_mode=ParseMode.HTML,
        )

        await self.base.recalculate_queues()

    async def handle_couple_input(self, data):
        self.set_pass_key(data)
        if (
            filters.TEXT.check_update(self.tgUpdate)
            and self.update.message.text[0] == CANCEL_CHR
        ):
            return await self.update.reply(
                self.l("passes-couple-request-cancelled"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        msg = self.update.update.message
        if (
            msg is None
            or (
                msg.forward_origin is None
                and (
                    msg.contact is None
                    or not isinstance(msg.contact, Contact)
                    or msg.contact.user_id is None
                    or msg.contact.user_id == self.update.user
                )
            )
            or not isinstance(msg.forward_origin, MessageOriginUser)
            or msg.forward_origin.sender_user.id == self.update.user
        ):
            return await self.update.reply(
                self.l("passes-couple-request-wrong-data"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
        other_user_id = msg.forward_origin.sender_user.id

        invitee = await self.base.user_db.find_one(
            {
                "user_id": other_user_id,
                "bot_id": self.update.bot.id,
            }
        )
        if (
            invitee is not None
            and self.pass_key in invitee
            and invitee[self.pass_key]["state"] == "payed"
        ):
            return await self.update.reply(
                self.l("passes-couple-request-invitee-payed", passKey=self.pass_key),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )

        user = await self.get_user()
        u_pass = {
            "role": user["role"],
            "state": "waiting-for-couple",
            "date_created": now_msk(),
            "type": "couple",
            "couple": other_user_id,
            "proof_admin": user.get("proof_admins", {}).get(
                self.pass_key,
                choice(self.base.payment_admins[self.pass_key]),
            ),
        }
        if self.pass_key in user:
            u_pass["date_created"] = user[self.pass_key]["date_created"]
            u_pass["role"] = user[self.pass_key]["role"]
            u_pass["proof_admin"] = user[self.pass_key].get(
                "proof_admin",
                choice(self.base.payment_admins[self.pass_key]),
            )
        await self.base.user_db.update_one(
            {"user_id": self.update.user, "bot_id": self.bot},
            {
                "$set": {
                    self.pass_key: u_pass,
                }
            },
        )
        user[self.pass_key] = u_pass
        keys = dict(u_pass)
        if self.is_passport_required():
            keys["name"] = user.get("legal_name", "")
        if invitee is not None:
            inv_update = await self.base.create_update_from_user(invitee["user_id"])
            inv_update.set_pass_key(self.pass_key)
            await inv_update.show_couple_invitation(user)
            await self.update.reply(
                self.l("passes-couple-saved-sent", passKey=self.pass_key),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
        else:
            await self.update.reply(
                self.l("passes-couple-saved", passKey=self.pass_key),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )

    async def handle_name_request(self, data):
        user = await self.get_user()
        if "legal_name_frozen" in user:
            return await self.after_legal_name_input(data)
        btns = []
        if "legal_name" in user:
            btns.append([user["legal_name"]])
        btns.append([CANCEL_CHR + self.l("cancel-command")])
        markup = ReplyKeyboardMarkup(btns, resize_keyboard=True)
        await self.update.reply(
            self.l("passes-legal-name-request-message"),
            reply_markup=markup,
            parse_mode=ParseMode.HTML,
        )
        await self.update.require_input(
            self.base.name,
            "handle_legal_name_input",
            f"{self.pass_key}|{data}",
            "handle_legal_name_timeout",
        )
    
    async def require_passport_data(self):
        await self.update.reply(
            self.l("passes-passport-data-required"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[
                InlineKeyboardButton(
                    self.l("passes-passport-data-button"),
                    callback_data=f"{self.base.name}|passport_data|{self.pass_key}"
                )
            ]]),
        )

    async def handle_passport_data_cmd(self):
        return await self.handle_cq_passport_data(PASS_RU)

    async def handle_cq_passport_data(self, pass_key: str):
        self.set_pass_key(pass_key)
        logger.debug(f"command to change passport data for: {self.update.user}")
        if self.pass_key not in PASS_KEYS:
            return await self.update.reply(
                self.l("passes-passport-data-required-beginning-message"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        return await self.handle_name_request("passport_data")

    async def handle_passport_request(self, data: str):
        user = await self.get_user()
        if "legal_name_frozen" in user:
            return await self.after_legal_name_input(data)
        btns = []
        if "passport_number" in user:
            btns.append([user["passport_number"]])
        btns.append([CANCEL_CHR + self.l("cancel-command")])
        markup = ReplyKeyboardMarkup(btns, resize_keyboard=True)
        await self.update.reply(
            self.l("passes-passport-request-message"),
            reply_markup=markup,
            parse_mode=ParseMode.HTML,
        )
        await self.update.require_input(
            self.base.name,
            "handle_passport_input",
            f"{self.pass_key}|{data}",
            "handle_passport_timeout",
        )
    
    async def handle_passport_input(self, data: str):
        pass_key, reason = data.split("|", maxsplit=2)
        if pass_key != "":
            self.set_pass_key(pass_key)
        user = await self.get_user()
        passport_number = self.update.message.text
        if passport_number[0] == CANCEL_CHR:
            return await self.update.reply(
                self.l("passes-pass-create-cancel"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        old_passport = user.get("passport_number", "")
        logger.debug(
            f"changing passport number for: {self.update.user} from {old_passport} to {passport_number}"
        )
        await self.base.user_db.update_one(
            {"user_id": self.update.user, "bot_id": self.bot},
            {"$set": {"passport_number": passport_number}},
        )
        await self.update.reply(
            self.l(
                "passes-passport-changed-message",
                passportNumber=passport_number,
            ),
            reply_markup=ReplyKeyboardRemove(),
            parse_mode=ParseMode.HTML,
        )
        await self.after_legal_name_input(reason, "passport")
    
    async def handle_passport_timeout(self, data):
        return await self.update.reply(
            self.l("passes-passport-timeout"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardRemove(),
        )

    async def handle_legal_name_input(self, data: str):
        pass_key, reason = data.split("|", maxsplit=2)
        if pass_key != "":
            self.set_pass_key(pass_key)
        user = await self.get_user()
        name = self.update.message.text
        if name[0] == CANCEL_CHR:
            return await self.update.reply(
                self.l("passes-pass-create-cancel"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        old_name = user["legal_name"] if "legal_name" in user else ""
        logger.debug(
            f"changing legal name for: {self.update.user} from {old_name} to {name}"
        )
        await self.base.user_db.update_one(
            {"user_id": self.update.user, "bot_id": self.bot},
            {"$set": {"legal_name": name}},
        )
        await self.update.reply(
            self.l(
                "passes-legal-name-changed-message",
                name=name,
            ),
            reply_markup=ReplyKeyboardRemove(),
            parse_mode=ParseMode.HTML,
        )
        await self.after_legal_name_input(reason)

    async def after_legal_name_input(self, data, input_type: str = "name"):
        if data == "cmd":
            return
        if input_type == "name" and self.is_passport_required():
            return await self.handle_passport_request(data)
        if data == "pass":
            await self.update.reply(
                self.l("passes-pass-role-select"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                self.l("passes-role-button-leader"),
                                callback_data=f"{self.base.name}|pass_role|{self.pass_key}|l",
                            ),
                            InlineKeyboardButton(
                                self.l("passes-role-button-follower"),
                                callback_data=f"{self.base.name}|pass_role|{self.pass_key}|f",
                            ),
                        ],
                        [
                            InlineKeyboardButton(
                                self.l("passes-role-button-cancel"),
                                callback_data=f"{self.base.name}|pass_exit",
                            ),
                        ],
                    ]
                ),
            )
        elif data not in ["cmd", "passport_data"]:
            await self.accept_invitation(data)

    async def handle_cq_pass_role(self, key: str, role: str):
        self.set_pass_key(key)
        new_role = "leader" if role.startswith("l") else "follower"
        setter = {
            "role": new_role,
        }
        if len(self.base.payment_admins[self.pass_key]) == 1:
            setter["proof_admins." + self.pass_key] = self.base.payment_admins[
                self.pass_key
            ][0]
        await self.base.user_db.update_one(
            {"user_id": self.update.user, "bot_id": self.bot}, {"$set": setter}
        )

        if len(self.base.payment_admins[self.pass_key]) > 1:
            await self.handle_cq_change_admin(self.pass_key, "passes-pass-role-saved")
        else:
            await self.update.edit_or_reply(
                self.l(
                    "passes-pass-admin-saved",
                ),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-solo"),
                                callback_data=f"{self.base.name}|solo|{self.pass_key}",
                            ),
                            InlineKeyboardButton(
                                self.l("passes-button-couple"),
                                callback_data=f"{self.base.name}|couple|{self.pass_key}",
                            ),
                        ],
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-cancel"),
                                callback_data=f"{self.base.name}|pass_exit",
                            ),
                        ],
                    ]
                ),
            )

    async def handle_cq_role(self, role: str):
        new_role = "leader" if role.startswith("l") else "follower"
        await self.base.user_db.update_one(
            {"user_id": self.update.user, "bot_id": self.bot},
            {"$set": {"role": new_role}},
        )

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

    async def assign_pass(
        self,
        pass_key: str,
        price: int | None = None,
        type: str | None = None,
        comment: str | None = None,
        skip_in_balance_count: bool = False,
        proof_admin: int | None = None,
    ):
        self.set_pass_key(pass_key)
        user = await self.base.user_db.find_one(
            {
                "user_id": self.update.user,
                "bot_id": self.bot,
                self.pass_key + ".state": "waitlist",
            }
        )
        if user is None:
            return False

        uids = [self.update.user]
        if "couple" in user[self.pass_key]:
            uids.append(user[self.pass_key]["couple"])
        in_q = {"$in": uids}
        matcher = {
            "user_id": in_q,
            "bot_id": self.bot,
            self.pass_key + ".state": "waitlist",
        }
        if "couple" in user[self.pass_key]:
            matcher[self.pass_key + ".couple"] = in_q
        else:
            matcher[self.pass_key + ".couple"] = {"$exists": False}

        sets = {
            self.pass_key + ".state": "assigned" if price is None or price > 0 else "payed",
            self.pass_key + ".price": price
            if price is not None
            else CURRENT_PRICE[self.pass_key] * len(uids),
            self.pass_key + ".date_assignment": now_msk(),
        }
        if price is not None and price == 0:
            assert proof_admin is not None, "proof_admin must be set for free pass"
            sets[self.pass_key + ".proof_received"] = now_msk()
            sets[self.pass_key + ".proof_file"] = "free_pass"
            sets[self.pass_key + ".proof_admin"] = proof_admin
            sets[self.pass_key + ".proof_accepted"] = now_msk()
        if comment is not None:
            sets[self.pass_key + ".comment"] = comment
        if type is not None:
            sets[self.pass_key + ".type"] = type
        if skip_in_balance_count:
            sets[self.pass_key + ".skip_in_balance_count"] = True
        result = await self.base.user_db.update_many(matcher, {"$set": sets})
        have_changes = result.modified_count > 0
        if result.matched_count < len(uids):
            r2 = await self.base.user_db.update_many(
                {
                    "user_id": in_q,
                    "bot_id": self.bot,
                },
                {"$unset": {self.pass_key: ""}},
            )
            have_changes = have_changes or r2.modified_count > 0
        del matcher[self.pass_key + ".state"]
        users = await self.base.user_db.find(matcher).to_list(None)
        for user in users:
            if result.matched_count < len(uids):
                if len(uids) > 1:
                    upd = await self.base.create_update_from_user(user["user_id"])
                    await upd.update.reply(
                        upd.l("passes-error-couple-not-found"),
                        parse_mode=ParseMode.HTML,
                    )
            else:
                logger.info(
                    f"assigned pass to {user['user_id']}, role {user[self.pass_key]['role']}, name {user.get('legal_name', client_user_name(user))}"
                )
                upd = await self.base.create_update_from_user(user["user_id"])
                upd.set_pass_key(self.pass_key)
                await upd.show_pass_edit(
                    user, user[self.pass_key], "passes-pass-assigned" if price is None or price > 0 else "passes-pass-free-assigned"
                )
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
            reply_markup=InlineKeyboardMarkup(
                [
                    [
                        InlineKeyboardButton(
                            self.l("passes-role-button-leader"),
                            callback_data=f"{self.base.name}|role|l",
                        ),
                        InlineKeyboardButton(
                            self.l("passes-role-button-follower"),
                            callback_data=f"{self.base.name}|role|f",
                        ),
                    ],
                    [
                        InlineKeyboardButton(
                            self.l("passes-role-button-cancel"),
                            callback_data=f"{self.base.name}|role_exit",
                        ),
                    ],
                ]
            ),
        )

    async def inform_pass_cancelled(self):
        await self.update.reply(
            await self.format_message("passes-pass-cancelled-by-other"),
            parse_mode=ParseMode.HTML,
        )

    async def cancel_pass(self) -> bool:
        user = await self.base.user_db.find_one(
            {
                "user_id": self.update.user,
                "bot_id": self.bot,
                self.pass_key + ".state": {"$ne": "payed"},
            }
        )
        uids = [self.update.user]
        if (
            user is not None
            and "couple" in user[self.pass_key]
            and user[self.pass_key]["state"] != "waiting-for-couple"
        ):
            uids.append(user[self.pass_key]["couple"])
            upd = await self.base.create_update_from_user(user[self.pass_key]["couple"])
            upd.set_pass_key(self.pass_key)
            await upd.inform_pass_cancelled()
        result = await self.base.user_db.update_many(
            {
                "user_id": {"$in": uids},
                "bot_id": self.bot,
                self.pass_key + ".state": {"$ne": "payed"},
            },
            {
                "$unset": {
                    self.pass_key: "",
                }
            },
        )
        return result.modified_count > 0

    async def notify_no_more_passes(self, pass_key):
        self.set_pass_key(pass_key)
        await self.base.user_db.update_one(
            {
                "user_id": self.update.user,
                "bot_id": self.bot,
                self.pass_key + ".state": "waitlist",
            },
            {
                "$set": {
                    self.pass_key + ".no_more_passes_notification_sent": now_msk(),
                }
            },
        )
        user = await self.get_user()
        await self.show_pass_edit(user, user[self.pass_key], "passes-added-to-waitlist")

    async def notify_deadline_close(self, suffix: str = ""):
        result = await self.base.user_db.update_one(
            {
                "user_id": self.update.user,
                "bot_id": self.bot,
                self.pass_key + ".state": "assigned",
                self.pass_key + ".notified_deadline_close" + suffix: {"$exists": False},
            },
            {
                "$set": {
                    self.pass_key + ".notified_deadline_close" + suffix: now_msk(),
                }
            },
        )
        if result.modified_count > 0:
            await self.update.reply(
                self.l("passes-payment-deadline-close", passKey=self.pass_key),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )

    async def decline_due_deadline(self, pass_key):
        self.set_pass_key(pass_key)
        result = await self.base.user_db.update_one(
            {
                "user_id": self.update.user,
                "bot_id": self.update.bot.id,
                self.pass_key + ".couple": {"$exists": True},
            },
            {
                "$unset": {self.pass_key + ".couple": ""},
            },
        )
        if result.modified_count > 0:
            await self.update.reply(
                await self.format_message("passes-couple-didnt-answer"),
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-solo"),
                                callback_data=f"{self.base.name}|solo|{self.pass_key}",
                            ),
                            InlineKeyboardButton(
                                self.l("passes-button-couple"),
                                callback_data=f"{self.base.name}|couple|{self.pass_key}",
                            ),
                        ],
                        [
                            InlineKeyboardButton(
                                self.l("passes-button-cancel"),
                                callback_data=f"{self.base.name}|pass_exit",
                            ),
                        ],
                    ]
                ),
                parse_mode=ParseMode.HTML,
            )

    async def decline_invitation_due_deadline(self, pass_key):
        self.set_pass_key(pass_key)
        await self.update.reply(
            await self.format_message("passes-invitation-timeout"),
            parse_mode=ParseMode.HTML,
        )

    async def cancel_due_deadline(self, pass_key):
        self.set_pass_key(pass_key)
        success = await self.cancel_pass()
        if success:
            await self.update.reply(
                self.l("passes-payment-deadline-exceeded"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )

    async def handle_passes_assign(self):
        assert self.update.user in self.base.config.telegram.admins, (
            f"{self.update.user} is not admin"
        )
        args_list = self.update.parse_cmd_arguments()

        parser = SilentArgumentParser()
        parser.add_argument("--pass_key", type=str, help="Pass key")
        parser.add_argument("--price", type=int, help="Custom price")
        parser.add_argument("--type", type=str, help="Custom type")
        parser.add_argument("--create_last", action="store_true", help="Create using last settings")
        parser.add_argument(
            "--leader",
            action="store_true",
            help="Create if not exists, with role leader",
        )
        parser.add_argument(
            "--follower",
            action="store_true",
            help="Create if not exists, with role follower",
        )
        parser.add_argument(
            "--create_name", type=str, help="Create with this legal name"
        )
        parser.add_argument("--skip", action="store_true", help="Skip in balance count")
        parser.add_argument("--comment", type=str, help="Comment message")
        parser.add_argument("recipients", nargs="*", help="Recipients")

        args = parser.parse_args(args_list[1:])
        logger.debug(f"passes_assign {args=}, {args_list=}")
        assert args.pass_key in PASS_KEYS, f"wrong pass key {args.pass_key}"
        assert args.price is None or args.price >= 0, f"wrong price {args.price}"
        assigned = []
        for recipient in args.recipients:
            try:
                user_id = int(recipient)
                user = await self.base.user_db.find_one(
                    {
                        "user_id": user_id,
                        "bot_id": self.bot,
                        args.pass_key + ".state": "waitlist",
                    }
                )
                if user is None:
                    if ((
                        args.leader is not None or args.follower is not None
                    ) and args.create_name is not None):
                        await self.base.user_db.update_one(
                            {
                                "user_id": user_id,
                                "bot_id": self.bot,
                            },
                            {
                                "$set": {
                                    "legal_name": args.create_name,
                                    args.pass_key: {
                                        "state": "waitlist",
                                        "type": "solo",
                                        "proof_admin": self.base.payment_admins[
                                            args.pass_key
                                        ][0],
                                        "role": "leader" if args.leader else "follower",
                                        "date_created": now_msk(),
                                    },
                                },
                            },
                        )
                    elif args.create_last:
                        user = await self.base.user_db.find_one(
                            {
                                "user_id": user_id,
                                "bot_id": self.bot,
                            }
                        )
                        last_role = user.get(
                            "role",
                            user.get("pass_2025_1", {}).get(
                                "role",
                                user.get("pass_2025_2", {}).get(
                                    "role", None,
                                )))
                        if last_role is None:
                            raise ValueError(
                                f"user {user_id} has no last role to create pass from"
                            )
                        await self.base.user_db.update_one(
                            {
                                "user_id": user_id,
                                "bot_id": self.bot,
                            },
                            {
                                "$set": {
                                    args.pass_key: {
                                        "state": "waitlist",
                                        "type": "solo",
                                        "proof_admin": self.base.payment_admins[
                                            args.pass_key
                                        ][0],
                                        "role": last_role,
                                        "date_created": now_msk(),
                                    },
                                },
                            },
                        )
                    else:
                        raise ValueError(
                            f"user {user_id} with pass {args.pass_key} not found"
                        )
                upd = await self.base.create_update_from_user(user_id)

                await upd.assign_pass(
                    args.pass_key, args.price, args.type, args.comment, args.skip, self.update.user
                )
                logger.info(f"pass {args.pass_key=} assigned to {recipient=}")
                assigned.append(user_id)
            except Exception as err:
                logger.error(f"passes_assign {err=}, {recipient=}", exc_info=1)
        await self.update.reply(
            f"passes_assign done: {assigned}", parse_mode=ParseMode.HTML
        )
        await self.base.recalculate_queues()
    
    async def handle_passes_uncouple(self):
        """Admin command: /passes_uncouple <pass_key> <user_id>
        Splits a couple pass into two solo passes for the specified user and their partner.
        Logic:
          - Validate admin rights
          - Load user doc with couple pass (type == couple)
          - Load coupled user doc; ensure both have pass entries
          - Split total price in half (integer division) if present
          - Update both passes: set type solo, remove couple link, set new price
        """
        assert self.update.user in self.base.config.telegram.admins, (
            f"{self.update.user} is not admin"
        )
        args_list = self.update.parse_cmd_arguments()
        # Expect: ['/passes_uncouple', pass_key, user_id]
        if len(args_list) < 3:
            return await self.update.reply(
                "Usage: /passes_uncouple <pass_key> <user_id>",
                parse_mode=None,
            )
        pass_key = args_list[1]
        if pass_key not in PASS_KEYS:
            return await self.update.reply(
                f"Unknown pass_key {pass_key}", parse_mode=None
            )
        self.set_pass_key(pass_key)
        try:
            target_user_id = int(args_list[2])
        except ValueError:
            return await self.update.reply(
                f"Invalid user id: {args_list[2]}", parse_mode=None
            )

        # Load target user with couple pass
        user = await self.base.user_db.find_one(
            {
                "user_id": target_user_id,
                "bot_id": self.bot,
                pass_key: {"$exists": True},
                pass_key + ".type": "couple",
            }
        )
        if user is None:
            return await self.update.reply(
                f"User {target_user_id} does not have a couple pass {pass_key}",
                parse_mode=None,
            )
        couple_id = user[pass_key].get("couple")
        if couple_id is None:
            return await self.update.reply(
                f"User {target_user_id} does not have a couple partner recorded", parse_mode=None
            )
        couple_user = await self.base.user_db.find_one(
            {
                "user_id": couple_id,
                "bot_id": self.bot,
                pass_key: {"$exists": True},
            }
        )
        if couple_user is None or couple_user.get(pass_key, {}).get("type") != "couple":
            return await self.update.reply(
                f"Couple user {couple_id} does not have a matching couple pass", parse_mode=None
            )
        # Optional: verify reciprocal link if present
        # Compute new price per person
        total_price = user[pass_key].get("price")
        new_price = None
        if isinstance(total_price, (int, float)) and total_price > 0:
            new_price = int(total_price / 2)

        # Prepare updates
        set_updates = {pass_key + ".type": "solo"}
        if new_price is not None:
            set_updates[pass_key + ".price"] = new_price

        # Update both users
        await self.base.user_db.update_one(
            {"user_id": target_user_id, "bot_id": self.bot},
            {"$set": set_updates, "$unset": {pass_key + ".couple": ""}},
        )
        await self.base.user_db.update_one(
            {"user_id": couple_id, "bot_id": self.bot},
            {"$set": set_updates, "$unset": {pass_key + ".couple": ""}},
        )
        await self.update.reply(
            f"Uncoupled users {target_user_id} and {couple_id} for pass {pass_key}. New price per user: {new_price if new_price is not None else 'unchanged'}",
            parse_mode=None,
        )
        await self.base.recalculate_queues()

    async def handle_passes_cancel(self):
        args_list = self.update.parse_cmd_arguments()

        parser = SilentArgumentParser()
        parser.add_argument("--pass_key", type=str, help="Pass key", default=PASS_BY)
        parser.add_argument("recipients", nargs="*", help="Recipients")

        args = parser.parse_args(args_list[1:])
        logger.debug(f"passes_cancel {args=}, {args_list=}")
        assert args.pass_key in PASS_KEYS, f"wrong pass key {args.pass_key}"
        self.set_pass_key(args.pass_key)
        assert (self.update.user in self.base.config.telegram.admins or
                self.base.is_payment_admin(self.update.user, self.pass_key)), (
            f"{self.update.user} is not admin"
        )
        cancelled = []
        recipients = map(int, args.recipients)
        for recipient in recipients:
            try:
                upd = await self.base.create_update_from_user(recipient)
                user = await upd.get_user()
                if self.pass_key not in user:
                    continue
                if user[self.pass_key]["type"] == "couple":
                    if user[self.pass_key]["couple"] not in recipients:
                        result = await self.base.user_db.update_one(
                            {
                                "user_id": user[self.pass_key]["couple"],
                                "bot_id": self.bot,
                                self.pass_key + ".couple": {"$eq": recipient},
                            },
                            {
                                "$unset": {
                                    self.pass_key + ".couple": "",
                                },
                                "$set": {
                                    self.pass_key + ".type": "solo",
                                },
                                "$mul": {self.pass_key + ".price": 0.5},
                            },
                        )
                        if result.modified_count > 0:
                            logger.info(
                                f"pass {args.pass_key=} for {recipient=} couple {user[self.pass_key]['couple']} was changed to solo"
                            )
                        else:
                            logger.error(
                                f"pass {args.pass_key=} for {recipient=} couple {user[self.pass_key]['couple']} was not changed to solo"
                            )
                result = await self.base.user_db.update_one(
                    {
                        "user_id": recipient,
                        "bot_id": self.bot,
                        self.pass_key: {"$exists": True},
                    },
                    {
                        "$unset": {
                            self.pass_key: "",
                        },
                    },
                )
                if result.modified_count <= 0:
                    logger.info(
                        f"pass {self.pass_key=} for {recipient=} was not cancelled"
                    )
                else:
                    logger.info(f"pass {self.pass_key=} cancelled for {recipient=}")
                    cancelled.append(recipient)
            except Exception as err:
                logger.error(f"passes_cancel {err=}, {recipient=}", exc_info=1)
        await self.update.reply(
            f"passes_cancel done: {cancelled}", parse_mode=ParseMode.HTML
        )
        await self.base.recalculate_queues()
    
    async def handle_passes_switch_to_me_cmd(self):
        args_list = self.update.parse_cmd_arguments()
        tail = args_list[1:]
        if len(tail) == 0:
            return await self.update.reply(
                "Usage: /passes_switch_to_me [<pass_key>=PASS_KEY] <user_id>",
                parse_mode=None,
            )
        pass_key = PASS_KEY
        target_user_raw = None
        if len(tail) == 1:
            target_user_raw = tail[0]
        else:
            if tail[0] in PASS_KEYS:
                pass_key = tail[0]
                target_user_raw = tail[1]
            else:
                target_user_raw = tail[0]
        if pass_key not in PASS_KEYS:
            return await self.update.reply(
                f"Unknown pass key {pass_key}", parse_mode=None
            )
        try:
            target_user_id = int(target_user_raw)
        except Exception:
            return await self.update.reply(
                "Invalid user id", parse_mode=None,
            )
        self.set_pass_key(pass_key)
        if not (
            self.update.user in self.base.config.telegram.admins
            or self.base.is_payment_admin(self.update.user, self.pass_key)
        ):
            return await self.update.reply(
                f"{self.update.user} is not admin for {self.pass_key}",
                parse_mode=None,
            )
        user_doc = await self.base.user_db.find_one(
            {
                "bot_id": self.bot,
                "user_id": target_user_id,
                self.pass_key: {"$exists": True},
            }
        )
        if user_doc is None:
            return await self.update.reply(
                f"User {target_user_id} does not have pass {self.pass_key}",
                parse_mode=None,
            )
        uids = [user_doc["user_id"]]
        if "couple" in user_doc[self.pass_key]:
            uids.append(user_doc[self.pass_key]["couple"])
        admin_doc = await self.base.user_db.find_one(
            {
                "bot_id": self.bot,
                "user_id": self.update.user,
            }
        )
        await self.base.user_db.update_many(
            {
                "bot_id": self.bot,
                "user_id": {"$in": uids},
                self.pass_key: {"$exists": True},
            },
            {
                "$set": {
                    self.pass_key + ".proof_admin": self.update.user,
                    "proof_admins." + self.pass_key: self.update.user,
                }
            },
        )
        for uid in uids:
            try:
                upd = await self.base.create_update_from_user(uid)
                upd.set_pass_key(self.pass_key)
                await upd.update.reply(
                    await upd.format_message(
                        "passes-admin-changed",
                        admin=admin_doc,
                    ),
                    parse_mode=ParseMode.HTML,
                )
            except Exception as err:
                logger.error(
                    "Failed to notify user %s about new admin: %s", uid, err, exc_info=1
                )
        await self.update.reply(
            f"Admin for pass {self.pass_key} switched to you for users {uids}",
            parse_mode=None,
        )
    
    async def handle_passes_table_cmd(self):
        if self.update.user in self.base.config.telegram.admins:
            pass_keys_for_this_user = PASS_KEYS
        else:
            pass_keys_for_this_user = [
                key
                for key in PASS_KEYS
                if key in self.base.payment_admins
                and self.base.is_payment_admin(self.update.user, key)
            ]
        assert pass_keys_for_this_user, (
            f"{self.update.user} is not admin for any pass key"
        )
        import openpyxl
        from openpyxl.styles import Font, Alignment

        wb = openpyxl.Workbook()
        ws = wb.active
        ws.title = "Passes"

        fields = {
            "user_id": {"name": "User ID", "location": "user"},
            "username": {"name": "Username", "location": "user"},
            "first_name": {"name": "First Name", "location": "user"},
            "last_name": {"name": "Last Name", "location": "user"},
            "print_name": {"name": "Print Name", "location": "user"},
            "legal_name": {"name": "Legal Name", "location": "user"},
            "language_code": {"name": "Language code", "location": "user", "length": 2},
            "pass_key": {
                "func": lambda u, pk: pk,
                "name": "Pass Key",
            },
            "state": {"name": "State", "location": "pass"},
            "type": {"name": "Type", "location": "pass"},
            "role": {"name": "Role", "location": "pass"},
            "couple": {"name": "Couple", "location": "pass"},
            "price": {"name": "Price", "location": "pass"},
            "price_per_one": {
                "func": lambda u, pk: u[pk].get("price",0) / 2 if u[pk].get('type') == 'couple' else u[pk].get('price',""),
                "name": "Price per one",
            },
            "date_created": {"name": "Date Created", "location": "pass"},
            "date_assignment": {"name": "Date Assignment", "location": "pass"},
            "proof_admin": {"name": "Proof Admin", "location": "pass"},
            "proof_received": {"name": "Proof Received", "location": "pass"},
            "proof_accepted": {"name": "Proof Accepted", "location": "pass"},
            "proof_rejected": {"name": "Proof Rejected", "location": "pass"},
            "proof_file": {"name": "Proof File ID", "location": "pass", "length": 5},
            "skip_in_balance_count": {"name": "Skip in Balance Count", "location": "pass"},
            "comment": {"name": "Comment", "location": "pass"},
        }

        bold = Font(bold=True)
        center = Alignment(horizontal="center")
        ws.append([field["name"] for field in fields.values()])
        for cell in ws["1:1"]:
            cell.font = bold
            cell.alignment = center

        for i, field in enumerate(fields.values()):
            field["column_number"] = i + 1
            field["column_letter"] = ws.cell(row=1, column=i + 1).column_letter

        for pass_key in pass_keys_for_this_user:
            async for user in self.base.user_db.find(
                    {
                        "bot_id": self.bot,
                        pass_key: {"$exists": True},
                    }
                ):
                    row = []
                    for field, info in fields.items():
                        if "func" in info:
                            row.append(info["func"](user, pass_key))
                        elif info["location"] == "user":
                            row.append(user.get(field, ""))
                        elif info["location"] == "pass":
                            row.append(user[pass_key].get(field, ""))
                    ws.append(row)
        ws.freeze_panes = "A2"
        ws.auto_filter.ref = ws.dimensions
        ws.auto_filter.add_sort_condition(fields["date_created"]["column_letter"] + "2:" + fields["date_created"]["column_letter"] + str(ws.max_row))
        for i, field in enumerate(fields.values()):
            if "length" in field:
                ws.column_dimensions[field["column_letter"]].width = field["length"]
            else: # auto width
                max_length = max(
                    len(str(cell.value)) if cell.value is not None else 0
                    for cell in ws[field["column_letter"]]
                )
                ws.column_dimensions[field["column_letter"]].width = max(max_length, 3)

        wb.save("passes.xlsx")
        await self.update.bot.send_document(
            self.update.user,
            open("passes.xlsx", "rb"),
            caption="Passes table",
        )
        import os
        os.remove("passes.xlsx")


class Passes(BasePlugin):
    name = "passes"

    def __init__(self, base_app):
        super().__init__(base_app)
        self.base_app.passes = self
        self.payment_admins: dict[str, list[int]] = {key: [] for key in PASS_KEYS}
        self.hidden_payment_admins: dict[str, list[int]] = {
            key: [] for key in PASS_KEYS
        }
        for key in PASS_KEYS:
            if isinstance(self.config.passes.events[key].payment_admin, int):
                self.payment_admins[key].append(
                    self.config.passes.events[key].payment_admin
                )
            elif isinstance(self.config.passes.events[key].payment_admin, list):
                self.payment_admins[key].extend(
                    self.config.passes.events[key].payment_admin
                )
            hidden_admins = (
                self.config.passes.events[key].hidden_payment_admins
                if hasattr(self.config.passes.events[key], "hidden_payment_admins")
                else None
            )
            if isinstance(hidden_admins, int):
                self.hidden_payment_admins[key].append(hidden_admins)
            elif isinstance(hidden_admins, list):
                self.hidden_payment_admins[key].extend(hidden_admins)
        self.user_db: AgnosticCollection = base_app.users_collection
        self._command_checkers = [
            CommandHandler(self.name, self.handle_start),
            CommandHandler("passes_assign", self.handle_passes_assign_cmd),
            CommandHandler("passes_cancel", self.handle_passes_cancel_cmd),
            CommandHandler("passes_switch_to_me", self.handle_passes_switch_to_me_cmd),
            CommandHandler("passes_uncouple", self.handle_passes_uncouple_cmd),
            CommandHandler("passes_table", self.handle_passes_table_cmd),
            CommandHandler("legal_name", self.handle_name_cmd),
            CommandHandler("passport", self.handle_passport_data_cmd),
            CommandHandler("role", self.handle_role_cmd),
        ]
        self._cbq_handler = CallbackQueryHandler(
            self.handle_callback_query, pattern=f"^{self.name}\\|.*"
        )
        self._queue_lock = Lock()
        create_task(self._timeout_processor())

    def get_all_payment_admins(self, pass_key: str) -> list[int]:
        admins = self.payment_admins.get(pass_key, [])
        hidden = self.hidden_payment_admins.get(pass_key, [])
        # dict.fromkeys preserves order while removing duplicates
        return list(dict.fromkeys(admins + hidden))

    def is_payment_admin(self, user_id: int, pass_key: str) -> bool:
        return user_id in self.get_all_payment_admins(pass_key)

    def pick_payment_admin(self, pass_key: str) -> int | None:
        visible = self.payment_admins.get(pass_key, [])
        if len(visible) > 0:
            return choice(visible)
        hidden = self.hidden_payment_admins.get(pass_key, [])
        if len(hidden) > 0:
            logger.warning(
                "No visible payment_admins for %s, falling back to hidden_payment_admins",
                pass_key,
            )
            return choice(hidden)
        raise Exception("No payment admins configured for pass %s", pass_key)

    async def get_all_passes(self, pass_key: str = PASS_RU, with_unpaid: bool = False, with_waitlist: bool = False) -> list[dict]:
        states = ["payed"]
        if with_unpaid:
            states.append("assigned")
        query = {
            "bot_id": self.bot.id,
            pass_key: {"$exists": True},
        }
        if not with_waitlist:
            query[pass_key + ".state"] = {"$in": states}
        return await self.user_db.find(query).to_list(None)

    async def _timeout_processor(self) -> None:
        bot_started: Event = self.base_app.bot_started
        await bot_started.wait()
        logger.info("timeout processor started")
        for pass_key in PASS_KEYS:
            async for user in self.user_db.find(
                {
                    "bot_id": self.bot.id,
                    pass_key: {"$exists": True},
                    pass_key + ".proof_admin": {"$exists": False},
                }
            ):
                try:
                    new_admin = self.pick_payment_admin(pass_key)
                    await self.user_db.update_one(
                        {
                            "user_id": user["user_id"],
                            "bot_id": self.bot.id,
                        },
                        {
                            "$set": {
                                pass_key + ".proof_admin": new_admin,
                            }
                        },
                    )
                except Exception as e:
                    logger.error(
                        "Exception in Passes._timeout_processor setting initial proof_admin "+
                        f"for user {user['user_id']}, pass {pass_key}: {e}",
                        exc_info=1,
                    )
        processed_waiting: set[int] = set()
        for pass_key in PASS_KEYS:
            async for user in self.user_db.find(
                {
                    "bot_id": self.bot.id,
                    pass_key + ".state": "waitlist",
                    pass_key: {"$exists": True},
                }
            ):
                try:
                    if user["user_id"] in processed_waiting:
                        continue
                    uids = [user["user_id"]]
                    if "couple" in user[pass_key]:
                        uids.append(user[pass_key]["couple"])
                    processed_waiting.update(uids)
                    current_admin = user[pass_key].get("proof_admin")
                    if current_admin in self.get_all_payment_admins(pass_key):
                        continue
                    new_admin = self.pick_payment_admin(pass_key)
                    if new_admin is None:
                        logger.error(
                            "Cannot reassign proof_admin for waitlist user %s, pass %s: no admins configured",
                            user["user_id"],
                            pass_key,
                        )
                        continue
                    await self.user_db.update_many(
                        {
                            "bot_id": self.bot.id,
                            "user_id": {"$in": uids},
                            pass_key + ".state": "waitlist",
                        },
                        {
                            "$set": {
                                pass_key + ".proof_admin": new_admin,
                            }
                        },
                    )
                    admin_doc = await self.user_db.find_one(
                        {
                            "bot_id": self.bot.id,
                            "user_id": new_admin,
                        }
                    )
                    for uid in uids:
                        try:
                            upd = await self.create_update_from_user(uid)
                            upd.set_pass_key(pass_key)
                            await upd.update.reply(
                                await upd.format_message(
                                    "passes-admin-changed",
                                    admin=admin_doc,
                                ),
                                parse_mode=ParseMode.HTML,
                            )
                        except Exception as e:
                            logger.error(
                                "Failed to notify user %s about new admin for %s: %s",
                                uid,
                                pass_key,
                                e,
                                exc_info=1,
                            )
                except Exception as e:
                    logger.error(
                        "Exception in Passes._timeout_processor reassigning proof_admin "+
                        f"for waitlist user {user['user_id']}, pass {pass_key}: {e}",
                        exc_info=1,
                    )
        
        if self.config.passes.events[PASS_RU].require_passport:
            async for user in self.user_db.find(
                {
                    "bot_id": self.bot.id,
                    "passport_number": {"$exists": False},
                    PASS_RU: {"$exists": True},
                    PASS_RU + ".state": {"$in": ["assigned", "payed"]},
                    "notified_passport_data_required": {"$exists": False},
                }
            ):
                try:
                    upd = await self.create_update_from_user(user["user_id"])
                    upd.set_pass_key(PASS_RU)
                    await upd.require_passport_data()
                    await self.user_db.update_one(
                        {
                            "bot_id": self.bot.id,
                            "user_id": user["user_id"],
                            "passport_number": {"$exists": False},
                            PASS_RU: {"$exists": True},
                        },
                        {
                            "$set": {
                                "notified_passport_data_required": True
                            }
                        }
                    )
                except Exception as e:
                    logger.error(
                        "Exception in Passes._timeout_processor passport notification "+
                        f"for user {user['user_id']}: {e}",
                        exc_info=1,
                    )

        await self.recalculate_queues()
        while True:
            for pass_key in PASS_KEYS:
                try:
                    await sleep(TIMEOUT_PROCESSOR_TICK)
                    async for user in self.user_db.find(
                        {
                            "bot_id": self.bot.id,
                            pass_key + ".state": "assigned",
                            pass_key + ".notified_deadline_close": {
                                "$lt": now_msk()
                                - PAYMENT_TIMEOUT
                                + PAYMENT_TIMEOUT_NOTIFY,
                            },
                        }
                    ):
                        upd = await self.create_update_from_user(user["user_id"])
                        await upd.cancel_due_deadline(pass_key)
                    async for user in self.user_db.find(
                        {
                            "bot_id": self.bot.id,
                            pass_key + ".state": "waiting-for-couple",
                            pass_key + ".date_created": {
                                "$lt": now_msk() - INVITATION_TIMEOUT,
                            },
                        }
                    ):
                        upd = await self.create_update_from_user(user["user_id"])
                        await upd.decline_due_deadline(pass_key)
                        upd = await self.create_update_from_user(
                            user[pass_key]["couple"]
                        )
                        await upd.decline_invitation_due_deadline(pass_key)
                    async for user in self.user_db.find(
                        {
                            "bot_id": self.bot.id,
                            pass_key + ".state": "assigned",
                            pass_key + ".date_assignment": {
                                "$lt": now_msk() - PAYMENT_TIMEOUT_NOTIFY,
                            },
                            pass_key + ".notified_deadline_close": {"$exists": False},
                        }
                    ):
                        upd = await self.create_update_from_user(user["user_id"])
                        upd.set_pass_key(pass_key)
                        await upd.notify_deadline_close()
                    async for user in self.user_db.find(
                        {
                            "bot_id": self.bot.id,
                            pass_key + ".state": "assigned",
                            pass_key + ".notified_deadline_close": {
                                "$lt": now_msk()
                                - PAYMENT_TIMEOUT_NOTIFY2
                                + PAYMENT_TIMEOUT_NOTIFY,
                            },
                            pass_key + ".notified_deadline_close2": {"$exists": False},
                        }
                    ):
                        upd = await self.create_update_from_user(user["user_id"])
                        upd.set_pass_key(pass_key)
                        await upd.notify_deadline_close("2")
                except Exception as e:
                    logger.error(
                        "Exception in Passes._timeout_processor %s", e, exc_info=1
                    )

    async def recalculate_queues(self) -> None:
        for key in PASS_KEYS:
            await self.recalculate_queues_pk(key)

    async def recalculate_queues_pk(self, pass_key: str) -> None:
        try:
            natural_queue_exhausted = False
            while True:
                aggregation = await self.user_db.aggregate(
                    [
                        {
                            "$match": {
                                "$and": [
                                    {"bot_id": self.bot.id},
                                    {pass_key: {"$exists": True}},
                                    {
                                        "$or": [
                                            {
                                                pass_key + ".skip_in_balance_count": {
                                                    "$ne": True
                                                }
                                            },
                                            {
                                                pass_key + ".skip_in_balance_count": {
                                                    " $exists": False
                                                }
                                            },
                                        ]
                                    },
                                ],
                            },
                        },
                        {
                            "$group": {
                                "_id": {
                                    "state": f"${pass_key}.state",
                                    "role": f"${pass_key}.role",
                                },
                                "count": {"$count": {}},
                            }
                        },
                    ]
                ).to_list(None)
                couples = {
                    group["_id"]: group["count"]
                    for group in await self.user_db.aggregate(
                        [
                            {
                                "$match": {
                                    pass_key + ".couple": {"$exists": True},
                                    pass_key + ".role": "leader",
                                    "bot_id": self.bot.id,
                                },
                            },
                            {
                                "$group": {
                                    "_id": f"${pass_key}.state",
                                    "count": {"$count": {}},
                                }
                            },
                        ]
                    ).to_list(None)
                }
                counts = {
                    "leader": dict(),
                    "follower": dict(),
                }
                max_assigned = 0
                for group in aggregation:
                    if len(group["_id"]) == 0:
                        continue
                    counts[group["_id"]["role"]][group["_id"]["state"]] = group["count"]
                    if (
                        group["_id"]["state"] == "assigned"
                        and max_assigned < group["count"]
                    ):
                        max_assigned = group["count"]
                counts["leader"]["RA"] = counts["leader"].get("payed", 0) + counts[
                    "leader"
                ].get("assigned", 0)
                counts["follower"]["RA"] = counts["follower"].get("payed", 0) + counts[
                    "follower"
                ].get("assigned", 0)
                ra = (
                    counts["leader"]["RA"]
                    if counts["leader"]["RA"] >= counts["follower"]["RA"]
                    else counts["follower"]["RA"]
                )
                if ra > self.config.passes.events[pass_key].amount_cap_per_role:
                    break
                max_assigned -= couples.get("assigned", 0)
                if counts["leader"]["RA"] != counts["follower"]["RA"]:
                    target_group = (
                        "follower"
                        if counts["leader"]["RA"] > counts["follower"]["RA"]
                        else "leader"
                    )
                    success = await self.assign_pass(target_group, pass_key, counts)
                    if not success:
                        natural_queue_exhausted = True
                        break
                    continue
                else:
                    if max_assigned > MAX_CONCURRENT_ASSIGNMENTS:
                        break
                    target_group = (
                        "follower"
                        if counts["leader"].get("waitlist", 0)
                        > counts["follower"].get("waitlist", 0)
                        else "leader"
                    )
                    success = await self.assign_pass(target_group, pass_key, counts)
                    if not success:
                        natural_queue_exhausted = True
                        break
                    continue
            if natural_queue_exhausted:
                while True:
                    aggregation = await self.user_db.aggregate(
                        [
                            {
                                "$match": {
                                    pass_key: {"$exists": True},
                                    "bot_id": self.bot.id,
                                },
                            },
                            {
                                "$group": {
                                    "_id": {
                                        "state": f"${pass_key}.state",
                                        "role": f"${pass_key}.role",
                                    },
                                    "count": {"$count": {}},
                                }
                            },
                        ]
                    ).to_list(None)
                    counts = {
                        "leader": dict(),
                        "follower": dict(),
                    }
                    for group in aggregation:
                        if len(group["_id"]) == 0:
                            continue
                        counts[group["_id"]["role"]][group["_id"]["state"]] = group[
                            "count"
                        ]
                    counts["leader"]["RA"] = counts["leader"].get("payed", 0) + counts[
                        "leader"
                    ].get("assigned", 0)
                    counts["follower"]["RA"] = counts["follower"].get(
                        "payed", 0
                    ) + counts["follower"].get("assigned", 0)
                    ra = (
                        counts["leader"]["RA"]
                        if counts["leader"]["RA"] >= counts["follower"]["RA"]
                        else counts["follower"]["RA"]
                    )
                    if ra > self.config.passes.events[pass_key].amount_cap_per_role:
                        break
                    success = await self.assign_pass("couple", pass_key, counts)
                    if not success:
                        break
                    continue

            have_unnotified = True
            while have_unnotified:
                selected = await self.user_db.find(
                    {
                        "bot_id": self.bot.id,
                        pass_key + ".state": "waitlist",
                        pass_key + ".no_more_passes_notification_sent": {
                            "$exists": False
                        },
                    }
                ).to_list(100)
                if len(selected) == 0:
                    logger.info("no unnotified candidates in the waiting list")
                    break
                for user in selected:
                    upd = await self.create_update_from_user(user["user_id"])
                    await upd.notify_no_more_passes(pass_key)
        except Exception as e:
            logger.error(f"Exception in recalculate_queues: {e}", exc_info=1)

        try:
            async for user in self.user_db.find(
                {
                    "bot_id": self.bot.id,
                    pass_key: {"$exists": True},
                    pass_key + ".sent_to_hype_thread": {"$exists": False},
                }
            ):
                await self.user_db.update_one(
                    {
                        "user_id": user["user_id"],
                        "bot_id": self.bot.id,
                        pass_key: {"$exists": True},
                    },
                    {
                        "$set": {
                            pass_key + ".sent_to_hype_thread": now_msk(),
                        }
                    },
                )
                if self.config.passes.events[pass_key].thread_channel != "":
                    try:
                        ch = self.config.passes.events[pass_key].thread_channel
                        if isinstance(ch, str):
                            ch = "@" + ch
                        logger.debug(f"chat id: {ch}, type {type(ch)}")
                        args = {
                            "name": client_user_name(user),
                            "role": user[pass_key]["role"],
                            "passKey": pass_key,
                        }
                        await self.bot.send_message(
                            chat_id=ch,
                            message_thread_id=self.config.passes.events[
                                pass_key
                            ].thread_id,
                            text=self.base_app.localization(
                                "passes-announce-user-registered",
                                args=args,
                                locale=self.config.passes.events[
                                    pass_key
                                ].thread_locale,
                            ),
                            parse_mode=ParseMode.HTML,
                        )
                    except Exception as e:
                        logger.error(
                            f"Exception in recalculate_queues: {e}", exc_info=1
                        )
        except Exception as e:
            logger.error(f"Exception in recalculate_queues: {e}", exc_info=1)

    async def assign_pass(
        self,
        role: str,
        pass_key: str,
        counts: dict[str, dict[str, int]] | None = None,
    ) -> bool:
        match = {
            "bot_id": self.bot.id,
            pass_key + ".state": "waitlist",
        }
        if role == "couple":
            match[pass_key + ".role"] = "leader"
            match[pass_key + ".couple"] = {"$exists": True}
        else:
            match[pass_key + ".role"] = role
        pipeline = [
            {"$match": match},
            {"$sort": {pass_key + ".date_created": 1, "user_id": 1}},
        ]
        cap_per_role = self.config.passes.events[pass_key].amount_cap_per_role
        role_counts = counts or {}
        leader_ra = role_counts.get("leader", {}).get("RA", 0)
        follower_ra = role_counts.get("follower", {}).get("RA", 0)
        scanned = 0
        async for candidate in self.user_db.aggregate(pipeline):
            scanned += 1
            pass_info = candidate.get(pass_key, {})
            is_couple = "couple" in pass_info
            if counts is not None:
                if is_couple or role == "couple":
                    if leader_ra >= cap_per_role or follower_ra >= cap_per_role:
                        continue
                elif role_counts.get(role, {}).get("RA", 0) >= cap_per_role:
                    return False
            upd = await self.create_update_from_user(candidate["user_id"])
            assigned = await upd.assign_pass(pass_key)
            if assigned:
                return True
            if QUEUE_LOOKAHEAD and scanned >= QUEUE_LOOKAHEAD:
                break
        return False

    async def create_update_from_user(self, user: int) -> PassUpdate:
        upd = TGState(user, self.base_app)
        await upd.get_state()
        return PassUpdate(self, upd)

    def test_message(self, message: Update, state, web_app_data):
        for checker in self._command_checkers:
            if checker.check_update(message):
                return PRIORITY_BASIC, checker.callback
        return PRIORITY_NOT_ACCEPTING, None

    def test_callback_query(self, query: Update, state):
        if self._cbq_handler.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None

    def create_update(self, update) -> PassUpdate:
        return PassUpdate(self, update)
    
    async def handle_passport_data_cmd(self, update: TGState):
        return await self.create_update(update).handle_passport_data_cmd()

    async def handle_callback_query(self, updater):
        return await self.create_update(updater).handle_callback_query()

    async def handle_start(self, update: TGState):
        return await self.create_update(update).handle_start()

    async def handle_name_cmd(self, update: TGState):
        return await self.create_update(update).handle_cq_name()

    async def handle_passes_assign_cmd(self, update: TGState):
        return await self.create_update(update).handle_passes_assign()

    async def handle_passes_cancel_cmd(self, update: TGState):
        return await self.create_update(update).handle_passes_cancel()
    
    async def handle_passes_switch_to_me_cmd(self, update: TGState):
        return await self.create_update(update).handle_passes_switch_to_me_cmd()
    
    async def handle_passes_uncouple_cmd(self, update: TGState):
        return await self.create_update(update).handle_passes_uncouple()

    async def handle_role_cmd(self, update: TGState):
        return await self.create_update(update).handle_role_cmd()

    async def handle_legal_name_input(self, update, data):
        return await self.create_update(update).handle_legal_name_input(data)

    async def handle_legal_name_timeout(self, update, data):
        return await self.create_update(update).handle_legal_name_timeout(data)

    async def handle_passport_input(self, update, data):
        return await self.create_update(update).handle_passport_input(data)

    async def handle_passport_timeout(self, update, data):
        return await self.create_update(update).handle_passport_timeout(data)

    async def handle_payment_proof_input(self, update, data):
        return await self.create_update(update).handle_payment_proof_input(data)

    async def handle_couple_input(self, update, data):
        return await self.create_update(update).handle_couple_input(data)

    async def handle_couple_timeout(self, update, data):
        return await self.create_update(update).handle_couple_timeout(data)

    async def handle_payment_proof_timeout(self, update, data):
        return await self.create_update(update).handle_payment_proof_timeout(data)
    
    async def handle_passes_table_cmd(self, update: TGState):
        return await self.create_update(update).handle_passes_table_cmd()
