"""Behavioral HTTP contract checks for the isolated synthetic counted provider."""
from concurrent.futures import ThreadPoolExecutor
import base64
import http.client
import json
from pathlib import Path
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
import uuid

from counted_provider import FixtureServer, LIMIT, SYNTHETIC_IMAGE_TOKENS, encoded, synthetic_count

class CountedProviderTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix="zinusya-counted-qa-")
        self.mailbox = Path(self.folder.name)
        self.server = FixtureServer(("127.0.0.1", 0), self.mailbox, "fixture-only-key", wait_seconds=2)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = "http://127.0.0.1:" + str(self.server.server_port)
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)
        self.folder.cleanup()

    def post(self, path, body, client=None, key="fixture-only-key"):
        headers = {"Authorization": "Bearer " + key, "Content-Type": "application/json"}
        if client: headers["X-Client-Request-Id"] = client
        request = urllib.request.Request(self.url + path, data=json.dumps(body).encode(), headers=headers)
        with self.opener.open(request, timeout=3) as result:
            return json.load(result)

    def model_body(self):
        return {"model": "synthetic-model", "input": [{"role": "user", "content": "No business action"}], "max_output_tokens": 100,
                "instructions": "Synthetic QA only", "tools": [{"type": "function", "name": "inspect", "parameters": {"type": "object"}}]}

    def test_count_is_explicit_synthetic_contract_without_model_dispatch(self):
        for text, expected in (("", 7), ("aa", 7), ("aaa", 8), ("Ж", 7), ("😀", 8)):
            body = {"model": "m", "input": [text]}
            with self.subTest(text=text):
                result = self.post("/v1/responses/input_tokens", body)
                self.assertEqual(result, {"object": "response.input_tokens", "input_tokens": expected})
                self.assertEqual(result, self.post("/v1/responses/input_tokens", {"input": [text], "model": "m"}))
        self.assertEqual(list(self.mailbox.iterdir()), [])

    def test_manual_tool_response_and_exact_client_replay(self):
        client = str(uuid.uuid4()); body = self.model_body()
        response = {"ID": "fixture-response", "Text": "Ready", "Calls": [{"ID": "call-1", "Name": "inspect", "Arguments": {"message": "Тест"}}],
                    "Usage": {"InputTokens": 100, "OutputTokens": 5, "CachedInputTokens": 10, "CacheWriteTokens": 2}}
        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(self.post, "/v1/responses", body, client)
            request = self.mailbox / (client + ".request.json")
            until = time.monotonic() + 2
            while not request.exists() and time.monotonic() < until: time.sleep(0.01)
            saved = json.loads(request.read_bytes())
            self.assertEqual(saved["ClientRequestID"], client)
            self.assertEqual(saved["Input"], body["input"])
            self.assertEqual(saved["Tools"], body["tools"])
            temporary = self.mailbox / "reply.tmp"
            temporary.write_text(json.dumps(response), encoding="utf-8")
            temporary.replace(self.mailbox / (client + ".response.json"))
            result = future.result(timeout=2)
        self.assertEqual(result["status"], "completed")
        self.assertEqual(json.loads(result["output"][0]["arguments"]), {"message": "Тест"})
        self.assertEqual(result["usage"]["input_tokens"], 100)
        self.assertEqual(self.post("/v1/responses", body, client), result)
        prior = request.read_bytes(); changed = dict(body, instructions="Different request")
        with self.assertRaises(urllib.error.HTTPError) as error:
            self.post("/v1/responses", changed, client)
        self.assertEqual(error.exception.code, 409)
        self.assertEqual(request.read_bytes(), prior)

    def test_image_payload_bytes_are_opaque_but_text_still_grows(self):
        # The paused Q06 fixture attached this public PNG plus a 35937-byte mask.
        picture = Path(__file__).resolve().parents[3] / "internal/avatar/frame/zns_frame.png"
        original = "data:image/png;base64," + base64.b64encode(picture.read_bytes()).decode()
        mask = "data:image/png;base64," + base64.b64encode(bytes(35937)).decode()
        body = self.model_body()
        body["input"] = [{"role": "user", "content": [
            {"type": "input_text", "text": "Inspect this synthetic image and its alpha mask."},
            {"type": "input_image", "image_url": original},
            {"type": "input_text", "text": "Alpha mask for the preceding image."},
            {"type": "input_image", "image_url": mask},
        ]}]
        before = encoded(body)
        self.assertGreater(len(before), 2_000_000)
        self.assertLessEqual(len(before), LIMIT)
        previous_count = (len(before) + 3) // 4
        self.assertGreater(previous_count, 272000)
        result = self.post("/v1/responses/input_tokens", body)["input_tokens"]
        self.assertGreaterEqual(result, 2 * SYNTHETIC_IMAGE_TOKENS)
        self.assertLess(result, 272000)
        self.assertEqual(synthetic_count(body), result)
        self.assertEqual(encoded(body), before, "Counting must not rewrite the actual vision request")
        body["input"][0]["content"][0]["text"] += "x" * 400
        self.assertEqual(synthetic_count(body), result + 100)
        body["input"][0]["content"][1]["image_url"] = "data:image/png;base64,AA=="
        self.assertEqual(synthetic_count(body), result + 100)
        self.assertEqual(list(self.mailbox.iterdir()), [])

    def test_count_does_not_hide_image_shaped_text_or_tool_arguments(self):
        body = {"model": "m", "input": [
            {"type": "function_call", "arguments": {"type": "input_image", "image_url": "x" * 400}},
            {"role": "user", "content": [{"type": "input_text", "text": "x" * 400}]},
        ]}
        self.assertEqual(synthetic_count(body), (len(encoded(body)) + 3) // 4)

    def test_non_list_or_missing_input_keeps_original_count(self):
        for body in ({"model": "m"}, {"model": "m", "input": "Plain string Ж😀"}):
            with self.subTest(body=body):
                before = encoded(body)
                self.assertEqual(synthetic_count(body), max(1, (len(before) + 3) // 4))
                self.assertEqual(encoded(body), before)

    def test_request_body_limit_remains_bounded_for_both_endpoints(self):
        for endpoint in ("/v1/responses", "/v1/responses/input_tokens"):
            with self.subTest(endpoint=endpoint):
                connection = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=2)
                try:
                    connection.putrequest("POST", endpoint)
                    connection.putheader("Authorization", "Bearer fixture-only-key")
                    connection.putheader("Content-Length", str(LIMIT + 1))
                    connection.endheaders()
                    response = connection.getresponse()
                    self.assertEqual(response.status, 413)
                    response.read()
                finally:
                    connection.close()
        self.assertEqual(list(self.mailbox.iterdir()), [])

    def test_missing_authority_or_invalid_id_cannot_create_mailbox_request(self):
        for client, key, status in ((str(uuid.uuid4()), "wrong", 401), ("../outside", "fixture-only-key", 400)):
            with self.assertRaises(urllib.error.HTTPError) as error:
                self.post("/v1/responses", self.model_body(), client, key)
            self.assertEqual(error.exception.code, status)
        self.assertEqual(list(self.mailbox.iterdir()), [])

    def test_missing_manual_reply_keeps_original_request_for_recovery(self):
        self.server.wait_seconds = 0.05
        client = str(uuid.uuid4())
        with self.assertRaises(urllib.error.HTTPError) as error:
            self.post("/v1/responses", self.model_body(), client)
        self.assertEqual(error.exception.code, 504)
        self.assertTrue((self.mailbox / (client + ".request.json")).is_file())
        self.assertFalse((self.mailbox / (client + ".response.json")).exists())

    def test_missing_usage_is_not_synthesized_as_known_zero(self):
        for usage in (None, {"InputTokens": 1, "OutputTokens": 1, "CachedInputTokens": 0}):
            client = str(uuid.uuid4())
            value = {"ID": "bad-usage", "Text": "Not an accounted success"}
            if usage is not None: value["Usage"] = usage
            (self.mailbox / (client + ".response.json")).write_text(json.dumps(value), encoding="utf-8")
            with self.assertRaises(urllib.error.HTTPError) as error:
                self.post("/v1/responses", self.model_body(), client)
            self.assertEqual(error.exception.code, 400)

    def test_output_usage_over_request_bound_is_incomplete(self):
        client = str(uuid.uuid4())
        response = {"ID": "too-large", "Text": "Bound exceeded",
                    "Usage": {"InputTokens": 1, "OutputTokens": 101, "CachedInputTokens": 0, "CacheWriteTokens": 0}}
        (self.mailbox / (client + ".response.json")).write_text(json.dumps(response), encoding="utf-8")
        result = self.post("/v1/responses", self.model_body(), client)
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["usage"]["output_tokens"], 101)

    def test_stalled_bodies_expire_and_release_all_request_slots(self):
        self.server.io_timeout_seconds = 0.2
        clients = []
        try:
            for _ in range(4):
                connection = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=2)
                connection.putrequest("POST", "/v1/responses/input_tokens")
                connection.putheader("Authorization", "Bearer fixture-only-key")
                connection.putheader("Content-Length", "100")
                connection.endheaders(b"{")
                clients.append(connection)
            for connection in clients:
                response = connection.getresponse()
                self.assertEqual(response.status, 408)
                response.read()
        finally:
            for connection in clients: connection.close()
        self.assertEqual(self.post("/v1/responses/input_tokens", {"model": "m", "input": ["aaa"]})["input_tokens"], 8)
        self.assertEqual(list(self.mailbox.iterdir()), [])

    def test_trickling_bodies_have_total_deadline_and_release_slots(self):
        self.server.io_timeout_seconds = 0.2
        clients = []
        sockets = []
        stop = threading.Event()
        def trickle():
            while not stop.wait(0.03):
                for connection in sockets:
                    try: connection.send(b" ")
                    except OSError: pass
        sender = None
        started = time.monotonic()
        try:
            for _ in range(4):
                connection = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=2)
                connection.putrequest("POST", "/v1/responses/input_tokens")
                connection.putheader("Authorization", "Bearer fixture-only-key")
                connection.putheader("Content-Length", "10000")
                connection.endheaders(b"{")
                clients.append(connection)
                sockets.append(connection.sock)
            sender = threading.Thread(target=trickle, daemon=True)
            sender.start()
            for connection in clients:
                try:
                    response = connection.getresponse()
                    self.assertEqual(response.status, 408)
                    response.read()
                except ConnectionError:
                    # Closing while the peer still sends can reset TCP before 408 arrives.
                    pass
            self.assertLess(time.monotonic() - started, 2)
        finally:
            stop.set()
            if sender: sender.join(timeout=2)
            for connection in clients: connection.close()
        self.assertEqual(self.post("/v1/responses/input_tokens", {"model": "m", "input": ["aaa"]})["input_tokens"], 8)
        self.assertEqual(list(self.mailbox.iterdir()), [])

    def test_native_compaction_is_explicitly_unsupported(self):
        with self.assertRaises(urllib.error.HTTPError) as error:
            self.post("/v1/responses/compact", self.model_body())
        self.assertEqual(error.exception.code, 404)
        self.assertEqual(list(self.mailbox.iterdir()), [])

if __name__ == "__main__":
    unittest.main()
