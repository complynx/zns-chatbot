"""Process-level contract for the mock provider with an unanswered inference."""
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

import stand
import urllib.error
import urllib.request
import uuid

from test_telegram_mock import TLS_CERT, TLS_KEY


@unittest.skipUnless(os.name == "posix", "SIGTERM handling requires a POSIX process")
class ProviderProcessTests(unittest.TestCase):
    def test_sigterm_exits_cleanly_and_preserves_unanswered_request(self):
        with tempfile.TemporaryDirectory(prefix="zinusya-provider-process-") as directory:
            folder = Path(directory)
            mailbox = folder / "mailbox"
            mailbox.mkdir()
            here = Path(__file__).resolve().parent
            shutil.copyfile(here / "provider_main.py", folder / "provider_main.py")
            shutil.copyfile(here.parent / "test" / "counted_provider.py", folder / "counted_provider.py")
            with socket.socket() as reserved:
                reserved.bind(("127.0.0.1", 0))
                port = reserved.getsockname()[1]
            cert, key = folder / "tls.crt", folder / "tls.key"
            cert.write_text(TLS_CERT)
            key.write_text(TLS_KEY)
            env = dict(os.environ, ZINUSYA_SYNTHETIC_COUNTED_QA="1", ZINUSYA_COUNTED_QA_KEY="process-fixture-key")
            process = subprocess.Popen([sys.executable, "-u", str(folder / "provider_main.py"),
                                        "--mailbox", str(mailbox), "--port", str(port),
                                        "--tls-cert", str(cert), "--tls-key", str(key)],
                                       env=env, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(cert))))
            stalled = None
            def post(path, body, request_id=None):
                headers = {"Authorization": "Bearer process-fixture-key", "Content-Type": "application/json"}
                if request_id:
                    headers["X-Client-Request-Id"] = request_id
                request = urllib.request.Request("https://127.0.0.1:" + str(port) + path,
                                                 data=json.dumps(body).encode(), headers=headers)
                with opener.open(request, timeout=5) as response:
                    return json.load(response)
            try:
                deadline = time.monotonic() + 5
                while True:
                    self.assertIsNone(process.poll(), "Provider exited before readiness")
                    try:
                        result = post("/v1/responses/input_tokens", {"model": "m", "input": ["ready"]})
                        self.assertEqual(result["object"], "response.input_tokens")
                        break
                    except urllib.error.URLError:
                        self.assertLess(time.monotonic(), deadline, "Provider did not become ready")
                        time.sleep(0.01)
                # A peer that never sends ClientHello must not block other clients.
                stalled = socket.create_connection(("127.0.0.1", port), timeout=2)
                began = time.monotonic()
                self.assertEqual(post("/v1/responses/input_tokens", {"model": "m", "input": ["ready"]})["object"],
                                 "response.input_tokens")
                self.assertLess(time.monotonic() - began, 2, "Stalled TLS peer blocked accept loop")
                request_id = str(uuid.uuid4())
                body = {"model": "m", "input": [{"role": "user", "content": "Wait"}], "max_output_tokens": 10}
                with ThreadPoolExecutor(max_workers=1) as pool:
                    pending = pool.submit(post, "/v1/responses", body, request_id)
                    saved = mailbox / (request_id + ".request.json")
                    deadline = time.monotonic() + 2
                    while not saved.exists():
                        self.assertLess(time.monotonic(), deadline, "Pending request was not published")
                        time.sleep(0.01)
                    before = saved.read_bytes()
                    self.assertEqual(json.loads(before)["ClientRequestID"], request_id)
                    process.terminate()
                    self.assertEqual(process.wait(timeout=2), 0)
                    with self.assertRaises((http.client.RemoteDisconnected, ConnectionError, urllib.error.URLError)):
                        pending.result(timeout=2)
                    self.assertEqual(saved.read_bytes(), before)
                    self.assertFalse((mailbox / (request_id + ".response.json")).exists())
            finally:
                if stalled is not None:
                    stalled.close()
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=2)
                diagnostic = process.stderr.read(4096)
                process.stderr.close()
                if sys.exc_info()[0] is not None and diagnostic:
                    sys.stderr.write(diagnostic.decode(errors="replace"))


class ProviderLayoutTests(unittest.TestCase):
    def test_legacy_start_refuses_before_any_container_operation(self):
        with patch.object(stand, "load", return_value=(Path("unused"), {"stage": "stopped"})), \
             patch.object(stand, "ownership") as ownership, patch.object(stand, "run") as run:
            with self.assertRaisesRegex(AssertionError, "Legacy provider layout"):
                stand.start("legacy")
            ownership.assert_not_called()
            run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
