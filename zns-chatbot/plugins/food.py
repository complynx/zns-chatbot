from ..config import full_link
from ..tg_state import TGState
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler
from telegram.constants import ParseMode
from motor.core import AgnosticCollection
import datetime # Added for timestamping orders
import json # Added for loading menu
from ..plugins.passes import PASS_KEY # Import PASS_KEY
from random import choice # Added for selecting admin
from bson import ObjectId # Added for MongoDB ObjectId
from telegram import ReplyKeyboardMarkup, ReplyKeyboardRemove # Added for payment proof input
from ..telegram_links import client_user_link_html, client_user_name # For admin/user info
import logging # Ensure logging is imported

logger = logging.getLogger(__name__)

# Constants for price calculation based on menu.mjs logic
LUNCH_NO_SOUP_PRICE = 555
LUNCH_WITH_SOUP_PRICE = 665
LUNCH_SUB_CATEGORIES = ["main", "side", "salad"]
CANCEL_CHR = chr(0xE007F) # Tag cancel, similar to passes.py


class FoodUpdate:
    base: 'Food'
    update: TGState
    bot: int
    user: dict | None
    pass_key: str

    def __init__(self, base: 'Food', update: TGState) -> None:
        self.base = base
        self.update = update
        self.l = update.l # noqa: E741
        self.tgUpdate = update.update
        self.bot = self.update.bot.id
        self.user = None
        self.pass_key = PASS_KEY # Use the imported PASS_KEY

    async def get_user(self):
        if self.user is None:
            self.user = await self.update.get_user() # Populate self.user if it's None
        return self.user

    async def handle_start(self):
        user = await self.get_user()
        if not user:
            await self.update.reply(self.l("user-is-none"), parse_mode=ParseMode.HTML) # Handle missing user
            return

        food_order = await self.base.get_order(user["user_id"], self.pass_key)
        
        # Prepare initial message and buttons (without message-specific IDs in WebApp URL yet)
        initial_message_text, initial_buttons_list = await self._prepare_order_message_and_buttons(
            food_order, 
            self.pass_key
        )
        
        sent_message = await self.update.reply(
            initial_message_text, 
            reply_markup=InlineKeyboardMarkup(initial_buttons_list) if initial_buttons_list else None, 
            parse_mode=ParseMode.HTML
        )

        if sent_message:
            actual_msg_id = sent_message.message_id
            actual_chat_id = sent_message.chat.id

            # Re-prepare buttons with the final WebApp URL including message/chat IDs
            _, final_buttons_list = await self._prepare_order_message_and_buttons(
                food_order,
                self.pass_key,
                actual_msg_id,
                actual_chat_id
            )

            try:
                await self.update.bot.edit_message_reply_markup(
                    chat_id=actual_chat_id,
                    message_id=actual_msg_id,
                    reply_markup=InlineKeyboardMarkup(final_buttons_list)
                )
            except Exception as e:
                logger.error(f"Failed to edit reply markup for message {actual_msg_id} in chat {actual_chat_id} for user {user.get('user_id')}: {e}. WebApp URL will not have full origin context.")
        else:
            logger.error(f"Failed to send initial message in handle_start for user {user.get('user_id') if user else 'unknown'}")

    async def _prepare_order_message_and_buttons(self, order_doc: dict | None, pass_key: str, orig_msg_id: int | None = None, orig_chat_id: int | None = None) -> tuple[str, list]:
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
        
        # Add origin_info to the button URLs if available
        final_menu_url_params = menu_url_params_base
        if orig_msg_id is not None and orig_chat_id is not None:
            final_menu_url_params += f"&orig_msg_id={orig_msg_id}&orig_chat_id={orig_chat_id}"
        
        menu_url_for_buttons = full_link(self.base.base_app, f"/menu?{final_menu_url_params}")

        if order_doc:
            order_total = order_doc.get("total", 0.0) 
            order_id_str = str(order_doc['_id'])

            # Logic adapted from existing handle_start and show_order_status_after_save
            # Case 1: Order effectively doesn't exist or is empty (and not paid/submitted)
            # This condition might need refinement based on exactly how get_order behaves when an order is "deleted" (empty order_details)
            if not order_doc.get("order_details") and not order_doc.get("is_complete") and \
               order_doc.get("payment_status") not in ["paid", "proof_submitted"]:
                message_text = self.l("food-no-order")
                buttons.append([InlineKeyboardButton(self.l("food-button-create-order"), web_app=WebAppInfo(menu_url_for_buttons))])
            elif order_doc.get("payment_status") == "paid":
                message_text = self.l("food-order-is-paid", total=order_total)
                buttons.append([InlineKeyboardButton(self.l("food-button-view-order"), web_app=WebAppInfo(menu_url_for_buttons))])
            elif order_doc.get("payment_status") == "proof_submitted":
                message_text = self.l("food-order-proof-submitted", total=order_total)
                buttons.append([InlineKeyboardButton(self.l("food-button-view-order"), web_app=WebAppInfo(menu_url_for_buttons))])
            elif order_doc.get("is_complete"):
                message_text = self.l("food-order-exists-payable", total=order_total)
                buttons.append([InlineKeyboardButton(self.l("food-button-pay"), callback_data=f"{self.base.name}|pay|{order_id_str}")])
                buttons.append([InlineKeyboardButton(self.l("food-button-edit-order"), web_app=WebAppInfo(menu_url_for_buttons))])
                buttons.append([InlineKeyboardButton(self.l("food-button-delete-order"), callback_data=f"{self.base.name}|delete_order|{order_id_str}")])
            else:  # Order exists but not complete
                message_text = self.l("food-order-exists-not-complete", total=order_total)
                buttons.append([InlineKeyboardButton(self.l("food-button-edit-order"), web_app=WebAppInfo(menu_url_for_buttons))])
                buttons.append([InlineKeyboardButton(self.l("food-button-delete-order"), callback_data=f"{self.base.name}|delete_order|{order_id_str}")])
        else: # No order_doc (e.g., first time interaction, or order was truly deleted and get_order returns None)
            message_text = self.l("food-no-order")
            buttons.append([InlineKeyboardButton(self.l("food-button-create-order"), web_app=WebAppInfo(menu_url_for_buttons))])
        
        buttons.append([InlineKeyboardButton(self.l("food-button-exit"), callback_data=f"{self.base.name}|exit")])
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
            lc = self.update.language_code # Ensure language code is set, defaulting to current user's language
        keys = {}
        keys["adminEmoji"] = admin.get("emoji", "")
        keys["adminLink"] = client_user_link_html(admin, language_code=lc)
        keys["adminName"] = client_user_name(admin, language_code=lc)
        keys["phoneContact"] = admin.get("phone_contact", "nophone")
        keys["phoneSBP"] = admin.get("phone_sbp", "nosbp")
        # Align bank info fetching with passes.py style
        keys["banks"] = admin.get("banks_en", "")  # Default to English bank info
        keys["banks"] = admin.get(f"banks_{lc}", keys["banks"]) # Override with language-specific if available, else keep English
        return keys

    async def handle_cq_pay(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(self.l("food-order-not-found"), parse_mode=ParseMode.HTML)
            return

        order = await self.base.food_db.find_one({"_id": order_id, "user_id": self.update.user})

        if not order:
            await self.update.reply(self.l("food-order-not-found"), parse_mode=ParseMode.HTML)
            return

        if not order.get("is_complete"):
            await self.update.reply(self.l("food-order-not-complete-for-payment"), parse_mode=ParseMode.HTML)
            return

        payment_status = order.get("payment_status")
        order_total = order.get("total", 99999.0)

        if payment_status == "paid":
            await self.update.reply(self.l("food-order-already-paid"), parse_mode=ParseMode.HTML)
            return
        if payment_status == "proof_submitted":
            await self.update.reply(self.l("food-order-proof-already-submitted"), parse_mode=ParseMode.HTML)
            return
        
        # Ensure an admin is assigned for payment proof
        if not order.get("proof_admin"):
            if self.base.food_admins:
                assigned_admin_id = choice(self.base.food_admins)
                await self.base.food_db.update_one(
                    {"_id": order_id},
                    {"$set": {"proof_admin": assigned_admin_id}}
                )
                order["proof_admin"] = assigned_admin_id
            else:
                logger.error("No food_admins configured to assign for payment.")
                await self.update.reply(self.l("food-payment-admins-not-configured"), parse_mode=ParseMode.HTML)
                return
        
        proof_admin_id = order.get("proof_admin")
        # This check is now more critical if the choice above failed or food_admins was empty.
        if not proof_admin_id:
             logger.error(f"Order {order_id} has no proof_admin after attempting assignment.")
             await self.update.reply(self.l("food-payment-admins-not-configured"), parse_mode=ParseMode.HTML)
             return

        proof_admin_user_obj = await self.base.user_db.find_one({"user_id": proof_admin_id, "bot_id": self.bot})

        if not proof_admin_user_obj:
            logger.error(f"Food payment admin user object not found for ID: {proof_admin_id}")
            await self.update.reply(self.l("food-payment-admin-error"), parse_mode=ParseMode.HTML)
            return

        payment_detail_keys = self.admin_to_keys(proof_admin_user_obj, self.update.language_code)
        payment_detail_keys["total"] = order_total

        await self.update.edit_message_text( # Assuming this is an edit of a message with a "Pay" button
            self.l("food-payment-request-callback-message", **payment_detail_keys),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]) 
        )

        await self.update.reply(
            self.l("food-payment-request-waiting-message", **payment_detail_keys),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup([[CANCEL_CHR + self.l("cancel-command")]], resize_keyboard=True),
        )
        await self.update.require_anything(self.base.name, "handle_payment_proof_input", str(order_id), "handle_payment_proof_timeout")


    async def handle_payment_proof_input(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(self.l("food-order-not-found"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
            return

        if self.update.message and self.update.message.text and self.update.message.text.startswith(CANCEL_CHR):
            await self.update.reply(self.l("food-payment-proof-cancelled"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
            return

        if not (self.update.message and (self.update.message.photo or self.update.message.document)):
            await self.update.reply(self.l("food-payment-proof-wrong-data"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
            return


        file_id = ""
        file_ext = ".dat" # Default extension
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
            

        order = await self.base.food_db.find_one({"_id": order_id, "user_id": self.update.user})
        if not order or order.get("payment_status") not in [None, "rejected"] or not order.get("proof_admin"):
            await self.update.reply(self.l("food-cannot-submit-proof-now"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
            return
        
        proof_admin_id = order.get("proof_admin")
        # This check should ideally not be needed if logic in handle_cq_pay is robust
        if not proof_admin_id:
            logger.error(f"Order {order_id} missing proof_admin during proof submission.")
            await self.update.reply(self.l("food-payment-admin-error"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
            return

        update_result = await self.base.food_db.update_one(
            {"_id": order_id},
            {
                "$set": {
                    "payment_status": "proof_submitted",
                    "proof_file": f"{file_id}{file_ext}",
                    "proof_received_date": datetime.datetime.now(datetime.timezone.utc)
                }
            }
        )

        if update_result.modified_count == 0:
            await self.update.reply(self.l("food-order-proof-already-submitted"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)
            return

        await self.update.reply(self.l("food-payment-proof-forwarded"), reply_markup=ReplyKeyboardRemove(), parse_mode=ParseMode.HTML)

        admin_user_obj = await self.base.user_db.find_one({"user_id": proof_admin_id, "bot_id": self.bot})
        client_user_obj = await self.get_user() # Ensures self.user is populated

        if admin_user_obj and client_user_obj:
            admin_lang = admin_user_obj.get("language_code", "en") # Default to 'en' if admin has no lang set
            
            # Create a temporary TGState for the admin to use their localization
            admin_tg_state = TGState(admin_user_obj["user_id"], self.base.base_app, bot_instance=self.update.bot)
            admin_tg_state.language_code = admin_lang # Set admin's language
            admin_l = admin_tg_state.l 

            forward_message = await self.update.forward_message(proof_admin_id)
            if forward_message:
                 await self.update.bot.send_message(
                    chat_id=proof_admin_id,
                    text=admin_l(
                        "food-adm-payment-proof-received",
                        link=client_user_link_html(client_user_obj, language_code=admin_lang),
                        total=order.get("total", 99999.0)
                    ),
                    reply_markup=InlineKeyboardMarkup([[
                        InlineKeyboardButton(admin_l("food-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{order_id_str}"),
                        InlineKeyboardButton(admin_l("food-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{order_id_str}")
                    ]]),
                    parse_mode=ParseMode.HTML
                )
            else:
                logger.error(f"Failed to forward payment proof message for order {order_id_str} to admin {proof_admin_id}")
                # Optionally notify client or try another way
        else:
            logger.error(f"Could not find admin ({proof_admin_id}) or client ({self.update.user}) for payment proof notification of order {order_id_str}")

    async def handle_payment_proof_timeout(self, order_id_str: str):
        await self.update.reply(
            self.l("food-payment-proof-timeout"),
            reply_markup=ReplyKeyboardRemove(),
            parse_mode=ParseMode.HTML
        )

    async def notify_payment_status(self, status_key: str): # status_key e.g. "food-payment-proof-accepted"
        if not self.update: # self.update is the client's TGState
            logger.error("notify_payment_status called without a valid client TGState.")
            return

        client_user_id = self.update.user 
        if not client_user_id:
            logger.error("notify_payment_status: client_user_id is missing from TGState.")
            return

        order = await self.base.get_order(client_user_id, self.pass_key)

        if not order:
            logger.warning(f"notify_payment_status: No order found for user {client_user_id} with pass_key {self.pass_key} to notify status {status_key}.")
            return

        order_id = order["_id"]
        order_total = order.get("total", 0.0) 

        client_user_obj = await self.update.get_user() 

        client_name_on_order = order.get("name_on_order", None)
        if not client_name_on_order and client_user_obj:
            client_name_on_order = client_user_name(client_user_obj, language_code=self.update.language_code)
        elif not client_name_on_order:
             client_name_on_order = "Customer" # Fallback

        message_text = self.update.l(
            status_key,
            name=client_name_on_order, # Use name on order if available
            total=order_total,
            orderId=str(order_id) # Pass orderId if FTL needs it
        )

        try:
            await self.update.bot.send_message(
                chat_id=client_user_id, # Send to the client
                text=message_text,
                parse_mode=ParseMode.HTML
            )
            logger.info(f"Notified client {client_user_id} about order {order_id} status: {status_key}")
        except Exception as e:
            logger.error(f"Failed to send payment status notification {status_key} to client {client_user_id} for order {order_id}: {e}", exc_info=True)

    async def handle_cq_adm_acc(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML)
            return

        if self.update.user not in self.base.food_admins:
            await self.update.reply(self.l("food-not-authorized-admin"), parse_mode=ParseMode.HTML)
            return
            
        order = await self.base.food_db.find_one({"_id": order_id})
        if not order:
            await self.update.reply(self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML)
            return

        if order.get("payment_status") == "paid":
            await self.update.edit_message_text(
                self.l("food-adm-payment-already-processed-or-error"),
                reply_markup=None, # Remove buttons
                parse_mode=ParseMode.HTML
            )
            return

        result = await self.base.food_db.update_one(
            {"_id": order_id, "payment_status": "proof_submitted"},
            {"$set": {"payment_status": "paid", "payment_confirmed_by": self.update.user, "payment_confirmed_date": datetime.datetime.now(datetime.timezone.utc)}}
        )

        if result.modified_count > 0:
            await self.update.edit_message_text( # Edit admin's message
                self.l("food-adm-payment-accepted-msg", orderId=order_id_str),
                reply_markup=None, # Remove buttons
                parse_mode=ParseMode.HTML
            )
            # Notify client
            client_user_id = order.get("user_id")
            if client_user_id:
                client_update = await self.base.create_food_update_for_user(client_user_id)
                if client_update:
                    await client_update.notify_payment_status("food-payment-proof-accepted")
        else:
            await self.update.edit_message_text(
                 self.l("food-adm-payment-already-processed-or-error"),
                 reply_markup=None, # Remove buttons
                 parse_mode=ParseMode.HTML
            )
            
    async def handle_cq_adm_rej(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML)
            return

        if self.update.user not in self.base.food_admins:
            await self.update.reply(self.l("food-not-authorized-admin"), parse_mode=ParseMode.HTML)
            return
        
        order = await self.base.food_db.find_one({"_id": order_id})
        if not order:
            await self.update.reply(self.l("food-order-not-found-admin"), parse_mode=ParseMode.HTML)
            return

        if order.get("payment_status") == "rejected" or order.get("payment_status") == "paid":
             await self.update.edit_message_text(
                self.l("food-adm-payment-already-processed-or-error"),
                reply_markup=None, # Remove buttons
                parse_mode=ParseMode.HTML
            )
             return

        result = await self.base.food_db.update_one(
            {"_id": order_id, "payment_status": "proof_submitted"},
            {"$set": {"payment_status": "rejected", "payment_rejected_by": self.update.user, "payment_rejected_date": datetime.datetime.now(datetime.timezone.utc)}}
        )
        if result.modified_count > 0:
            await self.update.edit_message_text( # Edit admin's message
                self.l("food-adm-payment-rejected-msg", orderId=order_id_str),
                reply_markup=None, # Remove buttons
                parse_mode=ParseMode.HTML
            )
            # Notify client
            client_user_id = order.get("user_id")
            if client_user_id:
                client_update = await self.base.create_food_update_for_user(client_user_id)
                if client_update:
                    await client_update.notify_payment_status("food-payment-proof-rejected-retry")
        else:
            await self.update.edit_message_text(
                 self.l("food-adm-payment-already-processed-or-error"),
                 reply_markup=None, # Remove buttons
                 parse_mode=ParseMode.HTML
            )

    async def handle_cq_exit(self): # New handler for exit button
        await self.update.edit_message_text(
            self.l("food-message-exited"),
            reply_markup=None, # Remove buttons
            parse_mode=ParseMode.HTML
        )

    async def handle_cq_delete_order(self, order_id_str: str):
        try:
            order_id = ObjectId(order_id_str)
        except Exception:
            await self.update.reply(self.l("food-order-not-found"), parse_mode=ParseMode.HTML)
            return

        user_id = self.update.user
        if not user_id:
            await self.update.reply(self.l("user-is-none"), parse_mode=ParseMode.HTML)
            return

        # Fetch the order directly to check its current state
        order = await self.base.food_db.find_one({"_id": order_id, "user_id": user_id})

        if not order:
            await self.update.reply(self.l("food-order-not-found"), parse_mode=ParseMode.HTML)
            return

        if order.get("payment_status") in ["paid", "proof_submitted"]:
            await self.update.edit_message_text(
                self.l("food-order-cannot-delete-paid-submitted"),
                reply_markup=None, # Remove buttons or keep existing?
                parse_mode=ParseMode.HTML
            )
            # We might want to call show_order_status_after_save here if we want to re-render the buttons
            # For now, just inform and remove buttons from the confirmation message context.
            return

        # To "delete", we save it with empty order_details.
        # The save_order method needs origin_info to update the message correctly.
        # We should try to get it from the current callback query's message if possible,
        # or from the order if it was stored previously.
        original_message_id = self.update.callback_query.message.message_id
        chat_id = self.update.callback_query.message.chat.id

        pass_key_to_use = order.get("pass_key", self.pass_key)
        updated_order = await self.base.save_order(
            user_id=user_id, 
            pass_key=pass_key_to_use, 
            order_data={}, # Empty data to signify deletion
            original_message_id=original_message_id, 
            chat_id=chat_id
        )
        
        # save_order calls show_order_status_after_save, which should update the original message.
        # We can optionally send a confirmation message here if needed, but the message should already be updated.
        # For example, if the message is edited to show "No order", that acts as confirmation.
        # If we explicitly want to say "Order deleted" before it switches to "No order", we could add an edit here.
        # However, relying on show_order_status_after_save is cleaner.
        if updated_order is None: # This implies the order is now considered non-existent
            await self.update.edit_message_text( # Fallback edit if show_order_status_after_save didn't run or failed
                self.l("food-order-deleted-successfully"),
                reply_markup=None,
                parse_mode=ParseMode.HTML
            )
        # No explicit success message here as show_order_status_after_save should handle the UI update.


    async def show_order_status_after_save(self, order_doc: dict):
        origin_info = order_doc.get("origin_info")
        if not origin_info or not origin_info.get("message_id") or not origin_info.get("chat_id"):
            logger.warning(f"Order {order_doc.get('_id')} saved, but no origin_info to update message.")
            # Optionally send a new message if chat_id is known (e.g., self.update.chat_id)
            # but for now, if origin_info is incomplete, we can't target the original message.
            return

        original_message_id = origin_info["message_id"]
        target_chat_id = origin_info["chat_id"] # This is the chat where the original message was.

        user = await self.get_user()
        if not user:
            logger.error(f"Cannot show order status after save for user {self.update.user}: user object not found.")
            return

        pass_key_from_order = order_doc.get("pass_key", self.pass_key)
        
        message_text, buttons = await self._prepare_order_message_and_buttons(
            order_doc, 
            pass_key_from_order, 
            original_message_id, 
            target_chat_id
        )
        
        reply_markup = InlineKeyboardMarkup(buttons) if buttons else None

        try:
            await self.update.bot.edit_message_text(
                chat_id=target_chat_id,
                message_id=original_message_id,
                text=message_text,
                reply_markup=reply_markup,
                parse_mode=ParseMode.HTML
            )
            logger.info(f"Successfully edited message {original_message_id} in chat {target_chat_id} after order save for user {self.update.user}.")
        except Exception as e:
            logger.warning(f"Failed to edit message {original_message_id} in chat {target_chat_id} for user {self.update.user} (Error: {e}). Sending new message.", exc_info=False) # exc_info=False if too noisy
            try:
                await self.update.bot.send_message(
                    chat_id=target_chat_id, # Send to the same chat
                    text=message_text,
                    reply_markup=reply_markup,
                    parse_mode=ParseMode.HTML
                )
                logger.info(f"Sent new message to chat {target_chat_id} for user {self.update.user} after failed edit.")
            except Exception as e_send:
                logger.error(f"Failed to send new message to chat {target_chat_id} for user {self.update.user} (Error: {e_send}).", exc_info=True)


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

        self.food_admins = [int(admin_id) for admin_id in self.config.food.admins]

        self._command_checkers = [
            CommandHandler("food", self.handle_food_start_cmd)
        ]
        self._cbq_handler = CallbackQueryHandler(self.handle_food_callback_query_entry, pattern=f"^{self.name}\\|.*")

    def _load_menu(self) -> dict:
        import os
        from os.path import dirname as d
        with open(os.path.join(d(d(d(__file__))), "static", "menu_2025_1.json"), 'r', encoding='utf-8') as f:
            menu_data = json.load(f)
        
        return menu_data

    async def get_order(self, user_id: int, pass_key: str = PASS_KEY) -> dict | None:
        logger.debug(f"Attempting to get order for user_id: {user_id}, pass_key: {pass_key}")
        order = await self.food_db.find_one({
            "user_id": user_id,
            "pass_key": pass_key
        })
        if order:
            logger.debug(f"Order found for user_id: {user_id}, pass_key: {pass_key}. Order ID: {order.get('_id')}")
            # If order_details is None or an empty dict, and status is not protective, treat as no order.
            # An order with explicit "no-lunch" selections will have order_details populated.
            if not order.get("order_details") and order.get("payment_status") not in ["paid", "proof_submitted"]:
                logger.info(f"Order {order.get('_id')} for user {user_id} has empty/null details and is not paid/proof_submitted. Treating as no order.")
                return None
        else:
            logger.debug(f"No order found for user_id: {user_id}, pass_key: {pass_key}")
        return order

    async def get_order_by_id(self, order_id: ObjectId, pass_key: str) -> dict | None:
        logger.debug(f"Attempting to get order by order_id: {order_id}, pass_key: {pass_key}")
        order = await self.food_db.find_one({
            "_id": order_id,
            "pass_key": pass_key  # Ensure pass_key is checked
        })
        if order:
            logger.debug(f"Order found for order_id: {order_id}, pass_key: {pass_key}. User ID: {order.get('user_id')}")
            # If order_details is None or an empty dict, and status is not protective, treat as no order.
            if not order.get("order_details") and order.get("payment_status") not in ["paid", "proof_submitted"]:
                logger.info(f"Order {order.get('_id')} (user {order.get('user_id')}) has empty/null details and is not paid/proof_submitted. Treating as no order.")
                return None
        else:
            logger.debug(f"No order found for order_id: {order_id}, pass_key: {pass_key}")
        return order

    async def save_order(self, user_id: int, pass_key: str, order_data: dict, original_message_id: int | None = None, chat_id: int | None = None) -> dict | None:
        if not self.menu:
            logger.error("Menu not loaded. Cannot save order.")
            return None

        # Fetch existing order details first to check its status and content
        existing_order = await self.food_db.find_one({"user_id": user_id, "pass_key": pass_key})
        current_payment_status_from_db = existing_order.get("payment_status") if existing_order else None
        stored_order_details = existing_order.get("order_details") if existing_order else None
        stored_total = existing_order.get("total") if existing_order else None
        current_proof_admin_from_db = existing_order.get("proof_admin") if existing_order else None

        calculated_total_sum = 0
        order_is_complete = True 

        # --- Existing loop to calculate calculated_total_sum and order_is_complete from new order_data ---
        for day_key, day_menu_config in self.menu.items():
            day_has_lunch_in_menu = bool(day_menu_config.get("lunch"))
            order_for_this_day = order_data.get(day_key)

            # --- Lunch Completeness & Sum Calculation ---
            if day_has_lunch_in_menu:
                if not order_for_this_day or "lunch" not in order_for_this_day:
                    order_is_complete = False # Lunch data missing for a day that offers lunch
                    # Continue to check other days, but order is already incomplete
                else:
                    lunch_order_details = order_for_this_day["lunch"]
                    if not lunch_order_details: # e.g. "lunch": null
                         order_is_complete = False
                    else:
                        lunch_type = lunch_order_details.get("type")
                        lunch_items = lunch_order_details.get("items", {})
                        day_lunch_combo_complete_for_pricing = True

                        if lunch_type == "no-lunch":
                            pass # Considered complete for this day\'s lunch, no combo price added
                        elif lunch_type == "with-soup":
                            if lunch_items.get("soup_index") is None:
                                order_is_complete = False
                                day_lunch_combo_complete_for_pricing = False
                            for category in LUNCH_SUB_CATEGORIES:
                                if lunch_items.get(f"{category}_index") is None:
                                    order_is_complete = False
                                    day_lunch_combo_complete_for_pricing = False
                                    break
                            if day_lunch_combo_complete_for_pricing:
                                calculated_total_sum += LUNCH_WITH_SOUP_PRICE
                        elif lunch_type == "no-soup":
                            for category in LUNCH_SUB_CATEGORIES:
                                if lunch_items.get(f"{category}_index") is None:
                                    order_is_complete = False
                                    day_lunch_combo_complete_for_pricing = False
                                    break
                            if day_lunch_combo_complete_for_pricing:
                                calculated_total_sum += LUNCH_NO_SOUP_PRICE
                        else: # Invalid or missing lunch type (e.g. null, or something else)
                            order_is_complete = False
            
            # --- Dinner Sum Calculation ---
            if order_for_this_day and "dinner" in order_for_this_day:
                dinner_order_indices = order_for_this_day.get("dinner", [])
                dinner_menu_items_config = day_menu_config.get("dinner", [])

                if isinstance(dinner_order_indices, list) and dinner_menu_items_config:
                    for item_index_obj in dinner_order_indices:
                        try:
                            item_index = int(item_index_obj) # Client might send string indices
                            if 0 <= item_index < len(dinner_menu_items_config):
                                dinner_item_details = dinner_menu_items_config[item_index]
                                if dinner_item_details and isinstance(dinner_item_details.get("price"), (int, float)):
                                    calculated_total_sum += dinner_item_details["price"]
                                else:
                                    logger.warning(f"Dinner item {day_key}-dinner-{item_index} in order is missing price or has invalid price in menu config: {dinner_item_details}")
                            else:
                                logger.warning(f"Invalid dinner item index {item_index} for day {day_key} in order data. Max index: {len(dinner_menu_items_config)-1}")
                        except (ValueError, TypeError) as e:
                            logger.warning(f"Invalid dinner item index format: \'{item_index_obj}\' for day {day_key}. Error: {e}")
            elif day_menu_config.get("dinner") and not (order_for_this_day and "dinner" in order_for_this_day):
                # Dinner is offered in menu, but no dinner data in order for this day. This is fine, dinner is optional.
                pass
        # --- End of existing calculation loop ---

        # Check for modification attempts on paid/proof_submitted orders
        if existing_order and current_payment_status_from_db in ["paid", "proof_submitted"]:
            is_data_changed = (order_data != stored_order_details)
            # Compare new calculated total with the total stored from the existing order
            is_total_changed = (calculated_total_sum != stored_total)

            if is_data_changed or is_total_changed:
                error_msg = (
                    f"Attempt to modify order {existing_order['_id']} (user {user_id}) "
                    f"which is in status '{current_payment_status_from_db}'. Modifications are not allowed."
                )
                logger.error(error_msg)
                raise ValueError(error_msg)

        db_order_doc = {
            "user_id": user_id,
            "pass_key": pass_key,
            "order_details": order_data,
            "total": calculated_total_sum,
            "is_complete": order_is_complete,
            "last_updated": datetime.datetime.now(datetime.timezone.utc)
        }

        if original_message_id is not None and chat_id is not None:
            db_order_doc["origin_info"] = {"message_id": original_message_id, "chat_id": chat_id}
        
        # Determine payment_status for the document to be saved
        if order_is_complete:
            if current_payment_status_from_db in ["paid", "proof_submitted"] and \
               (not existing_order or (order_data == stored_order_details and calculated_total_sum == stored_total)):
                # If status was 'paid' or 'proof_submitted' and no actual change was made (error wasn't raised), preserve it.
                db_order_doc["payment_status"] = current_payment_status_from_db
            else:
                # Order is complete, but either wasn't paid/proof_submitted, 
                # or it was but changes were made (which would have reset it if not for the error being raised for paid/proof_submitted).
                # This path is taken if it's newly complete, or was editable and is still complete.
                db_order_doc["payment_status"] = None
        else: # Order is not complete
            db_order_doc["payment_status"] = None

        # Manage proof_admin
        if db_order_doc["payment_status"] == "paid" or db_order_doc["payment_status"] == "proof_submitted":
            # This means the status was preserved from current_payment_status_from_db
            if current_proof_admin_from_db:
                 db_order_doc["proof_admin"] = current_proof_admin_from_db
            else:
                 # This case (paid/proof_submitted but no proof_admin in existing order) implies an inconsistency.
                 # Log it, and set proof_admin to None if it was None.
                 db_order_doc["proof_admin"] = None 
                 if existing_order: # Only log warning if it's an existing order that's inconsistent
                    logger.warning(f"Order {existing_order['_id']} has status {db_order_doc['payment_status']} but no current_proof_admin_from_db during save.")
        else: # payment_status is None (incomplete, or complete but awaiting payment initiation)
            db_order_doc["proof_admin"] = None # Clear admin if not in active payment/paid state
        
        update_payload = {
            "$set": db_order_doc,
            "$setOnInsert": {"created_at": datetime.datetime.now(datetime.timezone.utc)}
        }
        
        result = await self.food_db.update_one(
            {"user_id": user_id, "pass_key": pass_key},
            update_payload,
            upsert=True
        )
        
        saved_order_doc = None # Initialize
        if result.upserted_id:
            logger.info(f"New order inserted for user_id: {user_id}, pass_key: {pass_key}. Order ID: {result.upserted_id}. Complete: {order_is_complete}, Total: {calculated_total_sum}")
            saved_order_doc = await self.get_order(user_id, pass_key)
        elif result.modified_count > 0:
            logger.info(f"Order updated for user_id: {user_id}, pass_key: {pass_key}. Complete: {order_is_complete}, Total: {calculated_total_sum}")
            saved_order_doc = await self.get_order(user_id, pass_key)
        else:
            # This case (matched_count > 0, modified_count == 0) means the new data was identical to existing.
            logger.info(f"Order for user_id: {user_id}, pass_key: {pass_key} was not modified (data identical). Complete: {order_is_complete}, Total: {calculated_total_sum}")
            # Still fetch to ensure consistency for the return value and potential message update
            saved_order_doc = await self.get_order(user_id, pass_key)

        if saved_order_doc and saved_order_doc.get("origin_info") and saved_order_doc["origin_info"].get("message_id") and saved_order_doc["origin_info"].get("chat_id"):
            client_food_update = await self.create_food_update_for_user(user_id)
            if client_food_update:
                # Ensure the TGState within client_food_update has a valid bot instance
                # This check is now implicitly handled by create_food_update_for_user if it returns a valid instance
                await client_food_update.show_order_status_after_save(saved_order_doc)
            else:
                logger.error(f"Could not create FoodUpdate for user {user_id} to update message after save.")
            
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
        await target_user_tg_state.get_state() # Load user data, language, and ensure bot is populated
        if not target_user_tg_state.bot:
            logger.error(f"Bot instance could not be determined for user {user_id} in create_food_update_for_user. FoodUpdate creation aborted.")
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
        """ Registered callback for TGState.require_anything. """
        return await self.create_update(update).handle_payment_proof_input(order_id_str)

    async def handle_payment_proof_timeout(self, update: TGState, order_id_str: str):
        """ Registered callback for TGState.require_anything. """
        return await self.create_update(update).handle_payment_proof_timeout(order_id_str)
