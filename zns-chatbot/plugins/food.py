from asyncio import Event, create_task, sleep
import datetime
import json
import logging
import csv
import io
from random import choice

from bson import ObjectId
from motor.core import AgnosticCollection
from telegram import (
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    ReplyKeyboardMarkup,
    ReplyKeyboardRemove,
    Update,
    WebAppInfo,
    InputFile,
)
from telegram.constants import ParseMode
from telegram.error import TelegramError
from telegram.ext import CallbackQueryHandler, CommandHandler

from ..config import full_link
from ..plugins.pass_keys import PASS_KEY
from ..plugins.massage import now_msk
from ..telegram_links import client_user_link_html, client_user_name
from ..tg_state import TGState
from .base_plugin import PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING, BasePlugin

logger = logging.getLogger(__name__)

LUNCH_NO_SOUP_PRICE = 555
LUNCH_WITH_SOUP_PRICE = 665
LUNCH_SUB_CATEGORIES = ["main", "side", "salad"]
CANCEL_CHR = chr(0xE007F)
ACTIVITIES = ["open", "yoga", "cacao", "soundhealing"]
CLASS_ACTIVITIES = ["yoga", "cacao", "soundhealing"]
NOTIFICATION_CHECK_INTERVAL = 30  # 30 seconds


