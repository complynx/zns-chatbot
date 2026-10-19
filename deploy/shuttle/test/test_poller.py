"""Behavioral test of ingress acknowledgements using HTTP peers and real cursor files."""
import json
import os
import signal
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch
import poller


class IngressTests(unittest.TestCase):
    def test_wrong_bot_preflight_stops_before_other_requests(self):
        requests = []

        class Peer(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                requests.append(self.path)
                data = json.dumps({'ok': True, 'result': {'id': 1000}}).encode()
                self.send_response(200)
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        server = ThreadingHTTPServer(('127.0.0.1', 0), Peer)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with patch.dict('os.environ', {'TEST_TELEGRAM_BOT_ID': '999'}):
                with self.assertRaisesRegex(RuntimeError, 'Unexpected bot identity'):
                    poller.check_bot('http://127.0.0.1:' + str(server.server_port) + '/')
            self.assertEqual(requests, ['/getMe'])
        finally:
            server.shutdown()
            server.server_close()
            thread.join()

    def test_nonprivate_update_does_not_block_retryable_private_acceptance(self):
        offsets, deliveries, callbacks = [], [], []
        actual_post = poller.post

        def post(url, body, *args, **kwargs):
            response = actual_post(url, body, *args, **kwargs)
            if url.endswith("getUpdates") and body["offset"] == 3:
                callbacks[0]()
            return response
        group = {'update_id': 1, 'message': {'from': {'id': 10}, 'chat': {'id': -10, 'type': 'group'}}}
        private = {'update_id': 2, 'message': {'from': {'id': 10}, 'chat': {'id': 10, 'type': 'private'}, 'text': 'hello'}}
        testcase = self

        class Peer(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                status, response = 200, {'ok': True}
                if self.path.endswith('getWebhookInfo'):
                    response['result'] = {'url': ''}
                elif self.path.endswith('getMe'):
                    response['result'] = {'id': 999, 'username': 'fixture'}
                elif self.path.endswith('getUpdates'):
                    offset = data['offset']
                    offsets.append(offset)
                    response['result'] = [u for u in (group, private) if u['update_id'] >= offset]
                elif self.path == '/ingress':
                    testcase.assertEqual(self.headers['X-Telegram-Bot-Api-Secret-Token'], 'fixture-secret')
                    deliveries.append(data['update_id'])
                    if len(deliveries) == 1:
                        status = 503
                    else:
                        response = {'task_id': 'persisted-task'}
                else:
                    status = 404
                encoded = json.dumps(response).encode()
                self.send_response(status)
                self.send_header('Content-Length', str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

        server = ThreadingHTTPServer(('127.0.0.1', 0), Peer)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as temporary:
                cursor = Path(temporary) / 'offset'
                base = 'http://127.0.0.1:' + str(server.server_port)
                with patch.dict('os.environ', {'SHUTTLE_TELEGRAM_TOKEN': '999:fake', 'TEST_TELEGRAM_BOT_ID': '999', 'TEST_TELEGRAM_USER_ID': '10',
                      'TEST_WEBHOOK_URL': base + '/ingress', 'TEST_WEBHOOK_SECRET': 'fixture-secret'}), \
                     patch.object(poller.signal, 'signal', side_effect=lambda _, callback: callbacks.append(callback)), \
                     patch.object(poller, 'post', side_effect=post), \
                     patch('builtins.print') as printed:
                    with self.assertRaises(SystemExit) as stopped:
                        poller.main(base=base + '/botfake/', cursor=cursor)
                    self.assertEqual(stopped.exception.code, 0)
                self.assertEqual(sum(call.args == ('Test ingress polling cycle completed',) for call in printed.call_args_list), 1)
                self.assertEqual(deliveries, [2, 2])
                self.assertEqual(offsets, [0, 2, 3])
                self.assertEqual(cursor.read_text(), '3')
        finally:
            server.shutdown()
            server.server_close()
            thread.join()


    @unittest.skipIf(sys.platform == 'win32', 'Requires POSIX SIGTERM delivery')
    def test_sigterm_interrupts_http_waits_without_acknowledging_pending_update(self):
        for blocked_path in ('getMe', 'getUpdates', 'ingress'):
            with self.subTest(blocked_path=blocked_path), tempfile.TemporaryDirectory() as temporary:
                blocked = threading.Event()
                release = threading.Event()
                recovered = threading.Event()
                release_recovery = threading.Event()
                deliveries, accepted = [], []

                class Peer(BaseHTTPRequestHandler):
                    def log_message(self, *_):
                        pass

                    def do_POST(self):
                        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                        action = self.path.rsplit('/', 1)[-1]
                        response = {'ok': True}
                        if action == 'getMe':
                            response['result'] = {'id': 999}
                        elif action == 'getWebhookInfo':
                            response['result'] = {'url': ''}
                        elif action == 'getUpdates':
                            if data['offset'] == 51:
                                recovered.set()
                                release_recovery.wait(10)
                            response['result'] = [{'update_id': 50, 'message': {
                                'from': {'id': 10}, 'chat': {'id': 10, 'type': 'private'}, 'text': 'pending'}}] if data['offset'] == 50 else []
                        elif action == 'ingress':
                            deliveries.append(data['update_id'])
                        if action == blocked_path and not release.is_set():
                            blocked.set()
                            release.wait(10)
                            # The first blocked request never obtains acceptance.
                            status = 503
                        else:
                            status = 200
                            if action == 'ingress':
                                accepted.append(data['update_id'])
                        encoded = json.dumps(response).encode()
                        try:
                            self.send_response(status)
                            self.send_header('Content-Length', str(len(encoded)))
                            self.end_headers()
                            self.wfile.write(encoded)
                        except (BrokenPipeError, ConnectionResetError):
                            pass

                server = ThreadingHTTPServer(('127.0.0.1', 0), Peer)
                thread = threading.Thread(target=server.serve_forever, daemon=True)
                thread.start()
                base = 'http://127.0.0.1:' + str(server.server_port) + '/'
                cursor = Path(temporary) / 'offset'
                cursor.write_text('50')
                environment = {**os.environ, 'SHUTTLE_TELEGRAM_TOKEN': '999:fake', 'TEST_TELEGRAM_BOT_ID': '999',
                    'TEST_TELEGRAM_USER_ID': '10', 'TEST_WEBHOOK_URL': base + 'ingress', 'TEST_WEBHOOK_SECRET': 'fixture-secret'}
                command = [sys.executable, '-u', '-c',
                    'import sys; from pathlib import Path; import poller; poller.main(base=sys.argv[1], cursor=Path(sys.argv[2]))', base, str(cursor)]
                child = None
                try:
                    child = subprocess.Popen(command, cwd=Path(__file__).parent, env=environment,
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                    self.assertTrue(blocked.wait(5), 'Child did not reach the blocked HTTP request')
                    child.send_signal(signal.SIGTERM)
                    self.assertEqual(child.wait(timeout=1), 0)
                    self.assertEqual(cursor.read_text(), '50')
                    self.assertEqual(accepted, [])
                    release.set()
                    if blocked_path == 'ingress':
                        child = subprocess.Popen(command, cwd=Path(__file__).parent, env=environment,
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                        self.assertTrue(recovered.wait(5), 'Restart did not acknowledge the recovered update')
                        self.assertEqual(cursor.read_text(), '51')
                        self.assertEqual(deliveries, [50, 50])
                        self.assertEqual(accepted, [50])
                        child.send_signal(signal.SIGTERM)
                        self.assertEqual(child.wait(timeout=1), 0)
                finally:
                    release.set()
                    release_recovery.set()
                    if child is not None and child.poll() is None:
                        child.kill()
                        child.wait(timeout=5)
                    server.shutdown()
                    server.server_close()
                    thread.join(timeout=5)


if __name__ == '__main__':
    unittest.main()
