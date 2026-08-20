import asyncio
import copy
import importlib
import unittest
from types import SimpleNamespace

from bson import ObjectId


orders_module = importlib.import_module("zns-chatbot.plugins.orders")


class _Result:
    def __init__(self, matched_count=0):
        self.matched_count = matched_count


class _Cursor:
    def __init__(self, documents):
        self.documents = documents

    async def to_list(self, _length):
        return copy.deepcopy(self.documents)


def _matches(document, query):
    for key, expected in query.items():
        value = document
        exists = True
        for part in key.split("."):
            if not isinstance(value, dict) or part not in value:
                exists = False
                break
            value = value[part]
        if isinstance(expected, dict) and "$exists" in expected:
            if exists != expected["$exists"]:
                return False
        elif not exists or value != expected:
            return False
    return True


def _project(document, projection):
    if projection is None:
        return copy.deepcopy(document)
    result = {}
    if projection.get("_id", 1) and "_id" in document:
        result["_id"] = document["_id"]
    for key, include in projection.items():
        if include and key in document:
            result[key] = document[key]
    return copy.deepcopy(result)


class _Collection:
    def __init__(self, documents=None):
        self.documents = [copy.deepcopy(document) for document in documents or []]
        self.lock = asyncio.Lock()

    async def create_index(self, *_args, **_kwargs):
        return "test-index"

    async def insert_one(self, document):
        async with self.lock:
            self.documents.append(copy.deepcopy(document))
        return _Result(1)

    def find(self, query, projection=None):
        return _Cursor([
            _project(document, projection)
            for document in self.documents
            if _matches(document, query)
        ])

    async def find_one(self, query, projection=None):
        async with self.lock:
            for document in self.documents:
                if _matches(document, query):
                    return _project(document, projection)
        return None

    async def update_one(self, query, update, upsert=False):
        async with self.lock:
            for document in self.documents:
                if _matches(document, query):
                    self._apply_update(document, update)
                    return _Result(1)
            if upsert:
                document = {
                    key: value for key, value in query.items()
                    if not isinstance(value, dict)
                }
                self._apply_update(document, update)
                self.documents.append(document)
                return _Result(0)
        return _Result(0)

    async def find_one_and_update(self, query, update, sort=None):
        async with self.lock:
            matches = [document for document in self.documents if _matches(document, query)]
            if not matches:
                return None
            if sort:
                key, direction = sort[0]
                matches.sort(key=lambda document: document[key], reverse=direction < 0)
            document = matches[0]
            original = copy.deepcopy(document)
            self._apply_update(document, update)
            return original

    @staticmethod
    def _apply_update(document, update):
        document.update(update.get("$setOnInsert", {}))
        document.update(update.get("$set", {}))
        for key in update.get("$unset", {}):
            document.pop(key, None)


def _orders_service(event_number=2026):
    service = orders_module.Orders.__new__(orders_module.Orders)
    service.config = SimpleNamespace(event_number=event_number)
    service.food_db = _Collection()
    service.capacity_db = _Collection()
    service._capacity_slots_ready = set()
    service._capacity_slots_lock = asyncio.Lock()
    return service


class ShuttleCapacityTests(unittest.IsolatedAsyncioTestCase):
    async def test_only_43_concurrent_reservations_succeed(self):
        service = _orders_service()
        order_ids = [ObjectId() for _ in range(44)]

        results = await asyncio.gather(*(
            service.reserve_shuttle_seat(order_id) for order_id in order_ids
        ))

        self.assertEqual(sum(results), orders_module.SHUTTLE_CAPACITY)
        self.assertFalse(await service.shuttle_available())

    async def test_releasing_a_seat_allows_another_order(self):
        service = _orders_service()
        order_ids = [ObjectId() for _ in range(orders_module.SHUTTLE_CAPACITY)]
        for order_id in order_ids:
            self.assertTrue(await service.reserve_shuttle_seat(order_id))

        self.assertFalse(await service.reserve_shuttle_seat(ObjectId()))
        await service.release_shuttle_seat(order_ids[0])
        self.assertTrue(await service.reserve_shuttle_seat(ObjectId()))

    async def test_existing_transfer_order_can_still_open_when_full(self):
        service = _orders_service()
        for _ in range(orders_module.SHUTTLE_CAPACITY):
            self.assertTrue(await service.reserve_shuttle_seat(ObjectId()))

        choice = {"extras": {"shuttle": {"count": 1}}}
        self.assertTrue(await service.shuttle_available(choice))
        self.assertTrue(orders_module.choice_has_shuttle(choice))

    async def test_full_transfer_rejects_order_without_saving_it(self):
        service = _orders_service()
        for _ in range(orders_module.SHUTTLE_CAPACITY):
            self.assertTrue(await service.reserve_shuttle_seat(ObjectId()))
        update = orders_module.OrdersUpdate.__new__(orders_module.OrdersUpdate)
        update.base = service
        update.user = 123
        update.config = SimpleNamespace(event_number=2026)
        choice = {"extras": {"shuttle": 60}}

        with self.assertRaises(orders_module.ShuttleFullError):
            await update.create_order(choice)

        self.assertEqual(service.food_db.documents, [])


class GrodnoExcursionCapacityTests(unittest.IsolatedAsyncioTestCase):
    async def test_overview_tour_is_limited_to_20_places(self):
        service = _orders_service()
        order_ids = [ObjectId() for _ in range(21)]

        results = await asyncio.gather(*(
            service.reserve_service_seat(
                orders_module.GRODNO_OVERVIEW_SERVICE,
                order_id,
            )
            for order_id in order_ids
        ))

        self.assertEqual(sum(results), 20)
        self.assertFalse(await service.service_available(
            orders_module.GRODNO_OVERVIEW_SERVICE
        ))

    async def test_gorodnitsa_tour_is_limited_to_25_places(self):
        service = _orders_service()
        order_ids = [ObjectId() for _ in range(26)]

        results = await asyncio.gather(*(
            service.reserve_service_seat(
                orders_module.GRODNO_GORODNITSA_SERVICE,
                order_id,
            )
            for order_id in order_ids
        ))

        self.assertEqual(sum(results), 25)

    async def test_full_overview_tour_rejects_order_without_saving_it(self):
        service = _orders_service()
        for _ in range(20):
            self.assertTrue(await service.reserve_service_seat(
                orders_module.GRODNO_OVERVIEW_SERVICE,
                ObjectId(),
            ))
        update = orders_module.OrdersUpdate.__new__(orders_module.OrdersUpdate)
        update.base = service
        update.user = 123
        update.config = SimpleNamespace(event_number=2026)
        choice = {"extras": {orders_module.GRODNO_OVERVIEW_SERVICE: 25}}

        with self.assertRaises(orders_module.CapacityFullError) as context:
            await update.create_order(choice)

        self.assertEqual(context.exception.service, orders_module.GRODNO_OVERVIEW_SERVICE)
        self.assertEqual(service.food_db.documents, [])

    def test_two_grodno_variants_are_rejected(self):
        choice = {"extras": {
            orders_module.GRODNO_OVERVIEW_SERVICE: 25,
            orders_module.GRODNO_GORODNITSA_SERVICE: 25,
        }}

        with self.assertRaises(orders_module.InvalidExcursionChoiceError):
            orders_module.validate_excursion_choice(choice)


if __name__ == "__main__":
    unittest.main()
