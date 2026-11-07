"""Process-level mock Telegram shutdown and persisted-message contract."""
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import unittest
from urllib.error import URLError
from urllib.request import HTTPSHandler, ProxyHandler, Request, build_opener

from test_telegram_mock import TLS_CERT, TLS_KEY

# Bind ephemeral loopback ports in the child; main's config checks and lifecycle
# run unchanged. This keeps the test independent of shared host service ports.
CHILD = '''
import sys
import telegram_mock as mock
original = mock.make_server
plain, secure = map(int, sys.argv[2:4])
def listen(config, **options):
    options.update(host='127.0.0.1', port=secure if options.get('tls') else plain)
    return original(config, **options)
mock.make_server = listen
sys.argv = ['telegram_mock', '--config', sys.argv[1]]
mock.main()
'''


@unittest.skipUnless(os.name == 'posix', 'SIGTERM handling requires a POSIX process')
class TelegramProcessTests(unittest.TestCase):
    def test_sigterm_clean_exit_preserves_acknowledged_message_across_restart(self):
        here = Path(__file__).resolve().parent
        with tempfile.TemporaryDirectory(prefix='zinusya-telegram-process-') as directory:
            folder = Path(directory)
            cert, key = folder / 'tls.crt', folder / 'tls.key'
            cert.write_text(TLS_CERT)
            key.write_text(TLS_KEY)
            config = folder / 'mock.json'
            config.write_text(json.dumps({
                'token': '900001:process-qa-token', 'qa_token': 'process-qa-secret',
                'webhook_secret': 'process-webhook-secret',
                'database': str(folder / 'mock.sqlite3'),
                'webhook_url': 'http://shuttle:8080/telegram/webhook',
                'tls_cert': str(cert), 'tls_key': str(key),
            }))
            with socket.socket() as first, socket.socket() as second:
                first.bind(('127.0.0.1', 0))
                second.bind(('127.0.0.1', 0))
                ports = first.getsockname()[1], second.getsockname()[1]
            opener = build_opener(ProxyHandler({}), HTTPSHandler(
                context=ssl.create_default_context(cafile=str(cert))))

            def call(path, body=None, secure=False, qa=False):
                url = ('https' if secure else 'http') + '://127.0.0.1:' + str(ports[bool(secure)]) + path
                headers = {'Content-Type': 'application/json'}
                if qa:
                    headers['Authorization'] = 'Bearer process-qa-secret'
                request = Request(url, None if body is None else json.dumps(body).encode(), headers)
                with opener.open(request, timeout=2) as response:
                    return json.load(response)

            def start():
                process = subprocess.Popen([sys.executable, '-u', '-c', CHILD,
                                            str(config), *map(str, ports)], cwd=here,
                                           stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
                try:
                    deadline = time.monotonic() + 5
                    while True:
                        self.assertIsNone(process.poll(), 'Mock exited before readiness')
                        try:
                            for secure in (False, True):
                                self.assertTrue(call('/bot900001:process-qa-token/getMe', {}, secure)['ok'])
                            return process
                        except (URLError, ConnectionError, http.client.HTTPException):
                            self.assertLess(time.monotonic(), deadline, 'Both mock listeners must become ready')
                            time.sleep(.01)
                except BaseException:
                    dispose(process)
                    raise

            def dispose(process):
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=2)
                diagnostics = process.stderr.read(4096)
                process.stderr.close()
                if sys.exc_info()[0] is not None and diagnostics:
                    sys.stderr.write(diagnostics.decode(errors='replace'))

            process = start()
            stalled = None
            try:
                saved = call('/bot900001:process-qa-token/sendMessage',
                             {'chat_id': 101, 'text': 'Acknowledged before graceful stop'})['result']
                # A daemon HTTP handler waiting on incomplete headers must not
                # retain the process after its main loop receives SIGTERM.
                stalled = socket.create_connection(('127.0.0.1', ports[0]), timeout=2)
                stalled.sendall(b'POST /unfinished HTTP/1.1\r\n')
                process.terminate()
                self.assertEqual(process.wait(timeout=2), 0)
            finally:
                if stalled is not None:
                    stalled.close()
                dispose(process)
            process = start()
            try:
                state = call('/qa/state', qa=True)
                matches = [m for m in state['messages'] if m['chat']['id'] == 101
                           and m['message_id'] == saved['message_id']]
                self.assertEqual(matches, [saved])
                next_message = call('/bot900001:process-qa-token/sendMessage',
                                    {'chat_id': 101, 'text': 'Acknowledged after restart'})['result']
                self.assertEqual(next_message['message_id'], saved['message_id'] + 1)
                process.terminate()
                self.assertEqual(process.wait(timeout=2), 0)
            finally:
                dispose(process)


if __name__ == '__main__':
    unittest.main()
