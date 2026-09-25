import copy
import datetime
import importlib
import json
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock, patch

from bson import ObjectId
from tornado.template import Loader

from test_orders_shuttle_capacity import _orders_service, orders_module


class OrdersDeadlineTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.clock = self.enterContext(patch.object(
            orders_module, "now_msk",
            return_value=datetime.datetime(2026, 9, 30, 23, 59, 59),
        ))
        self.service = _orders_service()
        self.update = orders_module.OrdersUpdate.__new__(orders_module.OrdersUpdate)
        self.update.base = self.service
        self.update.user = 123
        self.update.config = SimpleNamespace(event_key="grodno_26")
        self.update.update = SimpleNamespace(reply=AsyncMock(), edit_or_reply=AsyncMock())
        self.update.l = lambda key, **kwargs: key
        self.update.handle_cq_start = AsyncMock()
        self.extras = {"customer": "Alice Smith", "extras": {"preparty": 35}}
        day, meals = next(iter(self.service.menu["choices"].items()))
        mealtime, categories = next(iter(meals.items()))
        dish = next(iter(next(iter(categories.values()))))
        self.food = {
            **self.extras,
            "days": {day: {"mealtimes": {
                mealtime: {"dishes": [{"name": dish, "count": 1}]},
            }}},
        }

    async def store_order(self, choice):
        order = {
            "_id": ObjectId(), "user_id": 123, "event_key": "grodno_26",
            "created_at": datetime.datetime(2026, 9, 20),
            "choice": orders_module.canonicalize_choice(choice, self.service.menu),
        }
        await self.service.food_db.insert_one(order)
        return order

    def test_both_deadlines_are_exclusive(self):
        for choice, deadline in (
            (self.food, datetime.datetime(2026, 9, 25)),
            (self.extras, datetime.datetime(2026, 10, 1)),
        ):
            with self.subTest(deadline=deadline):
                self.clock.return_value = deadline - datetime.timedelta(microseconds=1)
                self.assertTrue(orders_module.orders_open(choice))
                self.clock.return_value = deadline
                self.assertFalse(orders_module.orders_open(choice))

    async def test_extras_can_be_created_and_edited_on_september_30(self):
        await self.update.create_order(self.extras)
        order = self.service.food_db.documents[0]
        await self.update.set_choice(str(order["_id"]), {
            **self.extras, "extras": {"excursion_minsk": 1},
        })
        self.assertEqual(self.service.food_db.documents[0]["choice"]["total"], 30)

    async def test_empty_meal_sections_do_not_block_extras(self):
        choice = copy.deepcopy(self.food)
        for day in choice["days"].values():
            for meal in day["mealtimes"].values():
                meal["dishes"] = []
        await self.update.create_order(choice)
        self.assertEqual(len(self.service.food_db.documents), 1)

    async def test_food_cannot_be_created_or_added_to_existing_extras(self):
        await self.update.create_order(self.food)
        self.assertEqual(self.service.food_db.documents, [])
        order = await self.store_order(self.extras)
        await self.update.set_choice(str(order["_id"]), self.food)
        self.assertEqual(self.service.food_db.documents, [order])

    async def test_old_food_order_cannot_be_changed_deleted_or_paid(self):
        order = await self.store_order(self.food)
        order_id = str(order["_id"])
        await self.update.set_choice(order_id, self.extras)
        await self.update.handle_cq_del(order_id)
        await self.update.handle_cq_pay(order_id)
        await self.update.handle_cq_cash(order_id, 456)
        self.assertEqual(self.service.food_db.documents, [order])

    async def test_extras_close_at_start_of_october(self):
        self.clock.return_value = datetime.datetime(2026, 10, 1)
        await self.update.create_order(self.extras)
        self.assertEqual(self.service.food_db.documents, [])
        order = await self.store_order(self.extras)
        await self.update.set_choice(str(order["_id"]), {"extras": {}})
        await self.update.handle_cq_del(str(order["_id"]))
        await self.update.handle_cq_cash(str(order["_id"]), 456)
        self.assertEqual(self.service.food_db.documents, [order])

    async def test_order_list_offers_new_extras_despite_unpaid_food(self):
        await self.store_order(self.food)
        self.service.name = "orders"
        self.service.base_app = SimpleNamespace(
            users_collection=SimpleNamespace(find=Mock(return_value=SimpleNamespace(
                to_list=AsyncMock(return_value=[]),
            ))),
        )
        self.update.bot = 1
        self.update.config.admins = []
        self.update.config.payment_admin_ru = 0
        self.update.update.language_code = "ru"
        self.update.update.get_user = AsyncMock(return_value={})
        with patch.object(orders_module, "full_link", side_effect=lambda app, path: "https://example.org" + path):
            await orders_module.OrdersUpdate.handle_cq_start(self.update)
        markup = self.update.update.edit_or_reply.call_args.kwargs["reply_markup"]
        labels = [button.text for row in markup.inline_keyboard for button in row]
        self.assertIn("orders-new-button", labels)
        self.assertNotIn("orders-order-pay-button", labels)

    async def test_payment_proof_skips_closed_food_order(self):
        food_order = await self.store_order(self.food)
        extras_order = await self.store_order(self.extras)
        self.service.name = "orders"
        self.service.activate_paid_order_capacity = AsyncMock()
        self.service.reconcile_capacity = AsyncMock()
        self.update.update.message = SimpleNamespace(document=SimpleNamespace(file_id="proof.jpg"))
        self.update.update.chat_id = 123
        self.update.update.message_id = 42
        await self.update.handle_payment()
        self.assertEqual(self.service.food_db.documents[0], food_order)
        self.assertEqual(self.service.food_db.documents[1]["proof_file"], "proof.jpg")
        self.service.activate_paid_order_capacity.assert_awaited_once_with(extras_order["_id"])

    async def test_http_rejects_food_and_accepts_extras_until_deadline(self):
        server = importlib.import_module("zns-chatbot.server")
        self.service.create_order = AsyncMock()
        for choice, current_time, allowed in (
            (self.extras, datetime.datetime(2026, 9, 30, 23, 59, 59), True),
            (self.food, datetime.datetime(2026, 9, 30), False),
            (self.extras, datetime.datetime(2026, 10, 1), False),
        ):
            with self.subTest(choice=choice, time=current_time):
                self.clock.return_value = current_time
                self.service.create_order.reset_mock()
                handler = SimpleNamespace(
                    app=SimpleNamespace(orders=self.service),
                    config=SimpleNamespace(telegram=SimpleNamespace(token=Mock())),
                    get_argument=Mock(return_value="signed-data"),
                    get_query_argument=Mock(return_value=""),
                    request=SimpleNamespace(body=json.dumps(choice)),
                    set_status=Mock(), write=Mock(),
                )
                with patch.object(server, "validate", return_value={"user": '{"id":123}'}):
                    await server.OrdersHandler.post(handler)
                if allowed:
                    self.service.create_order.assert_awaited_once()
                else:
                    handler.set_status.assert_called_once_with(403)
                    self.service.create_order.assert_not_awaited()


class OrdersTemplateTests(unittest.TestCase):
    def test_food_steps_are_omitted_only_for_extras_form(self):
        root = Path(__file__).resolve().parents[1]
        template = Loader(str(root / "templates")).load("orders.html")
        values = dict.fromkeys((
            "user_order", "user_order_id", "finish_button_text", "next_button_text",
            "validity_error_first_name", "validity_error_last_name",
            "shuttle_full_error", "grodno_excursion_availability",
            "grodno_excursion_full_errors", "placeholder_first_name",
            "placeholder_last_name", "placeholder_patronymus",
        ), "")
        for show_food, count in ((False, 0), (True, 5)):
            html = template.generate(
                **values, show_food=show_food, read_only=False,
                shuttle_available=True, debug_id="", lang="ru",
            ).decode()
            self.assertEqual(html.count(' meal">'), count)
            self.assertIn('class="excursions"', html)
