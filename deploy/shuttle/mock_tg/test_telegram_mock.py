"""Behavioral checks run against real localhost HTTP listeners.

Written by Codex (unverified/Codex desktop)
on behalf of Daniel Drizhuk
"""
import base64
import hashlib
import hmac
import http.client
import json
import socket
import ssl
from concurrent.futures import ThreadPoolExecutor
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import parse_qs
from urllib.request import Request, urlopen

from telegram_mock import make_server

# Public test-only TLS fixture. This key never protects deployed traffic.
TLS_CERT = '-----BEGIN CERTIFICATE-----\nMIIC2TCCAcGgAwIBAgIUafzCtcIUTEinRST/yCor/vAEhEMwDQYJKoZIhvcNAQEL\nBQAwHDEaMBgGA1UEAwwRbG9jYWxob3N0IGZpeHR1cmUwHhcNMjAwMTAxMDAwMDAw\nWhcNNDAwMTAxMDAwMDAwWjAcMRowGAYDVQQDDBFsb2NhbGhvc3QgZml4dHVyZTCC\nASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAJvsMVrRdten60MgZXP5hG+a\nrubNWsTx4GepQ4tmxO7j7RjwFDC2WHa+ekrf29dK/3Ad20U2izwXHk0HUgheDR4c\ng+zxURpHIT0pH3XS0nhbk+8EXxEkFhTmJeuZdT8Gc8TcPZo+wf6dMQHC30ruJT/H\n+t6SqrT44tOiNV5caDs+fdd5ocF8v7xSB1/AgL6dCtR2DUDAnnFBk9qHdLR/4ebq\n6ZQmxQoFAIb+yNcllnWV5bAmTB4c5rTS4PC5wrNhKhObLiafpcCqzQq7j2w1+mL7\nggDK08F/xOK/4+nl/74Rf9CW7t2yhvkIu5/PiGhysSxaMurM4ifhywwGTHQjLtcC\nAwEAAaMTMBEwDwYDVR0RBAgwBocEfwAAATANBgkqhkiG9w0BAQsFAAOCAQEACd6j\nQMGHxZrpMP26Q2DpilgVUaM/vG7v06S7hs4Tc3RxP7KwteffC6cE4GTKFzTlJlLz\nK/yC9bPkTSxkaud11v3/WHd6O2MLGB61vKccGpEJFJHizCj4Yd3XeWND+lqTImyC\nzmvjOemuIslg0HMnZrhTxVwr2UT4U3oFG8m5w7D/tYxkiCfwhCaV/mKyLmCCHdrd\nfY1oBZx43eCfHEhDd8dM0kiOSyRs4k01O+fAWQgYRxxZTs99hbDlSP5VZ+O4X07a\nM4zfsbxiXK/bu2g7vxUDqmf0x5f6P51eIQPKhKJtsz6/YQNFv3KSnucKt/n5MP35\nFdOk6GKvwZLH8M7W8w==\n-----END CERTIFICATE-----\n'
TLS_KEY = '-----BEGIN PRIVATE KEY-----\nMIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCb7DFa0XbXp+tD\nIGVz+YRvmq7mzVrE8eBnqUOLZsTu4+0Y8BQwtlh2vnpK39vXSv9wHdtFNos8Fx5N\nB1IIXg0eHIPs8VEaRyE9KR910tJ4W5PvBF8RJBYU5iXrmXU/BnPE3D2aPsH+nTEB\nwt9K7iU/x/rekqq0+OLTojVeXGg7Pn3XeaHBfL+8UgdfwIC+nQrUdg1AwJ5xQZPa\nh3S0f+Hm6umUJsUKBQCG/sjXJZZ1leWwJkweHOa00uDwucKzYSoTmy4mn6XAqs0K\nu49sNfpi+4IAytPBf8Tiv+Pp5f++EX/Qlu7dsob5CLufz4hocrEsWjLqzOIn4csM\nBkx0Iy7XAgMBAAECggEACxPMrdcTyl5zqGE00/h2IDetFVihijxk5PhxhAXZF9+5\niHwF8zABOO5vYcJlq/X4D88PNOubGLnLvU031sBXzjvYY40BZgyHoAC763L+4O4g\npGp7FoypxPJU31gXrFKZBvNerQaoYZ5pTOa7Vmd03gMnH4jr9892zfJIOFv4S70Y\nXlOF/6aJnnA7lA6vfp7FUaDwgGk1o5PA9sMPE2mnAfBlIsoihDzg0fYNb1AzZg6p\net96zDBCm4QYdRT9qGozweNsnqMcBLEkII5cMowqSlcZ/wZlNk7uIdZW3RpJ1JRx\nCbctnUDe4oy6VkPg7SR3NlYJ/dAOUEQl5olmamQ0gQKBgQDWVaIfgHolw1UJ5PhF\nPhlg40q2N6vpRad891mhjV+oooYFK2J+/kMKPRVP6pXfKHxd6dUueE+ztc1rLfhm\n1jzSoq/QVvM/HBdXcUx/7eqIU9tc/g6xkZ77xKO7uas+g7J07pjL0L4+9tMTjnKZ\nAksjjMmkTBVTpRKN+TBbP5aurQKBgQC6O7CQJ7BHAT5rNZlpZVaUzoQTOO6L//qc\nBwbLwlt42YpU1sW9xywhdYU3h4Xj0rRjLoLyx4C6tMGAsU0VoONzzqe1q56ehC6v\nkWEp1c6yzRHUnmy5rlSqSPh9VR1XuSWNLvTvyGtBqZk8k6kTaWB+9Cte5y6IVc7m\nReXx4GQYEwKBgD9Lxrb64Beq2cA0SEVak3ATb4rkYfZfxZiYC3Y0I93PMTjxmQLb\n37x0UYjqRBxE/oVVeslgalkuBV3zk3n7JrclqDokk209xH7kTK0y+6IK8atCs+Ao\nmwTLC3dy22l4xwjVNXZrTOOXTLpnxNv+22XCPAIzHdE33ITk5CzuWHd5AoGBALYn\nDdJhz2wTzl6ERXIuGVphCBPZGpG6pP+Za88+YcO5WzESBMVTm1l6nfsrRg0ImUFt\nS810zvdPNvjN8myWOgKafbUWkIXRGezFUHjf1FVUeP0ym3ueBNetmcbUymd0A0Ot\nSwuBR08pOGVks/n1ltl1rzoa+ouFCmJ2uasz5BMbAoGBALkrfjwpzmIVQc2XGHzO\n+B+Mdn/2EIBVVv/jE1oo9hsfzZJ6WQL4AGgo2kM4oMr3b6n9qJ0Cyq/jJxQPxIeR\nFSK/cmqsjSan7TGexdudRQkbyJcYh2Fqhj9IBnS4yInYhZMbmcN4vt4RoB4Pd6fl\n7LwF8DEawyBsImajY9hoKJKG\n-----END PRIVATE KEY-----\n'


