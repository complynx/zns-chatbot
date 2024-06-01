
from ..config import Config, full_link
from ..tg_state import TGState
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


def next_weekday_date(given_date: datetime.date, target_weekday: str) -> datetime.date:
    weekdays = ['monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday']
    given_weekday = given_date.weekday()
    target_weekday = weekdays.index(target_weekday.lower())
    
    if given_weekday == target_weekday:
        return given_date
    
    days_difference = target_weekday - given_weekday
    if days_difference < 0:
        days_difference += 7
    
    next_date = given_date + datetime.timedelta(days=days_difference)
    return next_date


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

def order_text(l, order, menu, for_assist=False):
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
                    if for_assist:
                        name += f" x{count}"
                    else:
                        name += f" *x{count}*"
                items_names.append(name)
            if len(items_names) >0:
                if for_assist:
                    meal = cart["day"] + ", " +cart["meal"] + " " + str(meal_total)+ "rub.\n"
                else:
                    meal = "🍽 <b>" + l("food-day-"+cart["day"]) + ", " + l("food-meal-"+cart["meal"]) + "</b> <code>" + str(meal_total)+ "</code>:\n"
                meal += ", ".join(items_names)
                ret.append(meal)
    if len(ret) > 0:
        if for_assist:
            txt = ""
            if "validation" in order and order["validation"]:
                txt += "paid "
            else:
                txt += "unpaid "
            txt += "order for " + order["receiver_name"] + "\n - " + "\n - ".join(ret) + f"\n total: {total}rub."
        else:
            txt = l("food-order-for", name=order["receiver_name"]) + "\n\n" + "\n\n".join(ret)
            txt += "\n\n"+ l("food-total", total=total)
        return txt
    return ""


cancel_chr = chr(0xE007F) # Tag cancel

