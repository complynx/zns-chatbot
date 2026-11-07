"""Token recovery is independent of model traffic and preserves the last usable token."""
import contextlib
import http.client
import io
import json
import ssl
import tempfile
import threading
import unittest
import urllib.error
from pathlib import Path
from unittest.mock import Mock, patch

import descriptor_token as token


def response(value):
    return io.BytesIO(json.dumps(value).encode())


def http_error(code):
    return urllib.error.HTTPError('http://private.invalid', code, 'private body', {}, io.BytesIO())


class TokenTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.state = Path(temporary.name)
        self.config = {'client_id': 'synthetic', 'client_secret': 'secret-never-log',
            'scope': 'synthetic-scope', 'subject': 'actor', 'token_url': 'https://iam.invalid/token'}
        self.reply = {'access_token': 'synthetic-token', 'expires_in': 120, 'token_type': 'Bearer'}
        self.caps = {'execution_profiles': ['native_files']}
        (self.state / 'actor.token').write_text('old-token')
        (self.state / 'ready.json').write_text('old-metadata')

    def test_explicit_tls_context_keeps_hostname_and_certificate_verification(self):
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        with patch.object(token.ssl, 'SSLContext', return_value=context), \
                patch.object(type(context), 'load_verify_locations') as load:
            opener = token.make_opener('synthetic-ca.pem')
        self.assertTrue(context.check_hostname)
        self.assertEqual(context.verify_mode, ssl.CERT_REQUIRED)
        load.assert_called_once_with(cafile='synthetic-ca.pem')
        self.assertTrue(any(isinstance(handler, token.NoRedirect) for handler in opener.handlers))

    def test_outage_after_issuance_recovers_without_replacing_old_state(self):
        opener = Mock()
        opener.open.side_effect = [response(self.reply), urllib.error.URLError('secret-never-log'),
            response(self.reply), response(self.caps)]
        stop = Mock()
        stop.is_set.return_value = False
        def wait(delay):
            if delay == 1:
                self.assertEqual((self.state / 'actor.token').read_text(), 'old-token')
                self.assertEqual((self.state / 'ready.json').read_text(), 'old-metadata')
                return False
            self.assertEqual(delay, 40)
            return True
        stop.wait.side_effect = wait
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            token.rotate(self.config, self.state, opener, stop)
        self.assertNotIn('secret-never-log', output.getvalue())
        self.assertEqual((self.state / 'actor.token').read_text(), 'synthetic-token')
        self.assertEqual(opener.open.call_count, 4)

    def test_backoff_is_bounded_and_stoppable(self):
        stop = Mock()
        stop.is_set.return_value = False
        stop.wait.side_effect = [False] * 7 + [True]
        with patch.object(token, 'refresh', side_effect=http_error(503)), contextlib.redirect_stdout(io.StringIO()):
            token.rotate(self.config, self.state, Mock(), stop)
        self.assertEqual([call.args[0] for call in stop.wait.call_args_list], [1, 2, 4, 8, 16, 30, 30, 30])
        self.assertEqual((self.state / 'actor.token').read_text(), 'old-token')

    def test_truncated_reply_retries_without_publishing_partial_token(self):
        opener = Mock()
        truncated = Mock()
        truncated.__enter__ = Mock(return_value=truncated)
        truncated.__exit__ = Mock(return_value=False)
        truncated.read.side_effect = http.client.IncompleteRead(b'private partial body')
        opener.open.side_effect = [truncated, response(self.reply), response(self.caps)]
        stop = Mock()
        stop.is_set.return_value = False
        def wait(delay):
            if delay == 1:
                self.assertEqual((self.state / 'actor.token').read_text(), 'old-token')
                self.assertEqual((self.state / 'ready.json').read_text(), 'old-metadata')
                return False
            return True
        stop.wait.side_effect = wait
        with contextlib.redirect_stdout(io.StringIO()):
            token.rotate(self.config, self.state, opener, stop)
        self.assertEqual((self.state / 'actor.token').read_text(), 'synthetic-token')

    def test_permanent_auth_and_tls_failures_do_not_retry(self):
        for error in (http_error(401), http_error(403),
                urllib.error.URLError(ssl.SSLCertVerificationError('invalid certificate'))):
            with self.subTest(error=type(error).__name__), patch.object(token, 'refresh', side_effect=error):
                stop = Mock()
                stop.is_set.return_value = False
                with self.assertRaises(type(error)):
                    token.rotate(self.config, self.state, Mock(), stop)
                stop.wait.assert_not_called()

    def test_capability_401_has_bounded_same_token_grace(self):
        opener = Mock()
        opener.open.side_effect = [response(self.reply), http_error(401), response(self.caps)]
        with patch.object(token.time, 'monotonic', side_effect=[0, 1]):
            self.assertEqual(token.refresh(self.config, self.state, opener, threading.Event()), 40)
        self.assertIs(opener.open.call_args_list[1].args[0], opener.open.call_args_list[2].args[0])

    def test_wrong_capability_never_publishes_token(self):
        opener = Mock()
        opener.open.side_effect = [response(self.reply), response({'execution_profiles': []})]
        with self.assertRaises(ValueError):
            token.refresh(self.config, self.state, opener, threading.Event())
        self.assertEqual((self.state / 'actor.token').read_text(), 'old-token')
        self.assertEqual((self.state / 'ready.json').read_text(), 'old-metadata')

    def test_expiry_is_measured_before_issuance_not_after_capability_check(self):
        opener = Mock()
        opener.open.side_effect = [response(self.reply), response(self.caps)]
        with patch.object(token.time, 'time', side_effect=[100, 130, 131]):
            token.refresh(self.config, self.state, opener, threading.Event())
        self.assertEqual(json.loads((self.state / 'ready.json').read_text())['expires_at'], 220)


if __name__ == '__main__':
    unittest.main()
