"""Zero-provider recovery proof with retained files and an idempotent inbox peer."""
import copy
import io
import json
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import uuid
from pathlib import Path
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

import receiver


class ReceiverTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.state = {'next_offset': 10, 'pending': None, 'frozen': None,
            'retained': 0, 'forwarded': 0, 'identity_checked': True}
        self.config = {'allowed_users': [42], 'webhook_url': 'http://synthetic.invalid/webhook',
            'webhook_secret': 'private-secret'}
        self.control = {'mode': 'forward', 'forward_after_update_id': 0}
        self.update = {'update_id': 10, 'message': {'from': {'id': 42},
            'chat': {'id': 42, 'type': 'private'}, 'text': 'private message'}}

    def accept(self):
        receiver.accept_update(self.root, self.state, self.config, self.control,
            self.update, None, lambda: self.control, 'synthetic-batch.json')

    def record(self):
        return json.loads((self.root / 'update-10.json').read_text())

    def test_safe_reads_retry_same_offset_and_keep_logs_content_free(self):
        clock = [0.0]
        sleeps = []
        def sleep(seconds):
            sleeps.append(seconds)
            clock[0] += seconds
        transient = urllib.error.URLError(TimeoutError('PRIVATE URL with token'))
        with patch.object(receiver, 'post', side_effect=[transient, {'ok': True, 'result': []}]) as posted, \
                patch.object(receiver.time, 'monotonic', side_effect=lambda: clock[0]), \
                patch.object(receiver.time, 'sleep', side_effect=sleep), patch('builtins.print') as logged:
            result = receiver.retry_post(None, 'private-url', {'offset': 10}, 'getUpdates', lambda: True)
        self.assertTrue(result['ok'])
        self.assertEqual([call.args[2] for call in posted.call_args_list], [{'offset': 10}, {'offset': 10}])
        self.assertEqual(sum(sleeps), 1)
        event = json.loads(logged.call_args.args[0])
        self.assertEqual(event['method'], 'getUpdates')
        self.assertNotIn('PRIVATE', json.dumps(event))
        self.assertNotIn('private-url', json.dumps(event))

    def test_no_retry_contract_for_paid_dispatch(self):
        with patch.object(receiver, 'post', side_effect=AssertionError('must not dispatch')):
            with self.assertRaises(ValueError):
                receiver.retry_post(None, 'private-url', {}, 'Responses', lambda: True)

    def test_rate_limits_and_permanent_failures_are_distinct(self):
        rate = urllib.error.HTTPError('secret-url', 429, 'private error', {},
            io.BytesIO(b'{"parameters":{"retry_after":120}}'))
        self.assertEqual(receiver.retry_delay(rate, 0), 120)
        for code in (400, 401, 403, 409):
            with self.subTest(code=code):
                error = urllib.error.HTTPError('secret', code, 'private error', {}, io.BytesIO())
                self.assertIsNone(receiver.retry_delay(error, 0))
        cert = urllib.error.URLError(ssl.SSLCertVerificationError('invalid certificate'))
        self.assertIsNone(receiver.retry_delay(cert, 0))
        self.assertIsNone(receiver.retry_delay(OSError('disk'), 0))
        self.assertIsNone(receiver.retry_delay(urllib.error.URLError('unknown URL type'), 0))

    def test_restart_recovers_only_read_intents_at_durable_cursor(self):
        for method in receiver.READ_METHODS:
            state = {**self.state, 'pending': {'method': method}, 'frozen': 'orphaned_transport_unknown'}
            if method == 'getUpdates':
                state['pending']['offset'] = 10
            result = receiver.recover(self.root, state, self.config)
            self.assertEqual(result['next_offset'], 10)
            self.assertIsNone(result['pending'])
            self.assertIsNone(result['frozen'])
            self.assertFalse(result['identity_checked'])
        state['pending']['offset'] = 11
        with self.assertRaises(ValueError):
            receiver.recover(self.root, state, self.config)
        state = {**self.state, 'pending': {'method': 'getMe'}, 'frozen': 'receiver_permanent_requires_inspection'}
        self.assertEqual(receiver.recover(self.root, state, self.config), state)

    def test_uncertain_inbox_ack_repeats_exact_update_with_one_business_acceptance(self):
        task = str(uuid.uuid4())
        accepted = set()
        payloads = []
        clock = [0.0]
        def post(_, url, body, headers, timeout):
            payloads.append(copy.deepcopy(body))
            accepted.add(body['update_id'])
            if len(payloads) == 1:
                raise TimeoutError('ack lost after durable acceptance')
            return {'task_id': task, 'duplicate': True}
        with patch.object(receiver, 'post', side_effect=post), \
                patch.object(receiver.time, 'monotonic', side_effect=lambda: clock[0]), \
                patch.object(receiver.time, 'sleep', side_effect=lambda n: clock.__setitem__(0, clock[0] + n)), \
                patch('builtins.print'):
            self.accept()
        self.assertEqual(payloads, [self.update, self.update])
        self.assertEqual(accepted, {10})
        self.assertEqual(self.record()['outcome'], 'forwarded')
        self.assertEqual(self.state['next_offset'], 11)
        # Crash after receipt but before cursor persistence: do not dispatch again.
        self.state['next_offset'] = 10
        with patch.object(receiver, 'post', side_effect=AssertionError('must not resend')):
            self.accept()
        self.assertEqual(self.state['next_offset'], 11)

    def test_forward_pending_survives_crash_and_changed_target_is_held(self):
        with patch.object(receiver, 'retry_post', side_effect=SystemExit):
            with self.assertRaises(SystemExit):
                self.accept()
        self.assertEqual(self.record()['outcome'], 'forward_pending')
        self.assertEqual(self.state['next_offset'], 10)
        resumed = receiver.recover(self.root, self.state, self.config)
        self.assertIsNone(resumed['pending'])
        changed = {**self.config, 'webhook_url': 'http://different.invalid/webhook'}
        with self.assertRaises(ValueError):
            receiver.recover(self.root, self.state, changed)
        self.state = resumed
        with patch.object(receiver, 'retry_post', return_value={'task_id': str(uuid.uuid4()), 'duplicate': False}):
            self.accept()
        self.assertEqual(self.state['next_offset'], 11)

    def test_capture_is_terminal_but_legacy_unclassified_receipt_is_held(self):
        self.control['mode'] = 'capture'
        self.accept()
        self.assertEqual(self.record()['outcome'], 'captured')
        self.control['mode'] = 'forward'
        self.state['next_offset'] = 10
        with patch.object(receiver, 'post', side_effect=AssertionError('capture is not forward approval')):
            self.accept()
        legacy = self.record()
        legacy['outcome'] = 'retained'
        receiver.atomic(self.root / 'update-10.json', legacy)
        self.state['next_offset'] = 10
        with self.assertRaises(ValueError):
            self.accept()
        self.assertEqual(self.state['next_offset'], 10)

    def test_failed_cursor_write_keeps_memory_cursor(self):
        with patch.object(receiver, 'atomic', side_effect=OSError('disk failure')):
            with self.assertRaises(OSError):
                receiver.commit(self.root, self.state, next_offset=11)
        self.assertEqual(self.state['next_offset'], 10)

    def test_restart_consumes_retained_batch_without_needing_telegram_backlog(self):
        receiver.atomic(self.root / 'batch-original.json', [self.update])
        self.state.update(pending={'method': 'getUpdates', 'offset': 10}, frozen='orphaned_transport_unknown')
        self.state = receiver.recover(self.root, self.state, self.config)
        pending = receiver.retained_updates(self.root, self.state['next_offset'])
        self.assertEqual(pending, [{'update': self.update, 'batch': 'batch-original.json'}])
        with patch.object(receiver, 'retry_post', return_value={'task_id': str(uuid.uuid4())}) as posted:
            for record in pending:
                receiver.accept_update(self.root, self.state, self.config, self.control,
                    record['update'], None, lambda: self.control, record['batch'])
        self.assertEqual(posted.call_count, 1)
        self.assertEqual(posted.call_args.args[3], 'Shuttle-webhook')
        self.assertEqual(self.state['next_offset'], 11)
        self.assertEqual(receiver.retained_updates(self.root, 11), [])

    def test_hold_interrupts_retry_without_discarding_pending_forward(self):
        with patch.object(receiver, 'retry_post', side_effect=receiver.Paused):
            with self.assertRaises(receiver.Paused):
                self.accept()
        self.assertEqual(self.record()['outcome'], 'forward_pending')
        self.assertEqual(self.state['next_offset'], 10)

    @unittest.skipIf(sys.platform == 'win32', 'Service uses Linux process locking')
    def test_service_recovers_http_failures_and_restart_without_duplicate_acceptance(self):
        offsets, deliveries, accepted = [], [], set()
        acknowledged = threading.Event()
        task = str(uuid.uuid4())
        update = self.update
        class Peer(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                method = self.path.rsplit('/', 1)[-1]
                status, response = 200, {'ok': True}
                if method == 'getMe':
                    response['result'] = {'id': 6087685431}
                elif method == 'getWebhookInfo':
                    response['result'] = {'url': ''}
                elif method == 'getUpdates':
                    offsets.append(body['offset'])
                    if body['offset'] == 11:
                        acknowledged.set()
                        response['result'] = []
                        time.sleep(.1)
                    elif len(offsets) == 1:
                        status = 503
                    else:
                        response['result'] = [update]
                elif method == 'webhook':
                    deliveries.append(body)
                    accepted.add(body['update_id'])
                    status = 503 if len(deliveries) == 1 else 202
                    response = {'task_id': task, 'duplicate': len(deliveries) > 1}
                data = json.dumps(response).encode()
                self.send_response(status)
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                try:
                    self.wfile.write(data)
                except OSError:
                    pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Peer)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        base = 'http://127.0.0.1:' + str(server.server_port)
        config = {**self.config, 'bot_id': 6087685431, 'token': '6087685431:fake',
            'session': 'synthetic', 'initial_offset': 10, 'webhook_url': base + '/webhook'}
        cfg = self.root / 'config.json'
        cfg.write_text(json.dumps(config))
        code = "import receiver; original=receiver.post\ndef post(o,url,*a,**kw):\n if url.startswith('https://api.telegram.org/'):url=" + repr(base) + "+'/' +url.rsplit('/',1)[-1]\n return original(o,url,*a,**kw)\nreceiver.post=post;receiver.main()"
        def start():
            control = self.root / 'authorization.json'
            if control.exists():
                control.unlink()
            process = subprocess.Popen([sys.executable, '-c', code, '--state', str(self.root), '--config', str(cfg)],
                cwd=str(Path(receiver.__file__).parent), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            self.addCleanup(lambda: process.poll() is None and process.kill())
            end = time.monotonic() + 5
            while not control.exists():
                self.assertIsNone(process.poll(), 'receiver exited before readiness')
                self.assertLess(time.monotonic(), end)
                time.sleep(.02)
            self.assertEqual(json.loads(control.read_text()), {'armed': False})
            receiver.atomic(control, {**self.control, 'armed': True, 'session': 'synthetic',
                'unbounded': True, 'authorization_reference': 'synthetic proof'})
            return process
        for _ in range(2):
            acknowledged.clear()
            process = start()
            self.assertTrue(acknowledged.wait(10), 'durable cursor was not acknowledged')
            process.terminate()
            self.assertEqual(process.wait(timeout=5), 0)
        self.assertEqual(accepted, {10})
        self.assertEqual(deliveries, [update, update])
        self.assertEqual(offsets[:2], [10, 10])
        self.assertEqual(json.loads((self.root / 'journal.json').read_text())['next_offset'], 11)
        self.assertEqual(self.record()['outcome'], 'forwarded')


if __name__ == '__main__':
    unittest.main()
