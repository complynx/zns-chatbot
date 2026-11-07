"""Pure planning/target checks; no Docker, HTTP, credentials or stand state."""
import unittest
import ast
import importlib.util
import sys
from pathlib import Path

spec = importlib.util.spec_from_file_location("qa_operator", Path(__file__).with_name("operator_cli.py"))
qa = importlib.util.module_from_spec(spec)
spec.loader.exec_module(qa)
sys.modules["operator_cli"] = qa
scenario_spec = importlib.util.spec_from_file_location("qa_scenario", Path(__file__).with_name("scenario_db.py"))
scenario = importlib.util.module_from_spec(scenario_spec)
scenario_spec.loader.exec_module(scenario)
OP = "f378353c-57de-4b8a-9812-cbbf303d37ae"
EVENT = "81291d4a-548b-43cd-ae6a-b5200c146dfb"
USER = "4917f9dd-2601-489b-98fb-a52991b50cc9"


class PlanningTest(unittest.TestCase):
    def test_frontdoor_targets_have_main_without_importing_controllers(self):
        root = Path(__file__).parent
        tree = ast.parse((root / "qa.py").read_text(encoding="utf-8-sig"))
        commands = next(ast.literal_eval(n.value) for n in tree.body if isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "COMMANDS" for t in n.targets))
        for module in commands.values():
            body = ast.parse((root / (module + ".py")).read_text(encoding="utf-8-sig")).body
            self.assertTrue(any(isinstance(n, ast.FunctionDef) and n.name == "main" for n in body), module)

    def test_confirmation_preserves_original_operation_and_requires_proposal(self):
        args = {"event_id": EVENT, "user_id": USER, "role": "none", "expected_version": 3}
        with self.assertRaises(ValueError):
            qa.build("authority-confirm", args, {}, OP)
        result = qa.build("authority-confirm", args, {}, OP, EVENT)
        self.assertEqual(result["body"]["operation_id"], OP)
        self.assertEqual(result["body"]["idempotency_key"], OP)
        self.assertEqual(result["body"]["arguments"], args)

    def test_roles_and_versions_fail_closed(self):
        for role, version in (("ORG_OWNER", 0), ("administrator", -1), ("none", True)):
            with self.assertRaises(ValueError):
                qa.build("authority-prepare", {"event_id": EVENT, "user_id": USER, "role": role, "expected_version": version}, {}, OP)

    def test_unsupported_reset_and_untrusted_route_cannot_be_sent(self):
        for action in ("delete-telegram-account", "clear-history-sql", "approve-payment-sql"):
            with self.assertRaises(ValueError):
                qa.build(action, {}, {}, OP)
        with self.assertRaises(ValueError):
            qa.build("event", {}, {"event_id": "../../production"}, OP)

    def test_only_named_local_deployment(self):
        cfg = dict(project=qa.PROJECT, owner=qa.OWNER, bot_id=qa.BOT,
                   issuer="https://iam.zns.localhost:28443", organization_id="local-org",
                   base_url="https://iam.zns.localhost:28443")
        qa.validate_config(cfg)
        for patch in ({"owner": "other"}, {"base_url": "https://complynx.net"}, {"base_url": "http://localhost:28443"}):
            with self.assertRaises(ValueError):
                qa.validate_config({**cfg, **patch})

    def test_scenario_dsn_changes_only_database(self):
        original = "postgres://runtime:fake%21@postgres:5432/original?sslmode=disable"
        changed = scenario.dsn_database(original, "qa_empty")
        self.assertEqual(changed, "postgres://runtime:fake%21@postgres:5432/qa_empty?sslmode=disable")
        for invalid in ("x';DROP DATABASE original;--", "../other", "qa-empty"):
            with self.assertRaises(ValueError):
                scenario.identifier(invalid)


if __name__ == "__main__":
    unittest.main()
