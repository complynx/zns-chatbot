from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timedelta
import importlib
from types import MethodType, SimpleNamespace
import unittest


IMPORT_ERROR: Exception | None = None
passes_module = None
events_module = None
try:
    passes_module = importlib.import_module("zns-chatbot.plugins.passes")
    events_module = importlib.import_module("zns-chatbot.events")
except Exception as exc:  # pragma: no cover - environment-specific
    IMPORT_ERROR = exc


def _skip_reason() -> str:
    if IMPORT_ERROR is None:
        return ""
    return (
        "Cannot import zns-chatbot runtime dependencies required for passes tests: "
        f"{IMPORT_ERROR!r}"
    )


if passes_module is not None and events_module is not None:
    Passes = passes_module.Passes
    EventInfo = events_module.EventInfo
    EventPassType = events_module.EventPassType
    MAX_CONCURRENT_ASSIGNMENTS = passes_module.MAX_CONCURRENT_ASSIGNMENTS
else:  # pragma: no cover - guard for missing runtime deps
    Passes = object
    EventInfo = object
    EventPassType = object
    MAX_CONCURRENT_ASSIGNMENTS = 0


BASE_TS = datetime(2026, 1, 1, 12, 0, 0)


def make_event(
    *,
    key: str = "pass_2026_1",
    pass_types: tuple | None = None,
    assignment_rule: str = "paired",
    disable_max_concurrent_assignments: bool = False,
):
    if pass_types is None:
        pass_types = (
            EventPassType(
                amount=100,
                price=10000,
                start=BASE_TS - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
        )
    return EventInfo(
        key=key,
        amount_cap_per_role=80,
        payment_admin=[],
        hidden_payment_admins=[],
        finish_date=None,
        title_long={"default": key},
        title_short={"default": key},
        country_emoji="",
        thread_channel="",
        thread_id=None,
        thread_locale="en",
        require_passport=False,
        price=None,
        pass_types=pass_types,
        pass_assignment_rule=assignment_rule,
        disable_max_concurrent_assignments=disable_max_concurrent_assignments,
    )


def wl_doc(
    user_id: int,
    role: str,
    *,
    sec: int,
    couple: int | None = None,
    pass_key: str = "pass_2026_1",
    bot_id: int = 777,
) -> dict[str, object]:
    data: dict[str, object] = {
        "bot_id": bot_id,
        "pass_key": pass_key,
        "state": "waitlist",
        "user_id": user_id,
        "role": role,
        "date_created": BASE_TS + timedelta(seconds=sec),
    }
    if couple is not None:
        data["couple"] = couple
    return data


class FakeCursor:
    def __init__(self, supplier):
        self._supplier = supplier
        self._sort_spec: list[tuple[str, int]] = []
        self._iter_docs: list[dict[str, object]] = []

    def sort(self, spec):
        self._sort_spec = list(spec)
        return self

    def _sorted_docs(self) -> list[dict[str, object]]:
        docs = [dict(doc) for doc in self._supplier()]
        for field, direction in reversed(self._sort_spec):
            docs.sort(key=lambda doc: doc.get(field), reverse=direction < 0)
        return docs

    async def to_list(self, limit):
        docs = self._sorted_docs()
        if limit is None:
            return docs
        return docs[:limit]

    def __aiter__(self):
        self._iter_docs = self._sorted_docs()
        return self

    async def __anext__(self):
        if not self._iter_docs:
            raise StopAsyncIteration
        return self._iter_docs.pop(0)


class FakePassDB:
    def __init__(self, rig: "QueueScenarioRig"):
        self.rig = rig

    def find(self, query):
        return FakeCursor(lambda: self.rig.query(query))

    async def find_one(self, query):
        for doc in self.rig.waitlist_docs:
            if self.rig.matches(doc, query):
                return dict(doc)
        return None


class FakeQueueUpdate:
    def __init__(self, rig: "QueueScenarioRig", user_id: int):
        self.rig = rig
        self.user_id = user_id
        self.pass_key = ""

    def set_pass_key(self, pass_key: str) -> None:
        self.pass_key = pass_key

    async def notify_no_more_passes(self, pass_key: str):
        self.rig.notifications.append(self.user_id)
        live = self.rig.find_waitlist_doc(self.user_id)
        if live is not None:
            live["no_more_passes_notification_sent"] = BASE_TS

    async def assign_pass(
        self,
        pass_key: str,
        price: int | None = None,
        type: str | None = None,
        comment: str | None = None,
        skip_in_balance_count: bool = False,
        proof_admin: int | None = None,
        price_by_user: dict[int, int] | None = None,
        pass_type_index_by_user: dict[int, int] | None = None,
        ignore_date_blocks: bool = False,
    ):
        _ = (price, type, comment, skip_in_balance_count, proof_admin, ignore_date_blocks)
        return await self.rig.assign_from_update(
            self.user_id,
            pass_type_index_by_user=pass_type_index_by_user or {},
        )


@dataclass
class QueueScenarioRig:
    event: object
    leader_ra: int
    follower_ra: int
    waitlist_docs: list[dict[str, object]]
    assigned_leader: int = 0
    assigned_follower: int = 0
    failed_assignments: set[int] = field(default_factory=set)
    assignments: list[tuple[str, tuple[int, ...]]] = field(default_factory=list)
    assignment_attempts: list[int] = field(default_factory=list)
    assignment_calls: list[tuple[int, bool]] = field(default_factory=list)
    notifications: list[int] = field(default_factory=list)
    participants_total: int = 0
    participants_by_role: dict[str, int] = field(
        default_factory=lambda: {"leader": 0, "follower": 0}
    )
    tier_usage_total: dict[int, int] = field(default_factory=dict)
    tier_usage_by_role: dict[str, dict[int, int]] = field(
        default_factory=lambda: {"leader": {}, "follower": {}}
    )
    bot_id: int = 777
    pass_key: str = "pass_2026_1"

    def matches(self, doc: dict[str, object], query: dict[str, object]) -> bool:
        for key, expected in query.items():
            if isinstance(expected, dict):
                if "$exists" in expected:
                    exists_expected = bool(expected["$exists"])
                    exists_actual = key in doc
                    if exists_expected != exists_actual:
                        return False
                    continue
                return False
            if doc.get(key) != expected:
                return False
        return True

    def query(self, query: dict[str, object]) -> list[dict[str, object]]:
        if "sent_to_hype_thread" in query:
            return []
        if query.get("state") != "waitlist":
            return []
        result = [doc for doc in self.waitlist_docs if doc.get("state") == "waitlist"]
        marker = query.get("no_more_passes_notification_sent")
        if isinstance(marker, dict) and marker.get("$exists") is False:
            result = [doc for doc in result if "no_more_passes_notification_sent" not in doc]
        return result

    def find_waitlist_doc(
        self,
        user_id: int,
        *,
        couple: int | None = None,
    ) -> dict[str, object] | None:
        for doc in self.waitlist_docs:
            if doc.get("user_id") != user_id:
                continue
            if couple is None:
                return doc
            if doc.get("couple") == couple:
                return doc
        return None

    def remove_waitlist_docs(self, user_ids: set[int]) -> None:
        self.waitlist_docs = [
            doc for doc in self.waitlist_docs if int(doc.get("user_id", -1)) not in user_ids
        ]

    def collect_stats(self) -> dict[str, object]:
        leader_waitlist = sum(1 for doc in self.waitlist_docs if doc.get("role") == "leader")
        follower_waitlist = sum(
            1 for doc in self.waitlist_docs if doc.get("role") == "follower"
        )
        role_counts = {
            "leader": {
                "RA": self.leader_ra,
                "assigned": self.assigned_leader,
                "waitlist": leader_waitlist,
            },
            "follower": {
                "RA": self.follower_ra,
                "assigned": self.assigned_follower,
                "waitlist": follower_waitlist,
            },
        }
        full_role_counts = {
            "leader": {"assigned": self.assigned_leader},
            "follower": {"assigned": self.assigned_follower},
        }
        return {
            "role_counts": role_counts,
            "full_role_counts": full_role_counts,
            "participants_total": self.participants_total,
            "participants_by_role": {
                "leader": int(self.participants_by_role.get("leader", 0)),
                "follower": int(self.participants_by_role.get("follower", 0)),
            },
            "tier_usage_total": dict(self.tier_usage_total),
            "tier_usage_by_role": {
                "leader": dict(self.tier_usage_by_role.get("leader", {})),
                "follower": dict(self.tier_usage_by_role.get("follower", {})),
            },
        }

    async def assign_candidate(
        self,
        candidate_doc: dict[str, object],
    ) -> bool:
        uid = int(candidate_doc["user_id"])
        self.assignment_attempts.append(uid)
        if uid in self.failed_assignments:
            return False

        main_doc = self.find_waitlist_doc(uid)
        if main_doc is None:
            return False

        docs = [main_doc]
        if "couple" in main_doc:
            couple_user = int(main_doc["couple"])
            couple_doc = self.find_waitlist_doc(couple_user, couple=uid)
            if couple_doc is None:
                return False
            docs.append(couple_doc)

        leader_inc = sum(1 for doc in docs if doc.get("role") == "leader")
        follower_inc = sum(1 for doc in docs if doc.get("role") == "follower")
        self.leader_ra += leader_inc
        self.follower_ra += follower_inc
        self.assigned_leader += leader_inc
        self.assigned_follower += follower_inc

        assigned_users = tuple(sorted(int(doc["user_id"]) for doc in docs))
        assignment_kind = "couple" if len(docs) == 2 else "solo"
        self.assignments.append((assignment_kind, assigned_users))
        self.remove_waitlist_docs(set(assigned_users))
        return True

    async def assign_from_update(
        self,
        candidate_user_id: int,
        *,
        pass_type_index_by_user: dict[int, int],
    ) -> bool:
        if candidate_user_id in self.failed_assignments:
            self.assignment_attempts.append(candidate_user_id)
            return False
        main_doc = self.find_waitlist_doc(candidate_user_id)
        if main_doc is None:
            self.assignment_attempts.append(candidate_user_id)
            return False
        return await self.assign_candidate_with_tiers(
            main_doc,
            pass_type_index_by_user=pass_type_index_by_user,
        )

    async def assign_candidate_with_tiers(
        self,
        candidate_doc: dict[str, object],
        *,
        pass_type_index_by_user: dict[int, int],
    ) -> bool:
        uid = int(candidate_doc["user_id"])
        self.assignment_attempts.append(uid)
        if uid in self.failed_assignments:
            return False

        main_doc = self.find_waitlist_doc(uid)
        if main_doc is None:
            return False

        docs = [main_doc]
        if "couple" in main_doc:
            couple_user = int(main_doc["couple"])
            couple_doc = self.find_waitlist_doc(couple_user, couple=uid)
            if couple_doc is None:
                return False
            docs.append(couple_doc)

        leader_inc = sum(1 for doc in docs if doc.get("role") == "leader")
        follower_inc = sum(1 for doc in docs if doc.get("role") == "follower")
        self.leader_ra += leader_inc
        self.follower_ra += follower_inc
        self.assigned_leader += leader_inc
        self.assigned_follower += follower_inc
        self.participants_total += len(docs)
        self.participants_by_role["leader"] = (
            self.participants_by_role.get("leader", 0) + leader_inc
        )
        self.participants_by_role["follower"] = (
            self.participants_by_role.get("follower", 0) + follower_inc
        )

        for doc in docs:
            user_id = int(doc["user_id"])
            tier_index = pass_type_index_by_user.get(user_id)
            if isinstance(tier_index, int):
                self.tier_usage_total[tier_index] = (
                    self.tier_usage_total.get(tier_index, 0) + 1
                )
                role = doc.get("role")
                if role in {"leader", "follower"}:
                    role_tiers = self.tier_usage_by_role.setdefault(role, {})
                    role_tiers[tier_index] = role_tiers.get(tier_index, 0) + 1

        assigned_users = tuple(sorted(int(doc["user_id"]) for doc in docs))
        assignment_kind = "couple" if len(docs) == 2 else "solo"
        self.assignments.append((assignment_kind, assigned_users))
        self.remove_waitlist_docs(set(assigned_users))
        return True

    def build_passes(self):
        passes = Passes.__new__(Passes)

        async def fake_send_message(**kwargs):
            return None

        fake_bot = SimpleNamespace(id=self.bot_id, send_message=fake_send_message)
        passes.base_app = SimpleNamespace(
            bot=SimpleNamespace(bot=fake_bot),
            localization=lambda key, args=None, locale=None: key,
        )
        passes.pass_db = FakePassDB(self)
        passes.user_db = SimpleNamespace(find_one=self._user_find_one)

        async def update_pass_fields(
            _self,
            user_ids: list[int],
            pass_key: str,
            *,
            set_fields: dict | None = None,
            unset_fields: list[str] | None = None,
        ):
            return None

        async def collect_queue_stats(_self, pass_key: str):
            return self.collect_stats()

        def require_event(_self, pass_key: str):
            return self.event

        async def create_update_from_user(_self, user: int):
            return FakeQueueUpdate(self, user)

        async def assign_wl_candidate(
            _self,
            pass_key: str,
            event,
            stats: dict[str, object],
            candidate_doc: dict,
            *,
            allow_promo: bool,
        ) -> bool:
            self.assignment_calls.append((int(candidate_doc["user_id"]), allow_promo))
            return await self.assign_candidate(candidate_doc)

        passes.update_pass_fields = MethodType(update_pass_fields, passes)
        passes._collect_queue_stats = MethodType(collect_queue_stats, passes)
        passes.require_event = MethodType(require_event, passes)
        passes.create_update_from_user = MethodType(create_update_from_user, passes)
        passes._assign_wl_candidate = MethodType(assign_wl_candidate, passes)
        return passes

    async def _user_find_one(self, query):
        return {}


@unittest.skipIf(passes_module is None or events_module is None, _skip_reason())
class QueueAssignmentScenarioTests(unittest.IsolatedAsyncioTestCase):
    def _use_real_assigner(self, passes):
        passes._assign_wl_candidate = MethodType(Passes._assign_wl_candidate, passes)

    async def test_rule1_imbalance_assigns_top_minority_solo(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=10,
            follower_ra=7,
            waitlist_docs=[
                wl_doc(101, "leader", sec=0),
                wl_doc(202, "follower", sec=1),
            ],
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertGreaterEqual(len(rig.assignments), 1)
        self.assertEqual(rig.assignments[0], ("solo", (202,)))

    async def test_rule2a_double_solo_partial_failure_is_kept(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=5,
            follower_ra=5,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
            ],
            failed_assignments={2},
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignment_attempts[:2], [1, 2])
        self.assertEqual(rig.assignments, [("solo", (1,))])
        self.assertIn(2, rig.notifications)

    async def test_rule2a_double_solo_success_assigns_both(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=5,
            follower_ra=5,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
            ],
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments, [("solo", (1,)), ("solo", (2,))])

    async def test_double_solo_not_used_when_queue_is_imbalanced(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=9,
            follower_ra=6,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
            ],
        )
        passes = rig.build_passes()
        original_try_double = passes._try_double_solo_assignment
        double_calls = {"count": 0}

        async def wrapped_try_double(
            _self,
            pass_key: str,
            event,
            stats: dict[str, object],
            leader_doc: dict,
            follower_doc: dict,
        ) -> bool:
            double_calls["count"] += 1
            return await original_try_double(
                pass_key, event, stats, leader_doc, follower_doc
            )

        passes._try_double_solo_assignment = MethodType(wrapped_try_double, passes)
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(double_calls["count"], 0)
        self.assertEqual(rig.assignments[0], ("solo", (2,)))

    async def test_rule2b_top_couple_is_selected_when_present(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=6,
            follower_ra=6,
            waitlist_docs=[
                wl_doc(10, "leader", sec=0, couple=11),
                wl_doc(20, "follower", sec=0),
                wl_doc(11, "follower", sec=1, couple=10),
            ],
            failed_assignments={20},
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertGreaterEqual(len(rig.assignments), 1)
        self.assertEqual(rig.assignments[0], ("couple", (10, 11)))

    async def test_rule1_skips_minority_top_couple_then_rule2b_assigns_couple(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=9,
            follower_ra=7,
            waitlist_docs=[
                wl_doc(10, "leader", sec=0),
                wl_doc(20, "follower", sec=0, couple=30),
                wl_doc(30, "leader", sec=1, couple=20),
            ],
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments[0], ("couple", (20, 30)))

    async def test_shadowed_couple_eventually_reaches_top(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=4,
            follower_ra=4,
            waitlist_docs=[
                wl_doc(100, "leader", sec=0),
                wl_doc(300, "leader", sec=1, couple=400),
                wl_doc(200, "follower", sec=0),
                wl_doc(400, "follower", sec=1, couple=300),
            ],
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments[0], ("solo", (100,)))
        self.assertEqual(rig.assignments[1], ("solo", (200,)))
        self.assertEqual(rig.assignments[2], ("couple", (300, 400)))

    async def test_concurrency_limit_stops_assignment_and_notifies_waitlist(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=8,
            follower_ra=8,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
            ],
            assigned_leader=MAX_CONCURRENT_ASSIGNMENTS // 2,
            assigned_follower=MAX_CONCURRENT_ASSIGNMENTS // 2,
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments, [])
        self.assertEqual(set(rig.notifications), {1, 2})

    async def test_concurrency_limit_can_be_disabled_per_event(self):
        rig = QueueScenarioRig(
            event=make_event(disable_max_concurrent_assignments=True),
            leader_ra=8,
            follower_ra=8,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
            ],
            assigned_leader=MAX_CONCURRENT_ASSIGNMENTS // 2,
            assigned_follower=MAX_CONCURRENT_ASSIGNMENTS // 2,
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments, [("solo", (1,)), ("solo", (2,))])
        self.assertEqual(rig.notifications, [])

    async def test_fallback_order_target_then_other_role(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=49,
            follower_ra=49,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
            ],
            assigned_leader=3,
            assigned_follower=2,
            failed_assignments={1},
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        # Rule 2a is skipped due concurrency (+2 would exceed MAX), then
        # fallback tries target role first and then the other role.
        self.assertEqual(rig.assignment_attempts[:2], [1, 2])
        self.assertEqual(rig.assignments[0], ("solo", (2,)))

    async def test_no_deep_couple_search_when_tops_are_solo(self):
        rig = QueueScenarioRig(
            event=make_event(),
            leader_ra=4,
            follower_ra=4,
            waitlist_docs=[
                wl_doc(1, "leader", sec=0),
                wl_doc(2, "follower", sec=0),
                wl_doc(30, "leader", sec=1, couple=40),
                wl_doc(40, "follower", sec=1, couple=30),
            ],
            failed_assignments={1, 2},
        )
        passes = rig.build_passes()
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertFalse(any(kind == "couple" for kind, _ in rig.assignments))
        self.assertEqual(set(rig.notifications), {1, 2, 30, 40})

    async def test_waitlist_notified_when_no_tickets_left(self):
        event = make_event(
            assignment_rule="paired",
            pass_types=(
                EventPassType(
                    amount=2,
                    price=100,
                    start=BASE_TS - timedelta(days=1),
                    promo=False,
                    blocked_by_date=False,
                ),
            ),
        )
        rig = QueueScenarioRig(
            event=event,
            leader_ra=6,
            follower_ra=6,
            waitlist_docs=[wl_doc(1, "leader", sec=0)],
            participants_total=1,
            participants_by_role={"leader": 1, "follower": 0},
            tier_usage_total={0: 1},
            tier_usage_by_role={"leader": {0: 1}, "follower": {}},
        )
        passes = rig.build_passes()
        self._use_real_assigner(passes)
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments, [])
        self.assertEqual(rig.notifications, [1])

    async def test_waitlist_notified_when_date_block_prevents_assignment(self):
        event = make_event(
            pass_types=(
                EventPassType(
                    amount=100,
                    price=100,
                    start=BASE_TS + timedelta(days=1),
                    promo=False,
                    blocked_by_date=True,
                ),
            ),
        )
        rig = QueueScenarioRig(
            event=event,
            leader_ra=6,
            follower_ra=6,
            waitlist_docs=[wl_doc(1, "leader", sec=0)],
        )
        passes = rig.build_passes()
        self._use_real_assigner(passes)
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments, [])
        self.assertEqual(rig.notifications, [1])

    async def test_waitlist_notified_when_event_has_no_tiers(self):
        event = make_event(pass_types=())
        rig = QueueScenarioRig(
            event=event,
            leader_ra=6,
            follower_ra=6,
            waitlist_docs=[wl_doc(1, "leader", sec=0)],
        )
        passes = rig.build_passes()
        self._use_real_assigner(passes)
        await passes.recalculate_queues_pk(rig.pass_key)

        self.assertEqual(rig.assignments, [])
        self.assertEqual(rig.notifications, [1])


