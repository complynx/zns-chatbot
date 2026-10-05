import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer


CLOCK_NAMES = (
    "REGISTRATION_CLOCK_FILE",
    "REGISTRATION_CLOCK_INSTALLATION",
    "REGISTRATION_CLOCK_CASE",
    "REGISTRATION_CLOCK_DATABASE_ADDRESS",
    "REGISTRATION_CLOCK_ANCHOR",
)


class HealthTest(unittest.TestCase):
    binary = None
    command = None

    def run_health(self, status):
        requests = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append(self.path)
                self.send_response(status)
                self.end_headers()

            def log_message(self, *_):
                pass

        with tempfile.TemporaryDirectory() as directory:
            clock = pathlib.Path(directory) / "clock.json"
            original = b'{"revision":1,"fixture":"health-only"}'
            clock.write_bytes(original)
            environment = os.environ.copy()
            environment.update(dict(zip(CLOCK_NAMES, (
                str(clock),
                "010400000204",
                "c-registration-clock-20261001-v1",
                "postgres:5432/synthetic_qa_zns_registration_fixture",
                "2030-10-02T12:00:00Z",
            ))))
            parent_clock = {name: environment[name] for name in CLOCK_NAMES}
            server = HTTPServer(("127.0.0.1", 8080), Handler)
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                rejected = subprocess.run(
                    [self.binary, "health"], env=environment,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5,
                    check=False,
                )
                self.assertNotEqual(rejected.returncode, 0)
                self.assertEqual(requests, [])
                result = subprocess.run(
                    self.command, env=environment,
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=5,
                    check=False,
                )
                self.assertEqual(requests, ["/healthz"])
                self.assertEqual(
                    {name: environment[name] for name in CLOCK_NAMES}, parent_clock,
                )
                self.assertEqual(clock.read_bytes(), original)
                return result.returncode
            finally:
                server.shutdown()
                server.server_close()
                thread.join(timeout=5)
                self.assertFalse(thread.is_alive())

    def test_real_http_200_with_parent_clock_unchanged(self):
        self.assertEqual(self.run_health(200), 0)

    def test_real_http_503_is_not_healthy(self):
        self.assertNotEqual(self.run_health(503), 0)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    inputs = parser.add_mutually_exclusive_group(required=True)
    inputs.add_argument("--rendered", type=pathlib.Path)
    inputs.add_argument("--compose")
    parser.add_argument("--binary", default="/usr/local/bin/zns")
    args = parser.parse_args()
    if args.compose:
        config = subprocess.run(
            ["docker", "compose", "--project-name", "synthetic-qa-c-current",
             "--file", args.compose, "--profile", "maintenance",
             "config", "--format", "json"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15,
            check=False,
        )
        if config.returncode != 0:
            raise SystemExit("offline health composition failed")
        raw = config.stdout
    else:
        raw = args.rendered.read_bytes()
    rendered = json.loads(raw)
    if len(rendered["services"]) != 12:
        raise SystemExit("complete health composition required")
    print(f"HEALTH_RENDER services=12 sha256={hashlib.sha256(raw).hexdigest()}")
    health = rendered["services"]["app"]["healthcheck"]["test"]
    if health[:3] != ["CMD", "/bin/sh", "-c"] or len(health) != 4:
        raise SystemExit("health command shape mismatch")
    HealthTest.binary = args.binary
    HealthTest.command = health[1:]
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(HealthTest)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    passed = result.wasSuccessful() and result.testsRun == 2 and not result.skipped
    print(f"HEALTH_CONTROLS count={result.testsRun} skipped={len(result.skipped)} pass={str(passed).lower()}")
    raise SystemExit(0 if passed else 1)
