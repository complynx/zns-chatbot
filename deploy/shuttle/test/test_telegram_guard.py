"""Exercise the stand guard against a real HTTP upstream; never call Telegram."""
import http.client
import json
import threading
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

import poller
import telegram_guard


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.forwarded = []
        forwarded = self.forwarded

        class Peer(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                raw = self.rfile.read(int(self.headers["Content-Length"]))
                data = (self.headers['Content-Type'], raw) if self.path.endswith('/sendDocument') else json.loads(raw)
                forwarded.append((self.path, data))
                body = json.dumps({"ok": True, "result": {"message_id": 1}}).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_GET(self):
                forwarded.append((self.path, None))
                if self.path.endswith("redirect"):
                    self.send_response(302)
                    self.send_header("Location", "https://example.invalid/never-follow")
                    self.send_header("Content-Length", "0")
                    self.end_headers()
                    return
                body = b"12345"
                self.send_response(200)
                if not self.path.endswith("stream"):
                    self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        self.upstream = ThreadingHTTPServer(("127.0.0.1", 0), Peer)
        self.guard = telegram_guard.GuardServer(("127.0.0.1", 0), "999:fake", 42,
            "http://127.0.0.1:" + str(self.upstream.server_port))
        self.threads = []
        for server in (self.upstream, self.guard):
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            self.threads.append(thread)

    def tearDown(self):
        for server in (self.guard, self.upstream):
            server.shutdown()
            server.server_close()
        for thread in self.threads:
            thread.join()

    def request(self, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        return urllib.request.urlopen(urllib.request.Request(
            "http://127.0.0.1:" + str(self.guard.server_port) + path, data=data,
            headers={"Content-Type": "application/json"}), timeout=3)

    def test_only_authorized_recipient_reaches_upstream(self):
        for body in ({"chat_id": 43}, {"chat_id": "42"}, {"chat_id": True},
                     {"chat_id": 42, "reply_parameters": {"chat_id": 43}},
                     {"chat_id": 42, "business_connection_id": "other"}):
            with self.subTest(body=body), self.assertRaises(urllib.error.HTTPError) as error:
                self.request("/bot999:fake/sendMessage", body)
            self.assertEqual(error.exception.code, 403)
            error.exception.close()
        self.assertEqual(self.forwarded, [])
        body = {"chat_id": 42, "text": "Authorized message", "entities": [{"type": "bold", "offset": 0, "length": 10}]}
        with self.request("/bot999:fake/sendMessage", body) as response:
            self.assertTrue(json.load(response)["ok"])
        self.assertEqual(self.forwarded, [("/bot999:fake/sendMessage", body)])

    def test_unknown_methods_and_wrong_bot_never_reach_upstream(self):
        for path in ("/bot999:wrong/sendMessage", "/bot999:fake/sendPhoto", "/bot999:fake/getMe?x=1"):
            with self.subTest(path=path), self.assertRaises(urllib.error.HTTPError) as error:
                self.request(path, {"chat_id": 42})
            self.assertEqual(error.exception.code, 403)
            error.exception.close()
        self.assertEqual(self.forwarded, [])

    def test_duplicate_json_recipients_never_reach_upstream(self):
        for raw in (b'{"chat_id":43,"chat_id":42,"text":"x"}',
                    b'{"chat_id":42,"chat_id":43,"text":"x"}',
                    b'{"chat_id":42,"reply_parameters":{"chat_id":43,"chat_id":42}}',
                    b'{"chat_id":42,"reply_parameters":{"chat_id":42,"chat_id":43}}'):
            request = urllib.request.Request('http://127.0.0.1:'+str(self.guard.server_port)+
                '/bot999:fake/sendMessage',data=raw,headers={'Content-Type':'application/json'})
            with self.assertRaises(urllib.error.HTTPError) as error:
                urllib.request.urlopen(request,timeout=3)
            self.assertEqual(error.exception.code,400)
            error.exception.close()
        self.assertEqual(self.forwarded,[])

    def test_document_upload_preserves_bytes_and_only_allows_our_recipient(self):
        def upload(fields):
            raw = b''.join(b'--qa-boundary\r\nContent-Disposition: form-data; name="'+name.encode()+
                (b'"; filename="zinusya-avatar.jpg' if name == 'document' else b'')+
                b'"\r\n\r\n'+value+b'\r\n' for name,value in fields)+b'--qa-boundary--\r\n'
            request = urllib.request.Request('http://127.0.0.1:'+str(self.guard.server_port)+
                '/bot999:fake/sendDocument', data=raw, headers={'Content-Type':'multipart/form-data; boundary=qa-boundary'})
            return request, raw
        for fields in ([('chat_id',b'43'),('document',b'jpeg')],
                       [('chat_id',b'42'),('chat_id',b'43'),('document',b'jpeg')],
                       [('chat_id',b'42'),('document',b'jpeg'),('business_connection_id',b'other')]):
            request,_ = upload(fields)
            with self.assertRaises(urllib.error.HTTPError) as error:
                urllib.request.urlopen(request,timeout=3)
            self.assertEqual(error.exception.code,403)
            error.exception.close()
        self.assertEqual(self.forwarded,[])
        request,raw = upload([('chat_id',b'42'),('document',b'\xff\xd8synthetic-jpeg')])
        for content_type in ('multipart/form-data; xboundary=evil; boundary=qa-boundary',
                             'multipart/form-data; boundary=qa-boundary; boundary=evil'):
            request.add_header('Content-Type',content_type)
            with self.assertRaises(urllib.error.HTTPError) as error:
                urllib.request.urlopen(request,timeout=3)
            self.assertEqual(error.exception.code,403)
            error.exception.close()
        self.assertEqual(self.forwarded,[])
        request.add_header('Content-Type','multipart/form-data; boundary=qa-boundary')
        with urllib.request.urlopen(request,timeout=3) as response:
            self.assertTrue(json.load(response)['ok'])
        self.assertEqual(self.forwarded,[('/bot999:fake/sendDocument',
            ('multipart/form-data; boundary=qa-boundary',raw))])

    def test_metadata_and_file_reads_keep_the_expected_bot_path(self):
        with self.request("/bot999:fake/getFile", {"file_id": "known"}) as response:
            self.assertTrue(json.load(response)["ok"])
        with self.request("/file/bot999:fake/documents/a.pdf") as response:
            self.assertEqual(response.read(), b"12345")
        self.assertEqual([p for p, _ in self.forwarded], ["/bot999:fake/getFile", "/file/bot999:fake/documents/a.pdf"])

    def test_file_paths_cannot_escape_or_redirect(self):
        for path in ("../secret", "a/./secret", "a%2fsecret", "a?x=1", "/secret"):
            with self.subTest(path=path), self.assertRaises(urllib.error.HTTPError) as error:
                self.request("/file/bot999:fake/" + path)
            error.exception.close()
        self.assertEqual(self.forwarded, [])
        with self.assertRaises(urllib.error.HTTPError) as error:
            self.request("/file/bot999:fake/redirect")
        self.assertEqual(error.exception.code, 302)
        error.exception.close()
        self.assertEqual(len(self.forwarded), 1)

    def test_oversized_stream_is_incomplete_instead_of_silently_truncated(self):
        with patch.object(telegram_guard, "MAX_FILE", 4):
            with self.request("/file/bot999:fake/stream") as response:
                with self.assertRaises(http.client.IncompleteRead):
                    response.read()

    def test_ingress_filters_other_private_accounts_and_callbacks(self):
        for user in (42, 43):
            message = {"from": {"id": user}, "chat": {"id": user, "type": "private"}}
            self.assertEqual(poller.private_update({"message": message}, 42), user == 42)
            callback = {"from": {"id": user}, "message": message}
            self.assertEqual(poller.private_update({"callback_query": callback}, 42), user == 42)


if __name__ == "__main__":
    unittest.main()
