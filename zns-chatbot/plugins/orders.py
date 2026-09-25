import asyncio
import copy
import datetime
from html import escape
import re
import secrets
from motor.core import AgnosticCollection
from ..config import full_link
from ..tg_state import TGState
from telegram import InlineKeyboardMarkup, Update, InlineKeyboardButton, WebAppInfo
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, CallbackQueryHandler, MessageHandler, filters
from telegram.constants import ParseMode
from bson.objectid import ObjectId
from pymongo.errors import DuplicateKeyError
from ..telegram_links import client_user_link_html, client_user_name
import logging
from .massage import now_msk
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
SHUTTLE_CAPACITY = 53
SHUTTLE_SERVICE = "shuttle"
GRODNO_OVERVIEW_SERVICE = "excursion_grodno_overview"
GRODNO_GORODNITSA_SERVICE = "excursion_grodno_gorodnitsa"
CAPACITY_LIMITS = {
    SHUTTLE_SERVICE: SHUTTLE_CAPACITY,
    GRODNO_OVERVIEW_SERVICE: 20,
    GRODNO_GORODNITSA_SERVICE: 25,
}
CAPACITY_SERVICE_PRICES = {
    SHUTTLE_SERVICE: 65,
    GRODNO_OVERVIEW_SERVICE: 25,
    GRODNO_GORODNITSA_SERVICE: 25,
}
EXTRA_PRICES = {
    "preparty": 35,
    "excursion_minsk": 30,
    SHUTTLE_SERVICE: CAPACITY_SERVICE_PRICES[SHUTTLE_SERVICE],
    "excursion_grodno": 25,
    GRODNO_OVERVIEW_SERVICE: CAPACITY_SERVICE_PRICES[GRODNO_OVERVIEW_SERVICE],
    GRODNO_GORODNITSA_SERVICE: CAPACITY_SERVICE_PRICES[GRODNO_GORODNITSA_SERVICE],
}
CAPACITY_SERVICE_LABEL_KEYS = {
    SHUTTLE_SERVICE: "orders-capacity-service-shuttle",
    GRODNO_OVERVIEW_SERVICE: "orders-capacity-service-grodno-overview",
    GRODNO_GORODNITSA_SERVICE: "orders-capacity-service-grodno-gorodnitsa",
}
NOTIFICATION_CLAIM_TTL = datetime.timedelta(minutes=15)
ILLEGAL_XML_CHARACTER_RE = re.compile(r"[\x00-\x08\x0b\x0c\x0e-\x1f]")

# Local Grodno time (Europe/Minsk).
DEADLINE = datetime.datetime(2026, 9, 25)
EXTRAS_DEADLINE = datetime.datetime(2026, 10, 1)


def choice_has_food(choice):
    """Empty meal sections from the web form do not count as food."""
    return any(
        meal.get("dishes")
        for day in (choice or {}).get("days", {}).values()
        for meal in day.get("mealtimes", {}).values()
    )


def orders_open(choice=None):
    """Deadlines are exclusive and use local Grodno time."""
    deadline = DEADLINE if choice_has_food(choice) else EXTRAS_DEADLINE
    return now_msk() < deadline


class CapacityFullError(Exception):
    def __init__(self, service):
        super().__init__(f"capacity is full for {service}")
        self.service = service


class ShuttleFullError(CapacityFullError):
    def __init__(self):
        super().__init__(SHUTTLE_SERVICE)


class InvalidExcursionChoiceError(ValueError):
    pass


class InvalidOrderChoiceError(ValueError):
    pass


def choice_has_shuttle(choice):
    extras = choice.get("extras", {}) if isinstance(choice, dict) else {}
    return isinstance(extras, dict) and SHUTTLE_SERVICE in extras


def choice_capacity_services(choice):
    extras = choice.get("extras", {}) if isinstance(choice, dict) else {}
    if not isinstance(extras, dict):
        return set()
    return {service for service in CAPACITY_LIMITS if service in extras}


def order_has_payment_proof(order):
    """A submitted proof immediately makes limited services count as paid."""
    proof_file = order.get("proof_file")
    return (
        proof_file not in (None, "", "cash", False)
    ) or order.get("validation") is True


def unpaid_order_filter(**fields):
    """Mongo filter for orders that do not yet count as paid."""
    return {
        **fields,
        "validation": {"$ne": True},
        "$or": [
            {"proof_file": {"$exists": False}},
            {"proof_file": {"$in": [None, "", "cash", False]}},
        ],
    }


def xlsx_safe_value(value):
    """Prevent user-controlled text from being interpreted as an XLSX formula."""
    if isinstance(value, str):
        value = ILLEGAL_XML_CHARACTER_RE.sub("", value)
        if value.startswith(("=", "+", "-", "@")):
            return "'" + value
    return value


def new_payment_attempt_token():
    """Return a short token that keeps Telegram callback_data below 64 bytes."""
    return secrets.token_urlsafe(8)


def proof_attempt_filter(order_id, user_id, event_key, attempt_token=None):
    return {
        "_id": ObjectId(order_id),
        "user_id": user_id,
        "event_key": event_key,
        "validation": {"$ne": True},
        "proof_file": {
            "$exists": True,
            "$nin": [None, "", "cash", False],
        },
        "payment_attempt_token": (
            attempt_token if attempt_token else {"$exists": False}
        ),
    }


def payment_attempt_filter(order_id, attempt_token, event_key):
    """Match only the still-current pending payment attempt."""
    if not attempt_token:
        return {
            "_id": ObjectId(order_id),
            "event_key": event_key,
            "payment_attempt_token": {"$exists": False},
            "validation": {"$ne": True},
            "$or": [
                {"proof_file": {"$exists": True, "$nin": [None, "", "cash", False]}},
                {"proof_file": "cash"},
                {
                    "$and": [
                        {"proof_file": {"$exists": False}},
                        {"cash_requested_at": {"$exists": True}},
                    ],
                },
            ],
        }
    return {
        "_id": ObjectId(order_id),
        "event_key": event_key,
        "payment_attempt_token": attempt_token,
        "validation": {"$ne": True},
        "$or": [
            {"proof_file": {"$exists": True, "$nin": [None, "", "cash", False]}},
            {
                "$and": [
                    {"proof_file": {"$exists": False}},
                    {"cash_requested_at": {"$exists": True}},
                ],
            },
        ],
    }


def paid_order_snapshot_filter(order):
    """Return payment fields proving that an order snapshot is still paid."""
    if order.get("validation") is True:
        result = {"validation": True}
        if order.get("payment_attempt_token"):
            result["payment_attempt_token"] = order["payment_attempt_token"]
        return result
    proof_file = order.get("proof_file")
    if proof_file and proof_file != "cash":
        result = {"proof_file": proof_file}
        if order.get("payment_attempt_token"):
            result["payment_attempt_token"] = order["payment_attempt_token"]
        return result
    return {"_id": {"$exists": False}}


