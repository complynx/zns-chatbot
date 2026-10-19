"""Isolated Telegram Bot API fixture. No request is forwarded to Telegram.

Written by Codex (unverified/Codex desktop)
on behalf of Daniel Drizhuk
"""
import argparse
import base64
import hashlib
import hmac
import json
import secrets
import sqlite3
import ssl
import threading
import time
from email.parser import BytesParser
from email.policy import default
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlencode, urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener

MAX_BODY = 8 * 1024 * 1024
MAX_FILE = 4 * 1024 * 1024
STATE_LIMIT = 200
MAX_RECORDS = 10000
MAX_EVENT = 64 * 1024
STATE_BYTES = 4 * 1024 * 1024
JOURNAL_BYTES = 1024 * 1024
SNAPSHOT_BYTES = 768 * 1024


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        return None


class APIError(Exception):
    def __init__(self, description, code=400, parameters=None):
        self.description, self.code, self.parameters = description, code, parameters


def packed(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':'))


class Store:
    """One SQLite connection serializes mutations; webhook I/O holds no DB lock."""
    def __init__(self, config):
        self.config = dict(config)
        self.lock = threading.RLock()
        self.db = sqlite3.connect(config['database'], check_same_thread=False)
        self.db.execute('PRAGMA journal_mode=WAL')
        self.db.executescript('''
        CREATE TABLE IF NOT EXISTS journal(seq INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT, data TEXT);
        CREATE TABLE IF NOT EXISTS messages(chat_id INTEGER, message_id INTEGER, data TEXT, PRIMARY KEY(chat_id,message_id));
        CREATE TABLE IF NOT EXISTS attempts(id INTEGER PRIMARY KEY AUTOINCREMENT, method TEXT, request TEXT, outcome TEXT);
        CREATE TABLE IF NOT EXISTS updates(id INTEGER PRIMARY KEY AUTOINCREMENT, update_id INTEGER, data TEXT, outcome TEXT);
        CREATE TABLE IF NOT EXISTS files(id TEXT PRIMARY KEY, name TEXT, mime TEXT, body BLOB);
        CREATE TABLE IF NOT EXISTS holds(attempt_id INTEGER PRIMARY KEY, released INTEGER NOT NULL DEFAULT 0);
        CREATE TABLE IF NOT EXISTS faults(id INTEGER PRIMARY KEY AUTOINCREMENT, method TEXT, kind TEXT, remaining INTEGER, retry_after INTEGER);
        CREATE TABLE IF NOT EXISTS counters(name TEXT PRIMARY KEY, value INTEGER);
        CREATE TABLE IF NOT EXISTS raw_messages(chat_id INTEGER, message_id INTEGER, digest TEXT, PRIMARY KEY(chat_id,message_id));
        ''')
        self.db.commit()
        prefix = config['token'].split(':', 1)[0]
        self.bot = {'id': int(prefix) if prefix.isdigit() else 900000001, 'is_bot': True,
                    'first_name': 'QA Mock Bot', 'username': 'zinusya_qa_mock_bot'}

    def next_id(self, name):
        self.db.execute('INSERT INTO counters VALUES(?,1) ON CONFLICT(name) DO UPDATE SET value=value+1', (name,))
        return self.db.execute('SELECT value FROM counters WHERE name=?', (name,)).fetchone()[0]

    def journal(self, kind, data):
        encoded = packed(data)
        if len(encoded.encode()) + len(kind.encode()) + 128 > MAX_EVENT:
            raise APIError('journal event too large', 413)
        if self.db.execute('SELECT COUNT(*) FROM journal').fetchone()[0] >= MAX_RECORDS:
            raise APIError('QA journal capacity reached; use a fresh isolated volume', 507)
        self.db.execute('INSERT INTO journal(kind,data) VALUES(?,?)', (kind, encoded))

    def message(self, chat_id, message_id):
        row = self.db.execute('SELECT data FROM messages WHERE chat_id=? AND message_id=?', (int(chat_id), int(message_id))).fetchone()
        if not row:
            raise APIError('message to reference not found')
        return json.loads(row[0])

    def save_message(self, value, kind):
        self.journal(kind, value)
        self.db.execute('INSERT OR REPLACE INTO messages VALUES(?,?,?)',
                        (value['chat']['id'], value['message_id'], packed(value)))

    def import_raw_message(self, value):
        chat_id, message_id = value['chat']['id'], value['message_id']
        if not isinstance(chat_id, int) or not isinstance(message_id, int) or message_id < 1:
            raise APIError('raw message requires integer chat and positive message IDs')
        name = 'message:' + str(chat_id)
        self.db.execute('INSERT INTO counters VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=MAX(value,excluded.value)', (name, message_id))
        digest = hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        fingerprint = self.db.execute('SELECT digest FROM raw_messages WHERE chat_id=? AND message_id=?', (chat_id, message_id)).fetchone()
        if fingerprint:
            expected = fingerprint[0]
        else:
            existing = self.db.execute('SELECT data FROM messages WHERE chat_id=? AND message_id=?', (chat_id, message_id)).fetchone()
            expected = hashlib.sha256(json.dumps(json.loads(existing[0]), ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()).hexdigest() if existing else digest
        if digest != expected:
            conflict = {'chat_id': chat_id, 'message_id': message_id, 'expected_digest': expected, 'received_digest': digest}
            self.journal('raw_message_conflict', conflict)
            return conflict
        if not fingerprint:
            self.db.execute('INSERT INTO raw_messages VALUES(?,?,?)', (chat_id, message_id, digest))
            # An identical replay must not replace a subsequent materialized edit.
            if not existing:
                self.save_message(value, 'message_imported')
        return None

    def put_file(self, file_id, name, mime, body):
        if len(body) > MAX_FILE:
            raise APIError('file too large', 413)
        old = self.db.execute('SELECT length(body) FROM files WHERE id=?', (file_id,)).fetchone()
        total = self.db.execute('SELECT COALESCE(SUM(length(body)),0) FROM files').fetchone()[0]
        if total - (old[0] if old else 0) + len(body) > 32 * MAX_FILE:
            raise APIError('file storage capacity reached', 507)
        if not name or len(name) > 256 or len(file_id) > 256 or '/' in name or '\\' in name:
            raise APIError('invalid file name')
        self.db.execute('INSERT OR REPLACE INTO files VALUES(?,?,?,?)', (file_id, name, mime, body))
        return {'file_id': file_id, 'file_unique_id': hashlib.sha256(file_id.encode()).hexdigest()[:24],
                'file_name': name, 'mime_type': mime, 'file_size': len(body)}

    def file(self, file_id):
        row = self.db.execute('SELECT name,mime,body FROM files WHERE id=?', (file_id,)).fetchone()
        if not row:
            raise APIError('file not found', 404)
        return row

    def consume_fault(self, method):
        row = self.db.execute('SELECT id,kind,retry_after FROM faults WHERE method=? AND remaining>0 ORDER BY id LIMIT 1', (method,)).fetchone()
        if row:
            self.db.execute('UPDATE faults SET remaining=remaining-1 WHERE id=?', (row[0],))
            self.journal('fault_consumed', {'id': row[0], 'method': method, 'kind': row[1]})
        return row

    def bot_call(self, method, data, uploads):
        with self.lock:
            # Keep rejected attempts durable without committing a partial effect.
            with self.db:
                self.journal('attempt', {'method': method, 'request': data})
                cursor = self.db.execute('INSERT INTO attempts(method,request,outcome) VALUES(?,?,?)', (method, packed(data), 'pending'))
                attempt_id = cursor.lastrowid
                fault = self.consume_fault(method)
            disconnect = bool(fault and fault[1] == 'accepted_then_disconnect')
            hold = (attempt_id, fault[2]) if fault and fault[1] == 'accepted_then_hold' else None
            try:
                if fault:
                    if fault[1] == 'reject_429':
                        raise APIError('Too Many Requests: retry after', 429, {'retry_after': fault[2]})
                    if fault[1] == 'reject_entities':
                        raise APIError("Bad Request: can't parse entities")
                    if fault[1] == 'reject_500':
                        raise APIError('Internal Server Error', 500)
                if hold and self.db.execute('SELECT count(*) FROM holds WHERE released=0').fetchone()[0] >= 16:
                    raise APIError('release outstanding holds before another held send', 429)
                with self.db:
                    result = self.effect(method, data, uploads)
                    if hold:
                        self.db.execute('INSERT INTO holds(attempt_id) VALUES(?)', (attempt_id,))
                        self.journal('response_held', {'attempt_id': attempt_id, 'hold_seconds': hold[1]})
                    self.journal('result', {'attempt_id': attempt_id, 'result': result, 'disconnected': disconnect})
                    self.db.execute('UPDATE attempts SET outcome=? WHERE id=?', (packed({'ok': True, 'disconnected': disconnect}), attempt_id))
                return result, disconnect, hold
            except (APIError, ValueError, TypeError, KeyError, IndexError) as error:
                if not isinstance(error, APIError):
                    error = APIError('invalid Bot API request')
                with self.db:
                    self.db.execute('UPDATE attempts SET outcome=? WHERE id=?', (packed({'ok': False, 'error_code': error.code, 'description': error.description}), attempt_id))
                    self.journal('rejected', {'attempt_id': attempt_id, 'error_code': error.code})
                raise error

    def wait_hold(self, attempt_id, seconds):
        # Waiting never holds the SQLite lock; control and independent sends stay usable.
        deadline = time.monotonic() + seconds
        while True:
            with self.lock:
                if self.db.execute('SELECT released FROM holds WHERE attempt_id=?', (attempt_id,)).fetchone()[0]:
                    return True
            if time.monotonic() >= deadline:
                with self.lock, self.db:
                    self.journal('hold_expired', {'attempt_id': attempt_id})
                return False
            time.sleep(.05)

    def effect(self, method, data, uploads):
        if method == 'getMe':
            return self.bot
        if method == 'answerCallbackQuery':
            self.journal('callback_answer', data)
            return True
        if method == 'getFile':
            file_id = str(data['file_id'])
            name, mime, body = self.file(file_id)
            return {'file_id': file_id, 'file_unique_id': hashlib.sha256(file_id.encode()).hexdigest()[:24],
                    'file_size': len(body), 'file_path': 'fixtures/' + file_id}
        if method == 'editMessageText':
            old = self.message(data['chat_id'], data['message_id'])
            value = dict(old, text=str(data['text']), edit_date=int(time.time()))
            value.pop('entities', None)
            if data.get('entities'):
                value['entities'] = data['entities']
            value.pop('reply_markup', None)
            if 'reply_markup' in data:
                value['reply_markup'] = data['reply_markup']
            self.journal('message_before_edit', old)
            self.save_message(value, 'message_edited')
            return value
        if method not in ('sendMessage', 'sendDocument'):
            raise APIError('unsupported mock Bot API method: ' + method, 404)
        chat_id = int(data['chat_id'])
        previous = self.db.execute('SELECT data FROM messages WHERE chat_id=? ORDER BY message_id DESC LIMIT 1', (chat_id,)).fetchone()
        chat = json.loads(previous[0])['chat'] if previous else {'id': chat_id, 'type': 'private' if chat_id > 0 else 'supergroup'}
        value = {'message_id': self.next_id('message:' + str(chat_id)), 'date': int(time.time()),
                 'from': self.bot, 'chat': chat}
        if method == 'sendMessage':
            value['text'] = str(data['text'])
            if data.get('entities'):
                value['entities'] = data['entities']
        else:
            field = data.get('document', '')
            upload = uploads.get('document') or uploads.get(str(field).removeprefix('attach://'))
            if upload:
                file_id = 'upload_' + secrets.token_hex(12)
                value['document'] = self.put_file(file_id, *upload)
            else:
                file_id = str(field)
                value['document'] = self.put_file(file_id, *self.file(file_id))
            if 'caption' in data:
                value['caption'] = data['caption']
        if 'reply_markup' in data:
            value['reply_markup'] = data['reply_markup']
        reply_id = data.get('reply_to_message_id') or data.get('reply_parameters', {}).get('message_id')
        if reply_id:
            value['reply_to_message'] = self.message(chat_id, reply_id)
        self.save_message(value, 'message_sent')
        return value

    def incoming_message(self, data, media=False):
        user = data['user']
        if not isinstance(user, dict) or not isinstance(user.get('id'), int) or not user.get('first_name'):
            raise APIError('user requires integer id and first_name')
        chat = data.get('chat') or {'id': user['id'], 'type': 'private'}
        if not isinstance(chat.get('id'), int) or chat.get('type') not in ('private', 'group', 'supergroup'):
            raise APIError('invalid chat')
        value = {'message_id': self.next_id('message:' + str(chat['id'])), 'date': int(time.time()), 'from': dict(user, is_bot=False), 'chat': chat}
        if media:
            kind, file_id = data['kind'], data['file_id']
            if kind not in ('document', 'photo', 'voice', 'video', 'video_note', 'audio', 'animation'):
                raise APIError('unsupported media kind')
            name, mime, body = self.file(file_id)
            item = {'file_id': file_id, 'file_unique_id': hashlib.sha256(file_id.encode()).hexdigest()[:24], 'file_size': len(body)}
            if kind == 'photo':
                item.update(width=1, height=1)
                value[kind] = [item]
            else:
                item.update(file_name=name, mime_type=mime)
                if kind in ('voice', 'audio', 'video', 'video_note', 'animation'):
                    item['duration'] = 1
                if kind in ('video', 'animation'):
                    item.update(width=1, height=1)
                if kind == 'video_note':
                    item['length'] = 1
                value[kind] = item
            if 'caption' in data:
                value['caption'] = data['caption']
        else:
            value['text'] = str(data['text'])
        if data.get('reply_to_message_id'):
            value['reply_to_message'] = self.message(chat['id'], data['reply_to_message_id'])
        self.save_message(value, 'message_received')
        return {'update_id': self.update_id(data), 'message': value}

    def update_id(self, data):
        value = int(data['update_id']) if 'update_id' in data else self.next_id('update')
        self.db.execute('INSERT INTO counters VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=MAX(value,excluded.value)', ('update', value))
        return value

    def forward(self, update, row_id):
        # Intent and generated messages are committed together before webhook I/O.
        request = Request(self.config['webhook_url'], packed(update).encode(),
                          {'Content-Type': 'application/json', 'X-Telegram-Bot-Api-Secret-Token': self.config['webhook_secret']})
        try:
            opener = build_opener(ProxyHandler({}), NoRedirect())
            with opener.open(request, timeout=10) as response:
                response.read(4096)
                outcome = {'status': response.status}
        except HTTPError as error:
            error.close()
            outcome = {'status': error.code}
        except Exception as error:
            outcome = {'error': type(error).__name__}
        with self.lock, self.db:
            self.db.execute('UPDATE updates SET outcome=? WHERE id=?', (packed(outcome), row_id))
            self.journal('update_outcome', {'delivery_id': row_id, 'update_id': update['update_id'], 'outcome': outcome})
        return {'update': update, 'delivery_id': row_id, 'outcome': outcome}

    def qa(self, action, data):
        conflicts = []
        with self.lock, self.db:
            if action == 'file':
                body = base64.b64decode(data['data_base64'], validate=True)
                result = self.put_file(data.get('file_id') or 'fixture_' + secrets.token_hex(12), data['name'], data.get('mime_type', 'application/octet-stream'), body)
                self.journal('file_registered', result)
                return result
            if action == 'fault':
                if data['kind'] not in ('reject_429', 'reject_entities', 'reject_500', 'accepted_then_disconnect', 'accepted_then_hold', 'file_gone', 'file_truncated'):
                    raise APIError('unsupported fault')
                count = int(data.get('count', 1))
                if not 1 <= count <= 100:
                    raise APIError('count must be between 1 and 100')
                delay = max(1, int(data.get('retry_after', 1)))
                if data['kind'] == 'accepted_then_hold':
                    delay = data.get('hold_seconds', 60)
                    if data['method'] not in ('sendMessage', 'sendDocument') or type(delay) is not int or not 1 <= delay <= 60:
                        raise APIError('held sends require sendMessage/sendDocument and hold_seconds in 1..60')
                self.db.execute('INSERT INTO faults(method,kind,remaining,retry_after) VALUES(?,?,?,?)', (data['method'], data['kind'], count, delay))
                self.journal('fault_registered', data)
                return True
            if action == 'release':
                attempt_id = data.get('attempt_id')
                if type(attempt_id) is not int or attempt_id <= 0:
                    raise APIError('positive integer attempt_id required')
                row = self.db.execute('SELECT released FROM holds WHERE attempt_id=?', (attempt_id,)).fetchone()
                if row is None:
                    raise APIError('held attempt not found', 404)
                changed = self.db.execute('UPDATE holds SET released=1 WHERE attempt_id=? AND released=0', (attempt_id,))
                if changed.rowcount == 1:
                    self.journal('response_released', {'attempt_id': attempt_id})
                return {'attempt_id': attempt_id, 'released': True}
            if action == 'init-data':
                fields = {'user': packed(data['user']), 'auth_date': str(data.get('auth_date', int(time.time()))), 'query_id': secrets.token_hex(12)}
                secret = hmac.new(b'WebAppData', self.config['token'].encode(), hashlib.sha256).digest()
                fields['hash'] = hmac.new(secret, '\n'.join(k + '=' + fields[k] for k in sorted(fields)).encode(), hashlib.sha256).hexdigest()
                return {'init_data': urlencode(fields)}
            if action == 'update':
                update = data['update']
                self.update_id(update)
                self.journal('update_received', update)
                candidates = [update.get('message'), update.get('callback_query', {}).get('message')]
                for value in candidates:
                    if value is not None:
                        conflict = self.import_raw_message(value)
                        if conflict:
                            conflicts.append(conflict)
            elif action in ('message', 'media'):
                update = self.incoming_message(data, media=action == 'media')
            elif action == 'click':
                message = self.message(data['chat_id'], data['message_id'])
                markup = message.get('reply_markup', {})
                inline = 'inline_keyboard' in markup
                keyboard = markup.get('inline_keyboard', markup.get('keyboard', []))
                row, column = int(data['row']), int(data['column'])
                if row < 0 or column < 0:
                    raise APIError('button index must be nonnegative')
                button = keyboard[row][column]
                if isinstance(button, str):
                    button = {'text': button}
                if 'web_app' in button:
                    return {'launch_url': button['web_app']['url'], 'chat_id': message['chat']['id'], 'message_id': message['message_id']}
                if inline:
                    if 'callback_data' not in button:
                        raise APIError('button has no callback_data')
                    update = {'update_id': self.update_id(data), 'callback_query': {'id': 'qa_' + secrets.token_hex(12), 'from': data['user'], 'chat_instance': str(message['chat']['id']), 'message': message, 'data': button['callback_data']}}
                else:
                    update = self.incoming_message(dict(data, chat=message['chat'], text=button['text']))
            else:
                raise APIError('unknown QA endpoint', 404)
            self.journal('update_intent', update)
            row_id = self.db.execute('INSERT INTO updates(update_id,data,outcome) VALUES(?,?,?)', (update['update_id'], packed(update), 'pending')).lastrowid
        result = self.forward(update, row_id)
        if conflicts:
            result['conflicts'] = conflicts
        return result

    @staticmethod
    def bounded_page(rows, convert, limit, byte_budget):
        values, used = [], 2
        for row in rows:
            value = convert(row)
            size = len(packed(value).encode()) + (1 if values else 0)
            if len(values) >= limit or used + size > byte_budget:
                return values, True
            values.append(value)
            used += size
        return values, False

    def state(self, after, snapshots=True, limit=None):
        limit = STATE_LIMIT if limit is None else limit
        if not 1 <= limit <= 200 or after < 0:
            raise APIError('state requires after>=0 and limit between 1 and 200')
        with self.lock:
            rows = self.db.execute('SELECT seq,kind,data FROM journal WHERE seq>? ORDER BY seq LIMIT ?', (after, limit + 1))
            events, truncated = self.bounded_page(rows, lambda row: {'seq': row[0], 'kind': row[1], 'data': json.loads(row[2])},
                                                 limit, JOURNAL_BYTES if snapshots else STATE_BYTES - 1024)
            state = {'journal': events, 'cursor': events[-1]['seq'] if events else after, 'truncated': truncated,
                     'messages': [], 'attempts': [], 'updates': [], 'holds': [], 'snapshot_truncated': {}}
            queries = {
                'holds': ('SELECT attempt_id,released FROM holds ORDER BY attempt_id DESC LIMIT ?',
                          lambda row: {'attempt_id': row[0], 'released': bool(row[1])}),
                'messages': ('SELECT data FROM messages ORDER BY rowid DESC LIMIT ?', lambda row: json.loads(row[0])),
                'attempts': ('SELECT id,method,request,outcome FROM attempts ORDER BY id DESC LIMIT ?',
                             lambda row: {'id': row[0], 'method': row[1], 'request': json.loads(row[2]), 'outcome': json.loads(row[3]) if row[3] != 'pending' else row[3]}),
                'updates': ('SELECT id,update_id,data,outcome FROM updates ORDER BY id DESC LIMIT ?',
                            lambda row: {'delivery_id': row[0], 'update_id': row[1], 'update': json.loads(row[2]), 'outcome': json.loads(row[3]) if row[3] != 'pending' else row[3]}),
            }
            for section, (query, convert) in queries.items():
                if snapshots:
                    values, omitted = self.bounded_page(self.db.execute(query, (limit + 1,)), convert, limit, SNAPSHOT_BYTES)
                    state[section] = values
                else:
                    omitted = self.db.execute(query, (1,)).fetchone() is not None
                state['snapshot_truncated'][section] = omitted
            return state


class Handler(BaseHTTPRequestHandler):
    server_version = 'TelegramQAMock/1'

    def log_message(self, *args):
        pass  # URLs contain bot tokens. Never use HTTP request logging.

    def reply(self, status, data):
        body = packed(data).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def body(self):
        if self.headers.get('Transfer-Encoding'):
            raise APIError('chunked requests unsupported', 411)
        length = int(self.headers.get('Content-Length', 0))
        if not 0 <= length <= MAX_BODY:
            raise APIError('request body too large', 413)
        self.connection.settimeout(15)
        raw = self.rfile.read(length)
        if len(raw) != length:
            raise APIError('incomplete body')
        content_type = self.headers.get('Content-Type', '')
        if content_type.startswith('multipart/form-data'):
            message = BytesParser(policy=default).parsebytes(('Content-Type: ' + content_type + '\r\nMIME-Version: 1.0\r\n\r\n').encode() + raw)
            data, uploads = {}, {}
            for part in message.iter_parts():
                name = part.get_param('name', header='content-disposition')
                payload = part.get_payload(decode=True)
                if part.get_filename():
                    uploads[name] = (part.get_filename(), part.get_content_type(), payload)
                else:
                    data[name] = payload.decode()
        elif content_type.startswith('application/x-www-form-urlencoded'):
            data, uploads = {k: v[-1] for k, v in parse_qs(raw.decode(), keep_blank_values=True).items()}, {}
        else:
            data, uploads = json.loads(raw or b'{}'), {}
        for key in ('reply_markup', 'reply_parameters'):
            if isinstance(data.get(key), str):
                data[key] = json.loads(data[key])
        return data, uploads

    def do_GET(self):
        self.handle_request()

    def do_POST(self):
        self.handle_request()

    do_HEAD = do_POST
    do_OPTIONS = do_POST
    do_PUT = do_POST
    do_PATCH = do_POST
    do_DELETE = do_POST

    def proxy_app(self):
        # Only this fixed isolated origin can receive MiniApp requests.
        if self.headers.get('Transfer-Encoding'):
            raise APIError('chunked requests unsupported', 411)
        length = int(self.headers.get('Content-Length', 0))
        if not 0 <= length <= MAX_BODY:
            raise APIError('request body too large', 413)
        self.connection.settimeout(15)
        body = self.rfile.read(length) if length else None
        if body is not None and len(body) != length:
            raise APIError('incomplete body')
        headers = {name: self.headers[name] for name in ('Origin', 'Cookie', 'Content-Type', 'Accept', 'Authorization', 'X-Zinusya-Operation-Id') if self.headers.get(name) is not None}
        request = Request(self.server.store.config.get('app_url', 'http://shuttle:8080') + self.path, body, headers, method=self.command)
        opener = build_opener(ProxyHandler({}), NoRedirect())
        try:
            try:
                response = opener.open(request, timeout=10)
            except HTTPError as error:
                response = error
            with response:
                payload = response.read(MAX_BODY + 1)
                if len(payload) > MAX_BODY:
                    raise APIError('MiniApp response too large', 502)
                self.send_response(response.status)
                for name in ('Content-Type', 'Set-Cookie', 'Location'):
                    for value in response.headers.get_all(name, []):
                        self.send_header(name, value)
                self.send_header('Content-Length', str(len(payload)))
                self.end_headers()
                if self.command != 'HEAD':
                    self.wfile.write(payload)
        except APIError:
            raise
        except (OSError, HTTPError):
            raise APIError('isolated MiniApp unavailable', 502)

    def handle_request(self):
        store = self.server.store
        path = urlsplit(self.path)
        try:
            if path.path.startswith('/app/'):
                return self.proxy_app()
            if path.path == '/health' and self.command == 'GET':
                return self.reply(200, {'ok': True})
            if path.path.startswith('/qa/'):
                expected = 'Bearer ' + store.config['qa_token']
                if not hmac.compare_digest(self.headers.get('Authorization', ''), expected):
                    raise APIError('unauthorized', 401)
                if path.path == '/qa/state' and self.command == 'GET':
                    query = parse_qs(path.query)
                    after = int(query.get('after', ['0'])[0])
                    snapshots = query.get('snapshots', ['1'])[0]
                    if snapshots not in ('0', '1'):
                        raise APIError('snapshots must be 0 or 1')
                    limit = int(query['limit'][0]) if 'limit' in query else None
                    return self.reply(200, store.state(after, snapshots=snapshots == '1', limit=limit))
                if self.command != 'POST':
                    raise APIError('method not allowed', 405)
                data, uploads = self.body()
                return self.reply(200, store.qa(path.path[4:], data))
            prefix = '/bot' + store.config['token'] + '/'
            file_prefix = '/file/bot' + store.config['token'] + '/fixtures/'
            if path.path.startswith(file_prefix) and self.command == 'GET':
                with store.lock, store.db:
                    fault = store.consume_fault('file_download')
                    name, mime, body = store.file(path.path[len(file_prefix):])
                if fault and fault[1] == 'file_gone':
                    raise APIError('file no longer available', 404)
                self.send_response(200)
                self.send_header('Content-Type', mime)
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body[:len(body)//2] if fault and fault[1] == 'file_truncated' else body)
                return
            if not path.path.startswith(prefix):
                raise APIError('not found', 404)
            data, uploads = self.body() if self.command == 'POST' else ({k: v[-1] for k, v in parse_qs(path.query).items()}, {})
            result, disconnect, hold = store.bot_call(path.path[len(prefix):], data, uploads)
            if hold and not store.wait_hold(*hold):
                disconnect = True
            if disconnect:
                self.close_connection = True
                self.connection.close()
                return
            try:
                self.reply(200, {'ok': True, 'result': result})
            except OSError:
                self.close_connection = True  # The caller may have exited while its acknowledgment was held.
        except APIError as error:
            self.reply(error.code, dict({'ok': False, 'error_code': error.code, 'description': error.description}, **({'parameters': error.parameters} if error.parameters else {})))
        except (ValueError, TypeError, KeyError, IndexError):
            self.reply(400, {'ok': False, 'error_code': 400, 'description': 'invalid request'})


def make_server(config, host='0.0.0.0', port=8080, store=None, tls=False):
    server = ThreadingHTTPServer((host, port), Handler)
    server.daemon_threads = True
    server.store = store or Store(config)
    if tls:
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(config['tls_cert'], config['tls_key'])
        server.socket = context.wrap_socket(server.socket, server_side=True)
    return server


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    with open(args.config, encoding='utf-8') as source:
        config = json.load(source)
    if config['webhook_url'] != 'http://shuttle:8080/telegram/webhook':
        raise SystemExit('webhook_url must target the isolated shuttle service')
    if config.get('app_url', 'http://shuttle:8080') != 'http://shuttle:8080':
        raise SystemExit('app_url must target the isolated shuttle service')
    if not config.get('qa_token') or not config.get('webhook_secret'):
        raise SystemExit('QA token and webhook secret are required')
    store = Store(config)
    plain = make_server(config, store=store)
    secure = make_server(config, port=8443, store=store, tls=True)
    threading.Thread(target=plain.serve_forever, daemon=True).start()
    try:
        secure.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        plain.shutdown()
        plain.server_close()
        secure.server_close()
        store.db.close()


if __name__ == '__main__':
    main()