@unittest.skipIf(passes_module is None or events_module is None, _skip_reason())
class TierAndBalanceTests(unittest.TestCase):
    def test_double_solo_uses_strict_balance_gate(self):
        passes = Passes.__new__(Passes)
        role_counts = {
            "leader": {"RA": 10},
            "follower": {"RA": 7},
        }
        self.assertFalse(
            passes._can_assign_with_balance(
                role_counts,
                leader_delta=1,
                follower_delta=1,
                strict=True,
            )
        )
        self.assertTrue(
            passes._can_assign_with_balance(
                role_counts,
                leader_delta=1,
                follower_delta=1,
            )
        )

    def test_non_worsening_logic_for_single_and_couple_when_imbalanced(self):
        passes = Passes.__new__(Passes)
        role_counts = {
            "leader": {"RA": 10},
            "follower": {"RA": 7},
        }
        self.assertTrue(
            passes._can_assign_with_balance(role_counts, follower_delta=1)
        )
        self.assertFalse(
            passes._can_assign_with_balance(role_counts, leader_delta=1)
        )
        self.assertTrue(
            passes._can_assign_with_balance(
                role_counts,
                leader_delta=1,
                follower_delta=1,
            )
        )

    def test_blocked_promo_tier_halts_couple_scan(self):
        passes = Passes.__new__(Passes)
        now = datetime.now()
        pass_types = (
            EventPassType(
                amount=0,
                price=100,
                start=now - timedelta(days=1),
                promo=True,
                blocked_by_date=False,
            ),
            EventPassType(
                amount=1,
                price=100,
                start=now + timedelta(days=1),
                promo=True,
                blocked_by_date=True,
            ),
            EventPassType(
                amount=5,
                price=200,
                start=now + timedelta(days=2),
                promo=False,
                blocked_by_date=False,
            ),
        )

        blocked_index = passes._pick_tier_for_assignment(
            pass_types=pass_types,
            assignment_rule="distributed",
            tier_usage_total={},
            tier_usage_by_role={"leader": {}, "follower": {}},
            participants_total=0,
            participants_by_role={"leader": 0, "follower": 0},
            role=None,
            increment=2,
            allow_promo=False,
            enforce_date_blocks=True,
        )
        self.assertIsNone(blocked_index)

        unblocked_index = passes._pick_tier_for_assignment(
            pass_types=pass_types,
            assignment_rule="distributed",
            tier_usage_total={},
            tier_usage_by_role={"leader": {}, "follower": {}},
            participants_total=0,
            participants_by_role={"leader": 0, "follower": 0},
            role=None,
            increment=2,
            allow_promo=False,
            enforce_date_blocks=False,
        )
        self.assertEqual(unblocked_index, 2)

    def test_no_more_tickets_means_no_assignable_tier(self):
        passes = Passes.__new__(Passes)
        pass_types = (
            EventPassType(
                amount=1,
                price=100,
                start=datetime.now() - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
        )
        tier_index = passes._pick_tier_for_assignment(
            pass_types=pass_types,
            assignment_rule="distributed",
            tier_usage_total={0: 1},
            tier_usage_by_role={"leader": {0: 1}, "follower": {}},
            participants_total=1,
            participants_by_role={"leader": 1, "follower": 0},
            role=None,
            increment=1,
            allow_promo=True,
        )
        self.assertIsNone(tier_index)

    def test_resolve_candidate_tier_prices_can_ignore_date_blocks(self):
        now = datetime.now()
        pass_types = (
            EventPassType(
                amount=0,
                price=100,
                start=now + timedelta(days=1),
                promo=False,
                blocked_by_date=True,
            ),
            EventPassType(
                amount=5,
                price=200,
                start=now + timedelta(days=2),
                promo=False,
                blocked_by_date=False,
            ),
        )
        event = make_event(pass_types=pass_types, assignment_rule="paired")
        passes = Passes.__new__(Passes)

        def require_event(_self, pass_key: str):
            return event

        async def collect_queue_stats(_self, pass_key: str):
            return {
                "participants_total": 0,
                "participants_by_role": {"leader": 0, "follower": 0},
                "tier_usage_total": {},
                "tier_usage_by_role": {"leader": {}, "follower": {}},
            }

        passes.require_event = MethodType(require_event, passes)
        passes._collect_queue_stats = MethodType(collect_queue_stats, passes)
        passes.pass_db = SimpleNamespace(find_one=self._unused_find_one)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        blocked = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                42,
                {"role": "leader"},
                enforce_date_blocks=True,
            )
        )
        self.assertIsNone(blocked)

        unblocked = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                42,
                {"role": "leader"},
                enforce_date_blocks=False,
            )
        )
        self.assertIsNotNone(unblocked)
        assert unblocked is not None
        _, pass_type_index_by_user = unblocked
        self.assertEqual(pass_type_index_by_user[42], 1)

    def test_resolve_candidate_tier_prices_admin_mode_recovers_role_and_stale_couple(self):
        now = datetime.now()
        pass_types = (
            EventPassType(
                amount=10,
                price=100,
                start=now - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
        )
        event = make_event(pass_types=pass_types, assignment_rule="distributed")
        passes = Passes.__new__(Passes)

        def require_event(_self, pass_key: str):
            return event

        async def collect_queue_stats(_self, pass_key: str):
            return {
                "participants_total": 0,
                "participants_by_role": {"leader": 0, "follower": 0},
                "tier_usage_total": {},
                "tier_usage_by_role": {"leader": {}, "follower": {}},
            }

        async def find_couple_waitlist(query):
            _ = query
            return None

        async def find_user(query):
            _ = query
            return {
                "bot_id": 1,
                "user_id": 42,
                "role": "leader",
            }

        passes.require_event = MethodType(require_event, passes)
        passes._collect_queue_stats = MethodType(collect_queue_stats, passes)
        passes.pass_db = SimpleNamespace(find_one=find_couple_waitlist)
        passes.user_db = SimpleNamespace(find_one=find_user)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        blocked = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                42,
                {"couple": 777},
                enforce_date_blocks=True,
            )
        )
        self.assertIsNone(blocked)

        unblocked = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                42,
                {"couple": 777},
                enforce_date_blocks=False,
            )
        )
        self.assertIsNotNone(unblocked)
        assert unblocked is not None
        price_by_user, pass_type_index_by_user = unblocked
        self.assertEqual(set(price_by_user.keys()), {42})
        self.assertEqual(pass_type_index_by_user[42], 0)

    def test_assign_wl_candidate_stale_couple_falls_back_to_solo(self):
        now = datetime.now()
        event = make_event(
            assignment_rule="distributed",
            pass_types=(
                EventPassType(
                    amount=10,
                    price=100,
                    start=now - timedelta(days=1),
                    promo=True,
                    blocked_by_date=False,
                ),
            ),
        )
        passes = Passes.__new__(Passes)
        captured: dict[str, object] = {}

        async def find_couple_waitlist(query):
            _ = query
            return None

        async def create_update_from_user(_self, user_id: int):
            class FakeUpdate:
                async def assign_pass(
                    self,
                    pass_key: str,
                    *,
                    price_by_user=None,
                    pass_type_index_by_user=None,
                ):
                    captured["pass_key"] = pass_key
                    captured["price_by_user"] = dict(price_by_user or {})
                    captured["pass_type_index_by_user"] = dict(pass_type_index_by_user or {})
                    return True

            captured["user_id"] = user_id
            return FakeUpdate()

        passes.pass_db = SimpleNamespace(find_one=find_couple_waitlist)
        passes.create_update_from_user = MethodType(create_update_from_user, passes)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        result = self._run(
            passes._assign_wl_candidate(
                "pass_2026_1",
                event,
                {
                    "participants_total": 0,
                    "participants_by_role": {"leader": 0, "follower": 0},
                    "tier_usage_total": {},
                    "tier_usage_by_role": {"leader": {}, "follower": {}},
                },
                {
                    "bot_id": 1,
                    "pass_key": "pass_2026_1",
                    "user_id": 100,
                    "state": "waitlist",
                    "role": "leader",
                    "couple": 200,
                },
                allow_promo=False,
            )
        )
        self.assertTrue(result)
        self.assertEqual(captured.get("user_id"), 100)
        self.assertEqual(captured.get("price_by_user"), {100: 100})
        self.assertEqual(captured.get("pass_type_index_by_user"), {100: 0})

    def test_assign_wl_candidate_waiting_for_couple_is_not_assigned(self):
        now = datetime.now()
        event = make_event(
            assignment_rule="distributed",
            pass_types=(
                EventPassType(
                    amount=10,
                    price=100,
                    start=now - timedelta(days=1),
                    promo=False,
                    blocked_by_date=False,
                ),
            ),
        )
        passes = Passes.__new__(Passes)
        called = {"create_update": 0}

        async def create_update_from_user(_self, user_id: int):
            _ = user_id
            called["create_update"] += 1

            class FakeUpdate:
                async def assign_pass(self, *args, **kwargs):
                    _ = (args, kwargs)
                    return True

            return FakeUpdate()

        candidate = {
            "bot_id": 1,
            "pass_key": "pass_2026_1",
            "user_id": 100,
            "state": "waiting-for-couple",
            "type": "couple",
            "role": "leader",
            "couple": 200,
        }
        passes.create_update_from_user = MethodType(create_update_from_user, passes)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        result = self._run(
            passes._assign_wl_candidate(
                "pass_2026_1",
                event,
                {
                    "participants_total": 0,
                    "participants_by_role": {"leader": 0, "follower": 0},
                    "tier_usage_total": {},
                    "tier_usage_by_role": {"leader": {}, "follower": {}},
                },
                candidate,
                allow_promo=False,
            )
        )

        self.assertFalse(result)
        self.assertEqual(called.get("create_update"), 0)
        self.assertEqual(candidate.get("couple"), 200)

    def test_resolve_couple_skips_promo_tier(self):
        now = datetime.now()
        pass_types = (
            EventPassType(
                amount=100,
                price=100,
                start=now - timedelta(days=1),
                promo=True,
                blocked_by_date=False,
            ),
            EventPassType(
                amount=100,
                price=200,
                start=now - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
        )
        event = make_event(pass_types=pass_types, assignment_rule="distributed")
        passes = Passes.__new__(Passes)

        def require_event(_self, pass_key: str):
            return event

        async def collect_queue_stats(_self, pass_key: str):
            return {
                "participants_total": 0,
                "participants_by_role": {"leader": 0, "follower": 0},
                "tier_usage_total": {},
                "tier_usage_by_role": {"leader": {}, "follower": {}},
            }

        async def find_one(query):
            return {
                "bot_id": 1,
                "pass_key": "pass_2026_1",
                "user_id": 2,
                "state": "waitlist",
                "role": "follower",
                "couple": 1,
            }

        passes.require_event = MethodType(require_event, passes)
        passes._collect_queue_stats = MethodType(collect_queue_stats, passes)
        passes.pass_db = SimpleNamespace(find_one=find_one)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        resolved = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                1,
                {"role": "leader", "couple": 2},
            )
        )
        self.assertIsNotNone(resolved)
        assert resolved is not None
        _, pass_type_index_by_user = resolved
        self.assertEqual(pass_type_index_by_user[1], 1)
        self.assertEqual(pass_type_index_by_user[2], 1)

    def test_distributed_couple_uses_same_tier_for_both_roles(self):
        now = datetime.now()
        pass_types = (
            EventPassType(
                amount=100,
                price=100,
                start=now - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
            EventPassType(
                amount=100,
                price=200,
                start=now - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
        )
        event = make_event(pass_types=pass_types, assignment_rule="distributed")
        passes = Passes.__new__(Passes)

        def require_event(_self, pass_key: str):
            return event

        async def collect_queue_stats(_self, pass_key: str):
            return {
                "participants_total": 0,
                "participants_by_role": {"leader": 0, "follower": 0},
                "tier_usage_total": {},
                "tier_usage_by_role": {"leader": {}, "follower": {}},
            }

        async def find_one(query):
            return {
                "bot_id": 1,
                "pass_key": "pass_2026_1",
                "user_id": 2,
                "state": "waitlist",
                "role": "follower",
                "couple": 1,
            }

        passes.require_event = MethodType(require_event, passes)
        passes._collect_queue_stats = MethodType(collect_queue_stats, passes)
        passes.pass_db = SimpleNamespace(find_one=find_one)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        resolved = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                1,
                {"role": "leader", "couple": 2},
            )
        )
        self.assertIsNotNone(resolved)
        assert resolved is not None
        _, pass_type_index_by_user = resolved
        self.assertEqual(pass_type_index_by_user[1], pass_type_index_by_user[2])

    def test_paired_couple_can_split_into_different_tiers_by_role(self):
        now = datetime.now()
        pass_types = (
            EventPassType(
                amount=2,
                price=100,
                start=now - timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
            EventPassType(
                amount=2,
                price=200,
                start=now + timedelta(days=1),
                promo=False,
                blocked_by_date=False,
            ),
        )
        event = make_event(pass_types=pass_types, assignment_rule="paired")
        passes = Passes.__new__(Passes)

        def require_event(_self, pass_key: str):
            return event

        async def collect_queue_stats(_self, pass_key: str):
            return {
                "participants_total": 1,
                "participants_by_role": {"leader": 1, "follower": 0},
                "tier_usage_total": {0: 1},
                "tier_usage_by_role": {"leader": {0: 1}, "follower": {}},
            }

        async def find_one(query):
            return {
                "bot_id": 1,
                "pass_key": "pass_2026_1",
                "user_id": 2,
                "state": "waitlist",
                "role": "follower",
                "couple": 1,
            }

        passes.require_event = MethodType(require_event, passes)
        passes._collect_queue_stats = MethodType(collect_queue_stats, passes)
        passes.pass_db = SimpleNamespace(find_one=find_one)
        passes.base_app = SimpleNamespace(bot=SimpleNamespace(bot=SimpleNamespace(id=1)))

        resolved = self._run(
            passes.resolve_candidate_tier_prices(
                "pass_2026_1",
                1,
                {"role": "leader", "couple": 2},
            )
        )
        self.assertIsNotNone(resolved)
        assert resolved is not None
        _, pass_type_index_by_user = resolved
        self.assertEqual(pass_type_index_by_user[1], 1)
        self.assertEqual(pass_type_index_by_user[2], 0)

    async def _unused_find_one(self, query):
        return None

    def _run(self, coro):
        import asyncio

        return asyncio.run(coro)


@unittest.skipIf(passes_module is None or events_module is None, _skip_reason())
class RegistrationGuardTests(unittest.IsolatedAsyncioTestCase):
    async def test_new_pass_returns_not_started_message_when_sales_closed(self):
        PassUpdate = passes_module.PassUpdate
        event = make_event(
            pass_types=(
                EventPassType(
                    amount=100,
                    price=100,
                    start=datetime.now() + timedelta(days=2),
                    promo=False,
                    blocked_by_date=False,
                ),
            )
        )

        fake_update = _FakePassUpdateContext(user_id=10)
        base = _FakePassUpdateBase(event)
        upd = PassUpdate(base, fake_update)
        upd.set_pass_key(event.key)

        await upd.new_pass()

        self.assertGreaterEqual(len(fake_update.edit_calls), 1)
        first_call = fake_update.edit_calls[0]
        self.assertEqual(first_call["text"], "passes-sell-not-started")

    async def test_registered_user_bypasses_start_guard(self):
        PassUpdate = passes_module.PassUpdate
        event = make_event(
            pass_types=(
                EventPassType(
                    amount=100,
                    price=100,
                    start=datetime.now() + timedelta(days=2),
                    promo=False,
                    blocked_by_date=False,
                ),
            )
        )

        fake_update = _FakePassUpdateContext(user_id=11)
        base = _FakePassUpdateBase(event)
        upd = PassUpdate(base, fake_update)

        calls = {"show": 0, "new": 0}

        async def fake_get_user(_self):
            return {"user_id": 11}

        async def fake_get_pass(_self, pass_key=None, user_id=None):
            return {"state": "waitlist", "role": "leader"}

        async def fake_show(_self, user, existing_pass):
            calls["show"] += 1
            return "show-pass"

        async def fake_new(_self):
            calls["new"] += 1
            return "new-pass"

        upd.get_user = MethodType(fake_get_user, upd)
        upd.get_pass = MethodType(fake_get_pass, upd)
        upd.show_pass_edit = MethodType(fake_show, upd)
        upd.new_pass = MethodType(fake_new, upd)

        result = await upd.handle_cq_start(event.key)

        self.assertEqual(result, "show-pass")
        self.assertEqual(calls, {"show": 1, "new": 0})


@unittest.skipIf(passes_module is None or events_module is None, _skip_reason())
class ManualAdminAssignmentTests(unittest.IsolatedAsyncioTestCase):
    async def test_passes_assign_forces_ignore_date_blocks(self):
        PassUpdate = passes_module.PassUpdate
        captured: dict[str, object] = {}

        class FakeAssignTarget:
            async def assign_pass(self, *args, **kwargs):
                captured["ignore_date_blocks"] = kwargs.get("ignore_date_blocks")
                return True

        class FakePassDB:
            async def find_one(self, query, sort=None):
                _ = sort
                if (
                    query.get("user_id") == 123
                    and query.get("pass_key") == "pass_2026_1"
                    and query.get("state") == "waitlist"
                ):
                    return {
                        "bot_id": 777,
                        "pass_key": "pass_2026_1",
                        "user_id": 123,
                        "state": "waitlist",
                        "role": "leader",
                        "date_created": BASE_TS,
                    }
                return None

        class FakeAdminBase:
            def __init__(self):
                self.config = SimpleNamespace(
                    telegram=SimpleNamespace(admins=[999])
                )
                self.pass_keys = ["pass_2026_1"]
                self.pass_db = FakePassDB()

            def refresh_events_cache(self):
                return None

            def _pass_doc_to_data(self, pass_doc: dict):
                data = dict(pass_doc)
                for key in ("_id", "user_id", "pass_key", "bot_id"):
                    data.pop(key, None)
                return data

            async def create_update_from_user(self, user_id: int):
                _ = user_id
                return FakeAssignTarget()

            async def recalculate_queues(self):
                captured["recalculate_queues"] = True

        class FakeAdminUpdate:
            def __init__(self):
                self.user = 999
                self.bot = SimpleNamespace(id=777)
                self.update = SimpleNamespace()

            def l(self, message_id: str, **kwargs):
                _ = kwargs
                return message_id

            def parse_cmd_arguments(self):
                return [
                    "/passes_assign",
                    "--pass_key",
                    "pass_2026_1",
                    "123",
                ]

            async def reply(self, text: str, **kwargs):
                _ = kwargs
                captured["reply"] = text
                return text

        upd = PassUpdate(FakeAdminBase(), FakeAdminUpdate())
        await upd.handle_passes_assign()

        self.assertIs(captured.get("ignore_date_blocks"), True)
        self.assertIs(captured.get("recalculate_queues"), True)

    async def test_passes_assign_splits_assigned_couple_and_updates_recipient(self):
        PassUpdate = passes_module.PassUpdate
        captured: dict[str, object] = {}

        class FakePassDB:
            def __init__(self):
                self.docs = [
                    {
                        "bot_id": 777,
                        "pass_key": "pass_2026_1",
                        "user_id": 123,
                        "state": "assigned",
                        "type": "couple",
                        "couple": 456,
                        "price": 200,
                    },
                    {
                        "bot_id": 777,
                        "pass_key": "pass_2026_1",
                        "user_id": 456,
                        "state": "assigned",
                        "type": "couple",
                        "couple": 123,
                        "price": 200,
                    },
                ]

            @staticmethod
            def _matches(doc: dict[str, object], query: dict[str, object]) -> bool:
                for key, expected in query.items():
                    actual = doc.get(key)
                    if isinstance(expected, dict):
                        if "$in" in expected:
                            if actual not in expected["$in"]:
                                return False
                            continue
                        return False
                    if actual != expected:
                        return False
                return True

            async def find_one(self, query, sort=None):
                _ = sort
                for doc in self.docs:
                    if self._matches(doc, query):
                        return dict(doc)
                return None

            async def update_one(self, query, update):
                matched = 0
                modified = 0
                for doc in self.docs:
                    if not self._matches(doc, query):
                        continue
                    matched = 1
                    before = dict(doc)
                    set_data = update.get("$set", {})
                    unset_data = update.get("$unset", {})
                    for key, value in set_data.items():
                        doc[key] = value
                    for key in unset_data.keys():
                        doc.pop(key, None)
                    if doc != before:
                        modified = 1
                    break
                return SimpleNamespace(matched_count=matched, modified_count=modified)

            async def count_documents(self, query):
                return sum(1 for doc in self.docs if self._matches(doc, query))

        class FakeAdminBase:
            def __init__(self):
                self.config = SimpleNamespace(
                    telegram=SimpleNamespace(admins=[999])
                )
                self.pass_keys = ["pass_2026_1"]
                self.pass_db = FakePassDB()

            def refresh_events_cache(self):
                return None

            def _pass_doc_to_data(self, pass_doc: dict):
                data = dict(pass_doc)
                for key in ("_id", "user_id", "pass_key", "bot_id"):
                    data.pop(key, None)
                return data

            async def create_update_from_user(self, user_id: int):
                raise AssertionError(f"create_update_from_user should not be called: {user_id}")

            async def recalculate_queues(self):
                captured["recalculate_queues"] = True

            async def current_assignment_tier_number(self, pass_key: str):
                _ = pass_key
                return 3

            async def append_tier_amount(self, pass_key: str, tier: int, increment: int):
                captured["append_call"] = (pass_key, tier, increment)
                return 100 + increment

        class FakeAdminUpdate:
            def __init__(self):
                self.user = 999
                self.bot = SimpleNamespace(id=777)
                self.update = SimpleNamespace()

            def l(self, message_id: str, **kwargs):
                _ = kwargs
                return message_id

            def parse_cmd_arguments(self):
                return [
                    "/passes_assign",
                    "--pass_key",
                    "pass_2026_1",
                    "--price",
                    "150",
                    "--comment",
                    "manual",
                    "--append_to_tier",
                    "1",
                    "123",
                ]

            async def reply(self, text: str, **kwargs):
                _ = kwargs
                captured["reply"] = text
                return text

        fake_base = FakeAdminBase()
        upd = PassUpdate(fake_base, FakeAdminUpdate())
        await upd.handle_passes_assign()

        recipient_doc = next(d for d in fake_base.pass_db.docs if d["user_id"] == 123)
        partner_doc = next(d for d in fake_base.pass_db.docs if d["user_id"] == 456)

        self.assertEqual(recipient_doc.get("state"), "assigned")
        self.assertEqual(recipient_doc.get("type"), "solo")
        self.assertEqual(recipient_doc.get("price"), 150)
        self.assertEqual(recipient_doc.get("comment"), "manual")
        self.assertIsNone(recipient_doc.get("skip_in_balance_count"))
        self.assertNotIn("couple", recipient_doc)
        self.assertIn("date_assignment", recipient_doc)

        self.assertEqual(partner_doc.get("state"), "assigned")
        self.assertEqual(partner_doc.get("type"), "solo")
        self.assertNotIn("couple", partner_doc)

        self.assertEqual(
            captured.get("reply"),
            "passes_assign done: [123]\nappend_to_tier: tier=1, appended=1, new_amount=101",
        )
        self.assertEqual(captured.get("append_call"), ("pass_2026_1", 1, 1))
        self.assertIs(captured.get("recalculate_queues"), True)


class _FakePassUpdateContext:
    def __init__(self, user_id: int):
        self.bot = SimpleNamespace(id=777)
        self.user = user_id
        self.language_code = "en"
        self.update = SimpleNamespace()
        self.edit_calls: list[dict[str, object]] = []

    def l(self, message_id: str, **kwargs):
        return message_id

    async def edit_or_reply(
        self,
        text: str,
        *,
        reply_markup=None,
        parse_mode=None,
    ):
        self.edit_calls.append(
            {
                "text": text,
                "reply_markup": reply_markup,
                "parse_mode": parse_mode,
            }
        )
        return text

    async def reply(self, text: str, **kwargs):
        return text

    async def get_user(self):
        return {
            "user_id": self.user,
            "role": "leader",
            "proof_admins": {},
            "language_code": "en",
        }


class _FakePassUpdateBase:
    def __init__(self, event):
        self.event = event
        self.pass_keys = [event.key]
        self.pass_db = SimpleNamespace(find_one=self._find_one)
        self.payment_admins = {event.key: [1]}

    def refresh_events_cache(self):
        return None

    def require_event(self, pass_key: str):
        return self.event

    def get_event(self, pass_key: str):
        return self.event

    def get_event_localization_keys(self, pass_key: str, locale: str | None = None):
        return {}

    async def _find_one(self, query):
        return None

    async def get_pass_for_user(self, user_id: int, pass_key: str):
        return None


if __name__ == "__main__":
    unittest.main()