def payment_reservation_token(order):
    """Stable identity for the payment attempt owning a capacity reservation."""
    if order.get("payment_attempt_token"):
        return order["payment_attempt_token"]
    proof_file = order.get("proof_file")
    if proof_file not in (None, "", "cash", False):
        return f"legacy-proof:{proof_file}"
    if order.get("validation") is True:
        return f"legacy-validation:{order.get('validated_at', 'true')}"
    return None


def payment_attempt_time(order):
    return (
        order.get("payment_attempt_created_at")
        or order.get("proof_received")
        or order.get("validated_at")
        or order.get("created_at")
    )


def canonicalize_choice(choice, menu):
    """Validate an order and replace all client-provided prices and totals."""
    if not isinstance(choice, dict):
        raise InvalidOrderChoiceError("choice must be an object")
    menu_dishes = menu.get("dishes", {})
    service_items = menu.get("service_items", {})
    menu_choices = menu.get("choices", {})
    days = choice.get("days", {})
    extras = choice.get("extras", {})
    if not isinstance(days, dict) or not isinstance(extras, dict):
        raise InvalidOrderChoiceError("days and extras must be objects")

    result = {}
    for key in (
        "customer",
        "customer_first_name",
        "customer_last_name",
        "customer_patronymus",
    ):
        if key not in choice:
            continue
        value = str(choice.get(key, ""))
        if ILLEGAL_XML_CHARACTER_RE.search(value):
            raise InvalidOrderChoiceError(f"invalid characters in {key}")
        result[key] = value
    result["total"] = 0
    result["days"] = {}

    for day_key, day in days.items():
        if day_key not in menu_choices or not isinstance(day, dict):
            raise InvalidOrderChoiceError(f"unknown day {day_key}")
        mealtimes = day.get("mealtimes", {})
        if not isinstance(mealtimes, dict):
            raise InvalidOrderChoiceError("mealtimes must be an object")
        canonical_day = {"total": 0, "mealtimes": {}}
        for mealtime_key, meal in mealtimes.items():
            meal_menu = menu_choices[day_key].get(mealtime_key)
            if meal_menu is None or not isinstance(meal, dict):
                raise InvalidOrderChoiceError(
                    f"unknown mealtime {day_key}.{mealtime_key}"
                )
            allowed_dishes = {
                dish_key
                for category in meal_menu.values()
                for dish_key in category
            }
            dishes = meal.get("dishes", [])
            if not isinstance(dishes, list):
                raise InvalidOrderChoiceError("dishes must be an array")
            canonical_dishes = []
            service_counts = {}
            meal_total = 0
            for dish in dishes:
                if not isinstance(dish, dict):
                    raise InvalidOrderChoiceError("dish must be an object")
                name = dish.get("name")
                count = dish.get("count")
                if (
                    name not in allowed_dishes
                    or name not in menu_dishes
                    or isinstance(count, bool)
                    or not isinstance(count, int)
                    or count <= 0
                ):
                    raise InvalidOrderChoiceError(f"invalid dish {name}")
                definition = menu_dishes[name]
                price = definition["price"]
                dish_total = count * price
                canonical_dishes.append({
                    "name": name,
                    "count": count,
                    "price": price,
                    "total": dish_total,
                })
                meal_total += dish_total
                for service_key in definition.get("service", []):
                    service = service_items.get(service_key)
                    if service is None:
                        continue
                    if service.get("kind") == "utensil":
                        service_counts[service_key] = 1
                    else:
                        service_counts[service_key] = (
                            service_counts.get(service_key, 0) + count
                        )
            canonical_service_items = []
            service_total = 0
            for service_key, definition in service_items.items():
                count = service_counts.get(service_key, 0)
                if not count:
                    continue
                price = definition["price"]
                item_total = count * price
                canonical_service_items.append({
                    "name": service_key,
                    "count": count,
                    "price": price,
                    "total": item_total,
                })
                service_total += item_total
            meal_total += service_total
            canonical_day["mealtimes"][mealtime_key] = {
                "total": meal_total,
                "dishes": canonical_dishes,
                "service": {
                    "items": canonical_service_items,
                    "total": service_total,
                },
            }
            canonical_day["total"] += meal_total
        result["days"][day_key] = canonical_day
        result["total"] += canonical_day["total"]

    canonical_extras = {"total": 0}
    for extra_key in extras:
        if extra_key == "total":
            continue
        if extra_key not in EXTRA_PRICES:
            raise InvalidOrderChoiceError(f"unknown extra {extra_key}")
        price = EXTRA_PRICES[extra_key]
        canonical_extras[extra_key] = price
        canonical_extras["total"] += price
    result["extras"] = canonical_extras
    result["total"] = currency_ceil(result["total"] + canonical_extras["total"])
    return result


def choice_without_capacity_services(choice, services, menu=None):
    """Return a recalculated copy of a choice with unavailable services removed."""
    if menu is None:
        updated_choice = copy.deepcopy(choice)
    else:
        try:
            updated_choice = canonicalize_choice(choice, menu)
        except InvalidOrderChoiceError:
            # Legacy orders may contain dishes removed from the current menu.
            updated_choice = copy.deepcopy(choice)
    extras = updated_choice.get("extras", {})
    if not isinstance(extras, dict):
        return updated_choice, []

    removed = []
    removed_total = 0
    for service in services:
        if service not in extras:
            continue
        extras.pop(service)
        removed_total += CAPACITY_SERVICE_PRICES[service]
        removed.append(service)

    if not removed:
        return updated_choice, []

    extras["total"] = currency_ceil(max(0, extras.get("total", 0) - removed_total))
    updated_choice["total"] = currency_ceil(
        max(0, updated_choice.get("total", 0) - removed_total)
    )
    return updated_choice, removed


def validate_excursion_choice(choice):
    extras = choice.get("extras", {}) if isinstance(choice, dict) else {}
    if not isinstance(extras, dict):
        raise InvalidExcursionChoiceError("extras must be an object")
    selected = {
        service for service in (GRODNO_OVERVIEW_SERVICE, GRODNO_GORODNITSA_SERVICE)
        if service in extras
    }
    if "excursion_grodno" in extras or len(selected) > 1:
        raise InvalidExcursionChoiceError("select exactly one Grodno excursion variant")


