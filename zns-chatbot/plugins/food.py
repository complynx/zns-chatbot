
from ..config import Config, full_link
from ..telegram_links import client_user_link_html
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo, ReplyKeyboardMarkup, User, ReplyKeyboardRemove, Message
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import filters, CommandHandler, CallbackQueryHandler
from telegram.constants import ChatAction, ParseMode
from motor.core import AgnosticCollection
from bson.objectid import ObjectId
import datetime
import logging

logger = logging.getLogger(__name__)


def generate_random_string(length):
    import random
    import string
    characters = string.ascii_letters + string.digits
    return ''.join(random.choice(characters) for _ in range(length))


def calculate_sha256(input_string):
    import hashlib
    sha256_hash = hashlib.sha256(input_string.encode()).hexdigest()
    return sha256_hash

days_seq = ["wednesday", "friday", "saturday", "sunday"]
meals_seq = ["lunch", "dinner"]
meal_ids_seq = []
for day in days_seq:
    for meal in meals_seq:
        meal_ids_seq.append(day+"_"+meal)

def order_total(carts, menu):
    total = 0
    for cart in carts.values():
        for item_id in cart["items"]:
            item_m = menu["items"][item_id]
            total += item_m["price"]
    return total

def order_text(l, order, menu):
    ret = []
    total = 0
    if "carts" in order:
        for meal_id in meal_ids_seq:
            if not meal_id in order["carts"]:
                continue
            cart = order["carts"][meal_id]
            items_data = {}
            meal_total = 0
            for item_id in cart["items"]:
                if item_id in items_data:
                    items_data[item_id]["count"] += 1
                item_m = menu["items"][item_id]
                cat_id = item_m["category"]
                if "true_category" in item_m:
                    cat_id = item_m["true_category"]
                cat_m = menu["categories"][str(cat_id)]
                items_data[item_id] = {
                    "count":1,
                    "cat": cat_m,
                    "item": item_m,
                }
                meal_total += item_m["price"]
                total += item_m["price"]
            def sort_key(item):
                cat_position = item["cat"]["position"]
                item_priority = item["item"]["priority"]
                item_id = item["item"]["id"]
                return (cat_position, item_priority, item_id)
            items_data = [item for item in items_data.values()]
            items_data.sort(key=sort_key)
            items_names = []
            for item_d in items_data:
                count = item_d["count"]
                item = item_d["item"]
                name = item["name"]
                if count > 1:
                    name += f" *x{count}*"
                items_names.append(name)
            if len(items_names) >0:
                meal = "🍽 <b>" + l("food-day-"+cart["day"]) + ", " + l("food-meal-"+cart["meal"]) + "</b> <code>" + str(meal_total)+ "</code>:\n"
                meal += ", ".join(items_names)
                ret.append(meal)
    if len(ret) > 0:
        txt = l("food-order-for", name=order["receiver_name"]) + "\n\n" + "\n\n".join(ret)
        txt += "\n\n"+ l("food-total", total=total)
        return txt
    return ""

def order_msg(l, menu, order, user, base_name, app):
    order_id = str(order["_id"])
    kbd = [
        [InlineKeyboardButton(l("food-rename-button"), callback_data=f"{base_name}|ren|{order_id}")],
    ]
    msg = order_text(l, order, menu)
    if not "proof_received_at" in order:
        if "total" in order and order["total"] > 0:
            kbd.append([InlineKeyboardButton(
                l("food-confirm-payment-button"), callback_data=f"{base_name}|payment|{order_id}"
            )])
        check = calculate_sha256(f"{order['nonce']}{order_id}{user}")
        kbd.append([InlineKeyboardButton(
            l("food-edit-order-button"),
            web_app=WebAppInfo(full_link(app, f"/menu?order={order_id}&check={check}"))
        )])
        kbd.append([InlineKeyboardButton(
            l("food-cancel-order-button"),callback_data=f"{base_name}|del|{order_id}"
        )])
    kbd.append([InlineKeyboardButton(
        l("food-back-button"),callback_data=f"{base_name}|start"
    )])
    return msg, kbd