class FoodUpdate:
    base: 'Food'
    tgUpdate: Update
    user: int
    bot: int
    update: TGState
    _order = None

    def __init__(self, base, update: TGState) -> None:
        self.base = base
        self.update = update
        self.l = update.l
        self.user = update.user
        self.tgUpdate = update.update
        self.bot = self.update.bot.id

    async def get_user(self):
        return await self.update.get_user()

    def user_tg_name(self):
        if self.tgUpdate is not None:
            u = self.tgUpdate.effective_user
            return u.full_name
        if self.update.maybe_get_user() is not None:
            user = self.update.maybe_get_user()
            name = ""
            if "print_name" in user:
                name = user['print_name']
            else:
                fn = user['first_name'] if 'first_name' in user else ''
                ln = user['last_name'] if 'last_name' in user else ''
                name = (f"{fn} {ln}").strip()
            if name == "" and "username" in user and user["username"] is not None and user["username"] != "":
                name = user["username"]
            return name
        return ""
    
    async def get_user_names(self):
        user = await self.get_user()
        tg_name = self.user_tg_name()
        names = []
        if "known_names" in user:
            names = user["known_names"].copy()
        if not tg_name in names and tg_name != "":
            names.append(tg_name)
        return names
    
    async def get_user_orders(self):
        cursor = self.base.food_db.find({
            "user_id": {"$eq": self.user},
            "deleted": { "$exists": False },
        }).sort("created_at", 1)
        return await cursor.to_list(length=1000)
    
    async def get_order(self, order_id):
        if self._order is None or ObjectId(self._order["_id"]) != ObjectId(order_id):
            self._order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        return self._order

    async def update_order(self, order_id, request):
        return await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        }, request)

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
        await self.update.update_user({
            "$push": { "known_names": name }
        })
    
    async def handle_recipient_name(self, order_id):
        name = self.update.message.text
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
        await self.save_user_source(source)
    
    async def handle_cq_ed(self, order_id):
        msg = self.l("food-order-message-begin")
        order_msg, kbd = await self.get_order_msg(order_id)
        await self.update.edit_message_text(
            msg + "\n" + order_msg,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(kbd),
        )
        await self.save_user_source(self.update.message)

    async def handle_cq_ren(self, order_id):
        await self.update.edit_reply_markup(InlineKeyboardMarkup([]))
        return await self.request_name(order_id)

    async def handle_cq_payment(self, order_id):
        await self.update.edit_message_text(
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
        order_id = str(order_id)
        base_name = self.base.name
        kbd = [
            [InlineKeyboardButton(self.l("food-rename-button"), callback_data=f"{base_name}|ren|{order_id}")],
        ]
        msg = order_text(self.l, order, self.base.menu)
        if not "proof_received_at" in order:
            if "total" in order and order["total"] > 0:
                kbd.append([InlineKeyboardButton(
                    self.l("food-confirm-payment-button"), callback_data=f"{base_name}|payment|{order_id}"
                )])
            check = calculate_sha256(f"{order['nonce']}{order_id}{self.user}")
            kbd.append([InlineKeyboardButton(
                self.l("food-edit-order-button"),
                web_app=WebAppInfo(full_link(self.base.base_app, f"/menu?order={order_id}&check={check}"))
            )])
            kbd.append([InlineKeyboardButton(
                self.l("food-cancel-order-button"),callback_data=f"{base_name}|del|{order_id}"
            )])
        kbd.append([InlineKeyboardButton(
            self.l("food-back-button"),callback_data=f"{base_name}|start"
        )])
        return msg, kbd

    async def handle_cq_start(self):
        msg, buttons = await self.start_msg()
        if self.update.callback_query is not None:
            await self.update.edit_message_text(
                msg,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[btn] for btn in buttons])
            )
            await self.save_user_source(self.update.message)
        else:
            source = await self.update.reply(
                msg,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([[btn] for btn in buttons])
            )
            await self.save_user_source(source)
    
    async def handle_payment_proof(self, order_id):
        if filters.TEXT.check_update(self.tgUpdate) and self.update.message.text[0] == cancel_chr:
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
            doc = self.update.message.photo[-1]
            file_ext = ".jpg"
        else:
            doc = self.update.message.document
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
            await self.update.forward_message(self.base.config.food.payment_admin)
            await self.update.reply(
                l(
                    "food-adm-payment-proof-received",
                    link=client_user_link_html(user),
                    total=str(order["total"]),
                    name=order["receiver_name"],
                ),
                chat_id=self.base.config.food.payment_admin,
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
        ls = 'en'
        if "language_code" in user:
            ls=user["language_code"]
        def l(s, **kwargs):
            return self.base.base_app.localization(s, args=kwargs, locale=ls)
        await self.update.reply(
            l(
                "food-payment-proof-confirmed",
                name=order["receiver_name"],
            ),
            order["user_id"],
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
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
        ls = 'en'
        if "language_code" in user:
            ls=user["language_code"]
        def l(s, **kwargs):
            return self.base.base_app.localization(s, args=kwargs, locale=ls)
        await self.update.reply(
            l(
                "food-payment-proof-rejected",
                name=order["receiver_name"],
            ),
            order["user_id"],
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "food-adm-payment-proof-rejected",
                link=client_user_link_html(user),
                name=order["receiver_name"],
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )
    
    async def set_carts(self, order, new_carts, is_autosave):
        self._order = order
        await self.try_remove_user_source()
        total = order_total(new_carts, self.base.menu)
        await self.update_order(order["_id"], {
            "$set":{
                "carts": new_carts,
                "total": total
            }
        })
        if is_autosave:
            return
        if total == 0:
            await self.update_order(order["_id"], {
                "$set":{
                    "deleted": True,
                }
            })
            msg, kbd = await self.start_msg()
            kbd = [[btn] for btn in kbd]
        else:
            order["carts"] = new_carts
            order["total"] = total
            logger.debug(f"updated {order['_id']} carts")
            msg, kbd = await self.get_order_msg(order["_id"])
            msg = self.l("food-order-saved") + "\n" + msg
        source = await self.update.reply(
            msg, parse_mode=ParseMode.HTML, reply_markup=InlineKeyboardMarkup(kbd)
        )
        await self.save_user_source(source)
    
    async def create_order(self, new_carts):
        total = order_total(new_carts, self.base.menu)
        if total == 0:
            logger.debug("total is 0, won't create")
            return
        await self.try_remove_user_source()
        names = await self.get_user_names()
        name = names[0]
        res = await self.base.food_db.insert_one({
            "user_id": self.user,
            "receiver_name": name,
            "created_at": datetime.datetime.now(),
            "nonce": generate_random_string(20),
            "carts": new_carts,
            "total": total,
            "client_notified": datetime.datetime.now(),
        })
        order_id = str(res.inserted_id)
        logger.debug(f"created order for {self.user} for {name}: {order_id}")
        btns = [[name] for name in names]
        logger.debug(f"known names: {names}")
        btns.append([cancel_chr+self.l("cancel-command")])
        markup = ReplyKeyboardMarkup(
            btns,
            resize_keyboard=True
        )
        logger.warn(f"{btns}")
        await self.update.require_input(self.base.name, "handle_recipient_name", order_id)
        await self.update.reply(
            self.l("food-created-write-for-who"),
            reply_markup=markup,
            parse_mode=ParseMode.HTML,
        )

    async def handle_start(self):
        logger.debug(f"starting orders for: {self.user}")
        await self.handle_cq_start()

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

    async def try_remove_user_source(self):
        user = await self.get_user()
        if "food_webview_source" in user:
            logger.debug(f"trying remove message {user['food_webview_source']} for user {user['user_id']}")
            try:
                await self.update.edit_reply_markup(
                    chat_id=user['user_id'],
                    message_id=user['food_webview_source'],
                    reply_markup=InlineKeyboardMarkup([])
                )
                await self.update.delete_message(
                    chat_id=user['user_id'],
                    message_id=user['food_webview_source']
                )
            except Exception as e:
                logger.warn(f"failed to remove message {user['food_webview_source']} for user {user['user_id']}: {e}")
    
    async def save_user_source(self, source):
        if isinstance(source, Message):
            user_id = source.chat.id
            bot = source.get_bot().id
            source = source.message_id
        else:
            user_id = self.user
            bot = self.bot
        logger.debug(f"saving message {source} to user {user_id}")
        await self.base.user_db.update_one({
            "user_id": user_id,
            "bot_id": bot,
        }, {
            "$set": {
                "food_webview_source": source
            }
        })
    
    async def start_msg(self):
        orders = await self.get_user_orders()
        buttons = [InlineKeyboardButton(self.l(
            "food-order-button",
            created=order["created_at"].strftime("%d.%m"),
            name=order["receiver_name"],
        ), callback_data=f"{self.base.name}|ed|{str(order['_id'])}") for order in orders]
        if self.base.config.food.deadline > datetime.date.today():
            buttons.append(InlineKeyboardButton(
                self.l("food-new-order-button"),
                web_app=WebAppInfo(full_link(self.base.base_app, f"/menu"))
            ))
        msg = self.l("food-select-order")
        if len(orders) == 0:
            msg = self.l("food-no-orders-yet")
        return msg, buttons

    async def remind_about_order(self, order_doc):
        self._order = order_doc
        msg, kbd = await self.get_order_msg(order_doc["_id"])
        await self.update.reply(
            self.l("food-remind-about-order") + "\n" + msg,
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(kbd),
        )
        await self.base.food_db.update_one({
            "_id": order_doc["_id"]
        },{
            "$set":{
                "client_notified": datetime.datetime.now(),
            },
        })


NOTIFICATOR_LOOP = 3600
class Food(BasePlugin):
    name = "food"
    food_db: AgnosticCollection
    user_db: AgnosticCollection

    def __init__(self, base_app):
        from asyncio import create_task
        super().__init__(base_app)
        self.food_db = base_app.mongodb[self.config.mongo_db.food_collection]
        self.user_db = base_app.users_collection
        self.base_app.food = self
        self._meal_test = CommandHandler("meal", self.handle_start)
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
        self.menu = self.get_menu()
        create_task(self._notifier())
    
    async def _notifier(self):
        from asyncio import sleep, create_task
        logger.info(f"starting Food.notifier loop ")
        while self.config.food.deadline > datetime.date.today():
            await sleep(NOTIFICATOR_LOOP)
            try:
                async for doc in self.food_db.find({
                    "deleted": {"$exists":False},
                    "proof_received_at": {"$exists": False},
                     "$or": [
                        {"client_notified": {"$exists": False}},
                        {"client_notified": {"$lt": datetime.datetime.today() - datetime.timedelta(days=1)}}
                    ],
                }):
                    create_task(self.remind_about_order(doc))
            except Exception as e:
                logger.error("Exception in notifier: %s", e, exc_info=1)
    
    async def remind_about_order(self, order_doc):
        try:
            upd = await self.create_update_from_user(order_doc["user_id"])
            await upd.remind_about_order(order_doc)
        except Exception as e:
            logger.error("Exception in remind_about_order: %s", e, exc_info=1)

    async def out_of_stock(self, out_of_stock):
        import json
        await self.bot.send_message(
            self.config.food.out_of_stock_admin,
            "out of stock: ```json\n"+json.dumps(out_of_stock, indent=4) + "\n```",
            ParseMode.MARKDOWN_V2
        )
    
    async def get_all_orders(self):
        cursor = self.food_db.find({
            "deleted": { "$exists": False },
            "validation": { "$eq": True },
        }).sort("created_at", 1)
        orders = {}
        async for order in cursor:
            for cid, cart in order["carts"].items():
                if not cid in orders:
                    orders[cid] = {
                        "day": cart["day"],
                        "meal": cart["meal"],
                        "items": {},
                    }
                items = orders[cid]["items"]
                for item in cart["items"]:
                    if not item in items:
                        items[item] = 1
                    else:
                        items[item] += 1
        return orders

    async def get_order(self, order_id):
        return await self.food_db.find_one({"_id": ObjectId(order_id)})
    
    async def get_user_orders_assist(self, user_id: int) -> str:
        cursor = self.food_db.find({
            "user_id": {"$eq": user_id},
            "deleted": { "$exists": False },
        }).sort("created_at", 1)
        orders = await cursor.to_list(length=1000)
        ret = []
        for order in orders:
            ot = order_text(None, order, self.menu, True)
            if ot != "":
                ret.append(ot)
        if len(ret) == 0:
            return ""
        return "\n".join(ret)
        
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

    async def create_order(self, user_id, new_carts):
        upd = await self.create_update_from_user(user_id)
        return await upd.create_order(new_carts)
    
    async def set_carts(self, order, new_carts, is_autosave):
        upd = await self.create_update_from_order(order)
        return await upd.set_carts(order, new_carts, is_autosave)
    
    def test_message(self, message: Update, state, web_app_data):
        if self._meal_test.check_update(message):
            return PRIORITY_BASIC, self.handle_start
        return PRIORITY_NOT_ACCEPTING, None
    
    def test_callback_query(self, query: Update, state):
        if self._cbq_handler.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None
    
    async def create_update_from_order(self, order) -> FoodUpdate:
        return await self.create_update_from_user(order["user_id"])
    
    async def create_update_from_user(self, user) -> FoodUpdate:
        upd = TGState(user, self.base_app)
        await upd.get_state()
        return FoodUpdate(self, upd)
    
    def create_update(self, update) -> FoodUpdate:
        return FoodUpdate(self, update)
    
    async def handle_create_new(self, update):
        return await self.create_update(update).handle_cq_new()
    
    async def handle_recipient_name(self, update, data):
        return await self.create_update(update).handle_recipient_name(data)
        
    async def handle_start(self, update):
        return await self.create_update(update).handle_start()
