"""Reject false acceptance from incomplete or skipped Go test streams."""

import copy
import importlib.util
import json
import subprocess
from pathlib import Path
import sys
import tempfile
import time
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


class ShardAcceptance(unittest.TestCase):
    def summaries(self, count=4):
        names = [f"TestCase{number:02d}" for number in range(11)]
        summaries = []
        for index in range(count):
            selected = runner.partition(names, index, count)
            batch = runner.verify([*(event("pass", name) for name in reversed(selected)),
                                   event("pass")], selected, 0)
            batch.update(malformed_output=False, timed_out=False)
            summaries.append({"passed": True, "discovered": names, "selected": selected,
                              "shard_index": index, "shard_count": count, "batches": [batch]})
        return summaries

    def test_uneven_workers_cover_every_test_once_in_any_completion_order(self):
        self.assertEqual(runner.verify_shards(list(reversed(self.summaries())), 4), 11)
        names = self.summaries()[0]["discovered"]
        self.assertEqual(runner.partition(names, 0, 1), names)

    def test_missing_duplicate_or_inconsistent_workers_reject(self):
        summaries = self.summaries()
        invalid = [summaries[:-1], [summaries[0], *summaries[0:3]]]
        for key, value in [("shard_count", 3), ("shard_index", -1), ("shard_index", 4),
                           ("discovered", ["TestOther"]), ("selected", ["TestOther"])]:
            changed = copy.deepcopy(summaries)
            changed[0][key] = value
            invalid.append(changed)
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    runner.verify_shards(value, 4)

    def test_partial_duplicate_skipped_or_failed_evidence_rejects(self):
        invalid = []
        for key, value in [("passed", False), ("exit_code", 1), ("skips", ["TestCase00/child"]),
                           ("failures", ["TestCase00"]), ("unexpected_roots", ["TestOther"]),
                           ("malformed_output", True), ("timed_out", True),
                           ("passed_roots", ["TestCase00"]), ("expected", [])]:
            changed = self.summaries()
            changed[0]["batches"][0][key] = value
            invalid.append(changed)
        changed = self.summaries()
        changed[0]["batches"][0]["passed_roots"].append("TestCase00")
        invalid.append(changed)
        changed = self.summaries()
        changed[0]["batches"] = []
        invalid.append(changed)
        for value in invalid:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    runner.verify_shards(value, 4)

    def test_downloaded_artifacts_accept_without_database_and_reject_missing_worker(self):
        with tempfile.TemporaryDirectory() as directory:
            paths = []
            for summary in self.summaries():
                path = Path(directory) / f'integration-{summary["shard_index"]}' / 'summary.json'
                path.parent.mkdir()
                path.write_text(json.dumps(summary))
                paths.append(path)
            command = [sys.executable, str(Path(runner.__file__)), directory, '--verify-shards', '4']
            result = subprocess.run(command, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('11 roots passed across 4 shards', result.stdout)
            paths[0].unlink()
            self.assertNotEqual(subprocess.run(command, capture_output=True, timeout=10).returncode, 0)
            paths[0].write_text('{')
            self.assertNotEqual(subprocess.run(command, capture_output=True, timeout=10).returncode, 0)


class ProcessDeadline(unittest.TestCase):
    def test_completed_process_retains_output_and_exit_code(self):
        with tempfile.TemporaryDirectory() as directory:
            out, err = Path(directory) / 'stdout', Path(directory) / 'stderr'
            result = runner.run_logged([sys.executable, '-c',
                'import sys; print("output"); print("error", file=sys.stderr); sys.exit(7)'], out, err, 10)
            self.assertEqual(result, (7, False))
            self.assertIn('output', out.read_text())
            self.assertIn('error', err.read_text())

    def test_hung_process_is_reaped_and_partial_evidence_retained(self):
        with tempfile.TemporaryDirectory() as directory:
            out, err = Path(directory) / 'stdout', Path(directory) / 'stderr'
            start = time.monotonic()
            result = runner.run_logged([sys.executable, '-u', '-c',
                'import time; print("before timeout"); time.sleep(60)'], out, err, 1)
            self.assertEqual(result, (124, True))
            self.assertLess(time.monotonic() - start, 15)
            self.assertIn('before timeout', out.read_text())
            self.assertIn('Wall-clock timeout', err.read_text())

    @unittest.skipUnless(sys.platform.startswith('linux'), 'Linux process tree observation requires /proc')
    def test_timeout_terminates_child_as_well_as_parent(self):
        with tempfile.TemporaryDirectory() as directory:
            out, err = Path(directory) / 'stdout', Path(directory) / 'stderr'
            code = ('import subprocess,sys,time; '
                    'p=subprocess.Popen([sys.executable,"-c","import time; time.sleep(60)"]); '
                    'print(p.pid,flush=True); time.sleep(60)')
            self.assertEqual(runner.run_logged([sys.executable, '-c', code], out, err, 1), (124, True))
            pid = int(out.read_text().strip())
            # An orphan may remain a zombie briefly until the container's init reaps it.
            status = Path(f'/proc/{pid}/stat')
            for _ in range(50):
                if not status.exists() or status.read_text().split()[2] == 'Z':
                    break
                time.sleep(0.02)
            else:
                self.fail(f'Child {pid} survived parent timeout')


if __name__ == "__main__":
    unittest.main()
