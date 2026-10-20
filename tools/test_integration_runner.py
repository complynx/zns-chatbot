"""Reject false acceptance from incomplete or skipped Go test streams."""

import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("integration_runner", Path(__file__).with_name("test-integration.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


def event(action, name=None):
    return {"Action": action, "Test": name, "Package": runner.PACKAGE}


class CoverageAcceptance(unittest.TestCase):
    def test_complete_success_accepts(self):
        events = [event("pass", "TestOne/child"), event("pass", "TestOne"), event("pass")]
        self.assertTrue(runner.verify(events, ["TestOne"], 0)["passed"])

    def test_missing_duplicate_or_unexpected_root_rejects(self):
        for events in [[], [event("pass", "TestOne"), event("pass", "TestOne")],
                       [event("pass", "TestOne"), event("pass", "TestOther")]]:
            with self.subTest(events=events):
                self.assertFalse(runner.verify([*events, event("pass")], ["TestOne"], 0)["passed"])

    def test_nested_skip_or_failure_rejects(self):
        for action in ["skip", "fail"]:
            with self.subTest(action=action):
                events = [event(action, "TestOne/child"), event("pass", "TestOne"), event("pass")]
                self.assertFalse(runner.verify(events, ["TestOne"], 0)["passed"])

    def test_failed_process_or_missing_package_result_rejects(self):
        self.assertFalse(runner.verify([event("pass", "TestOne"), event("pass")], ["TestOne"], 1)["passed"])
        self.assertFalse(runner.verify([event("pass", "TestOne")], ["TestOne"], 0)["passed"])


if __name__ == "__main__":
    unittest.main()