class FoodUpdate:
    base: "Food"
    update: TGState
    bot: int
    user: dict | None
    pass_key: str

    def __init__(self, base: "Food", update: TGState) -> None:
        self.base = base
        self.update = update
        self.l = update.l  # noqa: E741
        self.tgUpdate = update.update
        self.bot = self.update.bot.id
        self.user = None
        self.pass_key = PASS_KEY

    async def get_user(self):
        if self.user is None:
            self.user = await self.update.get_user()
        return self.user

    async def handle_start(self):
        user = await self.get_user()
        if not user:
            await self.update.reply(self.l("user-is-none"), parse_mode=ParseMode.HTML)
            return

        food_order = await self.base.get_order(user["user_id"], self.pass_key)

        (
            initial_message_text,
            initial_buttons_list,
        ) = await self._prepare_order_message_and_buttons(food_order, self.pass_key)

        sent_message = await self.update.reply(
            initial_message_text,
            reply_markup=InlineKeyboardMarkup(initial_buttons_list)
            if initial_buttons_list
            else None,
            parse_mode=ParseMode.HTML,
        )

        if sent_message:
            actual_msg_id = sent_message.message_id
            actual_chat_id = sent_message.chat.id

            _, final_buttons_list = await self._prepare_order_message_and_buttons(
                food_order, self.pass_key, actual_msg_id, actual_chat_id
            )

            try:
                await self.update.bot.edit_message_reply_markup(
                    chat_id=actual_chat_id,
                    message_id=actual_msg_id,
                    reply_markup=InlineKeyboardMarkup(final_buttons_list),
                )
            except Exception as e:
                logger.error(
                    f"Failed to edit reply markup for message {actual_msg_id} in chat"
                    f" {actual_chat_id} for user {user.get('user_id')}: {e}. WebApp URL"
                    " will not have full origin context."
                )
        else:
            logger.error(
                f"Failed to send initial message in handle_start for user "
                f"{user.get('user_id') if user else 'unknown'}"
            )

    async def _prepare_order_message_and_buttons(
        self,
        order_doc: dict | None,
        pass_key: str,
        orig_msg_id: int | None = None,
        orig_chat_id: int | None = None,
    ) -> tuple[str, list]:
        """
        Prepares the message text and inline buttons based on the order document.
        Includes origin message/chat IDs in WebApp URLs if provided.
        """
        buttons = []
        message_text = ""
        if not order_doc and self.base.deadline < now_msk():
            # If no order and deadline has passed, show not accepting orders
            message_text = self.l("food-not-accepting-orders")
            return message_text, buttons

        # Base parameters for the WebApp URL
        menu_url_params_base = f"pass_key={pass_key}"
        if order_doc and order_doc.get("_id"):
            menu_url_params_base += f"&order_id={str(order_doc['_id'])}"

        menu_url_params_base += f"&lang={self.update.language_code}"

        # Add origin_info to the button URLs if available
        final_menu_url_params = menu_url_params_base
        if orig_msg_id is not None and orig_chat_id is not None:
            final_menu_url_params += (
                f"&orig_msg_id={orig_msg_id}&orig_chat_id={orig_chat_id}"
            )

        menu_url_for_buttons = full_link(
            self.base.base_app, f"/menu?{final_menu_url_params}"
        )

        if order_doc:
            order_total = order_doc.get("total", 0.0)
            order_id_str = str(order_doc["_id"])

            if order_doc.get("payment_status") == "paid":
                message_text = self.l("food-order-is-paid", total=order_total)
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-view-order"),
                            web_app=WebAppInfo(menu_url_for_buttons),
                        )
                    ]
                )
            elif order_doc.get("payment_status") == "proof_submitted":
                message_text = self.l("food-order-proof-submitted", total=order_total)
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-view-order"),
                            web_app=WebAppInfo(menu_url_for_buttons),
                        )
                    ]
                )
            elif (
                order_total == 0.0
                or (
                    not order_doc.get("order_details")
                    and not order_doc.get("is_complete")
                )  # payment_status not in ["paid", "proof_submitted"] is implicit here
            ):
                message_text = self.l("food-no-order")
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-create-order"),
                            web_app=WebAppInfo(menu_url_for_buttons),
                        )
                    ]
                )
            elif order_doc.get("is_complete"):
                message_text = self.l("food-order-exists-payable", total=order_total)
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-pay"),
                            callback_data=f"{self.base.name}|pay|{order_id_str}",
                        )
                    ]
                )
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-edit-order"),
                            web_app=WebAppInfo(menu_url_for_buttons),
                        )
                    ]
                )
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-delete-order"),
                            callback_data=f"{self.base.name}|delete_order|{order_id_str}",
                        )
                    ]
                )
            else:  # Order exists but not complete (and total > 0, not paid/proof_submitted)
                message_text = self.l(
                    "food-order-exists-not-complete", total=order_total
                )
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-edit-order"),
                            web_app=WebAppInfo(menu_url_for_buttons),
                        )
                    ]
                )
                buttons.append(
                    [
                        InlineKeyboardButton(
                            self.l("food-button-delete-order"),
                            callback_data=f"{self.base.name}|delete_order|{order_id_str}",
                        )
                    ]
                )
        else:
            message_text = self.l("food-no-order")
            buttons.append(
                [
                    InlineKeyboardButton(
                        self.l("food-button-create-order"),
                        web_app=WebAppInfo(menu_url_for_buttons),
                    )
                ]
            )

        buttons.append(
            [
                InlineKeyboardButton(
                    self.l("food-button-exit"), callback_data=f"{self.base.name}|exit"
                )
            ]
        )
        return message_text, buttons

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
        keys["banks"] = admin.get(f"banks_{lc}", admin.get("banks_en", ""))
        return keys

    async def handle_cq_pay(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(
                self.l("food-order-not-found"), parse_mode=ParseMode.HTML
            )
            return

        order = await self.base.food_db.find_one(
            {"_id": order_id, "user_id": self.update.user}
        )

        if now_msk() > self.base.deadline:
            await self.update.reply(
                self.l("food-not-accepting-orders"), parse_mode=ParseMode.HTML
            )
            return

        if not order:
            await self.update.reply(
                self.l("food-order-not-found"), parse_mode=ParseMode.HTML
            )
            return

        if not order.get("is_complete"):
            await self.update.reply(
                self.l("food-order-not-complete-for-payment"), parse_mode=ParseMode.HTML
            )
            return

        payment_status = order.get("payment_status")
        order_total = order.get("total", 99999.0)

        if payment_status == "paid":
            await self.update.reply(
                self.l("food-order-already-paid"), parse_mode=ParseMode.HTML
            )
            return
        if payment_status == "proof_submitted":
            await self.update.reply(
                self.l("food-order-proof-already-submitted"), parse_mode=ParseMode.HTML
            )
            return

        if not order.get("proof_admin"):
            if self.base.food_admins:
                assigned_admin_id = choice(self.base.food_admins)
                await self.base.food_db.update_one(
                    {"_id": order_id}, {"$set": {"proof_admin": assigned_admin_id}}
                )
                order["proof_admin"] = assigned_admin_id
            else:
                logger.error("No food_admins configured to assign for payment.")
                await self.update.reply(
                    self.l("food-payment-admins-not-configured"),
                    parse_mode=ParseMode.HTML,
                )
                return

        proof_admin_id = order.get("proof_admin")
        if not proof_admin_id:
            logger.error(
                f"Order {order_id} has no proof_admin after attempting assignment."
            )
            await self.update.reply(
                self.l("food-payment-admins-not-configured"), parse_mode=ParseMode.HTML
            )
            return

        proof_admin_user_obj = await self.base.user_db.find_one(
            {"user_id": proof_admin_id, "bot_id": self.bot}
        )

        if not proof_admin_user_obj:
            logger.error(
                f"Food payment admin user object not found for ID: {proof_admin_id}"
            )
            await self.update.reply(
                self.l("food-payment-admin-error"), parse_mode=ParseMode.HTML
            )
            return

        payment_detail_keys = self.admin_to_keys(
            proof_admin_user_obj, self.update.language_code
        )
        payment_detail_keys["total"] = order_total

        await self.update.edit_message_text(
            self.l("food-payment-request-callback-message", **payment_detail_keys),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
            disable_web_page_preview=True,
        )

        await self.update.reply(
            self.l("food-payment-request-waiting-message", **payment_detail_keys),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup(
                [[CANCEL_CHR + self.l("cancel-command")]], resize_keyboard=True
            ),
            disable_web_page_preview=True,
        )
        await self.update.require_anything(
            self.base.name,
            "handle_payment_proof_input",
            str(order_id),
            "handle_payment_proof_timeout",
        )

    async def handle_payment_proof_input(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(
                self.l("food-order-not-found"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        if (
            self.update.message
            and self.update.message.text
            and self.update.message.text.startswith(CANCEL_CHR)
        ):
            await self.update.reply(
                self.l("food-payment-proof-cancelled"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        if not (
            self.update.message
            and (self.update.message.photo or self.update.message.document)
        ):
            await self.update.reply(
                self.l("food-payment-proof-wrong-data"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        file_id = ""
        file_ext = ".dat"  # Default extension
        if self.update.message.photo:
            file_id = self.update.message.photo[-1].file_id
            file_ext = ".jpg"
        elif self.update.message.document:
            doc = self.update.message.document
            file_id = doc.file_id
            if doc.mime_type == "application/pdf":
                file_ext = ".pdf"
            elif doc.file_name:
                import os

                _, ext = os.path.splitext(doc.file_name)
                if ext:
                    file_ext = ext.lower()

        order = await self.base.food_db.find_one(
            {"_id": order_id, "user_id": self.update.user}
        )
        if (
            not order
            or order.get("payment_status") not in [None, "rejected"]
            or not order.get("proof_admin")
        ):
            await self.update.reply(
                self.l("food-cannot-submit-proof-now"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        proof_admin_id = order.get("proof_admin")
        if not proof_admin_id:
            logger.error(
                f"Order {order_id} missing proof_admin during proof submission."
            )
            await self.update.reply(
                self.l("food-payment-admin-error"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        update_result = await self.base.food_db.update_one(
            {"_id": order_id},
            {
                "$set": {
                    "payment_status": "proof_submitted",
                    "proof_file": f"{file_id}{file_ext}",
                    "proof_received_date": datetime.datetime.now(datetime.timezone.utc),
                }
            },
        )

        if update_result.modified_count == 0:
            await self.update.reply(
                self.l("food-order-proof-already-submitted"),
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        await self.update.reply(
            self.l("food-payment-proof-forwarded"),
            reply_markup=ReplyKeyboardRemove(),
            parse_mode=ParseMode.HTML,
        )

        admin_user_obj = await self.base.user_db.find_one(
            {"user_id": proof_admin_id, "bot_id": self.bot}
        )
        client_user_obj = await self.get_user()

        if admin_user_obj and client_user_obj:
            admin_lang = admin_user_obj.get("language_code", "en")

            admin_tg_state = TGState(
                admin_user_obj["user_id"],
                self.base.base_app,
            )
            admin_tg_state.language_code = admin_lang
            admin_l = admin_tg_state.l

            forward_message = await self.update.forward_message(proof_admin_id)
            if forward_message:
                await self.update.bot.send_message(
                    chat_id=proof_admin_id,
                    text=admin_l(
                        "food-adm-payment-proof-received",
                        link=client_user_link_html(
                            client_user_obj, language_code=admin_lang
                        ),
                        total=order.get("total", 99999.0),
                    ),
                    reply_markup=InlineKeyboardMarkup(
                        [
                            [
                                InlineKeyboardButton(
                                    admin_l("food-adm-payment-proof-accept-button"),
                                    callback_data=f"{self.base.name}|adm_acc|{order_id_str}",
                                ),
                                InlineKeyboardButton(
                                    admin_l("food-adm-payment-proof-reject-button"),
                                    callback_data=f"{self.base.name}|adm_rej|{order_id_str}",
                                ),
                            ]
                        ]
                    ),
                    parse_mode=ParseMode.HTML,
                    disable_web_page_preview=True,
                )
            else:
                logger.error(
                    f"Failed to forward payment proof message for order {order_id_str} to admin"
                    f" {proof_admin_id}"
                )
        else:
            logger.error(
                f"Could not find admin ({proof_admin_id}) or client ({self.update.user}) for"
                f" payment proof notification of order {order_id_str}"
            )

    async def handle_payment_proof_timeout(self, order_id_str: str):
        await self.update.reply(
            self.l("food-payment-proof-timeout"),
            reply_markup=ReplyKeyboardRemove(),
            parse_mode=ParseMode.HTML,
        )

    async def notify_payment_status(self, status_key: str):
        if not self.update:
            logger.error("notify_payment_status called without a valid client TGState.")
            return

        client_user_id = self.update.user
        if not client_user_id:
            logger.error(
                "notify_payment_status: client_user_id is missing from TGState."
            )
            return

        order = await self.base.food_db.find_one(
            {"user_id": client_user_id, "pass_key": self.pass_key}
        )

        if not order:
            logger.warning(
                f"notify_payment_status: No order found for user {client_user_id} with pass_key"
                f" {self.pass_key} to notify status {status_key}."
            )
            return

        order_id = order["_id"]
        order_total = order.get("total", 0.0)
        activities_total = self.activities_price(order.get("activities", {}))

        client_user_obj = await self.update.get_user()

        client_name_on_order = order.get("name_on_order", None)
        if not client_name_on_order and client_user_obj:
            client_name_on_order = client_user_name(
                client_user_obj, language_code=self.update.language_code
            )
        elif not client_name_on_order:
            client_name_on_order = "Customer"

        message_text = self.update.l(
            status_key,
            name=client_name_on_order,
            total=order_total,
            activitiesTotal=activities_total,
            orderId=str(order_id),
        )

        try:
            await self.update.bot.send_message(
                chat_id=client_user_id,
                text=message_text,
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )
            logger.info(
                f"Notified client {client_user_id} about order {order_id} status: {status_key}"
            )
        except Exception as e:
            logger.error(
                f"Failed to send payment status notification {status_key} to client "
                f"{client_user_id} for order {order_id}: {e}",
                exc_info=True,
            )

    async def handle_cq_adm_acc(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(
                self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML
            )
            return

        if ((self.update.user not in self.base.food_admins) and
             (self.update.user not in self.base.food_admins_old)):
            await self.update.reply(
                self.l("food-not-authorized-admin"), parse_mode=ParseMode.HTML
            )
            return

        order = await self.base.food_db.find_one({"_id": order_id})
        if not order:
            await self.update.reply(
                self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML
            )
            return

        if order.get("payment_status") == "paid":
            await self.update.edit_message_text(
                self.l("food-adm-payment-already-processed-or-error"),
                reply_markup=None,  # Remove buttons
                parse_mode=ParseMode.HTML,
            )
            return

        result = await self.base.food_db.update_one(
            {"_id": order_id, "payment_status": "proof_submitted"},
            {
                "$set": {
                    "payment_status": "paid",
                    "payment_confirmed_by": self.update.user,
                    "payment_confirmed_date": datetime.datetime.now(
                        datetime.timezone.utc
                    ),
                }
            },
        )

        if result.modified_count > 0:
            client_user_id = order.get("user_id")
            client_user_obj = None
            if client_user_id:
                client_user_obj = await self.base.user_db.find_one(
                    {"user_id": client_user_id, "bot_id": self.bot}
                )

            admin_lang = self.update.language_code  # Admin's language

            await self.update.edit_message_text(
                self.l(
                    "food-adm-payment-accepted-msg",
                    orderId=order_id_str,
                    link=client_user_link_html(
                        client_user_obj,
                        language_code=admin_lang,
                    ),
                    total=order.get("total"),
                ),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )
            # client_user_id is already fetched
            if client_user_id:
                client_update = await self.base.create_food_update_for_user(
                    client_user_id
                )
                if client_update:
                    await client_update.notify_payment_status(
                        "food-payment-proof-accepted"
                    )
        else:
            await self.update.edit_message_text(
                self.l("food-adm-payment-already-processed-or-error"),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
            )

    async def handle_cq_adm_rej(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(
                self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML
            )
            return

        if ((self.update.user not in self.base.food_admins) and
             (self.update.user not in self.base.food_admins_old)):
            await self.update.reply(
                self.l("food-not-authorized-admin"), parse_mode=ParseMode.HTML
            )
            return

        order = await self.base.food_db.find_one({"_id": order_id})
        if not order:
            await self.update.reply(
                self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML
            )
            return

        if (
            order.get("payment_status") == "rejected"
            or order.get("payment_status") == "paid"
        ):
            await self.update.edit_message_text(
                self.l("food-adm-payment-already-processed-or-error"),
                reply_markup=None,  # Remove buttons
                parse_mode=ParseMode.HTML,
            )
            return

        result = await self.base.food_db.update_one(
            {"_id": order_id, "payment_status": "proof_submitted"},
            {
                "$set": {
                    "payment_status": "rejected",
                    "payment_rejected_by": self.update.user,
                    "payment_rejected_date": datetime.datetime.now(
                        datetime.timezone.utc
                    ),
                }
            },
        )
        if result.modified_count > 0:
            client_user_id = order.get("user_id")
            client_user_obj = None
            if client_user_id:
                client_user_obj = await self.base.user_db.find_one(
                    {"user_id": client_user_id, "bot_id": self.bot}
                )

            admin_lang = self.update.language_code  # Admin's language

            await self.update.edit_message_text(
                self.l(
                    "food-adm-payment-rejected-msg",
                    orderId=order_id_str,
                    link=client_user_link_html(
                        client_user_obj,
                        language_code=admin_lang,
                    ),
                    total=order.get("total"),
                ),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )
            # client_user_id is already fetched
            if client_user_id:
                client_update = await self.base.create_food_update_for_user(
                    client_user_id
                )
                if client_update:
                    await client_update.notify_payment_status(
                        "food-payment-proof-rejected-retry"
                    )
        else:
            await self.update.edit_message_text(
                self.l("food-adm-payment-already-processed-or-error"),
                reply_markup=None,  # Remove buttons
                parse_mode=ParseMode.HTML,
            )

    async def handle_cq_exit(self):  # New handler for exit button
        await self.update.edit_message_text(
            self.l("food-message-exited"),
            reply_markup=None,  # Remove buttons
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_delete_order(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(
                self.l("food-order-not-found"), parse_mode=ParseMode.HTML
            )
            return

        user_id = self.update.user
        if not user_id:
            await self.update.reply(self.l("user-is-none"), parse_mode=ParseMode.HTML)
            return

        order = await self.base.food_db.find_one({"_id": order_id, "user_id": user_id})

        if not order:
            await self.update.reply(
                self.l("food-order-not-found"), parse_mode=ParseMode.HTML
            )
            return

        if order.get("payment_status") in ["paid", "proof_submitted"]:
            await self.update.edit_message_text(
                self.l("food-order-cannot-delete-paid-submitted"),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
            )
            return

        original_message_id = self.update.callback_query.message.message_id
        chat_id = self.update.callback_query.message.chat.id

        pass_key_to_use = order.get("pass_key", self.pass_key)
        updated_order = await self.base.save_order(
            user_id=user_id,
            pass_key=pass_key_to_use,
            order_data={},
            original_message_id=original_message_id,
            chat_id=chat_id,
        )

        if updated_order is None:
            await self.update.edit_message_text(
                self.l("food-order-deleted-successfully"),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
            )

    async def show_order_status_after_save(self, order_doc: dict):
        origin_info = order_doc.get("origin_info")
        if (
            not origin_info
            or not origin_info.get("message_id")
            or not origin_info.get("chat_id")
        ):
            logger.warning(
                f"Order {order_doc.get('_id')} saved, but no origin_info to update message."
            )
            return

        original_message_id = origin_info["message_id"]
        target_chat_id = origin_info["chat_id"]

        user = await self.get_user()
        if not user:
            logger.error(
                f"Cannot show order status after save for user {self.update.user}: user object"
                " not found."
            )
            return

        pass_key_from_order = order_doc.get("pass_key", self.pass_key)

        message_text, buttons = await self._prepare_order_message_and_buttons(
            order_doc, pass_key_from_order, original_message_id, target_chat_id
        )

        reply_markup = InlineKeyboardMarkup(buttons) if buttons else None

        try:
            await self.update.bot.edit_message_text(
                chat_id=target_chat_id,
                message_id=original_message_id,
                text=message_text,
                reply_markup=reply_markup,
                parse_mode=ParseMode.HTML,
            )
            logger.info(
                f"Successfully edited message {original_message_id} in chat {target_chat_id}"
                f" after order save for user {self.update.user}."
            )
        except TelegramError as e:
            if "Message is not modified" in str(e):
                pass
            else:
                raise e
        except Exception as e:
            logger.warning(
                f"Failed to edit message {original_message_id} in chat {target_chat_id} for"
                f" user {self.update.user} (Error: {e}). Sending new message.",
                exc_info=False,
            )
            try:
                await self.update.bot.send_message(
                    chat_id=target_chat_id,
                    text=message_text,
                    reply_markup=reply_markup,
                    parse_mode=ParseMode.HTML,
                )
                logger.info(
                    f"Sent new message to chat {target_chat_id} for user {self.update.user}"
                    " after failed edit."
                )
            except Exception as e_send:
                logger.error(
                    f"Failed to send new message to chat {target_chat_id} for user "
                    f"{self.update.user} (Error: {e_send}).",
                    exc_info=True,
                )

    async def handle_activities(self):
        """Handle the activities command."""
        user = await self.get_user()
        if not user:
            return

        # Get or create empty order
        order = await self.base.food_db.find_one(
            {"user_id": user["user_id"], "pass_key": self.pass_key}
        )
        if not order:
            order = {}

        # Check if activities have been paid for
        if (order.get("activities_payment_status") in ["paid", "proof_submitted"]):
            await self.handle_cq_submit_activities()
        else:
            # Proceed to activity selection
            await self.activities_message(order)

    async def can_reserve_cacao(self) -> bool:
        return await self.base.food_db.count_documents({
            "pass_key": self.pass_key,
            "activities.cacao": True
        }) < 38

    async def activities_message(self, order: dict):
        """Generate the activities message."""

        # Create 2x2 grid of activity buttons
        buttons = []
        untoggled_activities = ACTIVITIES.copy()
        untoggled_class_activities = CLASS_ACTIVITIES.copy()
        no_cacao = not await self.can_reserve_cacao()
        for i in range(0, len(ACTIVITIES), 2):
            row = []
            for activity in ACTIVITIES[i : i + 2]:
                status = "☑️" if order.get("activities", {}).get(activity) else "❌"
                if activity == "cacao" and no_cacao:
                    status = "🚫"
                if (
                    order.get("activities", {}).get(activity)
                    and activity in untoggled_activities
                ):
                    untoggled_activities.remove(activity)
                if (
                    order.get("activities", {}).get(activity)
                    and activity in untoggled_class_activities
                ):
                    untoggled_class_activities.remove(activity)
                row.append(
                    InlineKeyboardButton(
                        f"{status} {self.l(f'activity-{activity}')}",
                        callback_data=f"{self.base.name}|toggle_activity|{activity}",
                    )
                )
            buttons.append(row)

        buttons.append(
            [
                InlineKeyboardButton(
                    ("☑️" if len(untoggled_activities) == 0 else "❌")
                    + self.l("activity-button-all"),
                    callback_data=f"{self.base.name}|toggle_activity|all",
                ),
                InlineKeyboardButton(
                    ("☑️" if len(untoggled_class_activities) == 0 else "❌")
                    + self.l("activity-button-classes"),
                    callback_data=f"{self.base.name}|toggle_activity|classes",
                ),
            ]
        )

        # Add submit button
        buttons.append(
            [
                InlineKeyboardButton(
                    self.l("activity-button-submit"),
                    callback_data=f"{self.base.name}|submit_activities",
                )
            ]
        )

        await self.update.edit_or_reply(
            self.l("activity-select-message"),
            reply_markup=InlineKeyboardMarkup(buttons),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_toggle_activity(self, activity: str):
        """Handle activity toggle button press."""
        user = await self.get_user()
        if not user:
            raise Exception("User not found")

        # Get or create empty order
        order = await self.base.food_db.find_one(
            {"user_id": user["user_id"], "pass_key": self.pass_key}
        )
        if not order:
            order = {"activities": {}}

        no_cacao = not await self.can_reserve_cacao()
        # Toggle activity state
        activities = order.get("activities", {})
        if activity == "all":
            activities = {}
            for act in ACTIVITIES:
                if act != "cacao" or not no_cacao:
                    activities[act] = True
        elif activity == "classes":
            activities = {}
            for act in CLASS_ACTIVITIES:
                if act != "cacao" or not no_cacao:
                    activities[act] = True
        elif activity == "cacao" and no_cacao:
            activities["cacao"] = False
        else:
            activities[activity] = not activities.get(activity, False)
        order["activities"] = activities

        # Save order with updated activities
        result = await self.base.food_db.update_one(
            {
                "user_id": user["user_id"],
                "pass_key": self.pass_key,
            },
            {
                "$set": {"activities": activities},
                "$setOnInsert": {
                    "created_at": datetime.datetime.now(datetime.timezone.utc)
                },
            },
            upsert=True,
        )
        if result.modified_count < 1:
            logger.warning(
                f"Failed to update activities for user {self.user} with pass_key {self.pass_key}"
            )

        await self.activities_message(order)

    def activities_price(self, activities: dict) -> int:
        # Calculate minimal price
        has_party = activities.get("open", False)
        selected_practices = [
            act for act in CLASS_ACTIVITIES if activities.get(act, False)
        ]

        total_price = 0
        if has_party and len(selected_practices) > 0:
            total_price = 2500
        elif has_party:
            total_price = 2000
        elif len(selected_practices) > 2:
            total_price = 2000
        else:
            if activities.get("yoga", False):
                total_price += 750
            if activities.get("cacao", False):
                total_price += 1000
            if activities.get("soundhealing", False):
                total_price += 1000
        return total_price

    async def handle_cq_submit_activities(self):
        """Handle activities submission."""
        user = await self.get_user()
        assert user is not None, "User not found"

        order = await self.base.food_db.find_one(
            {"user_id": user["user_id"], "pass_key": self.pass_key}
        )
        if not order:
            order = {}
        # Get selected activities
        activities = order.get("activities", {})
        keys = {
            act: str(activities.get(act, False)) for act in ACTIVITIES
        }

        # Check and assign proof_admin if not set
        if not order.get("proof_admin"):
            if self.base.food_admins:
                assigned_admin_id = choice(self.base.food_admins)
                await self.base.food_db.update_one(
                    {"_id": order["_id"]},
                    {"$set": {"proof_admin": assigned_admin_id}},
                )
                order["proof_admin"] = assigned_admin_id
            else:
                logger.error("No food_admins configured to assign for activities.")
                # Optionally inform the user that admin assignment failed
                # await self.update.reply(self.l("food-payment-admins-not-configured"), parse_mode=ParseMode.HTML)

        proof_admin_id = order.get("proof_admin")
        admin_details = {}
        if proof_admin_id:
            admin_user_obj = await self.base.user_db.find_one(
                {"user_id": proof_admin_id, "bot_id": self.bot}
            )
            if admin_user_obj:
                admin_details = self.admin_to_keys(admin_user_obj, self.update.language_code)
            else:
                logger.error(f"Food admin user object not found for ID: {proof_admin_id}")
        keys.update(admin_details)

        action_buttons = []
        order_id = order.get("_id")
        total_price = self.activities_price(activities)

        if( order_id and total_price > 0 and
            order.get("activities_payment_status") not in ["paid", "proof_submitted"]):
            pay_button = InlineKeyboardButton(
                self.l("activity-button-pay"),  # Assuming this localization key will be added
                callback_data=f"{self.base.name}|activities_pay|{order_id}",
            )
            action_buttons.append([pay_button])
            keys["needPayment"] = "true"
        else:
            keys["needPayment"] = "false"

        exit_button = InlineKeyboardButton(
            self.l("activity-button-exit"),  # Assuming this localization key will be added
            callback_data=f"{self.base.name}|activities_exit",
        )
        action_buttons.append([exit_button])

        reply_markup = InlineKeyboardMarkup(action_buttons)

        await self.update.edit_or_reply(
            self.l(
                "activity-finished-message",
                **keys,
                totalPrice=total_price,
            ),
            reply_markup=reply_markup,
            disable_web_page_preview=True,
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_activities_exit(self):
        """Handle the exit button press from the activities finished message."""
        await self.update.edit_message_text(
            self.l("activity-message-exited"),  # Assuming this localization key will be added
            reply_markup=None,  # Remove buttons
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_activities_pay(self, order_id_str: str):
        """Handle the pay button press for activities."""
        order_id = ObjectId(order_id_str)

        order = await self.base.food_db.find_one(
            {"_id": order_id, "user_id": self.update.user}
        )
        assert order is not None, "Order not found"

        activities_payment_status = order.get("activities_payment_status")
        activities_total = self.activities_price(order.get("activities", {}))

        assert activities_payment_status not in ["paid", "proof_submitted"], "Activities already paid"
        assert activities_total > 0, "Activities total must be greater than zero"

        # Ensure proof_admin is assigned (can reuse logic from food payment if applicable)
        if not order.get("proof_admin"):
            assigned_admin_id = choice(self.base.food_admins)
            await self.base.food_db.update_one(
                {"_id": order_id}, {"$set": {"proof_admin": assigned_admin_id}}
            )
            order["proof_admin"] = assigned_admin_id

        proof_admin_id = order.get("proof_admin")
        assert proof_admin_id, "Proof admin must be assigned for activities payment"

        proof_admin_user_obj = await self.base.user_db.find_one(
            {"user_id": proof_admin_id, "bot_id": self.bot}
        )
        assert proof_admin_user_obj, "Proof admin user object must exist"

        payment_detail_keys = self.admin_to_keys(
            proof_admin_user_obj, self.update.language_code
        )
        payment_detail_keys["total"] = activities_total

        await self.update.edit_message_text(
            self.l("activity-payment-request-callback-message", **payment_detail_keys), # Placeholder
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
            disable_web_page_preview=True,
        )

        await self.update.reply(
            self.l("activity-payment-request-waiting-message", **payment_detail_keys), # Placeholder
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup(
                [[CANCEL_CHR + self.l("cancel-command")]], resize_keyboard=True
            ),
            disable_web_page_preview=True,
        )
        await self.update.require_anything(
            self.base.name,
            "handle_activities_payment_proof_input",
            f"{order_id_str}",
            "handle_activities_payment_proof_timeout",
        )

    async def handle_activities_payment_proof_input(self, order_id_str: str):
        if (
            self.update.message
            and self.update.message.text
            and self.update.message.text.startswith(CANCEL_CHR)
        ):
            await self.update.reply(
                self.l("activity-payment-proof-cancelled"), # Placeholder
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return
        
        order_id = ObjectId(order_id_str)

        if not (
            self.update.message
            and (self.update.message.photo or self.update.message.document)
        ):
            await self.update.reply(
                self.l("activity-payment-proof-wrong-data"), # Placeholder
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
            return

        file_id = ""
        file_ext = ".dat"
        if self.update.message.photo:
            file_id = self.update.message.photo[-1].file_id
            file_ext = ".jpg"
        elif self.update.message.document:
            doc = self.update.message.document
            file_id = doc.file_id
            if doc.mime_type == "application/pdf":
                file_ext = ".pdf"
            elif doc.file_name:
                import os
                _, ext = os.path.splitext(doc.file_name)
                if ext:
                    file_ext = ext.lower()

        order = await self.base.food_db.find_one(
            {"_id": order_id, "user_id": self.update.user}
        )
        assert order is not None, "Order not found"
        assert order.get("activities_payment_status") in [None, "rejected"], \
            "Activities payment must be in pending state to submit proof"
        
        proof_admin_id = order.get("proof_admin")
        assert proof_admin_id, "Proof admin must be assigned for activities payment"

        await self.base.food_db.update_one(
            {"_id": order_id},
            {
                "$set": {
                    "activities_payment_status": "proof_submitted",
                    "activities_proof_file": f"{file_id}{file_ext}",
                    "activities_proof_received_date": datetime.datetime.now(datetime.timezone.utc),
                }
            },
        )

        admin_user_obj = await self.base.user_db.find_one(
            {"user_id": proof_admin_id, "bot_id": self.bot}
        )
        client_user_obj = await self.get_user()
        activities_total = self.activities_price(order.get("activities", {}))
        assert admin_user_obj, "Admin user object must exist for activities payment proof"
        assert client_user_obj, "Client user object must exist for activities payment proof"

        admin_lang = admin_user_obj.get("language_code", "en")
        admin_tg_state = TGState(admin_user_obj["user_id"], self.base.base_app)
        admin_tg_state.language_code = admin_lang
        admin_l = admin_tg_state.l

        forward_message = await self.update.forward_message(proof_admin_id)
        if forward_message:
            await self.update.bot.send_message(
                chat_id=proof_admin_id,
                text=admin_l(
                    "activity-adm-payment-proof-received", # Placeholder
                    link=client_user_link_html(client_user_obj, language_code=admin_lang),
                    total=activities_total,
                ),
                reply_markup=InlineKeyboardMarkup(
                    [
                        [
                            InlineKeyboardButton(
                                admin_l("activity-adm-payment-proof-accept-button"), # Placeholder
                                callback_data=f"{self.base.name}|adm_activities_acc|{order_id_str}",
                            ),
                            InlineKeyboardButton(
                                admin_l("activity-adm-payment-proof-reject-button"), # Placeholder
                                callback_data=f"{self.base.name}|adm_activities_rej|{order_id_str}",
                            ),
                        ]
                    ]
                ),
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )

            await self.update.reply(
                self.l("activity-payment-proof-forwarded"), # Placeholder
                reply_markup=ReplyKeyboardRemove(),
                parse_mode=ParseMode.HTML,
            )
        else:
            raise Exception(
                f"Failed to forward activities payment proof for order {order_id_str} to admin"
            )

    async def handle_activities_payment_proof_timeout(self, order_id_str: str):
        # order_id_str = prefixed_order_id_str.replace("activities_", "", 1) # Not strictly needed for generic message
        await self.update.reply(
            self.l("activity-payment-proof-timeout"), # Placeholder
            reply_markup=ReplyKeyboardRemove(),
            parse_mode=ParseMode.HTML,
        )

    async def handle_cq_adm_activities_acc(self, order_id_str: str):
        """Handles the admin acceptance of an activities payment proof."""
        order_id = ObjectId(order_id_str)

        assert ((self.update.user in self.base.food_admins) or
                (self.update.user in self.base.food_admins_old)), "User is not a food admin."

        order = await self.base.food_db.find_one({"_id": order_id})
        assert order is not None, f"Order {order_id_str} not found."
        
        result = await self.base.food_db.update_one(
            {"_id": order_id, "activities_payment_status": "proof_submitted"}, # Assuming 'proof_submitted' is the status before acceptance
            {
                "$set": {
                    "activities_payment_status": "paid",
                    "activities_payment_confirmed_by": self.update.user,
                    "activities_payment_confirmed_date": datetime.datetime.now(
                        datetime.timezone.utc
                    ),
                }
            },
        )

        if result.modified_count > 0:
            client_user_id = order.get("user_id")
            client_user_obj = None
            if client_user_id:
                client_user_obj = await self.base.user_db.find_one(
                    {"user_id": client_user_id, "bot_id": self.bot}
                )

            admin_lang = self.update.language_code

            await self.update.edit_message_text(
                self.l(
                    "activity-adm-payment-accepted-msg", # Placeholder
                    orderId=order_id_str,
                    link=client_user_link_html(
                        client_user_obj,
                        language_code=admin_lang,
                    ),
                    total=self.activities_price(order.get("activities", {})),
                ),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )

            if client_user_id:
                client_update = await self.base.create_food_update_for_user(
                    client_user_id
                )
                if client_update:
                    # Need a new notification status key for activities payment accepted
                    await client_update.notify_payment_status(
                        "activity-payment-proof-accepted" # Placeholder
                    )

    async def handle_cq_adm_activities_rej(self, order_id_str: str):
        """Handles the admin rejection of an activities payment proof."""
        order_id = ObjectId(order_id_str)

        assert ((self.update.user in self.base.food_admins) or
                (self.update.user in self.base.food_admins_old)), "User is not a food admin."

        order = await self.base.food_db.find_one({"_id": order_id})
        assert order is not None, f"Order {order_id_str} not found."

        result = await self.base.food_db.update_one(
            {"_id": order_id, "activities_payment_status": "proof_submitted"},
            {
                "$set": {
                    "activities_payment_status": "rejected",
                    "activities_payment_rejected_by": self.update.user,
                    "activities_payment_rejected_date": datetime.datetime.now(
                        datetime.timezone.utc
                    ),
                }
            },
        )

        if result.modified_count > 0:
            client_user_id = order.get("user_id")
            client_user_obj = None
            if client_user_id:
                client_user_obj = await self.base.user_db.find_one(
                    {"user_id": client_user_id, "bot_id": self.bot}
                )

            admin_lang = self.update.language_code

            await self.update.edit_message_text(
                self.l(
                    "activity-adm-payment-rejected-msg",  # Placeholder
                    orderId=order_id_str,
                    link=client_user_link_html(
                        client_user_obj,
                        language_code=admin_lang,
                    ),
                    total=self.activities_price(order.get("activities", {})),
                ),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
                disable_web_page_preview=True,
            )

            if client_user_id:
                client_update = await self.base.create_food_update_for_user(
                    client_user_id
                )
                if client_update:
                    # Need a new notification status key for activities payment rejected
                    await client_update.notify_payment_status(
                        "activity-payment-proof-rejected-retry"  # Placeholder
                    )
    
    async def send_notification(self, notification_type: str) -> None:
        await self.update.reply(
            self.l(f"food-notification-message-{notification_type}"),
            parse_mode=ParseMode.HTML,
        )
        await self.base.food_db.update_one(
            {"user_id": self.update.user, "pass_key": self.pass_key},
            {"$set": {f"notification_{notification_type}_sent": True}},
        )

class Food(BasePlugin):
    name = "food"
    food_db: AgnosticCollection
    user_db: AgnosticCollection
    menu: dict
    food_admins: list[int]

    NO_LUNCH_RU = "Без обеда"
    COMBO_WITH_SOUP_RU = "Комбо с супом"
    COMBO_NO_SOUP_RU = "Комбо без супа"

    def __init__(self, base_app):
        super().__init__(base_app)
        self.food_db = base_app.mongodb[self.config.mongo_db.food_collection]
        self.user_db = base_app.users_collection
        self.base_app.food = self

        self.deadline = self.config.food.deadline

        self.menu = self._load_menu()

        self.food_admins = [
            int(admin_id) for admin_id in self.config.food.payment_admins
        ]
        if len(self.food_admins) == 0:
            self.food_admins = [int(admin_id) for admin_id in self.config.food.admins]
        self.food_admins_old = [
            int(admin_id) for admin_id in self.config.food.payment_admins_old
        ]

        self._command_checkers = [
            CommandHandler("food", self.handle_food_start_cmd),
            CommandHandler("activities", self.handle_activities_cmd),
            CommandHandler("exportfoodorders", self.handle_export_orders_cmd),
        ]
        self._cbq_handler = CallbackQueryHandler(
            self.handle_food_callback_query_entry, pattern=f"^{self.name}\\|.*"
        )
        create_task(self._notification_sender())

    async def _notification_sender(self) -> None:
        from .passes import Passes
        bot_started: Event = self.base_app.bot_started
        await bot_started.wait()
        passes_plugin: Passes = self.base_app.passes
        while True:
            try:
                # Wait for the next notification time
                await sleep(NOTIFICATION_CHECK_INTERVAL)
                passes = await passes_plugin.get_all_passes(with_unpaid=True)
                # Send notifications to users with pending orders
                if (now_msk() > ( # after the first notification time
                        self.deadline - self.config.food.notification_first_time
                    )
                    and now_msk() < ( # don't send double notifications, and add a gap
                        self.deadline - self.config.food.notification_last_time - self.config.food.notify_after
                    )):
                    async for order in self.food_db.find(
                        {
                            "pass_key": PASS_KEY,
                            "payment_status": {"$nin": ["paid", "proof_submitted"]},
                            "notification_first_sent": {"$ne": True},
                            "total": {"$gt": 0},  # Only notify if total > 0
                            "last_updated": {  # do not notify if user just updated order
                                "$lte": datetime.datetime.now(datetime.timezone.utc)
                                - self.config.food.notify_after
                            }
                        }
                    ):
                        user = order["user_id"]
                        try:
                            upd = await self.create_food_update_for_user(user)
                            await upd.send_notification(
                                "first"
                            )  # Notify with first notification
                        except Exception as e:
                            logger.error(
                                f"Error notifying {user=}: {e}",
                                exc_info=True,
                            )
                            continue
                    for user in passes:
                        try:
                            upd = await self.create_food_update_for_user(user["user_id"])
                            order = await self.get_order(
                                user_id=user["user_id"]
                            )
                            if (not order and
                                 not user.get(PASS_KEY, {}).get("notified_food_first", False)):
                                await upd.update.reply(
                                    upd.l("food-no-order-notification-first"),
                                    parse_mode=ParseMode.HTML
                                )
                                await self.user_db.update_one(
                                    {"user_id": user["user_id"]},
                                    {"$set": {f"{PASS_KEY}.notified_food_first": True}}
                                )
                        except Exception as e:
                            logger.error(
                                f"Error notifying {user=}: {e}",
                                exc_info=True,
                            )
                            continue
                # Send notifications to users with pending orders
                if (now_msk() > ( # after the last notification time
                        self.deadline - self.config.food.notification_last_time
                    )
                    and now_msk() < ( # don't send after the deadline
                        self.deadline
                    )):
                    async for order in self.food_db.find(
                        {
                            "pass_key": PASS_KEY,
                            "payment_status": {"$nin": ["paid", "proof_submitted"]},
                            "notification_last_sent": {"$ne": True},
                            "total": {"$gt": 0},  # Only notify if total > 0
                            "last_updated": {  # do not notify if user just updated order
                                "$lte": datetime.datetime.now(datetime.timezone.utc)
                                - self.config.food.notify_after
                            }
                        }
                    ):
                        user = order["user_id"]
                        try:
                            upd = await self.create_food_update_for_user(user)
                            await upd.send_notification(
                                "last"
                            )  # Notify with last notification
                        except Exception as e:
                            logger.error(
                                f"Error notifying {user=}: {e}",
                                exc_info=True,
                            )
                            continue
                    for user in passes:
                        try:
                            upd = await self.create_food_update_for_user(user["user_id"])
                            order = await self.get_order(
                                user_id=user["user_id"]
                            )
                            if (not order and
                                 not user.get(PASS_KEY, {}).get("notified_food_last", False)):
                                await upd.update.reply(
                                    upd.l("food-no-order-notification-last"),
                                    parse_mode=ParseMode.HTML
                                )
                                await self.user_db.update_one(
                                    {"user_id": user["user_id"]},
                                    {"$set": {f"{PASS_KEY}.notified_food_last": True}}
                                )
                        except Exception as e:
                            logger.error(
                                f"Error notifying {user=}: {e}",
                                exc_info=True,
                            )
                            continue
            except Exception as e:
                logger.error(f"Error in food notification sender: {e}", exc_info=True)


    def _load_menu(self) -> dict:
        import os
        from os.path import dirname as d

        with open(
            os.path.join(d(d(d(__file__))), "static", "menu_2025_1.json"),
            "r",
            encoding="utf-8",
        ) as f:
            menu_data = json.load(f)

        return menu_data

    async def get_order(self, user_id: int, pass_key: str = PASS_KEY) -> dict | None:
        logger.debug(
            f"Attempting to get order for user_id: {user_id}, pass_key: {pass_key}"
        )
        order = await self.food_db.find_one({"user_id": user_id, "pass_key": pass_key})
        if order:
            logger.debug(
                f"Order found for user_id: {user_id}, pass_key: {pass_key}. Order ID: "
                f"{order.get('_id')}"
            )
            if now_msk() > self.deadline:
                if not order.get("order_details") or order.get("total", 0) <= 0:
                    logger.info(
                        f"Order {order.get('_id')} for user {user_id} is past the deadline"
                        " and has no details or total <= 0. Treating as no order."
                    )
                    return None
                if order.get("payment_status") not in [
                    "paid",
                    "proof_submitted",
                ]:
                    logger.info(
                        f"Order {order.get('_id')} for user {user_id} is past the deadline"
                        " and not paid/proof_submitted. Treating as no order."
                    )
                    return None
            # If order_details is None or an empty dict, and status is not protective, treat as no order.
            # An order with explicit "no-lunch" selections will have order_details populated.
            if not order.get("order_details") and order.get("payment_status") not in [
                "paid",
                "proof_submitted",
            ]:
                logger.info(
                    f"Order {order.get('_id')} for user {user_id} has empty/null details"
                    " and is not paid/proof_submitted. Treating as no order."
                )
                return None
        else:
            logger.debug(f"No order found for user_id: {user_id}, pass_key: {pass_key}")
        return order

    async def get_order_by_id(self, order_id: ObjectId, pass_key: str) -> dict | None:
        logger.debug(
            f"Attempting to get order by order_id: {order_id}, pass_key: {pass_key}"
        )
        order = await self.food_db.find_one(
            {
                "_id": order_id,
                "pass_key": pass_key,  # Ensure pass_key is checked
            }
        )
        if order:
            logger.debug(
                f"Order found for order_id: {order_id}, pass_key: {pass_key}. User ID: "
                f"{order.get('user_id')}"
            )
            # If order_details is None or an empty dict, and status is not protective, treat as no order.
            if not order.get("order_details") and order.get("payment_status") not in [
                "paid",
                "proof_submitted",
            ]:
                logger.info(
                    f"Order {order.get('_id')} (user {order.get('user_id')}) has empty/null details"
                    " and is not paid/proof_submitted. Treating as no order."
                )
                return None
        else:
            logger.debug(
                f"No order found for order_id: {order_id}, pass_key: {pass_key}"
            )
        return order

    async def save_order(
        self,
        user_id: int,
        pass_key: str,
        order_data: dict,
        original_message_id: int | None = None,
        chat_id: int | None = None,
    ) -> dict | None:
        if not self.menu:
            logger.error("Menu not loaded. Cannot save order.")
            return None

        # Fetch existing order details first to check its status and content
        existing_order = await self.food_db.find_one(
            {"user_id": user_id, "pass_key": pass_key}
        )
        current_payment_status_from_db = (
            existing_order.get("payment_status") if existing_order else None
        )
        stored_order_details = (
            existing_order.get("order_details") if existing_order else None
        )
        stored_total = existing_order.get("total") if existing_order else None
        current_proof_admin_from_db = (
            existing_order.get("proof_admin") if existing_order else None
        )

        calculated_total_sum = 0
        order_is_complete = True  # Assume complete initially

        # --- Loop to calculate calculated_total_sum and order_is_complete from new order_data ---
        for day_key, day_menu_config in self.menu.items():
            day_has_lunch_in_menu = bool(day_menu_config.get("lunch"))
            order_for_this_day = order_data.get(day_key)

            # --- Lunch Processing ---
            if day_has_lunch_in_menu:
                if not order_for_this_day or order_for_this_day.get("lunch") is None:
                    # If menu offers lunch for this day, but order data has no lunch entry or it's null
                    order_is_complete = False
                else:
                    lunch_details = order_for_this_day["lunch"]
                    lunch_type = lunch_details.get("type")
                    # Default items to appropriate empty type if not provided or null
                    if lunch_type == "individual-items":
                        lunch_items_payload = lunch_details.get("items", [])
                    else:  # For combos or potentially other types if items were a dict
                        lunch_items_payload = lunch_details.get("items", {})

                    if lunch_type == "no-lunch":
                        pass  # Complete for this day's lunch, no price added.
                    elif lunch_type == "individual-items":
                        if isinstance(lunch_items_payload, list):
                            day_lunch_menu_items = day_menu_config.get("lunch", [])
                            for item_index_obj in lunch_items_payload:
                                try:
                                    item_index = int(
                                        item_index_obj
                                    )  # Client might send string indices
                                    if 0 <= item_index < len(day_lunch_menu_items):
                                        lunch_item_config = day_lunch_menu_items[
                                            item_index
                                        ]
                                        if lunch_item_config and isinstance(
                                            lunch_item_config.get("price"), (int, float)
                                        ):
                                            calculated_total_sum += lunch_item_config[
                                                "price"
                                            ]
                                        else:
                                            logger.warning(
                                                f"Individual lunch item {day_key}-lunch-{item_index} selected but has no/invalid price in menu config."
                                            )
                                    else:
                                        logger.warning(
                                            f"Invalid individual lunch item index {item_index} for {day_key} (max: {len(day_lunch_menu_items) - 1})."
                                        )
                                except (ValueError, TypeError):
                                    logger.warning(
                                        f"Invalid individual lunch item index format: '{item_index_obj}' for {day_key}."
                                    )
                        else:
                            logger.warning(
                                f"Malformed 'items' for 'individual-items' lunch on {day_key}. Expected list, got {type(lunch_items_payload)}."
                            )
                            order_is_complete = (
                                False  # Treat as an error making order incomplete
                            )

                    elif lunch_type == "combo-with-soup":
                        current_day_combo_complete = True
                        if (
                            not isinstance(lunch_items_payload, dict)
                            or lunch_items_payload.get("soup_index") is None
                        ):
                            current_day_combo_complete = False
                        if (
                            current_day_combo_complete
                        ):  # Only check sub-categories if soup is present
                            for category in LUNCH_SUB_CATEGORIES:  # main, side, salad
                                if lunch_items_payload.get(f"{category}_index") is None:
                                    current_day_combo_complete = False
                                    break

                        if current_day_combo_complete:
                            calculated_total_sum += LUNCH_WITH_SOUP_PRICE
                        else:
                            order_is_complete = False

                    elif lunch_type == "combo-no-soup":
                        current_day_combo_complete = True
                        if not isinstance(lunch_items_payload, dict):
                            current_day_combo_complete = (
                                False  # items payload itself is wrong type
                            )
                        else:
                            for category in LUNCH_SUB_CATEGORIES:  # main, side, salad
                                if lunch_items_payload.get(f"{category}_index") is None:
                                    current_day_combo_complete = False
                                    break

                        if current_day_combo_complete:
                            calculated_total_sum += LUNCH_NO_SOUP_PRICE
                        else:
                            order_is_complete = False
                    else:  # Invalid or missing lunch type when lunch_details object itself exists
                        logger.warning(
                            f"Invalid or missing lunch_type '{lunch_type}' for {day_key}."
                        )
                        order_is_complete = False

            # --- Dinner Sum Calculation (remains largely the same, but ensure robustness) ---
            if order_for_this_day and "dinner" in order_for_this_day:
                dinner_order_indices = order_for_this_day.get(
                    "dinner", []
                )  # Default to empty list
                dinner_menu_items_config = day_menu_config.get("dinner", [])

                if isinstance(dinner_order_indices, list) and dinner_menu_items_config:
                    for item_index_obj in dinner_order_indices:
                        try:
                            item_index = int(
                                item_index_obj
                            )  # Client might send string indices
                            if 0 <= item_index < len(dinner_menu_items_config):
                                dinner_item_details = dinner_menu_items_config[
                                    item_index
                                ]
                                if dinner_item_details and isinstance(
                                    dinner_item_details.get("price"), (int, float)
                                ):
                                    calculated_total_sum += dinner_item_details["price"]
                                else:
                                    logger.warning(
                                        f"Dinner item {day_key}-dinner-{item_index} in order is missing price or has invalid price in menu config: {dinner_item_details}"
                                    )
                            else:
                                logger.warning(
                                    f"Invalid dinner item index {item_index} for day {day_key} in order data. Max index: {len(dinner_menu_items_config) - 1}"
                                )
                        except (ValueError, TypeError) as e:
                            logger.warning(
                                f"Invalid dinner item index format: '{item_index_obj}' for day {day_key}. Error: {e}"
                            )
            elif day_menu_config.get("dinner") and not (
                order_for_this_day and "dinner" in order_for_this_day
            ):
                # Dinner is offered in menu, but no dinner data in order for this day. This is fine, dinner is optional and doesn't affect completeness.
                pass
        # --- End of calculation loop ---

        # Check for modification attempts on paid/proof_submitted orders
        if existing_order and current_payment_status_from_db in [
            "paid",
            "proof_submitted",
        ]:
            is_data_changed = order_data != stored_order_details
            # Compare new calculated total with the total stored from the existing order
            is_total_changed = calculated_total_sum != stored_total

            if is_data_changed or is_total_changed:
                error_msg = (
                    f"Attempt to modify order {existing_order['_id']} (user {user_id}) "
                    f"which is in status '{current_payment_status_from_db}'. Modifications"
                    " are not allowed."
                )
                logger.error(error_msg)
                raise ValueError(error_msg)

        db_order_doc = {
            "user_id": user_id,
            "pass_key": pass_key,
            "order_details": order_data,
            "total": calculated_total_sum,
            "is_complete": order_is_complete,
            "last_updated": datetime.datetime.now(datetime.timezone.utc),
        }

        if original_message_id is not None and chat_id is not None:
            db_order_doc["origin_info"] = {
                "message_id": original_message_id,
                "chat_id": chat_id,
            }

        # Determine payment_status for the document to be saved
        if order_is_complete:
            if current_payment_status_from_db in ["paid", "proof_submitted"] and (
                not existing_order
                or (
                    order_data == stored_order_details
                    and calculated_total_sum == stored_total
                )
            ):
                # If status was 'paid' or 'proof_submitted' and no actual change was made (error wasn't raised), preserve it.
                db_order_doc["payment_status"] = current_payment_status_from_db
            else:
                # Order is complete, but either wasn't paid/proof_submitted,
                # or it was but changes were made (which would have reset it if not for the error being raised for paid/proof_submitted).
                # This path is taken if it's newly complete, or was editable and is still complete.
                db_order_doc["payment_status"] = None
        else:  # Order is not complete
            db_order_doc["payment_status"] = None

        # Manage proof_admin
        if (
            db_order_doc["payment_status"] == "paid"
            or db_order_doc["payment_status"] == "proof_submitted"
        ):
            # This means the status was preserved from current_payment_status_from_db
            if current_proof_admin_from_db:
                db_order_doc["proof_admin"] = current_proof_admin_from_db
            else:
                # This case (paid/proof_submitted but no proof_admin in existing order) implies an inconsistency.
                # Log it, and set proof_admin to None if it was None.
                db_order_doc["proof_admin"] = None
                if (
                    existing_order
                ):  # Only log warning if it's an existing order that's inconsistent
                    logger.warning(
                        f"Order {existing_order['_id']} has status "
                        f"{db_order_doc['payment_status']} but no current_proof_admin_from_db "
                        "during save."
                    )
        else:  # payment_status is None (incomplete, or complete but awaiting payment initiation)
            db_order_doc["proof_admin"] = (
                None  # Clear admin if not in active payment/paid state
            )

        update_payload = {
            "$set": db_order_doc,
            "$setOnInsert": {
                "created_at": datetime.datetime.now(datetime.timezone.utc)
            },
        }

        result = await self.food_db.update_one(
            {"user_id": user_id, "pass_key": pass_key}, update_payload, upsert=True
        )

        saved_order_doc = None  # Initialize
        if result.upserted_id:
            logger.info(
                f"New order inserted for user_id: {user_id}, pass_key: {pass_key}. Order ID: "
                f"{result.upserted_id}. Complete: {order_is_complete}, Total: "
                f"{calculated_total_sum}"
            )
            saved_order_doc = await self.get_order(user_id, pass_key)
        elif result.modified_count > 0:
            logger.info(
                f"Order updated for user_id: {user_id}, pass_key: {pass_key}. Complete: "
                f"{order_is_complete}, Total: {calculated_total_sum}"
            )
            saved_order_doc = await self.get_order(user_id, pass_key)
        else:
            # This case (matched_count > 0, modified_count == 0) means the new data was identical to existing.
            logger.info(
                f"Order for user_id: {user_id}, pass_key: {pass_key} was not modified"
                " (data identical). Complete: {order_is_complete}, Total: "
                f"{calculated_total_sum}"
            )
            # Still fetch to ensure consistency for the return value and potential message update
            saved_order_doc = await self.get_order(user_id, pass_key)

        if (
            saved_order_doc
            and saved_order_doc.get("origin_info")
            and saved_order_doc["origin_info"].get("message_id")
            and saved_order_doc["origin_info"].get("chat_id")
        ):
            client_food_update = await self.create_food_update_for_user(user_id)
            if client_food_update:
                # Ensure the TGState within client_food_update has a valid bot instance
                # This check is now implicitly handled by create_food_update_for_user if it returns a valid instance
                await client_food_update.show_order_status_after_save(saved_order_doc)
            else:
                logger.error(
                    f"Could not create FoodUpdate for user {user_id} to update message"
                    " after save."
                )

        return saved_order_doc

    async def handle_food_start_cmd(self, update: TGState):
        return await self.create_update(update).handle_start()

    async def handle_food_callback_query_entry(self, update: TGState):
        return await self.create_update(update).handle_callback_query()

    def test_message(self, message: Update, state, web_app_data):
        for checker in self._command_checkers:
            if checker.check_update(message):
                return PRIORITY_BASIC, checker.callback
        return PRIORITY_NOT_ACCEPTING, None

    def test_callback_query(self, query: Update, state):
        if self._cbq_handler.check_update(query):
            return PRIORITY_BASIC, self._cbq_handler.callback
        return PRIORITY_NOT_ACCEPTING, None

    def create_update(self, update_obj: TGState) -> FoodUpdate:
        return FoodUpdate(self, update_obj)

    async def create_food_update_for_user(self, user_id: int) -> FoodUpdate | None:
        target_user_tg_state = TGState(user=user_id, app=self.base_app)
        await (
            target_user_tg_state.get_state()
        )  # Load user data, language, and ensure bot is populated
        if not target_user_tg_state.bot:
            logger.error(
                f"Bot instance could not be determined for user {user_id} in "
                "create_food_update_for_user. FoodUpdate creation aborted."
            )
            return None
        return self.create_update(target_user_tg_state)

    def admin_to_keys(self, admin: dict, lc: str | None = None) -> dict:
        assert isinstance(admin, dict)
        if lc is None:
            lc = "en"

        keys = {}
        keys["adminEmoji"] = admin.get("emoji", "🧑‍💼")
        keys["adminLink"] = client_user_link_html(admin, language_code=lc)
        keys["adminName"] = client_user_name(admin, language_code=lc)
        keys["phoneContact"] = admin.get("phone_contact", "nophone")
        keys["phoneSBP"] = admin.get("phone_sbp", "nosbp")
        keys["banks"] = admin.get("banks_en", "")
        if lc is not None:
            keys["banks"] = admin.get(f"banks_{lc}", keys["banks"])
        return keys

    # Wrapper methods for require_anything callbacks
    async def handle_payment_proof_input(self, update: TGState, order_id_str: str):
        """Registered callback for TGState.require_anything."""
        return await self.create_update(update).handle_payment_proof_input(order_id_str)

    async def handle_payment_proof_timeout(self, update: TGState, order_id_str: str):
        """Registered callback for TGState.require_anything."""
        return await self.create_update(update).handle_payment_proof_timeout(
            order_id_str
        )

    async def handle_activities_cmd(self, update: TGState):
        return await self.create_update(update).handle_activities()

    async def handle_activities_payment_proof_input(self, update: TGState, order_id_str: str):
        """Registered callback for TGState.require_anything for activities payment proof."""
        return await self.create_update(update).handle_activities_payment_proof_input(order_id_str)

    async def handle_activities_payment_proof_timeout(self, update: TGState, order_id_str: str):
        """Registered callback for TGState.require_anything for activities payment proof timeout."""
        return await self.create_update(update).handle_activities_payment_proof_timeout(order_id_str)

    async def handle_export_orders_cmd(self, update: TGState):
        assert update.user in self.config.food.admins, f"User {update.user} is not an admin."

        try:
            header = [
                "ID Пользователя",
                "Username",
                "Print Name",
                "Legal Name",
                "Оплачено",
                "Платеж Подтвержден",
                "Итого Сумма",
            ]
            
            day_meal_columns_info = [] # To store (day_key, meal_type, column_header_name)

            if self.menu:
                for day_key in self.menu.keys():
                    day_menu_config = self.menu[day_key]
                    day_name_ru = {
                        "friday": "Пятница",
                        "saturday": "Суббота",
                        "sunday": "Воскресенье",
                    }[day_key]
                    if day_menu_config.get("lunch"):
                        lunch_col_name = f"{day_name_ru} Обед"
                        header.append(lunch_col_name)
                        day_meal_columns_info.append({"day_key": day_key, "meal": "lunch", "header": lunch_col_name})
                    if day_menu_config.get("dinner"):
                        dinner_col_name = f"{day_name_ru} Ужин"
                        header.append(dinner_col_name)
                        day_meal_columns_info.append({"day_key": day_key, "meal": "dinner", "header": dinner_col_name})
            
            csv_rows = []
            csv_rows.append(header)

            orders_cursor = self.food_db.find({"pass_key": PASS_KEY})
            async for order in orders_cursor:
                user_id = order.get("user_id")
                user_doc = await self.user_db.find_one({"user_id": user_id, "bot_id": self.bot.bot.id})

                username = user_doc.get("username", "") if user_doc else ""
                print_name = user_doc.get("print_name", "") if user_doc else ""
                legal_name = user_doc.get("legal_name", "") if user_doc else ""

                is_paid = order.get("payment_status") == "paid"
                payment_confirmed = order.get("payment_confirmed_date") is not None
                total = order.get("total", 0.0)

                row_data = [
                    user_id,
                    username,
                    print_name,
                    legal_name,
                    "Да" if is_paid else "Нет",
                    "Да" if payment_confirmed else "Нет",
                    total,
                ]

                order_details = order.get("order_details", {})
                
                for col_info in day_meal_columns_info:
                    day_key = col_info["day_key"]
                    meal_type = col_info["meal"] 
                    
                    day_menu_config = self.menu.get(day_key, {})
                    order_details_for_day = order_details.get(day_key, {})
                    
                    items_str = self._extract_meal_textual_contents(meal_type, day_key, order_details_for_day, day_menu_config)

                    row_data.append(items_str)
                csv_rows.append(row_data)

            if not csv_rows or len(csv_rows) <= 1: 
                await update.reply("Нет заказов для экспорта.")
                return

            # Create the first CSV (orders export)
            output = io.StringIO()
            writer = csv.writer(output)
            writer.writerows(csv_rows)
            csv_data_bytes = output.getvalue().encode('utf-8')
            output.close()

            filename = f"food_orders_{datetime.datetime.now().strftime('%Y-%m-%d_%H-%M-%S')}.csv"
            
            input_file = InputFile(io.BytesIO(csv_data_bytes), filename=filename)
            await update.update.effective_message.reply_document(document=input_file, caption="Экспорт заказов на питание завершен.")

            # Create the second CSV (meal summary)
            await self._export_meal_summary(update)

        except Exception as e:
            logger.error(f"Error exporting food orders: {e}", exc_info=True)
            await update.reply(f"Произошла ошибка при экспорте заказов: {e}")

    async def _export_meal_summary(self, update: TGState):
        """Create and send a CSV export with meal summary statistics."""
        try:
            # Headers for the summary CSV
            summary_header = [
                "День",
                "Прием пищи", 
                "Название блюда",
                "Цена",
                "Количество заказов",
                "Сумма"
            ]
            
            summary_rows = [summary_header]
            
            # Day names mapping
            day_names_ru = {
                "friday": "Пятница",
                "saturday": "Суббота", 
                "sunday": "Воскресенье",
            }
            
            # Count occurrences of each meal item
            meal_counts = {}  # Structure: {day: {meal_type: {item_key: count}}}
            combo_counts = {}  # Structure: {day: {combo_type: count}}
            # Process all orders
            orders_cursor = self.food_db.find({
                "pass_key": PASS_KEY,
                "payment_status": "paid",
                "payment_confirmed_date": {"$exists": True},
            })
            async for order in orders_cursor:
                order_details = order.get("order_details", {})
                
                for day_key, day_order in order_details.items():
                    if day_key not in meal_counts:
                        meal_counts[day_key] = {"lunch": {}, "dinner": {}}
                    if day_key not in combo_counts:
                        combo_counts[day_key] = {}
                    
                    # Process lunch
                    if day_order and "lunch" in day_order:
                        lunch_details = day_order["lunch"]
                        if lunch_details:
                            lunch_type = lunch_details.get("type")
                            lunch_items = lunch_details.get("items")
                            
                            if lunch_type == "individual-items" and isinstance(lunch_items, list):
                                # Count individual lunch items
                                day_lunch_menu = self.menu.get(day_key, {}).get("lunch", [])
                                for item_index_obj in lunch_items:
                                    try:
                                        item_index = int(item_index_obj)
                                        if 0 <= item_index < len(day_lunch_menu):
                                            item_key = f"lunch_individual_{item_index}"
                                            meal_counts[day_key]["lunch"][item_key] = meal_counts[day_key]["lunch"].get(item_key, 0) + 1
                                    except (ValueError, TypeError):
                                        pass
                            
                            elif lunch_type in ["combo-with-soup", "combo-no-soup"]:
                                # Count combos
                                combo_counts[day_key][lunch_type] = combo_counts[day_key].get(lunch_type, 0) + 1
                                
                                # Count individual items within combos
                                if isinstance(lunch_items, dict):
                                    day_lunch_menu = self.menu.get(day_key, {}).get("lunch", [])
                                    
                                    # Count soup for combo-with-soup
                                    if lunch_type == "combo-with-soup":
                                        soup_index_obj = lunch_items.get("soup_index")
                                        if soup_index_obj is not None:
                                            try:
                                                soup_index = int(soup_index_obj)
                                                if 0 <= soup_index < len(day_lunch_menu):
                                                    item_key = f"lunch_combo_soup_{soup_index}"
                                                    meal_counts[day_key]["lunch"][item_key] = meal_counts[day_key]["lunch"].get(item_key, 0) + 1
                                            except (ValueError, TypeError):
                                                pass
                                    
                                    # Count main, side, salad for combos
                                    for category in ["main", "side", "salad"]:
                                        item_index_obj = lunch_items.get(f"{category}_index")
                                        if item_index_obj is not None:
                                            try:
                                                item_index = int(item_index_obj)
                                                if 0 <= item_index < len(day_lunch_menu):
                                                    item_key = f"lunch_combo_{category}_{item_index}"
                                                    meal_counts[day_key]["lunch"][item_key] = meal_counts[day_key]["lunch"].get(item_key, 0) + 1
                                            except (ValueError, TypeError):
                                                pass
                    
                    # Process dinner
                    if day_order and "dinner" in day_order:
                        dinner_indices = day_order.get("dinner", [])
                        day_dinner_menu = self.menu.get(day_key, {}).get("dinner", [])
                        for item_index_obj in dinner_indices:
                            try:
                                item_index = int(item_index_obj)
                                if 0 <= item_index < len(day_dinner_menu):
                                    item_key = f"dinner_{item_index}"
                                    meal_counts[day_key]["dinner"][item_key] = meal_counts[day_key]["dinner"].get(item_key, 0) + 1
                            except (ValueError, TypeError):
                                pass
            
            # Generate summary rows
            for day_key in sorted(meal_counts.keys()):
                day_name_ru = day_names_ru.get(day_key, day_key.capitalize())
                
                # Add combo summary rows first
                if day_key in combo_counts:
                    for combo_type, count in combo_counts[day_key].items():
                        combo_name = self.COMBO_WITH_SOUP_RU if combo_type == "combo-with-soup" else self.COMBO_NO_SOUP_RU
                        combo_price = LUNCH_WITH_SOUP_PRICE if combo_type == "combo-with-soup" else LUNCH_NO_SOUP_PRICE
                        total = combo_price * count
                        
                        summary_rows.append([
                            day_name_ru,
                            "Обед",
                            combo_name, 
                            combo_price,
                            count,
                            total
                        ])
                
                # Add individual meal items
                day_menu_config = self.menu.get(day_key, {})
                
                # Lunch items
                lunch_menu = day_menu_config.get("lunch", [])
                for item_key, count in meal_counts[day_key]["lunch"].items():
                    if item_key.startswith("lunch_individual_"):
                        item_index = int(item_key.split("_")[-1])
                        if 0 <= item_index < len(lunch_menu):
                            item = lunch_menu[item_index]
                            price = item.get("price", 0)
                            total = price * count
                            
                            summary_rows.append([
                                day_name_ru,
                                "Обед",
                                item.get("title_ru", f"Item {item_index}"),
                                price,
                                count, 
                                total
                            ])
                    
                    elif item_key.startswith("lunch_combo_"):
                        # For combo items, mark price as "Комбо" and no sum
                        parts = item_key.split("_")
                        if len(parts) >= 4:
                            category = parts[2]  # soup, main, side, salad
                            item_index = int(parts[3])
                            if 0 <= item_index < len(lunch_menu):
                                item = lunch_menu[item_index]
                                
                                summary_rows.append([
                                    day_name_ru,
                                    "Обед",
                                    item.get("title_ru", f"Item {item_index}"),
                                    "Комбо",
                                    count,
                                    ""  # No sum for combo components
                                ])
                
                # Dinner items  
                dinner_menu = day_menu_config.get("dinner", [])
                for item_key, count in meal_counts[day_key]["dinner"].items():
                    if item_key.startswith("dinner_"):
                        item_index = int(item_key.split("_")[-1])
                        if 0 <= item_index < len(dinner_menu):
                            item = dinner_menu[item_index]
                            price = item.get("price", 0)
                            total = price * count
                            
                            summary_rows.append([
                                day_name_ru,
                                "Ужин",
                                item.get("title_ru", f"Item {item_index}"),
                                price,
                                count,
                                total
                            ])
            
            if len(summary_rows) <= 1:
                await update.reply("Нет данных для сводки по блюдам.")
                return
            
            # Create and send the summary CSV
            summary_output = io.StringIO()
            summary_writer = csv.writer(summary_output)
            summary_writer.writerows(summary_rows)
            summary_csv_data = summary_output.getvalue().encode('utf-8')
            summary_output.close()
            
            summary_filename = f"meal_summary_{datetime.datetime.now().strftime('%Y-%m-%d_%H-%M-%S')}.csv"
            summary_input_file = InputFile(io.BytesIO(summary_csv_data), filename=summary_filename)
            await update.update.effective_message.reply_document(
                document=summary_input_file, 
                caption="Сводка по блюдам экспортирована."
            )
            
        except Exception as e:
            logger.error(f"Error exporting meal summary: {e}", exc_info=True)
            await update.reply(f"Произошла ошибка при экспорте сводки: {e}")

    def _extract_meal_textual_contents(self, meal_type: str, day_key: str, order_details_for_day: dict, day_menu_config: dict) -> str:
        """
        Extract textual contents for a specific meal type and day.
        
        Args:
            meal_type: Either "lunch" or "dinner"
            day_key: Day identifier (e.g., "friday", "saturday", "sunday")
            order_details_for_day: Order details for the specific day
            day_menu_config: Menu configuration for the specific day
            
        Returns:
            String description of the meal contents
        """
        items_str = ""

        if meal_type == "lunch":
            lunch_details = order_details_for_day.get("lunch")
            if lunch_details:
                lunch_type_selection = lunch_details.get("type")
                lunch_items_payload = lunch_details.get("items") 

                if lunch_type_selection == "no-lunch":
                    items_str = self.NO_LUNCH_RU
                elif lunch_type_selection == "individual-items":
                    item_indices = lunch_items_payload if isinstance(lunch_items_payload, list) else []
                    day_lunch_menu_config_individual = day_menu_config.get("lunch", [])
                    names = []
                    for item_index_obj in item_indices:
                        try:
                            item_index = int(item_index_obj)
                            if 0 <= item_index < len(day_lunch_menu_config_individual):
                                names.append(day_lunch_menu_config_individual[item_index].get("title_ru", f"Item {item_index}"))
                        except (ValueError, TypeError):
                            pass 
                    items_str = ", ".join(names)
                elif lunch_type_selection == "combo-with-soup" or lunch_type_selection == "combo-no-soup":
                    combo_base_name = self.COMBO_WITH_SOUP_RU if lunch_type_selection == "combo-with-soup" else self.COMBO_NO_SOUP_RU
                    combo_item_names = []
                    current_combo_items_payload = lunch_items_payload if isinstance(lunch_items_payload, dict) else {}
                    day_lunch_combo_menu_config = day_menu_config.get("lunch", {}) 
                    
                    lunch_sub_categories_for_combo = ["main", "side", "salad"]

                    if lunch_type_selection == "combo-with-soup":
                        soup_index_obj = current_combo_items_payload.get("soup_index")
                        if soup_index_obj is not None:
                            try:
                                soup_index = int(soup_index_obj)
                                if 0 <= soup_index < len(day_lunch_combo_menu_config):
                                    combo_item_names.append(day_lunch_combo_menu_config[soup_index].get("title_ru", f"Soup {soup_index}"))
                            except (ValueError, TypeError):
                                pass 

                    for category in lunch_sub_categories_for_combo:
                        item_index_obj = current_combo_items_payload.get(f"{category}_index")
                        if item_index_obj is not None:
                            try:
                                item_index = int(item_index_obj)
                                if 0 <= item_index < len(day_lunch_combo_menu_config):
                                    combo_item_names.append(day_lunch_combo_menu_config[item_index].get("title_ru", f"{category.capitalize()} {item_index}"))
                            except (ValueError, TypeError):
                                pass 
                    
                    if combo_item_names:
                        items_str = f"{combo_base_name}: {', '.join(combo_item_names)}"
                    else:
                        items_str = combo_base_name
        elif meal_type == "dinner":
            dinner_indices = order_details_for_day.get("dinner", [])
            day_dinner_menu_config = day_menu_config.get("dinner", [])
            if dinner_indices and day_dinner_menu_config:
                names = []
                for item_index_obj in dinner_indices:
                    try:
                        item_index = int(item_index_obj)
                        if 0 <= item_index < len(day_dinner_menu_config):
                            names.append(day_dinner_menu_config[item_index].get("title_ru", f"Item {item_index}"))
                    except (ValueError, TypeError):
                        pass 
                items_str = ", ".join(names)

        return items_str

    async def get_order_contents_by_user(self, user_id: int) -> dict:
        """
        Get the order contents for a specific user.
        Args:
            user_id: The ID of the user whose order contents are to be retrieved.
        
        Returns:
            Dictionary in format {day: {meal: "textual contents"}, "activities": [list of selected activities]}
            Example: {"friday": {"lunch": "Комбо с супом; Борщ, Котлета, Рис, Салат", "dinner": "Суп, Хлеб"}, "activities": ["Вечеринка", "Какао церемония"]}
        """
        if not self.menu:
            return {}
            
        # Find the user's order
        order = await self.food_db.find_one({
            "user_id": user_id,
            "pass_key": PASS_KEY,
            "payment_status": "paid",
            "payment_confirmed_date": {"$exists": True},
        })
        if not order:
            return {}
            
        result = {}
        order_details = order.get("order_details", {})
        
        # Process each day in the menu
        for day_key in self.menu.keys():
            day_menu_config = self.menu[day_key]
            order_details_for_day = order_details.get(day_key, {})
            day_result = {}
            
            # Process lunch if available for this day
            if day_menu_config.get("lunch"):
                lunch_contents = self._extract_meal_textual_contents("lunch", day_key, order_details_for_day, day_menu_config)
                if lunch_contents:  # Only add if there's content
                    day_result["lunch"] = lunch_contents
                    
            # Process dinner if available for this day  
            if day_menu_config.get("dinner"):
                dinner_contents = self._extract_meal_textual_contents("dinner", day_key, order_details_for_day, day_menu_config)
                if dinner_contents:  # Only add if there's content
                    day_result["dinner"] = dinner_contents
                    
            # Only add the day if it has meal contents
            if day_result:
                result[day_key] = day_result
        
        # Process activities - add selected activities with localization pattern
        activities = order.get("activities", {})
        selected_activities = []
        for activity_key in ACTIVITIES:
            if activities.get(activity_key, False):
                act_name = self.base_app.localization(
                    f"activity-{activity_key}",
                    args={},
                    locale="ru",
                )
                selected_activities.append(act_name)

        # Only add activities if any are selected
        if selected_activities:
            result["activities"] = selected_activities
                
        return result
