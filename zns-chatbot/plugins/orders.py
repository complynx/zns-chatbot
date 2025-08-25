import datetime
from motor.core import AgnosticCollection
from ..config import Config, full_link, Party
from ..tg_state import TGState
from telegram import InlineKeyboardMarkup, Update,ReplyKeyboardMarkup, ReplyKeyboardRemove, InlineKeyboardButton, WebAppInfo
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler, MessageHandler, filters
from telegram.constants import ParseMode
from bson.objectid import ObjectId
from ..telegram_links import client_user_link_html, client_user_name
import logging
from math import ceil

def currency_ceil(sum):
    # if sum < 100:
    #     return ceil(sum)
    
    # # Get the magnitude (order of the largest digit) of the number
    # magnitude = 10 ** (len(str(int(sum))) - 2)
    
    # # Normalize the number by dividing by the magnitude
    # normalized = sum / magnitude
    
    # # Round up to the nearest 0 or 5
    # ceil_normalized = ceil(normalized * 2) / 2
    
    # # Scale back to the original magnitude
    # rounded = ceil_normalized * magnitude
    
    # return rounded
    return ceil(sum * 100) / 100

logger = logging.getLogger(__name__)

BYN_TO_RUB = 30

class OrdersUpdate:
    base: 'Orders'
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
        self.config = update.config.orders
        self.tgUpdate = update.update
        self.bot = self.update.bot.id

    async def create_order(self, choice):
        await self.base.food_db.insert_one({
            "user_id": self.user,
            "event_number": self.config.event_number,
            "created_at": datetime.datetime.now(),
            "choice": choice,
        })
        return await self.handle_cq_start()

    async def set_choice(self, order_id, choice):
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id),
        },{
            "$set":{
                "choice": choice,
                "updated_at": datetime.datetime.now(),
            }
        })
        return await self.handle_cq_start()

    async def handle_cq_del(self, order_id):
        order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        if "proof_file" in order:
            return await self.handle_cq_start()
        await self.base.food_db.delete_one({"_id": ObjectId(order_id)})
        return await self.handle_cq_start()

    def get_order_total(self, order):
        total = currency_ceil(order["choice"]["total"])
        total_rub = currency_ceil(order["choice"]["total"] * BYN_TO_RUB)
        return total, total_rub
    
    async def handle_cq_pay(self, order_id):
        order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        if "proof_file" in order:
            return await self.handle_cq_start()
        total, total_rub = self.get_order_total(order)
        admins_be = await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists":True},
        }).to_list(None)
        btns = []
        for admin in admins_be:
            btns.append([
                InlineKeyboardButton(
                    self.l("orders-admin-belarus",
                        name=client_user_name(admin),
                        region=admin['payment_administrator_belarus']
                    ),
                    callback_data=f"{self.base.name}|cash|{order_id}|{admin['user_id']}"
                )
            ])
        await self.update.edit_or_reply(
            self.l("orders-message-payment-options",
                total=total,
                rutotal=total_rub,
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(btns+[[InlineKeyboardButton(
                self.l("orders-back-button"),
                callback_data=f"{self.base.name}|start"
            ), InlineKeyboardButton(
                self.l("orders-close-button"),
                callback_data=f"{self.base.name}|close"
            )]]),
        )

    async def handle_cq_cash(self, order_id, admin_id):
        # return await self.handle_cq_start()
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id),
        }, {
            "$set": {
                "proof_country": "be",
                "proof_file": "cash",
                "proof_received": datetime.datetime.now(),
            }
        })
        order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        total, _total_rub = self.get_order_total(order)
        admin = await self.base.base_app.users_collection.find_one({
            "user_id": int(admin_id),
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists":True},
        })
        if admin is not None:
            user = await self.update.get_user()
            lc = "ru"
            if "language_code" in admin:
                lc = admin["language_code"]
            def l(s, **kwargs):
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.update.reply(
                l(
                    "orders-adm-payment-cash-requested",
                    link=client_user_link_html(user),
                    total=total,
                    name=order["choice"]["customer"],
                ),
                chat_id=admin["user_id"],
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton(l("food-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{order_id}"),
                        InlineKeyboardButton(l("food-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{order_id}"),
                    ]
                ])
            )
        await self.update.edit_or_reply(
            self.l("orders-payment-cash-requested",
                link=client_user_link_html(admin),
                total=total,
                name=order["choice"]["customer"],
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_cq_start(self):
        orders = await self.base.food_db.find({
            "user_id": self.user,
            "event_number": self.config.event_number,
        }).sort("created_at", 1).to_list(None)
        user = await self.update.get_user()
        debug_param = ""
        if "debug_id" in user:
            debug_param = "&debug_id="+user["debug_id"]
        current_order = None
        btns = []
        for order in orders:
            if "proof_file" not in order:
                current_order = order
                break
            else:
                btns.append([InlineKeyboardButton(self.l(
                    "orders-order-button",
                    created=order["created_at"].strftime("%d.%m"),
                    name=order["choice"]["customer"],
                ), web_app=WebAppInfo(full_link(self.base.base_app, f"/orders?order_id={str(order['_id'])}&locale={self.update.language_code}{debug_param}")))])
        if current_order is not None:
            btns.append([InlineKeyboardButton(self.l(
                "orders-order-pay-button",
            ), callback_data=f"{self.base.name}|pay|{str(order['_id'])}")])
            btns.append([InlineKeyboardButton(self.l(
                "orders-order-unpaid-button",
                created=order["created_at"].strftime("%d.%m"),
                name=order["choice"]["customer"],
            ), web_app=WebAppInfo(full_link(self.base.base_app, f"/orders?order_id={str(order['_id'])}&locale={self.update.language_code}{debug_param}")))])
            btns.append([InlineKeyboardButton(
                self.l("orders-edit-button"),
                web_app=WebAppInfo(full_link(self.base.base_app, f"/orders?order_id={str(order['_id'])}&locale={self.update.language_code}{debug_param}"))
            )])
            btns.append([InlineKeyboardButton(self.l(
                "orders-order-delete-button",
            ), callback_data=f"{self.base.name}|del|{str(order['_id'])}")])
        else:
            btns.append([InlineKeyboardButton(
                self.l("orders-new-button"),
                web_app=WebAppInfo(full_link(self.base.base_app, f"/orders?locale={self.update.language_code}{debug_param}"))
            )])
        # Admin download button
        admins_be = await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists": True},
        }).to_list(None)
        admins_be_ids = {a["user_id"] for a in admins_be}
        if (self.user in self.config.admins or
            (self.config.payment_admin_ru and self.user == self.config.payment_admin_ru) or
            self.user in admins_be_ids):
            btns.append([InlineKeyboardButton("📥 XLSX", callback_data=f"{self.base.name}|xlsx")])
        btns.append([InlineKeyboardButton(
            self.l("orders-close-button"),
            callback_data=f"{self.base.name}|close"
        )])
        await self.update.edit_or_reply(self.l("orders-message-list"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup(btns),
        )
    
    async def handle_cq_close(self):
        await self.update.edit_or_reply(self.l("orders-closed"),
            parse_mode=ParseMode.HTML,
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
        logger.debug(f"starting orders for: {self.user}")
        await self.handle_cq_start()
    
    async def handle_cq_payed(self, order_id):
        # return await self.handle_cq_start()
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id),
        }, {
            "$set": {
                "proof_country": "ru",
            }
        })
        order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        _total_be, total = self.get_order_total(order)
        adm = self.base.config.orders.payment_admin_ru
        if adm>0:
            admin = await self.base.base_app.users_collection.find_one({
                "user_id": adm,
                "bot_id": self.bot,
            })
            user = await self.update.get_user()
            lc = "ru"
            if admin is not None and "language_code" in admin:
                lc = admin["language_code"]
            def l(s, **kwargs):
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.update.forward_message(
                adm,
                order["proof_chat_id"],
                order["proof_message_id"]
            )
            await self.update.reply(
                l(
                    "orders-adm-payment-proof-received",
                    link=client_user_link_html(user),
                    total=total,
                    name=order["choice"]["customer"],
                ),
                chat_id=adm,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton(l("food-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{order_id}"),
                        InlineKeyboardButton(l("food-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{order_id}"),
                    ]
                ])
            )
        await self.update.edit_or_reply(
            self.l("food-payment-proof-forwarded"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_cq_adm_acc(self, order_id):
        # Guard: only RU / BE payment admins or orders admins (assert style)
        admins_be_ids = {a["user_id"] for a in await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists": True},
        }).to_list(None)}
        assert (self.user in self.config.admins or
                (self.config.payment_admin_ru and self.user == self.config.payment_admin_ru) or
                self.user in admins_be_ids), f"{self.user} is not orders admin"
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        },{
            "$set":{
                "validated_at": datetime.datetime.now(),
                "validation": True,
            }
        })
        order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        user = await self.base.base_app.users_collection.find_one({
            "user_id": order["user_id"],
            "bot_id": self.bot,
        })
        ls = 'en'
        if "language_code" in user:
            ls=user["language_code"]
        def loc(s, **kwargs):
            return self.base.base_app.localization(s, args=kwargs, locale=ls)
        await self.update.reply(
            loc(
                "food-payment-proof-confirmed",
                name=order["choice"]["customer"],
            ),
            order["user_id"],
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "food-adm-payment-proof-confirmed",
                link=client_user_link_html(user),
                name=order["choice"]["customer"],
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )

    async def handle_cq_adm_rej(self, order_id):
        # Guard: only RU / BE payment admins or orders admins (assert style)
        admins_be_ids = {a["user_id"] for a in await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists": True},
        }).to_list(None)}
        assert (self.user in self.config.admins or
                (self.config.payment_admin_ru and self.user == self.config.payment_admin_ru) or
                self.user in admins_be_ids), f"{self.user} is not orders admin"
        await self.base.food_db.update_one({
            "_id": ObjectId(order_id)
        },{
            "$set":{
                "validated_at": datetime.datetime.now(),
                "validation": False,
            },
            "$unset": {
                "proof_file": "",
                "proof_received": "",
                "proof_chat_id": "",
                "proof_message_id": "",
            },
        })
        order = await self.base.food_db.find_one({"_id": ObjectId(order_id)})
        user = await self.base.base_app.users_collection.find_one({
            "user_id": order["user_id"],
            "bot_id": self.bot,
        })
        ls = 'en'
        if "language_code" in user:
            ls=user["language_code"]
        def loc(s, **kwargs):
            return self.base.base_app.localization(s, args=kwargs, locale=ls)
        await self.update.reply(
            loc(
                "food-payment-proof-rejected",
                name=order["choice"]["customer"],
            ),
            order["user_id"],
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "food-adm-payment-proof-rejected",
                link=client_user_link_html(user),
                name=order["choice"]["customer"],
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )
    
    async def handle_cq_pcancel(self, order_id):
        await self.base.food_db.update_one({"_id": ObjectId(order_id)}, {
            "$unset": {
                "proof_file": "",
                "proof_received": "",
                "proof_chat_id": "",
                "proof_message_id": "",
            }
        })
        await self.update.edit_or_reply(self.l("orders-closed"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_payment(self):
        logger.debug(f"handling payment for: {self.user}")
        order = await self.base.food_db.find_one({
            "user_id": self.user,
            "event_number": self.config.event_number,
            "proof_file": { "$exists": False },
        })
        if order is None:
            return await self.update.edit_or_reply(
                self.l("unsupported-message-error"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        doc = self.update.message.document
        await self.base.food_db.update_one({
            "_id": order["_id"]
        }, {
            "$set": {
                "proof_file": doc.file_id,
                "proof_chat_id": self.update.chat_id if self.update.chat_id is not None else self.update.user,
                "proof_message_id": self.update.message_id,
                "proof_received": datetime.datetime.now(),
            }
        })
        await self.update.edit_or_reply(
            self.l("orders-message-payed-where"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[InlineKeyboardButton(
                self.l("orders-payed-button"),
                callback_data=f"{self.base.name}|payed|{order['_id']}"
            )],[InlineKeyboardButton(
                self.l("orders-pay-cancel"),
                callback_data=f"{self.base.name}|pcancel|{order['_id']}"
            )]]),
        )

    async def handle_cq_xlsx(self):
        # Guard (assert style like passes)
        admins_be = await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists": True},
        }).to_list(None)
        admins_be_ids = {a["user_id"] for a in admins_be}
        assert (self.user in self.config.admins or
                (self.config.payment_admin_ru and self.user == self.config.payment_admin_ru) or
                self.user in admins_be_ids), f"{self.user} is not orders admin"
        import openpyxl
        from openpyxl.styles import Font, Alignment
        wb = openpyxl.Workbook()
        ws = wb.active
        ws.title = "Заказы"
        # Collect dish keys and russian names
        dish_keys = []
        dish_names_ru: dict[str,str] = {}
        for day, day_data in self.base.menu.get("choices", {}).items():
            for dish_type, dishes in day_data.items():
                if not isinstance(dishes, dict):
                    continue
                for dk, dv in dishes.items():
                    if dk not in dish_keys:
                        dish_keys.append(dk)
                        dish_names_ru[dk] = dv.get("name_ru", dk)
        # Base columns (english keys -> russian headers)
        base_fields = [
            ("order_id", "ID заказа"),
            ("user_id", "Пользователь"),
            ("customer", "Клиент"),
            ("created_at", "Создан"),
            ("updated_at", "Обновлён"),
            ("proof_country", "Страна оплаты"),
            ("validation", "Подтверждено"),
            ("total_byn", "Сумма BYN"),
            ("total_rub", "Сумма RUB"),
            ("extras_preparty", "Препати"),
            ("extras_excursion_minsk", "Экскурсия Минск"),
            ("extras_shuttle", "Шаттл"),
            ("extras_excursion_grodno", "Экскурсия Гродно"),
        ]
        header = [ru for _k, ru in base_fields] + [dish_names_ru.get(k, k) for k in dish_keys]
        ws.append(header)
        bold = Font(bold=True)
        center = Alignment(horizontal="center")
        for cell in ws["1:1"]:
            cell.font = bold
            cell.alignment = center
        totals = {k: {"count":0,"sum":0} for k in dish_keys}
        extras_totals = {"preparty":0,"excursion_minsk":0,"shuttle":0,"excursion_grodno":0}
        # Second sheet with detailed contents
        ws_details = wb.create_sheet("Содержимое")
        ws_details.append(["Пользователь","ID заказа","Клиент","День","Приём пищи","Блюдо / Активность","Количество"])
        for cell in ws_details["1:1"]:
            cell.font = bold
            cell.alignment = center
        day_ru = {"friday":"Пятница","saturday":"Суббота","sunday":"Воскресенье"}
        meal_ru = {"lunch":"Обед","dinner":"Ужин"}
        extras_ru = {
            "preparty":"Препати",
            "excursion_minsk":"Экскурсия по Минску",
            "shuttle":"Автобус Минск–Гродно",
            "excursion_grodno":"Экскурсия по Гродно",
        }
        async for order in self.base.food_db.find({"event_number": self.config.event_number}):
            choice = order.get("choice", {})
            total_byn = choice.get("total", 0)
            total_rub = total_byn * BYN_TO_RUB
            dish_counts = {k:0 for k in dish_keys}
            # Detailed dishes
            for day_key, day_data in choice.get("days", {}).items():
                for mealtime_key, mealtime in day_data.get("mealtimes", {}).items():
                    for dish in mealtime.get("dishes", []):
                        name_key = dish.get("name")
                        cnt = dish.get("count",0)
                        price = dish.get("price",0)
                        ru_name = dish_names_ru.get(name_key, name_key)
                        ws_details.append([
                            order.get("user_id",""),
                            str(order.get("_id")),
                            choice.get("customer",""),
                            day_ru.get(day_key, day_key),
                            meal_ru.get(mealtime_key, mealtime_key),
                            ru_name,
                            cnt,
                        ])
                        if name_key in dish_counts:
                            dish_counts[name_key] += cnt
                            totals[name_key]["count"] += cnt
                            totals[name_key]["sum"] += cnt*price
            # Extras rows
            extras = choice.get("extras", {})
            for ex_key, ex_ru in extras_ru.items():
                if ex_key in extras:
                    ws_details.append([
                        order.get("user_id",""),
                        str(order.get("_id")),
                        choice.get("customer",""),
                        day_ru.get("friday","Пятница"),  # day not specified -> reuse first day label or blank
                        "активности",
                        ex_ru,
                        1,
                    ])
                    extras_totals[ex_key]+=1
            row = [
                str(order.get("_id")),
                order.get("user_id",""),
                choice.get("customer",""),
                order.get("created_at",""),
                order.get("updated_at",""),
                order.get("proof_country",""),
                order.get("validation",""),
                currency_ceil(total_byn),
                currency_ceil(total_rub),
                1 if "preparty" in extras else 0,
                1 if "excursion_minsk" in extras else 0,
                1 if "shuttle" in extras else 0,
                1 if "excursion_grodno" in extras else 0,
            ] + [dish_counts[k] for k in dish_keys]
            ws.append(row)
        ws.freeze_panes = "A2"
        for col in ws.columns:
            max_length = 0
            col_letter = col[0].column_letter
            for cell in col:
                try:
                    if cell.value is None:
                        continue
                    length = len(str(cell.value))
                    if length>max_length:
                        max_length=length
                except Exception:
                    pass
            ws.column_dimensions[col_letter].width = min(max(max_length, 6), 40)
        for col in ws_details.columns:
            max_length = 0
            col_letter = col[0].column_letter
            for cell in col:
                try:
                    if cell.value is None:
                        continue
                    length = len(str(cell.value))
                    if length>max_length:
                        max_length=length
                except Exception:
                    pass
            ws_details.column_dimensions[col_letter].width = min(max(max_length, 6), 40)
        ws_totals = wb.create_sheet("Итоги")
        ws_totals.append(["Блюдо","Количество","Сумма BYN"])
        for cell in ws_totals["1:1"]:
            cell.font = bold
            cell.alignment = center
        for k,v in totals.items():
            ws_totals.append([dish_names_ru.get(k,k), v["count"], currency_ceil(v["sum"])])
        ws_extras = wb.create_sheet("Активности")
        ws_extras.append(["Активность","Кол-во заказов"])
        for cell in ws_extras["1:1"]:
            cell.font = bold
            cell.alignment = center
        for k,v in extras_totals.items():
            ws_extras.append([extras_ru.get(k,k), v])
        file_name = "orders.xlsx"
        wb.save(file_name)
        await self.update.bot.send_document(
            self.update.user,
            open(file_name, "rb"),
            caption="Orders XLSX",
        )
        import os
        os.remove(file_name)
        await self.handle_cq_start()

class Orders(BasePlugin):
    name = "orders"
    food_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.base_app.orders = self
        self.food_db = base_app.mongodb[self.config.mongo_db.food_collection]
        self._checker = CommandHandler(self.name, self.handle_start)
        self._file_checker = MessageHandler(filters.Document.PDF, self.handle_payment)
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
        self.menu = self.get_menu()

    def get_menu(self):
        from os.path import dirname as d
        from os.path import join
        import json
        menu_file = join(d(d(d(__file__))), "static", "menu_belarus.json")
        with open(menu_file, "r", encoding="utf-8") as mf:
            return json.load(mf)

    async def create_update_from_user(self, user) -> OrdersUpdate:
        upd = TGState(user, self.base_app)
        await upd.get_state()
        return OrdersUpdate(self, upd)
    
    async def create_order(self, user_id, choice):
        upd = await self.create_update_from_user(user_id)
        return await upd.create_order(choice)
    
    async def set_choice(self, order, choice):
        upd = await self.create_update_from_user(order["user_id"])
        return await upd.set_choice(order["_id"], choice)

    async def order_by_id(self, order_id):
        return await self.food_db.find_one({"_id": ObjectId(order_id)})

    def test_message(self, message: Update, state, web_app_data):
        if self._checker.check_update(message):
            return PRIORITY_BASIC, self.handle_start
        if self._file_checker.check_update(message):
            return PRIORITY_BASIC, self.handle_payment
        return PRIORITY_NOT_ACCEPTING, None
    
    def test_callback_query(self, query: Update, state):
        if self._cbq_handler.check_update(query):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None
    
    def create_update(self, update) -> OrdersUpdate:
        return OrdersUpdate(self, update)
    
    async def handle_callback_query(self, updater):
        return await self.create_update(updater).handle_callback_query()

    async def handle_start(self, update: TGState):
        return await self.create_update(update).handle_start()

    async def handle_payment(self, update: TGState):
        return await self.create_update(update).handle_payment()