cancel_chr = chr(0xE007F) # Tag cancel

class FoodUpdate:
    base: 'Food'
    tgUpdate: Update
    tgUser: User
    user: int
    bot: int
    _user = None
    _order = None

    def __init__(self, base, update) -> None:
        self.base = base
        self.update = update
        self.l = update.l
        self.user = update.user
        self.tgUpdate = update.update
        self.tgUser = update.update.effective_user
        self.bot = self.update.context.bot.id

    async def get_user(self):
        if self._user is None:
            self._user = await self.base.user_db.find_one({
                "user_id": self.user,
                "bot_id": self.bot,
            })
        return self._user

    def user_tg_name(self):
        u = self.tgUser
        return (f"{u.first_name} {u.last_name}").strip()
    
    async def get_user_names(self):
        user = await self.get_user()
        tg_name = self.user_tg_name()
        names = []
        if "known_names" in user:
            names = user["known_names"].copy()
        if not tg_name in names:
            names.append(tg_name)
        return names
    
    async def get_user_orders(self):
        return await self.base.get_user_orders(self.user)
    
    async def get_order(self, order_id):
        if self._order is None or ObjectId(self._order["_id"]) != ObjectId(order_id):
            self._order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        return self._order
    
    async def request_name(self, data):
        names = await self.get_user_names()
        btns = [[name] for name in names]
        logger.debug(f"known names: {names}")
        btns.append([cancel_chr+self.l("cancel-command")])
        markup = ReplyKeyboardMarkup(
            btns,
            resize_keyboard=True
        )
        await self.update.reply(self.l("food-write-for-who"), reply_markup=markup, parse_mode=ParseMode.HTML)
        await self.update.require_input(self.base.name, "handle_recipient_name", data)

    async def save_new_name(self, name):
        await self.base.user_db.update_one({
            "user_id": self.user,
            "bot_id": self.bot,
        }, {
            "$push": { "known_names": name }
        })
    
    async def handle_recipient_name(self, order_id):
        name = self.tgUpdate.message.text
        logger.debug(f"recipient name: {repr(name)}")
        if name[0] != cancel_chr:
            names = await self.get_user_names()
            if not name in names:
                await self.save_new_name(name)
            upd_msg = self.l("food-name-updated", name=name)
            await self.base.food_db.update_one({
                "_id": ObjectId(order_id)
            },{
                "$set":{
                    "receiver_name": name,
                }
            })
            logger.debug(f"updated name to {name} for {order_id} for {self.user}")
        else:
            upd_msg = self.l("food-name-update-cancelled")
            order = await self.get_order(order_id)
            name = order["receiver_name"]
        await self.update.reply(upd_msg, parse_mode=ParseMode.HTML, reply_markup=ReplyKeyboardRemove())
        msg = self.l("food-order-message-begin", name=name)
        order_msg, kbd = await self.get_order_msg(order_id)
        msg += "\n" + order_msg
        logger.debug(f"msg {msg}, kbd {kbd}")
        source = await self.update.reply(msg, parse_mode=ParseMode.HTML, reply_markup=InlineKeyboardMarkup(kbd))
        await self.base.save_user_source(source)
    
    async def handle_cq_ed(self, order_id):
        msg = self.l("food-order-message-begin")
        order_msg, kbd = await self.get_order_msg(order_id)
        await self.tgUpdate.callback_query.edit_message_text(
            msg + "\n" + order_msg,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(kbd),
        )
        await self.base.save_user_source(self.tgUpdate.callback_query.message)

    async def handle_cq_ren(self, order_id):
        self.tgUpdate.callback_query.edit_message_reply_markup(InlineKeyboardMarkup([]))
        return await self.request_name(order_id)

    async def handle_cq_payment(self, order_id):
        await self.tgUpdate.callback_query.edit_message_text(
            self.l("food-payment-instructions"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[]]),
        )
        logger.debug(f"order callback payment {self.user} {order_id}")
        await self.update.reply(self.l("food-payment-instructions-proof"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardMarkup(
                [[cancel_chr+self.l("cancel-command")]],
                resize_keyboard=True
            ),
        )
        await self.update.require_anything(self.base.name, "handle_payment_proof", order_id)
    
    async def handle_cq_del(self, order_id):
        logger.debug(f"deleting order {self.user} {order_id}")
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        },{
            "$set":{
                "deleted": True,
            }
        })
        await self.handle_cq_start()
    
    async def get_order_msg(self, order_id):
        order = await self.get_order(order_id)
        return order_msg(self.l, self.base.menu, order, self.user, self.base.name, self.base.base_app)

    async def handle_cq_start(self):
        orders = await self.get_user_orders()
        logger.debug(f"user orders for {self.user}: {orders}")
        msg, buttons = self.base.start_msg(self.l, orders)
        if self.tgUpdate.callback_query is not None:
            await self.tgUpdate.callback_query.edit_message_text(
                msg,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[btn] for btn in buttons])
            )
            await self.base.save_user_source(self.tgUpdate.callback_query.message)
        else:
            source = await self.update.reply(
                msg,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[btn] for btn in buttons])
            )
            await self.base.save_user_source(source)
    
    async def handle_payment_proof(self, order_id):
        if filters.TEXT.check_update(self.tgUpdate) and self.tgUpdate.message.text[0] == cancel_chr:
            return await self.update.reply(
                self.l("food-payment-proof-cancelled"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        if not (filters.PHOTO | filters.Document.ALL).check_update(self.tgUpdate):
            return await self.update.reply(
                self.l("food-payment-proof-failed"),
                parse_mode=ParseMode.HTML,
                reply_markup=ReplyKeyboardRemove(),
            )
        if filters.PHOTO.check_update(self.tgUpdate):
            doc = self.tgUpdate.message.photo[-1]
            file_ext = ".jpg"
        else:
            doc = self.tgUpdate.message.document
            import mimetypes
            file_ext = mimetypes.guess_extension(doc.mime_type)
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        },{
            "$set":{
                "proof_received_at": datetime.datetime.now(),
                "proof_file": f"{doc.file_id}{file_ext}",
            }
        })
        order = await self.get_order(order_id)
        if self.base.config.food.payment_admin>0:
            admin = await self.base.user_db.find_one({
                "user_id": self.base.config.food.payment_admin,
                "bot_id": self.bot,
            })
            user = await self.get_user()
            lc = "ru"
            if admin is not None and "language_code" in admin:
                lc = admin["language_code"]
            def l(s, **kwargs):
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.tgUpdate.message.forward(self.base.config.food.payment_admin)
            await self.base.base_app.bot.bot.send_message(
                self.base.config.food.payment_admin,
                l(
                    "food-adm-payment-proof-received",
                    link=client_user_link_html(user),
                    total=str(order["total"]),
                    name=order["receiver_name"],
                ),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton(l("food-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{order_id}"),
                        InlineKeyboardButton(l("food-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{order_id}"),
                    ]
                ])
            )
        await self.update.reply(
            self.l("food-payment-proof-forwarded"),
            parse_mode=ParseMode.HTML,
            reply_markup=ReplyKeyboardRemove()
        )

    async def handle_cq_adm_acc(self, order_id):
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        },{
            "$set":{
                "validated_at": datetime.datetime.now(),
                "validation": True,
            }
        })
        order = await self.get_order(order_id)
        user = await self.base.user_db.find_one({
            "user_id": order["user_id"],
            "bot_id": self.bot,
        })
        def l(s, **kwargs):
            return self.base.base_app.localization(s, args=kwargs, locale=user["language_code"])
        await self.base.base_app.bot.bot.send_message(
            order["user_id"],
            l(
                "food-payment-proof-confirmed",
                name=order["receiver_name"],
            ),
            parse_mode=ParseMode.HTML
        )
        await self.tgUpdate.callback_query.edit_message_text(
            self.l(
                "food-adm-payment-proof-confirmed",
                link=client_user_link_html(user),
                name=order["receiver_name"],
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )

    async def handle_cq_adm_rej(self, order_id):
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        },{
            "$set":{
                "validated_at": datetime.datetime.now(),
                "validation": False,
            },
            "$unset": { "proof_received_at": "" },
        })
        order = await self.get_order(order_id)
        user = await self.base.user_db.find_one({
            "user_id": order["user_id"],
            "bot_id": self.bot,
        })
        def l(s, **kwargs):
            return self.base.base_app.localization(s, args=kwargs, locale=user["language_code"])
        await self.base.base_app.bot.bot.send_message(
            order["user_id"],
            l(
                "food-payment-proof-rejected",
                name=order["receiver_name"],
            ),
            parse_mode=ParseMode.HTML
        )
        await self.tgUpdate.callback_query.edit_message_text(
            self.l(
                "food-adm-payment-proof-rejected",
                link=client_user_link_html(user),
                name=order["receiver_name"],
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )
    
    async def handle_start(self):
        logger.debug(f"starting orders for: {self.user}")
        await self.handle_cq_start()

    async def handle_callback_query(self):
        q = self.tgUpdate.callback_query
        await q.answer()
        logger.info(f"Received callback_query from {self.user}, data: {q.data}")
        data = q.data.split("|")
        fn = "handle_cq_" + data[1]
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            if callable(attr):
                return await attr(*data[2:])
        logger.error(f"unknown callback {data[1]}: {data[2:]}")


class Food(BasePlugin):
    name = "food"
    config: Config
    food_db: AgnosticCollection
    user_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.config = base_app.config
        self.food_db = base_app.mongodb[self.config.mongo_db.food_collection]
        self.user_db = base_app.users_collection
        self.base_app.food = self
        self._meal_test = CommandHandler("meal", self.handle_start)
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}|.*")
        self.menu = self.get_menu()
    
    async def save_user_source(self, source: Message):
        logger.debug(f"saving message {source}")
        await self.user_db.update_one({
            "user_id": source.chat.id,
            "bot_id": source.get_bot().id,
        }, {
            "$set": {
                "food_webview_source": source.message_id
            }
        })
        
    def start_msg(self, l, orders):
        buttons = [InlineKeyboardButton(l(
            "food-order-button",
            created=order["created_at"].strftime("%d.%m"),
            name=order["receiver_name"],
        ), callback_data=f"{self.name}|ed|{str(order['_id'])}") for order in orders]
        if self.config.food.deadline > datetime.date.today():
            buttons.append(InlineKeyboardButton(
                l("food-new-order-button"),
                web_app=WebAppInfo(full_link(self.base_app, f"/menu"))
            ))
        msg = l("food-select-order")
        if len(orders) == 0:
            msg = l("food-no-orders-yet")
        return msg, buttons
        
    def get_menu(self):
        from os.path import dirname as d
        from os.path import join
        import json
        menu_file = join(d(d(d(__file__))), "static", "menu.json")
        with open(menu_file, "r", encoding="utf-8") as mf:
            return json.load(mf)
        
    async def handle_payment_proof(self, updater, order_id):
        return await self.create_update(updater).handle_payment_proof(order_id)
    
    async def handle_callback_query(self, updater):
        return await self.create_update(updater).handle_callback_query()
    
    async def get_order(self, order_id):
        return await self.food_db.find_one({"_id": ObjectId(order_id)})
    
    async def get_user_orders(self, user_id):
        cursor = self.food_db.find({
            "user_id": {"$eq": user_id},
            "deleted": { "$exists": False },
        }).sort("created_at", 1)
        return await cursor.to_list(length=1)

    async def try_remove_user_source(self, user):
        if "food_webview_source" in user:
            logger.debug(f"trying remove message {user['food_webview_source']} for user {user['user_id']}")
            try:
                await self.base_app.bot.bot.edit_message_reply_markup(
                    user['user_id'],
                    user['food_webview_source'],
                    reply_markup=InlineKeyboardMarkup([])
                )
                await self.base_app.bot.bot.delete_message(
                    user['user_id'],
                    user['food_webview_source']
                )
            except Exception as e:
                logger.warn(f"failed to remove message {user['food_webview_source']} for user {user['user_id']}: {e}", exc_info=True)

    async def create_order(self, user_id, new_carts):
        total = order_total(new_carts, self.menu)
        if total == 0:
            return
        user = await self.user_db.find_one({
            "user_id": user_id,
            "bot_id": self.base_app.bot.bot.id,
        })
        await self.try_remove_user_source(user)
        def l(s, **kwargs):
            return self.base_app.localization(s, args=kwargs, locale=user["language_code"])
        name = ""
        names = []
        if "known_names" in user and len(user["known_names"]) > 0:
            name = user["known_names"][0]
            names = names.copy()
        if name == "":
            name = (f"{user['first_name']} {user['last_name']}").strip()
        if not name in names:
            names.append(name)
        res = await self.food_db.insert_one({
            "user_id": user_id,
            "receiver_name": name,
            "created_at": datetime.datetime.now(),
            "nonce": generate_random_string(20),
            "carts": new_carts,
            "total": total
        })
        order_id = str(res.inserted_id)
        logger.debug(f"created order for {user_id} for {name}: {order_id}")
        btns = [[name] for name in names]
        logger.debug(f"known names: {names}")
        btns.append([cancel_chr+l("cancel-command")])
        markup = ReplyKeyboardMarkup(
            btns,
            resize_keyboard=True
        )
        if not "state" in user:
            user["state"] = {}
        user["state"]["state"] = "waiting_text"
        user["state"]["plugin"] = self.name
        user["state"]["plugin_callback"] = "handle_recipient_name"
        user["state"]["plugin_data"] = order_id
        
        await self.user_db.update_one({
            "user_id": user_id,
            "bot_id": self.base_app.bot.bot.id,
        }, {
            "$set": {
                "state": user["state"],
            }
        })
        await self.base_app.bot.bot.send_message(
            user_id,
            l("food-created-write-for-who"),
            reply_markup=markup,
            parse_mode=ParseMode.HTML,
        )
    
    async def set_carts(self, order, new_carts, is_autosave):
        user = await self.user_db.find_one({
            "user_id": order["user_id"],
            "bot_id": self.base_app.bot.bot.id,
        })
        await self.try_remove_user_source(user)
        total = order_total(new_carts, self.menu)
        await self.food_db.update_one({
            "_id": order["_id"]
        },{
            "$set":{
                "carts": new_carts,
                "total": total
            }
        })
        if is_autosave:
            return
        if total == 0:
            await self.food_db.update_one({
                "_id": order["_id"]
            },{
                "$set":{
                    "deleted": True,
                }
            })
            orders = await self.get_user_orders(order["user_id"])
            msg, kbd = self.start_msg(self.l, orders)
        else:
            order["carts"] = new_carts
            def l(s, **kwargs):
                return self.base_app.localization(s, args=kwargs, locale=user["language_code"])
            logger.debug(f"updated {order['_id']} carts")
            msg, kbd = order_msg(l, self.menu, order, order["user_id"], self.name, self.base_app)
            msg = l("food-order-saved") + "\n" + msg
        source = await self.base_app.bot.bot.send_message(
            order["user_id"], msg, parse_mode=ParseMode.HTML, reply_markup=InlineKeyboardMarkup(kbd)
        )
        await self.save_user_source(source)
    
    def test_message(self, message: Update, state, web_app_data):
        if self._meal_test.check_update(message):
            return PRIORITY_BASIC, self.handle_start
        return PRIORITY_NOT_ACCEPTING, None
    
    def test_callback_query(self, query: Update, state):
        if self._cbq_handler.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None
    
    def create_update(self, update) -> FoodUpdate:
        return FoodUpdate(self, update)
    
    async def handle_create_new(self, update):
        return await self.create_update(update).handle_cq_new()
    
    async def handle_recipient_name(self, update, data):
        return await self.create_update(update).handle_recipient_name(data)
        
    async def handle_start(self, update):
        return await self.create_update(update).handle_start()