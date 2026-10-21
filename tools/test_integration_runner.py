"""Reject false acceptance from incomplete or skipped Go test streams."""

import importlib.util
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
