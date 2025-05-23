import datetime
import json
import logging
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
)
from telegram.constants import ParseMode
from telegram.error import TelegramError
from telegram.ext import CallbackQueryHandler, CommandHandler

from ..config import full_link
from ..plugins.passes import PASS_KEY
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

        order = await self.base.get_order(client_user_id, self.pass_key)

        if not order:
            logger.warning(
                f"notify_payment_status: No order found for user {client_user_id} with pass_key"
                f" {self.pass_key} to notify status {status_key}."
            )
            return

        order_id = order["_id"]
        order_total = order.get("total", 0.0)

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

        if self.update.user not in self.base.food_admins:
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

        if self.update.user not in self.base.food_admins:
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
            if e.message == "Message is not modified":
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
            {"user_id": user["_id"], "pass_key": self.pass_key}
        )
        if not order:
            order = {}

        await self.activities_message(order)

    async def activities_message(self, order: dict):
        """Generate the activities message."""

        # Create 2x2 grid of activity buttons
        buttons = []
        untoggled_activities = ACTIVITIES.copy()
        untoggled_class_activities = CLASS_ACTIVITIES.copy()
        for i in range(0, len(ACTIVITIES), 2):
            row = []
            for activity in ACTIVITIES[i : i + 2]:
                status = "☑️" if order.get("activities", {}).get(activity) else "❌"
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
            {"user_id": user["_id"], "pass_key": self.pass_key}
        )
        if not order:
            order = {"activities": {}}

        # Toggle activity state
        activities = order.get("activities", {})
        if activity == "all":
            activities = {}
            for act in ACTIVITIES:
                activities[act] = True
        elif activity == "classes":
            activities = {}
            for act in CLASS_ACTIVITIES:
                activities[act] = True
        else:
            activities[activity] = not activities.get(activity, False)
        order["activities"] = activities

        # Save order with updated activities
        result = await self.base.food_db.update_one(
            {
                "user_id": user["_id"],
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
                f"Failed to update activities for user {user['_id']} with pass_key {self.pass_key}"
            )

        await self.activities_message(order)

    async def handle_cq_submit_activities(self):
        """Handle activities submission."""
        user = await self.get_user()
        assert user is not None, "User not found"

        order = await self.base.food_db.find_one(
            {"user_id": user["_id"], "pass_key": self.pass_key}
        )
        if not order:
            order = {}
        # Get selected activities
        activities = order.get("activities", {})

        selected_activities = {
            act: str(activities.get(act, False)) for act in ACTIVITIES
        }

        await self.update.edit_message_text(
            self.l("activity-finished-message", **selected_activities),
            reply_markup=None,
            parse_mode=ParseMode.HTML,
        )


class Food(BasePlugin):
    name = "food"
    food_db: AgnosticCollection
    user_db: AgnosticCollection
    menu: dict
    food_admins: list[int]

    def __init__(self, base_app):
        super().__init__(base_app)
        self.food_db = base_app.mongodb[self.config.mongo_db.food_collection]
        self.user_db = base_app.users_collection
        self.base_app.food = self

        self.menu = self._load_menu()

        self.food_admins = [
            int(admin_id) for admin_id in self.config.food.payment_admins
        ]
        if len(self.food_admins) == 0:
            self.food_admins = [int(admin_id) for admin_id in self.config.food.admins]

        self._command_checkers = [
            CommandHandler("food", self.handle_food_start_cmd),
            CommandHandler("activities", self.handle_activities_cmd),
        ]
        self._cbq_handler = CallbackQueryHandler(
            self.handle_food_callback_query_entry, pattern=f"^{self.name}\\|.*"
        )

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