def capacity_full_error(service):
    if service == SHUTTLE_SERVICE:
        return ShuttleFullError()
    return CapacityFullError(service)

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

    def current_event_filter(self, **fields):
        return {"event_key": self.config.event_key, **fields}

    async def order_is_open(self, order_id):
        order = await self.base.food_db.find_one(self.current_event_filter(
            _id=ObjectId(order_id), user_id=self.user,
        ))
        return order is not None and orders_open(order.get("choice"))

    async def create_order(self, choice):
        choice = canonicalize_choice(choice, self.base.menu)
        # Block creating orders after deadline
        if not orders_open(choice):
            return await self.update.reply(
                self.l("orders-closed"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        validate_excursion_choice(choice)
        for service in sorted(choice_capacity_services(choice)):
            if not await self.base.service_available(service):
                raise capacity_full_error(service)
        await self.base.food_db.insert_one({
            "_id": ObjectId(),
            "user_id": self.user,
            "event_key": self.config.event_key,
            "created_at": datetime.datetime.now(),
            "choice": choice,
        })
        return await self.handle_cq_start()

    async def set_choice(self, order_id, choice):
        choice = canonicalize_choice(choice, self.base.menu)
        # Block modifying orders after deadline
        if not orders_open(choice) or not await self.order_is_open(order_id):
            return await self.update.reply(
                self.l("orders-closed"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        validate_excursion_choice(choice)
        order_oid = ObjectId(order_id)
        order_filter = self.current_event_filter(_id=order_oid, user_id=self.user)
        previous_order = await self.base.food_db.find_one(order_filter)
        if previous_order is None:
            raise ValueError(f"order {order_id} not found in current event")
        if order_has_payment_proof(previous_order):
            raise ValueError(f"paid order {order_id} cannot be changed")
        for service in sorted(choice_capacity_services(choice)):
            if not await self.base.service_available(service):
                raise capacity_full_error(service)
        result = await self.base.food_db.update_one(unpaid_order_filter(
            _id=order_oid,
            user_id=self.user,
            event_key=self.config.event_key,
        ), {
            "$set":{
                "choice": choice,
                "updated_at": datetime.datetime.now(),
            },
            "$unset": {
                "cash_requested_at": "",
                "payment_attempt_token": "",
                "payment_attempt_created_at": "",
                "proof_admin": "",
                "proof_country": "",
                "proof_file": "",
                "proof_received": "",
                "proof_chat_id": "",
                "proof_message_id": "",
            },
        })
        if result.matched_count == 0:
            raise ValueError(f"order {order_id} not found")
        return await self.handle_cq_start()

    async def handle_cq_del(self, order_id):
        # Disallow deleting after deadline
        if not await self.order_is_open(order_id):
            return await self.handle_cq_start()
        guarded_filter = unpaid_order_filter(
            _id=ObjectId(order_id),
            user_id=self.user,
            event_key=self.config.event_key,
        )
        order = await self.base.food_db.find_one(guarded_filter)
        if order is None:
            return await self.handle_cq_start()
        result = await self.base.food_db.delete_one(guarded_filter)
        if result.deleted_count == 0:
            return await self.handle_cq_start()
        for service in choice_capacity_services(order.get("choice", {})):
            await self.base.release_service_seat(service, order["_id"])
        return await self.handle_cq_start()

    def get_order_total(self, order):
        total = currency_ceil(order["choice"]["total"])
        total_rub = currency_ceil(order["choice"]["total"] * BYN_TO_RUB)
        return total, total_rub
    
    async def handle_cq_pay(self, order_id):
        order = await self.base.food_db.find_one(self.current_event_filter(
            _id=ObjectId(order_id), user_id=self.user
        ))
        if order is None:
            return await self.handle_cq_start()
        if order_has_payment_proof(order) or not orders_open(order.get("choice")):
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
        # Block cash confirmation creation after deadline
        if not await self.order_is_open(order_id):
            return await self.handle_cq_start()
        # return await self.handle_cq_start()
        admin = await self.base.base_app.users_collection.find_one({
            "user_id": int(admin_id),
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists":True},
        })
        if admin is not None:
            attempt_token = new_payment_attempt_token()
            attempt_created_at = datetime.datetime.now()
            order_filter = unpaid_order_filter(
                _id=ObjectId(order_id),
                user_id=self.user,
                event_key=self.config.event_key,
            )
            result = await self.base.food_db.update_one(order_filter, {
                "$set": {
                    "proof_country": "be",
                    "proof_admin": int(admin_id),
                    "cash_requested_at": attempt_created_at,
                    "payment_attempt_token": attempt_token,
                    "payment_attempt_created_at": attempt_created_at,
                },
                "$unset": {
                    "proof_file": "",
                    "proof_received": "",
                    "proof_chat_id": "",
                    "proof_message_id": "",
                },
            })
            if result.matched_count == 0:
                return await self.handle_cq_start()
            order = await self.base.food_db.find_one(order_filter)
            if order is None:
                return await self.handle_cq_start()
            total, _total_rub = self.get_order_total(order)
            user = await self.update.get_user()
            lc = "ru"
            if "language_code" in admin:
                lc = admin["language_code"]
            def loc(s, **kwargs):
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.update.reply(
                loc(
                    "orders-adm-payment-cash-requested",
                    link=client_user_link_html(user),
                    total=total,
                    name=escape(str(order["choice"]["customer"])),
                ),
                chat_id=admin["user_id"],
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton(loc("food-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{order_id}|{attempt_token}"),
                        InlineKeyboardButton(loc("food-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{order_id}|{attempt_token}"),
                    ]
                ])
            )
        await self.update.edit_or_reply(
            self.l("orders-payment-cash-requested",
                link=client_user_link_html(admin),
                total=total,
                name=escape(str(order["choice"]["customer"])),
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_cq_start(self):
        orders = await self.base.food_db.find({
            "user_id": self.user,
            "event_key": self.config.event_key,
        }).sort("created_at", 1).to_list(None)
        user = await self.update.get_user()
        debug_param = ""
        if "debug_id" in user:
            debug_param = "&debug_id="+user["debug_id"]
        current_order = None
        btns = []
        for order in orders:
            if not order_has_payment_proof(order) and orders_open(order.get("choice")):
                if current_order is None:
                    current_order = order
            else:
                btns.append([InlineKeyboardButton(self.l(
                    "orders-order-button",
                    created=order["created_at"].strftime("%d.%m"),
                    name=order["choice"]["customer"],
                ), web_app=WebAppInfo(full_link(self.base.base_app, f"/orders?order_id={str(order['_id'])}&locale={self.update.language_code}{debug_param}")))])
        if orders_open():
            if current_order is not None:
                order = current_order
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
    
    async def handle_cq_paid(self, order_id):
        owner_filter = self.current_event_filter(
            _id=ObjectId(order_id), user_id=self.user
        )
        order = await self.base.food_db.find_one(owner_filter)
        if order is None or not order_has_payment_proof(order):
            return await self.handle_cq_start()
        attempt_token = order.get("payment_attempt_token")
        order_filter = proof_attempt_filter(
            order_id, self.user, self.config.event_key, attempt_token
        )
        result = await self.base.food_db.update_one(order_filter, {
            "$set": {
                "proof_country": "ru",
            }
        })
        if result.matched_count == 0:
            return await self.handle_cq_start()
        order = await self.base.food_db.find_one(order_filter)
        if order is None:
            return await self.handle_cq_start()
        callback_suffix = f"|{attempt_token}" if attempt_token else ""
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
            def loc(s, **kwargs):
                return self.base.base_app.localization(s, args=kwargs, locale=lc)
            await self.update.forward_message(
                adm,
                order["proof_chat_id"],
                order["proof_message_id"]
            )
            await self.update.reply(
                loc(
                    "orders-adm-payment-proof-received",
                    link=client_user_link_html(user),
                    total=total,
                    name=escape(str(order["choice"]["customer"])),
                ),
                chat_id=adm,
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([
                    [
                        InlineKeyboardButton(loc("food-adm-payment-proof-accept-button"), callback_data=f"{self.base.name}|adm_acc|{order_id}{callback_suffix}"),
                        InlineKeyboardButton(loc("food-adm-payment-proof-reject-button"), callback_data=f"{self.base.name}|adm_rej|{order_id}{callback_suffix}"),
                    ]
                ])
            )
        await self.update.edit_or_reply(
            self.l("food-payment-proof-forwarded"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_cq_adm_acc(self, order_id, attempt_token=None):
        # Guard: only RU / BE payment admins or orders admins (assert style)
        admins_be_ids = {a["user_id"] for a in await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists": True},
        }).to_list(None)}
        assert (self.user in self.config.admins or
                (self.config.payment_admin_ru and self.user == self.config.payment_admin_ru) or
                self.user in admins_be_ids), f"{self.user} is not orders admin"
        order_filter = payment_attempt_filter(
            order_id, attempt_token, self.config.event_key
        )
        result = await self.base.food_db.update_one(order_filter, {
            "$set":{
                "validated_at": datetime.datetime.now(),
                "validation": True,
            }
        })
        if result.matched_count == 0:
            return await self.handle_cq_start()
        order = await self.base.food_db.find_one(self.current_event_filter(
            _id=ObjectId(order_id)
        ))
        if order is None:
            return await self.handle_cq_start()
        order, _removed = await self.base.activate_paid_order_capacity(order["_id"])
        await self.base.reconcile_capacity()
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
                name=escape(str(order["choice"]["customer"])),
            ),
            order["user_id"],
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "food-adm-payment-proof-confirmed",
                link=client_user_link_html(user),
                name=escape(str(order["choice"]["customer"])),
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )

    async def handle_cq_adm_rej(self, order_id, attempt_token=None):
        # Guard: only RU / BE payment admins or orders admins (assert style)
        admins_be_ids = {a["user_id"] for a in await self.base.base_app.users_collection.find({
            "bot_id": self.bot,
            "payment_administrator_belarus": {"$exists": True},
        }).to_list(None)}
        assert (self.user in self.config.admins or
                (self.config.payment_admin_ru and self.user == self.config.payment_admin_ru) or
                self.user in admins_be_ids), f"{self.user} is not orders admin"
        order_filter = payment_attempt_filter(
            order_id, attempt_token, self.config.event_key
        )
        order_before_rejection = await self.base.food_db.find_one_and_update(order_filter, {
            "$set":{
                "validated_at": datetime.datetime.now(),
                "validation": False,
            },
            "$unset": {
                "proof_file": "",
                "proof_received": "",
                "proof_chat_id": "",
                "proof_message_id": "",
                "cash_requested_at": "",
                "payment_attempt_token": "",
                "payment_attempt_created_at": "",
            },
        })
        if order_before_rejection is None:
            return await self.handle_cq_start()
        await self.base.release_order_capacity(order_before_rejection)
        order = await self.base.food_db.find_one(self.current_event_filter(
            _id=ObjectId(order_id)
        ))
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
                name=escape(str(order["choice"]["customer"])),
            ),
            order["user_id"],
            parse_mode=ParseMode.HTML
        )
        await self.update.edit_message_text(
            self.l(
                "food-adm-payment-proof-rejected",
                link=client_user_link_html(user),
                name=escape(str(order["choice"]["customer"])),
            ),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([])
        )
    
    async def handle_cq_pcancel(self, order_id, attempt_token=None):
        # Block canceling proof after deadline
        if not await self.order_is_open(order_id):
            return await self.update.edit_or_reply(self.l("orders-closed"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        order_filter = self.current_event_filter(
            _id=ObjectId(order_id),
            user_id=self.user,
            validation={"$ne": True},
            proof_file={
                "$exists": True,
                "$nin": [None, "", "cash", False],
            },
            payment_attempt_token=(
                attempt_token if attempt_token
                else {"$exists": False}
            ),
        )
        order = await self.base.food_db.find_one(order_filter)
        result = await self.base.food_db.update_one(order_filter, {
            "$unset": {
                "proof_file": "",
                "proof_received": "",
                "proof_chat_id": "",
                "proof_message_id": "",
                "payment_attempt_token": "",
                "payment_attempt_created_at": "",
                "cash_requested_at": "",
                "proof_admin": "",
                "proof_country": "",
            }
        })
        if order is not None and result.matched_count == 1:
            await self.base.release_order_capacity(order)
        await self.update.edit_or_reply(self.l("orders-closed"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([]),
        )

    async def handle_payment(self):
        logger.debug(f"handling payment for: {self.user}")
        # After deadline, ignore new payment proofs
        if not orders_open():
            return await self.update.edit_or_reply(
                self.l("orders-closed"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        order = None
        async for candidate in self.base.food_db.find(unpaid_order_filter(
            user_id=self.user,
            event_key=self.config.event_key,
        )).sort("created_at", 1):
            if orders_open(candidate.get("choice")):
                order = candidate
                break
        if order is None:
            return await self.update.edit_or_reply(
                self.l("unsupported-message-error"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        doc = self.update.message.document
        attempt_token = new_payment_attempt_token()
        attempt_created_at = datetime.datetime.now()
        result = await self.base.food_db.update_one(unpaid_order_filter(
            _id=order["_id"],
            user_id=self.user,
            event_key=self.config.event_key,
        ), {
            "$set": {
                "proof_file": doc.file_id,
                "proof_chat_id": self.update.chat_id if self.update.chat_id is not None else self.update.user,
                "proof_message_id": self.update.message_id,
                "proof_received": attempt_created_at,
                "payment_attempt_token": attempt_token,
                "payment_attempt_created_at": attempt_created_at,
            },
            "$unset": {
                "cash_requested_at": "",
                "proof_admin": "",
                "proof_country": "",
            },
        })
        if result.matched_count == 0:
            return await self.update.edit_or_reply(
                self.l("unsupported-message-error"),
                parse_mode=ParseMode.HTML,
                reply_markup=InlineKeyboardMarkup([]),
            )
        await self.base.activate_paid_order_capacity(order["_id"])
        await self.base.reconcile_capacity()
        await self.update.edit_or_reply(
            self.l("orders-message-paid-where"),
            parse_mode=ParseMode.HTML,
            reply_markup=InlineKeyboardMarkup([[InlineKeyboardButton(
                self.l("orders-paid-button"),
                callback_data=f"{self.base.name}|paid|{order['_id']}"
            )],[InlineKeyboardButton(
                self.l("orders-pay-cancel"),
                callback_data=(
                    f"{self.base.name}|pcancel|{order['_id']}|{attempt_token}"
                )
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
        # Collect menu and disposable-service columns.
        menu_dishes = self.base.menu.get("dishes", {})
        dish_keys = list(menu_dishes)
        dish_names_ru = {
            dish_key: dish.get("name_ru", dish_key)
            for dish_key, dish in menu_dishes.items()
        }
        service_items = self.base.menu.get("service_items", {})
        service_keys = list(service_items)
        service_names_ru = {
            service_key: service.get("name_ru", service_key)
            for service_key, service in service_items.items()
        }
        # Base columns (english keys -> russian headers)
        base_fields = [
            ("order_id", "ID заказа"),
            ("user_id", "Пользователь"),
            ("customer", "Клиент"),
            ("created_at", "Создан"),
            ("updated_at", "Обновлён"),
            ("proof_country", "Страна оплаты"),
            ("proof_admin", "Администратор оплаты"),
            ("paid", "Оплачено"),
            ("validation", "Подтверждено администратором"),
            ("total_byn", "Сумма BYN"),
            ("total_rub", "Сумма RUB"),
            ("extras_preparty", "Препати"),
            ("extras_excursion_minsk", "Экскурсия-квест Минск"),
            ("extras_shuttle", "Трансфер"),
            ("extras_excursion_grodno_overview", "Гродно: обзорная"),
            ("extras_excursion_grodno_gorodnitsa", "Гродно: Городница"),
            ("extras_excursion_grodno", "Гродно: вариант не указан"),
        ]
        header = (
            [ru for _k, ru in base_fields]
            + [dish_names_ru.get(k, k) for k in dish_keys]
            + [service_names_ru.get(k, k) for k in service_keys]
        )
        ws.append(header)
        bold = Font(bold=True)
        center = Alignment(horizontal="center")
        for cell in ws["1:1"]:
            cell.font = bold
            cell.alignment = center
        totals = {k: {"count":0,"sum":0} for k in dish_keys}
        service_totals = {k: {"count":0,"sum":0} for k in service_keys}
        extras_totals = {
            "preparty": 0,
            "excursion_minsk": 0,
            "shuttle": 0,
            GRODNO_OVERVIEW_SERVICE: 0,
            GRODNO_GORODNITSA_SERVICE: 0,
            "excursion_grodno": 0,
        }
        # Second sheet with detailed contents
        ws_details = wb.create_sheet("Содержимое")
        ws_details.append(["Пользователь","ID заказа","Клиент","День","Приём пищи","Блюдо / Активность","Оплачено","Количество"])
        for cell in ws_details["1:1"]:
            cell.font = bold
            cell.alignment = center
        day_ru = {"friday":"Пятница","saturday":"Суббота","sunday":"Воскресенье"}
        meal_ru = {"lunch":"Обед","dinner":"Ужин"}
        extras_ru = {
            "preparty":"Препати",
            "excursion_minsk":"Экскурсия-квест Минск",
            "shuttle":"Трансфер Минск–Гродно",
            GRODNO_OVERVIEW_SERVICE: "Гродно: обзорная экскурсия",
            GRODNO_GORODNITSA_SERVICE: "Гродно: экскурсия «Городница»",
            "excursion_grodno":"Экскурсия по Гродно (вариант не указан)",
        }
        async for order in self.base.food_db.find({"event_key": self.config.event_key}):
            choice = order.get("choice", {})
            paid = order_has_payment_proof(order)
            total_byn = choice.get("total", 0)
            total_rub = total_byn * BYN_TO_RUB
            dish_counts = {k:0 for k in dish_keys}
            service_counts = {k:0 for k in service_keys}
            # Detailed dishes
            for day_key, day_data in choice.get("days", {}).items():
                for mealtime_key, mealtime in day_data.get("mealtimes", {}).items():
                    for dish in mealtime.get("dishes", []):
                        name_key = dish.get("name")
                        cnt = dish.get("count",0)
                        price = dish.get("price",0)
                        ru_name = dish_names_ru.get(
                            name_key, xlsx_safe_value(name_key)
                        )
                        ws_details.append([
                            order.get("user_id",""),
                            str(order.get("_id")),
                            xlsx_safe_value(choice.get("customer", "")),
                            xlsx_safe_value(day_ru.get(day_key, day_key)),
                            xlsx_safe_value(meal_ru.get(mealtime_key, mealtime_key)),
                            ru_name,
                            paid,
                            xlsx_safe_value(cnt),
                        ])
                        if name_key in dish_counts:
                            dish_counts[name_key] += cnt
                            if paid:
                                totals[name_key]["count"] += cnt
                                totals[name_key]["sum"] += cnt*price
                    for service in mealtime.get("service", {}).get("items", []):
                        name_key = service.get("name")
                        cnt = service.get("count", 0)
                        price = service.get("price", 0)
                        ws_details.append([
                            order.get("user_id", ""),
                            str(order.get("_id")),
                            xlsx_safe_value(choice.get("customer", "")),
                            xlsx_safe_value(day_ru.get(day_key, day_key)),
                            xlsx_safe_value(meal_ru.get(mealtime_key, mealtime_key)),
                            service_names_ru.get(
                                name_key, xlsx_safe_value(name_key)
                            ),
                            paid,
                            xlsx_safe_value(cnt),
                        ])
                        if name_key in service_counts:
                            service_counts[name_key] += cnt
                            if paid:
                                service_totals[name_key]["count"] += cnt
                                service_totals[name_key]["sum"] += cnt * price
            # Extras rows
            extras = choice.get("extras", {})
            for ex_key, ex_ru in extras_ru.items():
                if ex_key in extras:
                    ws_details.append([
                        order.get("user_id",""),
                        str(order.get("_id")),
                        xlsx_safe_value(choice.get("customer", "")),
                        day_ru.get("friday","Пятница"),  # day not specified -> reuse first day label or blank
                        "активности",
                        ex_ru,
                        paid,
                        1,
                    ])
                    if paid:
                        extras_totals[ex_key]+=1
            row = [
                str(order.get("_id")),
                order.get("user_id",""),
                xlsx_safe_value(choice.get("customer", "")),
                order.get("created_at",""),
                order.get("updated_at",""),
                order.get("proof_country",""),
                order.get("proof_admin",""),
                paid,
                order.get("validation",""),
                currency_ceil(total_byn),
                currency_ceil(total_rub),
                1 if "preparty" in extras else 0,
                1 if "excursion_minsk" in extras else 0,
                1 if "shuttle" in extras else 0,
                1 if GRODNO_OVERVIEW_SERVICE in extras else 0,
                1 if GRODNO_GORODNITSA_SERVICE in extras else 0,
                1 if "excursion_grodno" in extras else 0,
            ] + [dish_counts[k] for k in dish_keys] + [service_counts[k] for k in service_keys]
            ws.append(row)
        ws.freeze_panes = "A2"
        ws.auto_filter.ref = ws.dimensions
        ws_details.freeze_panes = "A2"
        ws_details.auto_filter.ref = ws_details.dimensions
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
        ws_totals.append(["Блюдо","Количество оплачено","Сумма BYN оплачено"])
        for cell in ws_totals["1:1"]:
            cell.font = bold
            cell.alignment = center
        for k,v in totals.items():
            ws_totals.append([dish_names_ru.get(k,k), v["count"], currency_ceil(v["sum"])])
        ws_service = wb.create_sheet("Посуда")
        ws_service.append(["Позиция", "Количество оплачено", "Сумма BYN оплачено"])
        for cell in ws_service["1:1"]:
            cell.font = bold
            cell.alignment = center
        for k, v in service_totals.items():
            ws_service.append([
                service_names_ru.get(k, k),
                v["count"],
                currency_ceil(v["sum"]),
            ])
        ws_extras = wb.create_sheet("Активности")
        ws_extras.append(["Активность","Кол-во оплаченных заказов"])
        for cell in ws_extras["1:1"]:
            cell.font = bold
            cell.alignment = center
        for k,v in extras_totals.items():
            ws_extras.append([extras_ru.get(k,k), v])
        from io import BytesIO
        export_file = BytesIO()
        export_file.name = "orders.xlsx"
        wb.save(export_file)
        export_file.seek(0)
        await self.update.bot.send_document(
            self.update.user,
            export_file,
            filename="orders.xlsx",
            caption="Orders XLSX",
        )
        await self.handle_cq_start()

class Orders(BasePlugin):
    name = "orders"
    food_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.base_app.orders = self
        self.food_db = base_app.mongodb[self.config.mongo_db.food_collection]
        self.capacity_db = base_app.mongodb[
            self.config.mongo_db.food_collection + "_capacity"
        ]
        self._capacity_slots_ready = set()
        self._capacity_slots_lock = asyncio.Lock()
        self._checker = CommandHandler(self.name, self.handle_start)
        self._file_checker = MessageHandler(filters.Document.PDF, self.handle_payment)
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}\\|.*")
        self.menu = self.get_menu()
        asyncio.create_task(self._notification_sender())

    def _capacity_event_key(self):
        return self.config.orders.event_key

    async def _ensure_capacity_slots(self, service):
        capacity = CAPACITY_LIMITS[service]
        event_key = self._capacity_event_key()
        ready_key = (event_key, service)
        if ready_key in self._capacity_slots_ready:
            return
        async with self._capacity_slots_lock:
            if ready_key in self._capacity_slots_ready:
                return

            await self.capacity_db.create_index(
                [
                    ("event_key", 1),
                    ("service", 1),
                    ("reservation_id", 1),
                ],
                unique=True,
                partialFilterExpression={"reservation_id": {"$exists": True}},
            )
            for seat in range(capacity):
                await self.capacity_db.update_one(
                    {"_id": f"{event_key}:{service}:{seat}"},
                    {"$setOnInsert": {
                        "event_key": event_key,
                        "service": service,
                        "seat": seat,
                    }},
                    upsert=True,
                )

            candidate_orders = await self.food_db.find(
                {
                    "event_key": event_key,
                    f"choice.extras.{service}": {"$exists": True},
                },
                {
                    "_id": 1,
                    "created_at": 1,
                    "proof_file": 1,
                    "proof_received": 1,
                    "validated_at": 1,
                    "validation": 1,
                    "payment_attempt_token": 1,
                    "payment_attempt_created_at": 1,
                },
            ).to_list(None)
            existing_orders = [
                order for order in candidate_orders if order_has_payment_proof(order)
            ]
            existing_orders.sort(key=lambda order: (
                payment_attempt_time(order) or datetime.datetime.max,
                str(order["_id"]),
            ))
            existing_orders = existing_orders[:capacity]
            existing_order_ids = {order["_id"] for order in existing_orders}
            reserved_slots = await self.capacity_db.find(
                {
                    "event_key": event_key,
                    "service": service,
                    "reservation_id": {"$exists": True},
                },
                {"reservation_id": 1},
            ).to_list(None)
            reserved_order_ids = {
                slot["reservation_id"] for slot in reserved_slots
                if slot.get("reservation_id") in existing_order_ids
            }
            for slot in reserved_slots:
                reservation_id = slot.get("reservation_id")
                if reservation_id not in existing_order_ids:
                    await self.capacity_db.update_one(
                        {
                            "_id": slot["_id"],
                            "reservation_id": reservation_id,
                        },
                        {"$unset": {
                            "reservation_id": "",
                            "reservation_attempt_token": "",
                            "reservation_attempt_created_at": "",
                            "reserved_at": "",
                        }},
                    )

            for order in existing_orders:
                order_id = order["_id"]
                reservation_token = payment_reservation_token(order)
                if order_id in reserved_order_ids:
                    await self.capacity_db.update_one(
                        {
                            "event_key": event_key,
                            "service": service,
                            "reservation_id": order_id,
                        },
                        {"$set": {
                            "reservation_attempt_token": reservation_token,
                            "reservation_attempt_created_at": payment_attempt_time(order),
                        }},
                    )
                    continue
                await self.capacity_db.find_one_and_update(
                    {
                        "event_key": event_key,
                        "service": service,
                        "reservation_id": {"$exists": False},
                    },
                    {"$set": {
                        "reservation_id": order_id,
                        "reservation_attempt_token": reservation_token,
                        "reservation_attempt_created_at": payment_attempt_time(order),
                        "reserved_at": datetime.datetime.now(),
                    }},
                    sort=[("seat", 1)],
                )

            self._capacity_slots_ready.add(ready_key)

    async def _release_deleted_order_reservations(self, service):
        event_key = self._capacity_event_key()
        # Payment reservations refer to persisted orders. Bare reservations may
        # still be in flight and must not be treated as deleted orders.
        slots = await self.capacity_db.find({
            "event_key": event_key,
            "service": service,
            "reservation_id": {"$exists": True},
            "reservation_attempt_token": {"$exists": True},
        }).to_list(None)
        if not slots:
            return
        orders = await self.food_db.find({
            "_id": {"$in": [slot["reservation_id"] for slot in slots]},
            "event_key": event_key,
        }, {"_id": 1}).to_list(None)
        existing_ids = {order["_id"] for order in orders}
        for slot in slots:
            if slot["reservation_id"] in existing_ids:
                continue
            await self.capacity_db.update_one(
                {
                    "_id": slot["_id"],
                    "reservation_id": slot["reservation_id"],
                    "reservation_attempt_token": slot["reservation_attempt_token"],
                    "reserved_at": slot.get("reserved_at"),
                },
                {"$unset": {
                    "reservation_id": "",
                    "reservation_attempt_token": "",
                    "reservation_attempt_created_at": "",
                    "reserved_at": "",
                }},
            )

    async def reserve_service_seat(self, service, order_id, order_snapshot=None):
        await self._ensure_capacity_slots(service)
        await self._release_deleted_order_reservations(service)
        event_key = self._capacity_event_key()
        reservation_token = (
            payment_reservation_token(order_snapshot) if order_snapshot else None
        )
        reservation_created_at = (
            payment_attempt_time(order_snapshot) if order_snapshot else None
        )
        payment_filter = None
        if order_snapshot is not None:
            payment_filter = {
                "_id": ObjectId(order_id),
                "event_key": event_key,
                **paid_order_snapshot_filter(order_snapshot),
            }

        for _attempt in range(3):
            if payment_filter is not None:
                current = await self.food_db.find_one(payment_filter)
                if current is None:
                    return False
            existing = await self.capacity_db.find_one({
                "event_key": event_key,
                "service": service,
                "reservation_id": order_id,
            })
            if existing is not None:
                if reservation_token is None:
                    return True
                if existing.get("reservation_attempt_token") == reservation_token:
                    return True
                existing_created_at = existing.get(
                    "reservation_attempt_created_at"
                )
                if (
                    reservation_created_at is None
                    or (
                        existing_created_at is not None
                        and reservation_created_at <= existing_created_at
                    )
                ):
                    return False
                token_filter = (
                    {"$exists": False}
                    if "reservation_attempt_token" not in existing
                    else existing.get("reservation_attempt_token")
                )
                created_filter = (
                    {"$exists": False}
                    if "reservation_attempt_created_at" not in existing
                    else existing_created_at
                )
                adopted = await self.capacity_db.update_one(
                    {
                        "_id": existing["_id"],
                        "reservation_id": order_id,
                        "reservation_attempt_token": token_filter,
                        "reservation_attempt_created_at": created_filter,
                    },
                    {"$set": {
                        "reservation_attempt_token": reservation_token,
                        "reservation_attempt_created_at": reservation_created_at,
                        "reserved_at": datetime.datetime.now(),
                    }},
                )
                if adopted.matched_count == 1:
                    return True
                continue
            slot_update = {
                "reservation_id": order_id,
                "reserved_at": datetime.datetime.now(),
            }
            if reservation_token is not None:
                slot_update["reservation_attempt_token"] = reservation_token
                slot_update["reservation_attempt_created_at"] = (
                    reservation_created_at
                )
            try:
                claimed = await self.capacity_db.find_one_and_update(
                    {
                        "event_key": event_key,
                        "service": service,
                        "reservation_id": {"$exists": False},
                    },
                    {"$set": slot_update},
                    sort=[("seat", 1)],
                )
            except DuplicateKeyError:
                continue
            if claimed is not None:
                return True
        if reservation_created_at is None:
            return False
        current_priority = (reservation_created_at, str(order_id))
        while True:
            existing = await self.capacity_db.find_one({
                "event_key": event_key,
                "service": service,
                "reservation_id": order_id,
                "reservation_attempt_token": reservation_token,
            })
            if existing is not None:
                return True
            if payment_filter is not None:
                current = await self.food_db.find_one(payment_filter)
                if current is None:
                    return False
            reserved_slots = await self.capacity_db.find({
                "event_key": event_key,
                "service": service,
                "reservation_id": {"$exists": True},
            }).to_list(None)
            later_slots = [
                slot for slot in reserved_slots
                if slot.get("reservation_attempt_created_at") is not None
                and (
                    slot["reservation_attempt_created_at"],
                    str(slot.get("reservation_id")),
                ) > current_priority
            ]
            if not later_slots:
                try:
                    claimed = await self.capacity_db.find_one_and_update(
                        {
                            "event_key": event_key,
                            "service": service,
                            "reservation_id": {"$exists": False},
                        },
                        {"$set": slot_update},
                        sort=[("seat", 1)],
                    )
                except DuplicateKeyError:
                    continue
                if claimed is not None:
                    return True
                refreshed_slots = await self.capacity_db.find({
                    "event_key": event_key,
                    "service": service,
                    "reservation_id": {"$exists": True},
                }).to_list(None)
                def snapshot(slots):
                    return tuple(sorted(
                        (
                            str(slot.get("_id")),
                            str(slot.get("reservation_id")),
                            str(slot.get("reservation_attempt_token")),
                            str(slot.get("reservation_attempt_created_at")),
                        )
                        for slot in slots
                    ))
                if snapshot(refreshed_slots) == snapshot(reserved_slots):
                    return False
                continue
            displaced = max(later_slots, key=lambda slot: (
                slot["reservation_attempt_created_at"],
                str(slot["reservation_id"]),
            ))
            try:
                swapped = await self.capacity_db.update_one(
                    {
                        "_id": displaced["_id"],
                        "reservation_id": displaced["reservation_id"],
                        "reservation_attempt_token": displaced.get(
                            "reservation_attempt_token"
                        ),
                        "reservation_attempt_created_at": displaced[
                            "reservation_attempt_created_at"
                        ],
                    },
                    {"$set": {
                        "reservation_id": order_id,
                        "reservation_attempt_token": reservation_token,
                        "reservation_attempt_created_at": reservation_created_at,
                        "reserved_at": datetime.datetime.now(),
                    }},
                )
            except DuplicateKeyError:
                continue
            if swapped.matched_count == 1:
                return True
        return False

    async def release_service_seat(self, service, order_id, reservation_token=None):
        await self._ensure_capacity_slots(service)
        release_filter = {
            "event_key": self._capacity_event_key(),
            "service": service,
            "reservation_id": order_id,
        }
        if reservation_token is not None:
            release_filter["reservation_attempt_token"] = reservation_token
        await self.capacity_db.update_one(
            release_filter,
            {"$unset": {
                "reservation_id": "",
                "reservation_attempt_token": "",
                "reservation_attempt_created_at": "",
                "reserved_at": "",
            }},
        )

    async def service_available(self, service, current_choice=None, current_order_paid=False):
        if (
            current_order_paid
            and service in choice_capacity_services(current_choice or {})
        ):
            return True
        await self._ensure_capacity_slots(service)
        await self._release_deleted_order_reservations(service)
        free_slot = await self.capacity_db.find_one({
            "event_key": self._capacity_event_key(),
            "service": service,
            "reservation_id": {"$exists": False},
        })
        return free_slot is not None

    async def reserve_shuttle_seat(self, order_id):
        return await self.reserve_service_seat(SHUTTLE_SERVICE, order_id)

    async def release_shuttle_seat(self, order_id):
        await self.release_service_seat(SHUTTLE_SERVICE, order_id)

    async def shuttle_available(self, current_choice=None, current_order_paid=False):
        return await self.service_available(
            SHUTTLE_SERVICE, current_choice, current_order_paid
        )

    async def remove_capacity_services_from_order(
        self, order, services, reason="unpaid"
    ):
        updated_choice, removed = choice_without_capacity_services(
            order.get("choice", {}), services, self.menu
        )
        if not removed:
            return order, []

        old_total = currency_ceil(order.get("choice", {}).get("total", 0))
        new_total = currency_ceil(updated_choice.get("total", 0))
        notice = {
            "services": sorted(removed),
            "old_total": old_total,
            "new_total": new_total,
            "created_at": datetime.datetime.now(),
            "reason": reason,
            "sent": False,
        }
        if reason == "unpaid":
            update_filter = unpaid_order_filter(
                _id=order["_id"],
                event_key=self._capacity_event_key(),
                choice=order.get("choice", {}),
            )
        else:
            update_filter = {
                "_id": order["_id"],
                "event_key": self._capacity_event_key(),
                "choice": order.get("choice", {}),
                **paid_order_snapshot_filter(order),
            }
        result = await self.food_db.update_one(
            update_filter,
            {"$set": {
                "choice": updated_choice,
                "updated_at": datetime.datetime.now(),
                "capacity_notice": notice,
            }},
        )
        if result.matched_count == 0:
            current = await self.food_db.find_one({
                "_id": order["_id"],
                "event_key": self._capacity_event_key(),
            })
            return current or order, []

        updated_order = copy.deepcopy(order)
        updated_order["choice"] = updated_choice
        updated_order["capacity_notice"] = notice
        return updated_order, removed

    async def activate_paid_order_capacity(self, order_id):
        order = await self.food_db.find_one({
            "_id": ObjectId(order_id),
            "event_key": self._capacity_event_key(),
        })
        if order is None or not order_has_payment_proof(order):
            return order, []

        unavailable = []
        for service in sorted(choice_capacity_services(order.get("choice", {}))):
            if not await self.reserve_service_seat(service, order["_id"], order):
                unavailable.append(service)
        current = await self.food_db.find_one({
            "_id": order["_id"],
            "event_key": self._capacity_event_key(),
            **paid_order_snapshot_filter(order),
        })
        if current is None:
            await self.release_order_capacity(order)
            current = await self.food_db.find_one({
                "_id": order["_id"],
                "event_key": self._capacity_event_key(),
            })
            return current, []
        if unavailable:
            order, removed = await self.remove_capacity_services_from_order(
                current, unavailable, reason="proof_too_late"
            )
            return order, removed
        return current, []

    async def release_order_capacity(self, order):
        reservation_token = payment_reservation_token(order)
        for service in choice_capacity_services(order.get("choice", {})):
            await self.release_service_seat(
                service, order["_id"], reservation_token
            )

    async def _send_capacity_notice(self, order):
        claimed_at = datetime.datetime.now()
        claimed_order = await self.food_db.find_one_and_update(
            {
                "_id": order["_id"],
                "event_key": self._capacity_event_key(),
                "capacity_notice.sent": {"$ne": True},
                "$or": [
                    {"capacity_notice.sending_at": {"$exists": False}},
                    {"capacity_notice.sending_at": {
                        "$lte": claimed_at - NOTIFICATION_CLAIM_TTL
                    }},
                ],
            },
            {"$set": {"capacity_notice.sending_at": claimed_at}},
        )
        if claimed_order is None:
            return
        notice = claimed_order.get("capacity_notice", {})
        try:
            update = await self.create_update_from_user(claimed_order["user_id"])
            service_names = ", ".join(
                update.l(CAPACITY_SERVICE_LABEL_KEYS[service])
                for service in notice.get("services", [])
            )
            message_key = (
                "orders-capacity-proof-too-late"
                if notice.get("reason") == "proof_too_late"
                else "orders-capacity-unpaid-removed"
            )
            await update.update.reply(
                update.l(
                    message_key,
                    services=service_names,
                    total=notice.get("new_total", 0),
                ),
                parse_mode=ParseMode.HTML,
            )
        except Exception:
            await self.food_db.update_one(
                {
                    "_id": order["_id"],
                    "event_key": self._capacity_event_key(),
                    "capacity_notice.sending_at": claimed_at,
                },
                {"$unset": {"capacity_notice.sending_at": ""}},
            )
            raise
        await self.food_db.update_one(
            {
                "_id": order["_id"],
                "event_key": self._capacity_event_key(),
                "capacity_notice.sending_at": claimed_at,
            },
            {
                "$set": {"capacity_notice.sent": True},
                "$unset": {"capacity_notice.sending_at": ""},
            },
        )

    async def send_pending_capacity_notices(self):
        async for order in self.food_db.find({
            "event_key": self._capacity_event_key(),
            "capacity_notice.sent": {"$ne": True},
            "capacity_notice.services": {"$exists": True},
        }):
            try:
                await self._send_capacity_notice(order)
            except Exception:
                logger.exception(
                    "failed to send capacity notice for order %s", order.get("_id")
                )

    async def reconcile_capacity(self):
        event_key = self._capacity_event_key()
        orders = await self.food_db.find({"event_key": event_key}).to_list(None)
        paid_orders = [order for order in orders if order_has_payment_proof(order)]
        paid_orders.sort(key=lambda order: (
            payment_attempt_time(order) or datetime.datetime.max,
            str(order["_id"]),
        ))
        for order in paid_orders:
            try:
                await self.activate_paid_order_capacity(order["_id"])
            except Exception:
                logger.exception(
                    "failed to reconcile paid order %s", order.get("_id")
                )

        full_services = {
            service
            for service in CAPACITY_LIMITS
            if not await self.service_available(service)
        }
        if full_services:
            for order in orders:
                if order_has_payment_proof(order):
                    continue
                unavailable = choice_capacity_services(
                    order.get("choice", {})
                ) & full_services
                if unavailable:
                    try:
                        await self.remove_capacity_services_from_order(
                            order, unavailable
                        )
                    except Exception:
                        logger.exception(
                            "failed to remove unavailable services from order %s",
                            order.get("_id"),
                        )

        await self.send_pending_capacity_notices()

    async def send_due_payment_reminders(self, current_time=None):
        current_time = current_time or datetime.datetime.now()
        reminder_after = self.config.orders.payment_reminder_after
        cutoff = current_time - reminder_after
        candidate_filter = unpaid_order_filter(
            event_key=self._capacity_event_key(),
            created_at={"$lte": cutoff},
            **{
                "choice.total": {"$gt": 0},
                "payment_reminder_sent_at": {"$exists": False},
            },
        )
        async for order in self.food_db.find(candidate_filter):
            claimed_at = datetime.datetime.now()
            claimed_order = await self.food_db.find_one_and_update(
                unpaid_order_filter(
                    _id=order["_id"],
                    event_key=self._capacity_event_key(),
                    created_at={"$lte": cutoff},
                    **{"choice.total": {"$gt": 0}},
                    payment_reminder_sent_at={"$exists": False},
                ),
                {"$set": {"payment_reminder_sent_at": claimed_at}},
            )
            if claimed_order is None:
                continue
            try:
                update = await self.create_update_from_user(claimed_order["user_id"])
                await update.update.reply(
                    update.l(
                        "orders-payment-reminder",
                        name=escape(str(
                            claimed_order.get("choice", {}).get("customer", "")
                        )),
                        total=currency_ceil(
                            claimed_order.get("choice", {}).get("total", 0)
                        ),
                    ),
                    parse_mode=ParseMode.HTML,
                )
            except Exception:
                logger.exception(
                    "failed to send payment reminder for order %s", order.get("_id")
                )

    async def _ensure_order_indexes(self):
        await self.food_db.create_index([("event_key", 1)])
        await self.food_db.create_index([
            ("event_key", 1),
            ("created_at", 1),
            ("payment_reminder_sent_at", 1),
        ])

    async def _notification_sender(self):
        await self.base_app.bot_started.wait()
        try:
            await self._ensure_order_indexes()
        except Exception:
            logger.exception("failed to ensure orders indexes")
        while True:
            try:
                await self.reconcile_capacity()
            except Exception:
                logger.exception("orders capacity reconciliation failed")
            try:
                await self.send_due_payment_reminders()
            except Exception:
                logger.exception("orders payment reminder scan failed")
            await asyncio.sleep(
                self.config.orders.notification_check_interval.total_seconds()
            )

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
        return await self.food_db.find_one({
            "_id": ObjectId(order_id),
            "event_key": self.config.orders.event_key,
        })

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