class Webhook(BaseHTTPRequestHandler):
    def do_POST(self):
        value = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        self.server.received.append((value, self.headers.get('X-Telegram-Bot-Api-Secret-Token')))
        self.send_response(getattr(self.server, 'status', 200))
        if hasattr(self.server, 'redirect'):
            self.send_header('Location', self.server.redirect)
        self.send_header('Content-Length', '0')
        self.end_headers()

    def log_message(self, *args):
        pass


class MiniApp(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        self.server.observed.append({'method': self.command, 'path': self.path, 'origin': self.headers.get('Origin'),
                                     'cookie': self.headers.get('Cookie'), 'content_type': self.headers.get('Content-Type'),
                                     'authorization': self.headers.get('Authorization'), 'body': body})
        if self.path == '/app/avatars/drafts/upload':
            self.server.observed[-1]['operation_id'] = self.headers.get('X-Zinusya-Operation-Id')
        status = 200
        if self.path.startswith('/app/session/logout'):
            status = 200 if self.headers.get('Authorization') == 'ZinusyaSession fixture-session-token' else 403
            if status == 200:
                self.server.revoked = True
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Set-Cookie', 'session=fixture; Path=/app/; Secure; HttpOnly')
        self.send_header('Set-Cookie', 'csrf=fixture; Path=/app/; Secure')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        self.server.observed.append({'method': self.command, 'path': self.path, 'authorization': self.headers.get('Authorization')})
        if self.path in ('/app/avatars/private.jpg', '/app/avatars/denied.jpg'):
            body = b'private image' if self.path.endswith('/private.jpg') else b'access denied'
            self.send_response(200 if self.path.endswith('/private.jpg') else 403)
            self.send_header('Content-Type', 'image/jpeg' if self.path.endswith('/private.jpg') else 'text/plain')
            self.send_header('Cache-Control', 'private, no-store')
            self.send_header('Content-Disposition', 'attachment; filename="avatar.jpg"')
            self.send_header('Content-Security-Policy', "default-src 'none'; sandbox")
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.send_header('Referrer-Policy', 'no-referrer')
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if self.path == '/app/me':
            authenticated = self.headers.get('Authorization') == 'ZinusyaSession fixture-session-token' and not getattr(self.server, 'revoked', False)
            self.send_response(200 if authenticated else 401)
            self.send_header('Content-Length', '0')
            self.end_headers()
            return
        self.send_response(302)
        self.send_header('Location', '/app/redirected')
        self.send_header('Content-Length', '0')
        self.end_headers()

    def log_message(self, *args):
        pass


class MockTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.servers = []
        self.webhook = ThreadingHTTPServer(('127.0.0.1', 0), Webhook)
        self.webhook.received = []
        threading.Thread(target=self.webhook.serve_forever, daemon=True).start()
        self.config = {'token': '900001:fake-qa-token', 'qa_token': 'qa-secret', 'webhook_secret': 'webhook-secret',
                       'database': str(Path(self.temp.name) / 'mock.sqlite3'),
                       'webhook_url': f'http://127.0.0.1:{self.webhook.server_port}/telegram/webhook'}
        self.server = self.start(self.config)
        self.user1 = {'id': 101, 'first_name': 'One', 'language_code': 'en'}
        self.user2 = {'id': 202, 'first_name': 'Two', 'language_code': 'ru'}

    def start(self, config):
        server = make_server(config, '127.0.0.1', 0)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        self.servers.append(server)
        return server

    def tearDown(self):
        for server in self.servers:
            server.shutdown()
            server.server_close()
            server.store.db.close()
        self.webhook.shutdown()
        self.webhook.server_close()
        self.temp.cleanup()

    def call(self, path, data=None, server=None, auth=True, headers=None):
        server = server or self.server
        request_headers = {'Content-Type': 'application/json'}
        if auth:
            request_headers['Authorization'] = 'Bearer qa-secret'
        request_headers.update(headers or {})
        body = None if data is None else json.dumps(data).encode()
        request = Request(f'http://127.0.0.1:{server.server_port}{path}', body, request_headers)
        try:
            with urlopen(request, timeout=5) as response:
                return response.status, json.load(response)
        except HTTPError as error:
            return error.code, json.load(error)

    def qa(self, action, data):
        status, result = self.call('/qa/' + action, data)
        self.assertEqual(status, 200, result)
        return result

    def bot(self, method, data):
        status, result = self.call('/bot' + self.config['token'] + '/' + method, data)
        self.assertEqual(status, 200, result)
        return result['result']

    def state(self):
        return self.call('/qa/state')[1]

    def test_symbol_reads_return_exact_registered_originals_and_ordered_set(self):
        items = []
        for index, (animated, video) in enumerate(((False, False), (True, False), (False, True)), 1):
            file_id = 'original-' + str(index)
            self.qa('file', {'file_id': file_id, 'name': file_id, 'data_base64': 'c3ludGhldGlj'})
            items.append({'file_id': file_id, 'file_unique_id': 'unique-' + str(index),
                          'type': 'custom_emoji', 'set_name': 'Composition_set', 'custom_emoji_id': str(index),
                          'width': 100, 'height': 100, 'is_animated': animated, 'is_video': video})
        value = {'name': 'Composition_set', 'title': 'Composed symbol fixtures',
                 'sticker_type': 'custom_emoji', 'stickers': items}
        self.qa('sticker_set', value)
        self.assertEqual(self.bot('getStickerSet', {'name': 'Composition_set'}), value)
        self.assertEqual(self.bot('getCustomEmojiStickers', {'custom_emoji_ids': ['3', '1']}), [items[2], items[0]])
        self.assertEqual(self.bot('getCustomEmojiStickers', {'custom_emoji_ids': '["2"]'}), [items[1]])
        status, _ = self.call('/bot' + self.config['token'] + '/getStickerSet', {'name': 'missing'})
        self.assertEqual(status, 400)
        for bad in ([], [1], ['invalid'], ['1'] * 201):
            status, _ = self.call('/bot' + self.config['token'] + '/getCustomEmojiStickers',
                                  {'custom_emoji_ids': bad})
            self.assertEqual(status, 400)

    def test_reactions_replace_remove_and_reject_invalid_without_mutating_message(self):
        message = self.bot('sendMessage', {'chat_id': 101, 'text': 'Reaction target'})
        target = {'chat_id': 101, 'message_id': message['message_id']}
        for emoji in ('👍', '🙈'):
            self.assertTrue(self.bot('setMessageReaction', dict(target,
                reaction=[{'type': 'emoji', 'emoji': emoji}])))
        self.assertTrue(self.bot('setMessageReaction', dict(target, reaction=[])))
        for reactions in (None, {}, [{'type': 'paid'}], [{'type': 'custom_emoji', 'custom_emoji_id': '1'}],
                          [{'type': 'emoji', 'emoji': 'not-emoji'}],
                          [{'type': 'emoji', 'emoji': '👍'}, {'type': 'emoji', 'emoji': '🙈'}]):
            status, _ = self.call('/bot' + self.config['token'] + '/setMessageReaction',
                                  dict(target, reaction=reactions))
            self.assertEqual(status, 400)
        self.assertEqual(self.state()['messages'], [message])
        self.assertEqual(len([e for e in self.state()['journal'] if e['kind'] == 'message_reaction']), 3)

    def test_invalid_keyboard_arrays_are_rejected_without_message_effects(self):
        self.qa('file', {'file_id': 'keyboard-file', 'name': 'qa.txt', 'mime_type': 'text/plain',
                         'data_base64': base64.b64encode(b'synthetic keyboard test').decode()})
        original = self.bot('sendMessage', {'chat_id': 101, 'text': 'Original'})
        malformed = [{'inline_keyboard': None}, {'keyboard': None}, {'inline_keyboard': {}},
                     {'inline_keyboard': [1]}, {'keyboard': [1]},
                     {'inline_keyboard': [['plain text']]}, {'inline_keyboard': [[{'text': 'Missing action'}]]}]
        for method in ('sendMessage', 'sendDocument', 'editMessageText'):
            for markup in malformed:
                with self.subTest(method=method, markup=markup):
                    before = self.state()['messages']
                    data = {'chat_id': 101, 'text': 'Rejected', 'reply_markup': markup}
                    if method == 'sendDocument':
                        data['document'] = 'keyboard-file'
                    if method == 'editMessageText':
                        data['message_id'] = original['message_id']
                    status, outcome = self.call('/bot' + self.config['token'] + '/' + method, data)
                    self.assertEqual(status, 400, outcome)
                    self.assertFalse(outcome['ok'])
                    self.assertEqual(outcome['error_code'], 400)
                    self.assertEqual(self.state()['messages'], before)

    def test_empty_arrays_and_markup_edits_match_bot_api(self):
        for markup in ({}, {'inline_keyboard': []}, {'keyboard': []}, {'inline_keyboard': [None]},
                       {'inline_keyboard': [[]]}, {'keyboard': [None]}):
            message = self.bot('sendMessage', {'chat_id': 101, 'text': 'No buttons', 'reply_markup': markup})
            self.assertNotIn('reply_markup', message)
        inline = {'inline_keyboard': [[{'text': 'A', 'callback_data': 'a'}]]}
        message = self.bot('sendMessage', {'chat_id': 101, 'text': 'Original', 'reply_markup': inline})
        for markup in ({'keyboard': [[{'text': 'Reply'}]]}, {'remove_keyboard': True}, {'force_reply': True}):
            status, _ = self.call('/bot' + self.config['token'] + '/editMessageText',
                                  {'chat_id': 101, 'message_id': message['message_id'], 'text': 'Rejected', 'reply_markup': markup})
            self.assertEqual(status, 400)
        cleared = self.bot('editMessageReplyMarkup', {'chat_id': 101, 'message_id': message['message_id'],
                                                     'reply_markup': {'inline_keyboard': []}})
        self.assertNotIn('reply_markup', cleared)
        self.assertEqual(cleared['text'], 'Original')
        before = len(self.webhook.received)
        status, _ = self.call('/qa/click', {'user': self.user1, 'chat_id': 101,
                                           'message_id': message['message_id'], 'row': 0, 'column': 0})
        self.assertEqual(status, 400)
        self.assertEqual(len(self.webhook.received), before)

    def test_reply_keyboard_is_private_state_and_cannot_be_edited(self):
        markup = {'keyboard': [[{'text': 'Reply'}]]}
        sent = self.bot('sendMessage', {'chat_id': 101, 'text': 'Choice', 'reply_markup': markup})
        self.assertNotIn('reply_markup', sent)
        reply = self.qa('click', {'user': self.user1, 'chat_id': 101,
                                  'message_id': sent['message_id'], 'row': 0, 'column': 0})['update']['message']
        self.assertEqual(reply['text'], 'Reply')
        status, _ = self.call('/bot' + self.config['token'] + '/editMessageText',
                              {'chat_id': 101, 'message_id': sent['message_id'], 'text': 'Not editable'})
        self.assertEqual(status, 400)
        stored = next(value for value in self.state()['messages']
                      if value['message_id'] == sent['message_id'] and value['chat']['id'] == 101)
        self.assertEqual(stored['text'], 'Choice')

    def test_private_webapp_and_callback_byte_limits(self):
        button = {'text': 'Open', 'web_app': {'url': 'https://qa.invalid/app'}}
        markup = {'inline_keyboard': [[button]]}
        before = self.state()['messages']
        status, _ = self.call('/bot' + self.config['token'] + '/sendMessage',
                              {'chat_id': -100123, 'text': 'Invalid group button', 'reply_markup': markup})
        self.assertEqual(status, 400)
        self.assertEqual(self.state()['messages'], before)
        for length, expected in ((32, 200), (33, 400)):
            status, _ = self.call('/bot' + self.config['token'] + '/sendMessage',
                                  {'chat_id': 101, 'text': 'UTF8 bytes', 'reply_markup':
                                   {'inline_keyboard': [[{'text': 'Callback', 'callback_data': 'я' * length}]]}})
            self.assertEqual(status, expected)

    def test_chat_commands_menu_and_typing_are_stateful_without_messages(self):
        scope = {'type': 'chat', 'chat_id': 101}
        commands = [{'command': 'qa', 'description': 'QA menu'}]
        self.assertTrue(self.bot('setMyCommands', {'scope': scope, 'commands': commands}))
        self.assertTrue(self.bot('setMyCommands', {'scope': scope, 'language_code': 'pt',
                                                 'commands': [{'command': 'ola', 'description': 'Olá'}]}))
        self.assertEqual(self.bot('getMyCommands', {'scope': scope}), commands)
        self.assertEqual(self.bot('getMyCommands', {'scope': {'type': 'chat', 'chat_id': 202}}), [])
        menu = {'type': 'web_app', 'text': 'QA', 'web_app': {'url': 'https://qa.invalid/app'}}
        self.assertTrue(self.bot('setChatMenuButton', {'chat_id': 101, 'menu_button': menu}))
        other = self.start(dict(self.config))
        path = '/bot' + self.config['token'] + '/getChatMenuButton'
        self.assertEqual(self.call(path, {'chat_id': 101}, server=other)[1]['result'], menu)
        self.assertEqual(self.bot('getMyCommands', {'scope': scope}), commands)
        self.assertTrue(self.bot('setMyCommands', {'scope': scope, 'commands': None}))
        self.assertEqual(self.bot('getMyCommands', {'scope': scope}), [])
        self.assertTrue(self.bot('deleteMyCommands', {'scope': scope, 'language_code': 'pt'}))
        self.assertEqual(self.bot('getMyCommands', {'scope': scope, 'language_code': 'pt'}), [])
        self.assertTrue(self.bot('setChatMenuButton', {'chat_id': 101, 'menu_button': {'type': 'default'}}))
        self.assertEqual(self.bot('getChatMenuButton', {'chat_id': 101}), {'type': 'default'})
        self.assertTrue(self.bot('sendChatAction', {'chat_id': 101, 'action': 'typing'}))
        self.assertEqual(self.call('/bot' + self.config['token'] + '/sendChatAction',
                                   {'chat_id': 101, 'action': 'unknown'})[0], 400)
        self.assertEqual(self.state()['messages'], [])

    def test_auth_exact_token_unknown_methods_and_independent_stores(self):
        self.assertEqual(self.call('/health', auth=False), (200, {'ok': True}))
        self.assertEqual(self.call('/qa/state', auth=False)[0], 401)
        self.assertEqual(self.call('/qa/message', {'user': self.user1, 'text': 'secret'}, auth=False)[0], 401)
        self.assertEqual(self.call('/botwrong/getMe', {})[0], 404)
        self.assertEqual(self.call('/bot' + self.config['token'] + '/deleteMessage', {})[0], 404)
        self.assertTrue(self.bot('getMe', {})['is_bot'])
        other_config = dict(self.config, database=str(Path(self.temp.name) / 'second.sqlite3'))
        other = self.start(other_config)
        self.bot('sendMessage', {'chat_id': 101, 'text': 'only first store'})
        self.assertEqual(self.call('/qa/state', server=other)[1]['messages'], [])

    def test_multiple_users_groups_buttons_edits_and_replies(self):
        first = self.qa('message', {'user': self.user1, 'text': '/start'})['update']['message']
        second = self.qa('message', {'user': self.user2, 'text': '/start'})['update']['message']
        self.assertEqual(first['message_id'], second['message_id'])
        group = {'id': -100123, 'type': 'supergroup', 'title': 'QA room'}
        incoming = self.qa('message', {'user': self.user1, 'chat': group, 'text': 'hello'})['update']['message']
        incoming2 = self.qa('message', {'user': self.user2, 'chat': group, 'text': 'reply', 'reply_to_message_id': incoming['message_id']})['update']['message']
        self.assertEqual(incoming2['reply_to_message']['from']['id'], 101)
        sent = self.bot('sendMessage', {'chat_id': group['id'], 'text': 'choose', 'reply_to_message_id': incoming2['message_id'],
                        'reply_markup': {'inline_keyboard': [[{'text': 'yes', 'callback_data': 'yes'}]]}})
        callback = self.qa('click', {'user': self.user2, 'chat_id': group['id'], 'message_id': sent['message_id'], 'row': 0, 'column': 0})['update']['callback_query']
        self.assertEqual(callback['from']['id'], 202)
        self.assertEqual(callback['message']['chat']['id'], group['id'])
        self.assertEqual(callback['message']['message_id'], sent['message_id'])
        self.assertEqual(callback['data'], 'yes')
        self.assertTrue(self.bot('answerCallbackQuery', {'callback_query_id': callback['id']}))
        edited = self.bot('editMessageText', {'chat_id': group['id'], 'message_id': sent['message_id'], 'text': 'changed', 'reply_markup': {'inline_keyboard': [[{'text': 'OK', 'callback_data': 'ok'}]]}})
        self.assertEqual(edited['text'], 'changed')
        reply = self.qa('click', {'user': self.user2, 'chat_id': group['id'], 'message_id': sent['message_id'], 'row': 0, 'column': 0})['update']['callback_query']
        self.assertEqual(reply['data'], 'ok')
        self.assertEqual(reply['message']['chat']['id'], group['id'])
        old = [event for event in self.state()['journal'] if event['kind'] == 'message_before_edit']
        self.assertEqual(old[-1]['data']['text'], 'choose')
        self.assertTrue(all(secret == 'webhook-secret' for update, secret in self.webhook.received))

    def test_sent_entities_survive_http_response_snapshot_and_original_attempt(self):
        entities = [
            {'type': 'text_link', 'offset': 3, 'length': 3, 'url': 'tg://user?id=101'},
            {'type': 'text_mention', 'offset': 9, 'length': 3, 'user': self.user2},
        ]
        request = {'chat_id': 101, 'text': '🐈 One + Two', 'entities': entities}
        sent = self.bot('sendMessage', request)
        self.assertEqual(sent['entities'], entities)
        state = self.state()
        self.assertEqual(state['messages'][0]['entities'], entities)
        attempt = next(row for row in state['attempts'] if row['method'] == 'sendMessage')
        self.assertEqual(attempt['request'], request)
        self.assertTrue(attempt['outcome']['ok'])

    def test_edit_replaces_entities_and_plain_text_clears_them_without_rewriting_history(self):
        for clear in ({}, {'entities': []}):
            with self.subTest(clear=clear):
                original = [{'type': 'bold', 'offset': 0, 'length': 3}]
                sent = self.bot('sendMessage', {'chat_id': 101, 'text': 'One', 'entities': original})
                replacement = [{'type': 'text_link', 'offset': 0, 'length': 3, 'url': 'tg://user?id=202'}]
                edited = self.bot('editMessageText', {'chat_id': 101, 'message_id': sent['message_id'],
                                                    'text': 'Two', 'entities': replacement})
                self.assertEqual(edited['entities'], replacement)
                stored = next(row for row in self.state()['messages'] if row['message_id'] == sent['message_id'])
                self.assertEqual(stored['entities'], replacement)
                plain = self.bot('editMessageText', {'chat_id': 101, 'message_id': sent['message_id'],
                                                   'text': 'Plain', **clear})
                self.assertFalse(plain.get('entities'))
                state = self.state()
                stored = next(row for row in state['messages'] if row['message_id'] == sent['message_id'])
                self.assertEqual(stored['text'], 'Plain')
                self.assertFalse(stored.get('entities'))
                history = [row['data'] for row in state['journal']
                           if row['kind'] == 'message_before_edit' and row['data']['message_id'] == sent['message_id']]
                self.assertEqual([row['entities'] for row in history], [original, replacement])

    def test_file_roundtrip_multipart_and_media(self):
        fixture = self.qa('file', {'file_id': 'fixture1', 'name': 'hello.txt', 'mime_type': 'text/plain', 'data_base64': base64.b64encode(b'hello file').decode()})
        self.assertEqual(fixture['file_size'], 10)
        for kind in ('document', 'photo', 'voice', 'video', 'video_note', 'audio', 'animation'):
            update = self.qa('media', {'user': self.user1, 'kind': kind, 'file_id': fixture['file_id']})['update']
            self.assertIn(kind, update['message'])
        file = self.bot('getFile', {'file_id': fixture['file_id']})
        url = f'http://127.0.0.1:{self.server.server_port}/file/bot{self.config["token"]}/{file["file_path"]}'
        with urlopen(url) as response:
            self.assertEqual(response.read(), b'hello file')
        boundary = 'qa-multipart-boundary'
        body = ('--' + boundary + '\r\nContent-Disposition: form-data; name="chat_id"\r\n\r\n101\r\n--' + boundary + '\r\nContent-Disposition: form-data; name="document"; filename="upload.txt"\r\nContent-Type: text/plain\r\n\r\n').encode() + b'uploaded bytes\r\n' + ('--' + boundary + '--\r\n').encode()
        request = Request(f'http://127.0.0.1:{self.server.server_port}/bot{self.config["token"]}/sendDocument', body, {'Content-Type': 'multipart/form-data; boundary=' + boundary})
        with urlopen(request) as response:
            document = json.load(response)['result']['document']
        self.assertEqual(document['file_name'], 'upload.txt')
        file = self.bot('getFile', {'file_id': document['file_id']})
        with urlopen(f'http://127.0.0.1:{self.server.server_port}/file/bot{self.config["token"]}/{file["file_path"]}') as response:
            self.assertEqual(response.read(), b'uploaded bytes')
        self.qa('fault', {'method': 'file_download', 'kind': 'file_gone'})
        with self.assertRaises(HTTPError) as failure:
            urlopen(url)
        self.assertEqual(failure.exception.code, 404)
        self.qa('fault', {'method': 'file_download', 'kind': 'file_truncated'})
        with urlopen(url) as response:
            with self.assertRaises(http.client.IncompleteRead):
                response.read()

    def test_private_keyboards_reject_another_recipient(self):
        for button, inline in [({'text': 'Confirm', 'callback_data': 'confirm'}, True),
                               ({'text': 'Reply choice'}, False),
                               ({'text': 'Open app', 'web_app': {'url': 'https://example.test/app/'}}, True)]:
            with self.subTest(button=button['text']):
                markup = {'inline_keyboard' if inline else 'keyboard': [[button]]}
                sent = self.bot('sendMessage', {'chat_id': 101, 'text': 'Private choice', 'reply_markup': markup})
                click = {'user': self.user2, 'chat_id': 101, 'message_id': sent['message_id'], 'row': 0, 'column': 0}
                received = len(self.webhook.received)
                status, _ = self.call('/qa/click', click)
                self.assertEqual(status, 403)
                self.assertEqual(len(self.webhook.received), received)
                click['user'] = self.user1
                result = self.qa('click', click)
                if 'web_app' in button:
                    self.assertEqual(result['launch_url'], button['web_app']['url'])
                elif inline:
                    self.assertEqual(result['update']['callback_query']['from']['id'], 101)
                else:
                    self.assertEqual(result['update']['message']['from']['id'], 101)

    def test_idle_tls_peer_does_not_block_other_https_requests(self):
        cert_path, key_path = Path(self.temp.name) / 'cert.pem', Path(self.temp.name) / 'key.pem'
        cert_path.write_text(TLS_CERT)
        key_path.write_text(TLS_KEY)
        config = dict(self.config, tls_cert=str(cert_path), tls_key=str(key_path))
        secure = make_server(config, '127.0.0.1', 0, store=self.server.store, tls=True)
        secure.handle_error = lambda *_: None  # Closing the deliberately idle peer is expected.
        threading.Thread(target=secure.serve_forever, daemon=True).start()
        stalled = socket.create_connection(('127.0.0.1', secure.server_port), timeout=2)
        try:
            context = ssl.create_default_context(cafile=str(cert_path))
            with urlopen(f'https://127.0.0.1:{secure.server_port}/health', context=context, timeout=1) as response:
                self.assertEqual(json.load(response), {'ok': True})
        finally:
            stalled.close()
            secure.shutdown()
            secure.server_close()

    def test_https_listener_shares_store_and_requires_qa_auth(self):
        cert_path = Path(self.temp.name) / 'cert.pem'
        key_path = Path(self.temp.name) / 'key.pem'
        cert_path.write_text(TLS_CERT)
        key_path.write_text(TLS_KEY)
        config = dict(self.config, tls_cert=str(cert_path), tls_key=str(key_path))
        secure = make_server(config, '127.0.0.1', 0, store=self.server.store, tls=True)
        threading.Thread(target=secure.serve_forever, daemon=True).start()
        self.servers.append(secure)
        context = ssl.create_default_context(cafile=str(cert_path))
        url = f'https://127.0.0.1:{secure.server_port}'
        with self.assertRaises(HTTPError) as failure:
            urlopen(url + '/qa/state', context=context)
        self.assertEqual(failure.exception.code, 401)
        self.bot('sendMessage', {'chat_id': 101, 'text': 'shared listeners'})
        request = Request(url + '/qa/state', headers={'Authorization': 'Bearer qa-secret'})
        with urlopen(request, context=context) as response:
            self.assertEqual(json.load(response)['messages'][0]['text'], 'shared listeners')
        with urlopen(url + '/bot' + self.config['token'] + '/getMe', context=context) as response:
            self.assertTrue(json.load(response)['result']['is_bot'])

    def test_concurrent_users_and_failed_webhook_outcome(self):
        group = {'id': -222, 'type': 'group', 'title': 'Concurrent QA'}
        def ingress(index):
            return self.qa('message', {'user': self.user1 if index % 2 else self.user2,
                                      'chat': group, 'text': str(index)})
        with ThreadPoolExecutor(max_workers=4) as executor:
            results = list(executor.map(ingress, range(8)))
        self.assertEqual(len({item['update']['update_id'] for item in results}), 8)
        self.assertEqual(len({item['update']['message']['message_id'] for item in results}), 8)
        self.assertEqual({item['update']['message']['from']['id'] for item in results}, {101, 202})
        self.assertEqual(self.bot('sendMessage', {'chat_id': group['id'], 'text': 'group response'})['chat'], group)
        self.webhook.status = 503
        result = self.qa('message', {'user': self.user1, 'text': 'failed delivery'})
        self.assertEqual(result['outcome'], {'status': 503})
        self.assertEqual(self.state()['updates'][0]['outcome'], {'status': 503})
        self.webhook.status = 302
        self.webhook.redirect = f'http://127.0.0.1:{self.webhook.server_port}/redirected'
        result = self.qa('message', {'user': self.user1, 'text': 'redirect must stop'})
        self.assertEqual(result['outcome'], {'status': 302})
        status, result = self.call('/bot' + self.config['token'] + '/sendMessage', {'chat_id': 101})
        self.assertEqual(status, 400)
        self.assertEqual(self.state()['attempts'][0]['outcome']['ok'], False)

    def test_state_cursor_exposes_truncation_without_losing_journal(self):
        import telegram_mock
        previous_limit = telegram_mock.STATE_LIMIT
        telegram_mock.STATE_LIMIT = 2
        try:
            self.bot('sendMessage', {'chat_id': 101, 'text': 'first'})
            self.bot('sendMessage', {'chat_id': 202, 'text': 'second'})
            self.bot('sendMessage', {'chat_id': 303, 'text': 'third'})
            state = self.state()
            self.assertTrue(state['truncated'])
            self.assertTrue(state['snapshot_truncated']['messages'])
            self.assertEqual(len(state['messages']), 2)
            events = list(state['journal'])
            while state['truncated']:
                state = self.call('/qa/state?after=' + str(state['cursor']))[1]
                events.extend(state['journal'])
            self.assertEqual([event['seq'] for event in events], list(range(1, 10)))
        finally:
            telegram_mock.STATE_LIMIT = previous_limit

    def test_miniapp_proxy_preserves_session_origin_and_errors_without_redirect(self):
        upstream = ThreadingHTTPServer(('127.0.0.1', 0), MiniApp)
        upstream.observed = []
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        self.server.store.config['app_url'] = f'http://127.0.0.1:{upstream.server_port}'
        try:
            connection = http.client.HTTPConnection('127.0.0.1', self.server.server_port, timeout=5)
            headers = {'Origin': 'https://localhost:18443', 'Cookie': 'session=old', 'Content-Type': 'application/json'}
            for path, status in (('/app/session/bootstrap?mode=qa', 200), ('/app/session/logout', 403)):
                connection.request('POST', path, b'{"init_data":"fixture"}', headers)
                response = connection.getresponse()
                self.assertEqual(response.status, status)
                self.assertEqual(response.read(), b'{"init_data":"fixture"}')
                self.assertEqual(response.headers['Content-Type'], 'application/json')
                self.assertEqual(len(response.headers.get_all('Set-Cookie')), 2)
                self.assertEqual(upstream.observed[-1], {'method': 'POST', 'path': path, 'origin': headers['Origin'],
                                                       'cookie': headers['Cookie'], 'content_type': headers['Content-Type'],
                                                       'authorization': None, 'body': b'{"init_data":"fixture"}'})
            before = len(upstream.observed)
            connection.request('GET', '/app/?test=query')
            response = connection.getresponse()
            self.assertEqual(response.status, 302)
            self.assertEqual(response.headers['Location'], '/app/redirected')
            response.read()
            self.assertEqual(len(upstream.observed), before + 1)
            connection.close()
        finally:
            upstream.shutdown()
            upstream.server_close()

    def test_miniapp_proxy_preserves_private_media_headers_on_success_and_denial(self):
        upstream = ThreadingHTTPServer(('127.0.0.1', 0), MiniApp)
        upstream.observed = []
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        self.server.store.config['app_url'] = f'http://127.0.0.1:{upstream.server_port}'
        connection = http.client.HTTPConnection('127.0.0.1', self.server.server_port, timeout=5)
        try:
            for path, status, body in (('/app/avatars/private.jpg', 200, b'private image'),
                                       ('/app/avatars/denied.jpg', 403, b'access denied')):
                with self.subTest(status=status):
                    connection.request('GET', path)
                    response = connection.getresponse()
                    self.assertEqual(response.status, status)
                    self.assertEqual(response.read(), body)
                    self.assertEqual(response.headers['Cache-Control'], 'private, no-store')
                    self.assertEqual(response.headers['Content-Disposition'], 'attachment; filename="avatar.jpg"')
                    self.assertEqual(response.headers['Content-Security-Policy'], "default-src 'none'; sandbox")
                    self.assertEqual(response.headers['X-Content-Type-Options'], 'nosniff')
                    self.assertEqual(response.headers['Referrer-Policy'], 'no-referrer')
                    self.assertEqual(int(response.headers['Content-Length']), len(body))
        finally:
            connection.close()
            upstream.shutdown()
            upstream.server_close()

    def test_miniapp_avatar_upload_preserves_operation_and_binary_body(self):
        upstream = ThreadingHTTPServer(('127.0.0.1', 0), MiniApp)
        upstream.observed = []
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        self.server.store.config['app_url'] = f'http://127.0.0.1:{upstream.server_port}'
        connection = http.client.HTTPConnection('127.0.0.1', self.server.server_port, timeout=5)
        operation = '7357d826-bb9d-4e47-85ab-32a61ba22b39'
        body = bytes([137, 80, 78, 71, 0, 255, 13, 10])
        try:
            connection.request('POST', '/app/avatars/drafts/upload', body, {
                'Origin': 'https://localhost:18443', 'Authorization': 'ZinusyaSession fixture',
                'Content-Type': 'application/octet-stream', 'X-Zinusya-Operation-Id': operation})
            response = connection.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(response.read(), body)
            self.assertEqual(upstream.observed[-1]['body'], body)
            self.assertEqual(upstream.observed[-1]['operation_id'], operation)
            self.assertEqual(upstream.observed[-1]['authorization'], 'ZinusyaSession fixture')
        finally:
            connection.close()
            upstream.shutdown()
            upstream.server_close()

    def test_miniapp_tls_proxy_authenticates_session_header_and_logout(self):
        upstream = ThreadingHTTPServer(('127.0.0.1', 0), MiniApp)
        upstream.observed = []
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        cert_path, key_path = Path(self.temp.name) / 'app-cert.pem', Path(self.temp.name) / 'app-key.pem'
        cert_path.write_text(TLS_CERT)
        key_path.write_text(TLS_KEY)
        config = dict(self.config, tls_cert=str(cert_path), tls_key=str(key_path))
        self.server.store.config['app_url'] = f'http://127.0.0.1:{upstream.server_port}'
        secure = make_server(config, '127.0.0.1', 0, store=self.server.store, tls=True)
        threading.Thread(target=secure.serve_forever, daemon=True).start()
        self.servers.append(secure)
        connection = http.client.HTTPSConnection('127.0.0.1', secure.server_port,
                                                context=ssl.create_default_context(cafile=str(cert_path)), timeout=5)
        headers = {'Authorization': 'ZinusyaSession fixture-session-token', 'Content-Type': 'application/json'}
        try:
            for method, path, sent_headers, expected in (
                    ('POST', '/app/session/bootstrap', {'Content-Type': 'application/json'}, 200),
                    ('GET', '/app/me', {}, 401),
                    ('GET', '/app/me', headers, 200),
                    ('POST', '/app/session/logout', headers, 200),
                    ('GET', '/app/me', headers, 401)):
                connection.request(method, path, b'{}' if method == 'POST' else None, sent_headers)
                response = connection.getresponse()
                self.assertEqual(response.status, expected)
                response.read()
                self.assertEqual(upstream.observed[-1]['authorization'], sent_headers.get('Authorization'))
            before = len(upstream.observed)
            connection.request('GET', '/qa/state', headers=headers)
            response = connection.getresponse()
            self.assertEqual(response.status, 401)
            response.read()
            self.assertEqual(len(upstream.observed), before)
        finally:
            connection.close()
            upstream.shutdown()
            upstream.server_close()

    def test_raw_updates_materialize_reply_targets_and_preserve_edits_on_replay(self):
        message = {'message_id': 77, 'date': 1, 'from': self.user1, 'chat': {'id': 101, 'type': 'private'}, 'text': 'raw incoming'}
        update = {'update_id': 20, 'message': message}
        self.qa('update', {'update': update})
        reply = self.bot('sendMessage', {'chat_id': 101, 'text': 'real reply', 'reply_to_message_id': 77})
        self.assertEqual(reply['message_id'], 78)
        self.assertEqual(reply['reply_to_message'], message)
        embedded = dict(message, message_id=500, text='original bot text', **{'from': self.server.store.bot})
        callback = {'update_id': 21, 'callback_query': {'id': 'raw_callback', 'from': self.user2, 'message': embedded, 'data': 'choice', 'chat_instance': '101'}}
        self.qa('update', {'update': callback})
        self.bot('editMessageText', {'chat_id': 101, 'message_id': 500, 'text': 'later edit'})
        replay = self.qa('update', {'update': callback})
        self.assertEqual(replay['update'], callback)
        state = self.state()
        self.assertEqual(next(item for item in state['messages'] if item['message_id'] == 500)['text'], 'later edit')
        conflicting = json.loads(json.dumps(callback))
        conflicting['callback_query']['message']['text'] = 'conflicting raw payload'
        result = self.qa('update', {'update': conflicting})
        self.assertEqual(result['conflicts'][0]['message_id'], 500)
        state = self.state()
        self.assertEqual(next(item for item in state['messages'] if item['message_id'] == 500)['text'], 'later edit')
        self.assertEqual(len([event for event in state['journal'] if event['kind'] == 'message_imported']), 2)
        self.assertTrue(any(event['kind'] == 'raw_message_conflict' for event in state['journal']))
        self.assertEqual(self.bot('sendMessage', {'chat_id': 101, 'text': 'next id'})['message_id'], 501)

    def test_large_unicode_state_is_byte_bounded_and_journal_exports_completely(self):
        text = '界' * 18000
        for index in range(205):
            self.bot('sendMessage', {'chat_id': 101, 'text': text + str(index)})
        state = self.state()
        self.assertLessEqual(len(json.dumps(state, ensure_ascii=False).encode()), 4 * 1024 * 1024)
        self.assertTrue(state['truncated'])
        self.assertTrue(state['snapshot_truncated']['messages'])
        self.assertTrue(state['snapshot_truncated']['attempts'])
        cursor, sequences = 0, []
        while True:
            status, page = self.call('/qa/state?snapshots=0&limit=200&after=' + str(cursor))
            self.assertEqual(status, 200)
            self.assertLessEqual(len(json.dumps(page, ensure_ascii=False).encode()), 4 * 1024 * 1024)
            self.assertEqual(page['messages'], [])
            self.assertEqual(page['attempts'], [])
            sequences.extend(event['seq'] for event in page['journal'])
            self.assertGreater(page['cursor'], cursor)
            cursor = page['cursor']
            if not page['truncated']:
                break
        self.assertEqual(sequences, list(range(1, 616)))
        self.assertEqual(self.call('/qa/state?limit=0')[0], 400)
        self.assertEqual(self.call('/qa/state?limit=201')[0], 400)
        self.assertEqual(self.call('/qa/state?snapshots=wrong')[0], 400)

    def test_ingress_event_size_and_capacity_failures_roll_back_atomically(self):
        import telegram_mock
        before = self.state()
        oversized = {'update_id': 900, 'message': {'message_id': 900, 'date': 1, 'from': self.user1,
                     'chat': {'id': 101, 'type': 'private'}, 'text': '界' * 23000}}
        self.assertEqual(self.call('/qa/update', {'update': oversized})[0], 413)
        self.assertEqual(self.state(), before)
        previous_capacity = telegram_mock.MAX_RECORDS
        telegram_mock.MAX_RECORDS = 1
        try:
            self.assertEqual(self.call('/qa/message', {'user': self.user1, 'text': 'capacity failure'})[0], 507)
            self.assertEqual(self.state(), before)
        finally:
            telegram_mock.MAX_RECORDS = previous_capacity
        received = self.qa('message', {'user': self.user1, 'text': 'after rollback'})
        self.assertEqual(received['update']['update_id'], 1)
        self.assertEqual(received['update']['message']['message_id'], 1)

    def test_init_data_signature(self):
        result = self.qa('init-data', {'user': self.user1, 'auth_date': 1700000000})
        fields = {k: v[0] for k, v in parse_qs(result['init_data']).items()}
        signature = fields.pop('hash')
        secret = hmac.new(b'WebAppData', self.config['token'].encode(), hashlib.sha256).digest()
        expected = hmac.new(secret, '\n'.join(k + '=' + fields[k] for k in sorted(fields)).encode(), hashlib.sha256).hexdigest()
        self.assertEqual(signature, expected)
        self.assertEqual(json.loads(fields['user']), self.user1)

    def test_accepted_hold_keeps_control_and_other_sends_available_then_releases_once(self):
        self.qa('fault', {'method': 'sendMessage', 'kind': 'accepted_then_hold'})
        with ThreadPoolExecutor(max_workers=1) as pool:
            response = pool.submit(self.call, '/bot' + self.config['token'] + '/sendMessage',
                                   {'chat_id': 101, 'text': 'accepted, reply held'})
            hold_id = None
            try:
                deadline = time.monotonic() + 3
                while time.monotonic() < deadline:
                    state = self.state()
                    if state['holds']:
                        hold_id = state['holds'][0]['attempt_id']
                        break
                    time.sleep(.01)
                self.assertIsNotNone(hold_id)
                self.assertFalse(response.done(), 'Accepted message must remain unacknowledged until released')
                self.assertEqual(state['messages'][0]['text'], 'accepted, reply held')
                self.assertEqual(state['attempts'][0]['id'], hold_id)
                self.assertTrue(state['attempts'][0]['outcome']['ok'])
                other = self.bot('sendMessage', {'chat_id': 202, 'text': 'independent send'})
                self.assertEqual(other['chat']['id'], 202)
                self.assertFalse(response.done())
                self.assertEqual(self.call('/qa/release', {'attempt_id': hold_id}, auth=False)[0], 401)
                reopened = self.start(self.config)
                durable = self.call('/qa/state?after=0', server=reopened)[1]
                self.assertEqual(durable['holds'][0]['attempt_id'], hold_id)
                self.assertFalse(durable['holds'][0]['released'])
                with ThreadPoolExecutor(max_workers=2) as releasers:
                    replies = list(releasers.map(lambda server: self.call('/qa/release', {'attempt_id': hold_id}, server=server),
                                                 (self.server, reopened)))
                self.assertEqual(replies, [(200, {'attempt_id': hold_id, 'released': True})] * 2)
                releases = [event for event in self.state()['journal'] if event['kind'] == 'response_released']
                self.assertEqual([event['data']['attempt_id'] for event in releases], [hold_id])
                status, result = response.result(timeout=3)
                self.assertEqual(status, 200)
                self.assertTrue(result['ok'])
                self.assertEqual(result['result']['text'], 'accepted, reply held')
                self.assertEqual(len(self.state()['attempts']), 2)
                before = self.state()
                self.qa('release', {'attempt_id': hold_id})
                self.assertEqual(self.state(), before, 'Repeated release must not journal or send another effect')
                self.assertEqual(self.call('/qa/release', {'attempt_id': hold_id + 100})[0], 404)
                self.assertEqual(self.call('/qa/release', {'attempt_id': True})[0], 400)
            finally:
                for hold in self.state().get('holds', []):
                    if not hold['released']:
                        self.qa('release', {'attempt_id': hold['attempt_id']})

    def test_hold_expiry_disconnects_after_one_accepted_effect(self):
        for seconds in (0, 61, True):
            self.assertEqual(self.call('/qa/fault', {'method': 'sendMessage', 'kind': 'accepted_then_hold',
                                                   'hold_seconds': seconds})[0], 400)
        self.qa('fault', {'method': 'sendMessage', 'kind': 'accepted_then_hold', 'hold_seconds': 1})
        with self.assertRaises((http.client.RemoteDisconnected, ConnectionError)):
            self.call('/bot' + self.config['token'] + '/sendMessage', {'chat_id': 101, 'text': 'accepted before expiry'})
        state = self.state()
        self.assertEqual(len(state['messages']), 1)
        self.assertEqual(len(state['attempts']), 1)
        self.assertEqual(state['messages'][0]['text'], 'accepted before expiry')
        self.assertTrue(any(event['kind'] == 'hold_expired' for event in state['journal']))
        hold = state['holds'][0]
        self.qa('release', {'attempt_id': hold['attempt_id']})
        self.assertEqual(len(self.state()['messages']), 1)

    def test_reject_attempt_vs_accepted_disconnect_restart_and_replay(self):
        self.qa('fault', {'method': 'sendMessage', 'kind': 'reject_429', 'retry_after': 3})
        status, error = self.call('/bot' + self.config['token'] + '/sendMessage', {'chat_id': 101, 'text': 'rejected'})
        self.assertEqual(status, 429)
        self.assertEqual(error['parameters']['retry_after'], 3)
        self.assertEqual(self.state()['messages'], [])
        self.assertEqual(self.state()['attempts'][0]['outcome']['ok'], False)
        self.qa('fault', {'method': 'sendMessage', 'kind': 'accepted_then_disconnect'})
        with self.assertRaises((http.client.RemoteDisconnected, ConnectionError)):
            self.call('/bot' + self.config['token'] + '/sendMessage', {'chat_id': 101, 'text': 'accepted'})
        self.assertEqual(self.state()['messages'][0]['text'], 'accepted')
        update = {'update_id': 12345, 'message': {'message_id': 77, 'date': 1, 'from': self.user1, 'chat': {'id': 101, 'type': 'private'}, 'text': 'exact replay'}}
        first = self.qa('update', {'update': update})
        second = self.qa('update', {'update': update})
        self.assertEqual(first['update'], second['update'])
        self.assertNotEqual(first['delivery_id'], second['delivery_id'])
        self.assertEqual(first['outcome'], {'status': 200})
        self.qa('fault', {'method': 'sendMessage', 'kind': 'reject_entities'})
        old = self.server
        old.shutdown()
        old.server_close()
        old.store.db.close()
        self.servers.remove(old)
        self.server = self.start(self.config)
        self.assertIn('accepted', [item['text'] for item in self.state()['messages']])
        self.assertEqual(len(self.state()['updates']), 2)
        status, error = self.call('/bot' + self.config['token'] + '/sendMessage', {'chat_id': 101, 'text': 'bad entities'})
        self.assertEqual(status, 400)
        message = self.bot('sendMessage', {'chat_id': 101, 'text': 'retry succeeds'})
        self.assertEqual(message['message_id'], 78)
        ingress = self.qa('message', {'user': self.user1, 'text': 'new id'})
        self.assertGreater(ingress['update']['update_id'], 12345)
        state = self.state()
        cursor = state['cursor']
        self.assertEqual(self.call('/qa/state?after=' + str(cursor))[1]['journal'], [])
        self.assertEqual([item[0]['update_id'] for item in self.webhook.received[:2]], [12345, 12345])


if __name__ == '__main__':
    unittest.main()
