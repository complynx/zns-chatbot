"""Owned local ingress. Retain updates before acknowledgment; start HELD.

Only Telegram reads and exact Shuttle inbox submissions may retry. The latter
use Shuttle's durable (bot_id, update_id) deduplication, never model replay.
"""
import argparse
import hashlib
import http.client
import json
import os
import signal
import ssl
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

LIMIT = 4 << 20
READ_METHODS = ('getMe', 'getWebhookInfo', 'getUpdates')


def atomic(path, value):
    temporary = path.with_suffix('.next')
    with temporary.open('w', encoding='utf-8') as stream:
        json.dump(value, stream)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    if os.name == 'nt':
        return  # Directory fsync is used by the Linux service.
    descriptor = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def commit(root, state, **changes):
    updated = {**state, **changes}
    atomic(root / 'journal.json', updated)
    state.update(changes)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_):
        return None


class Paused(Exception):
    pass


def post(opener, url, body, headers=None, timeout=40):
    request = urllib.request.Request(url, data=json.dumps(body).encode(),
        headers={'Content-Type': 'application/json', **(headers or {})})
    with opener.open(request, timeout=timeout) as response:
        raw = response.read(LIMIT + 1)
        if len(raw) > LIMIT:
            raise ValueError('Receiver response exceeds limit')
        return json.loads(raw)


def retry_delay(error, attempt):
    delay = min(30, 2 ** min(attempt, 5))
    if isinstance(error, urllib.error.HTTPError):
        if error.code not in (408, 429, 500, 502, 503, 504):
            error.close()
            return None
        retry = error.headers.get('Retry-After', '') if error.headers else ''
        if error.code == 429:
            try:
                retry = json.loads(error.read(4096)).get('parameters', {}).get('retry_after', retry)
            except (ValueError, OSError):
                pass
        error.close()
        if str(retry).isdecimal():
            return max(delay, int(retry)) if int(retry) <= 3600 else None
        return delay
    reason = error.reason if isinstance(error, urllib.error.URLError) else error
    if isinstance(reason, ssl.SSLError) and not isinstance(reason, ssl.SSLEOFError):
        return None
    if isinstance(error, urllib.error.URLError) and isinstance(reason, OSError):
        return delay
    if isinstance(error, (TimeoutError, ConnectionError, http.client.IncompleteRead)):
        return delay
    if isinstance(error, (json.JSONDecodeError, UnicodeDecodeError)) and attempt < 2:
        return delay
    return None


def retry_post(opener, url, body, method, authorized, headers=None, timeout=40):
    if method not in (*READ_METHODS, 'Shuttle-webhook'):
        raise ValueError('No retry contract for this operation')
    attempt = 0
    while True:
        if not authorized():
            raise Paused()
        try:
            return post(opener, url, body, headers, timeout)
        except Exception as error:
            delay = retry_delay(error, attempt)
            if delay is None:
                raise
            print(json.dumps({'event': 'receiver_retry', 'method': method,
                'error_class': type(error).__name__, 'status': getattr(error, 'code', 0),
                'retry_seconds': delay, 'attempt': attempt + 1}), flush=True)
            # Observe a hold promptly, even during a long Telegram RetryAfter.
            end = time.monotonic() + delay
            while time.monotonic() < end:
                if not authorized():
                    raise Paused()
                time.sleep(min(.5, max(0, end - time.monotonic())))
            attempt += 1


def eligible(update, users):
    callback = update.get('callback_query')
    message = callback.get('message', {}) if callback else update.get('message', {})
    user = callback.get('from', {}) if callback else message.get('from', {})
    chat = message.get('chat', {})
    uid = user.get('id')
    return (type(uid) is int and uid in users and not user.get('is_bot', False)
        and chat.get('type') == 'private' and chat.get('id') == uid)


def valid_control(control, session):
    lifetime = control.get('unbounded') is True or time.time() < control.get('expires_unix', 0)
    return (control.get('armed') is True and control.get('session') == session
        and bool(control.get('authorization_reference')) and lifetime
        and control.get('mode') in ('capture', 'forward'))


def webhook_key(config):
    return hashlib.sha256((config['webhook_url'] + '\n' + config['webhook_secret']).encode()).hexdigest()


def recover(root, state, config):
    pending = state.get('pending')
    if not pending:
        return state
    method = pending.get('method')
    legacy = state.get('frozen') in (None, 'receiver_transport_or_contract_requires_inspection',
        'orphaned_transport_unknown')
    if method in READ_METHODS and legacy:
        if method == 'getUpdates' and pending.get('offset') != state['next_offset']:
            raise ValueError('Poll offset differs from durable cursor')
    elif method == 'Shuttle-webhook' and legacy:
        record = json.loads((root / ('update-' + str(pending['update_id']) + '.json')).read_bytes())
        if (record['outcome'] not in ('forward_pending', 'forwarded')
                or record.get('webhook_key') != webhook_key(config)):
            raise ValueError('Legacy or changed webhook receipt requires reconciliation')
    else:
        return {**state, 'frozen': state.get('frozen') or 'orphaned_transport_unknown'}
    return {**state, 'pending': None, 'frozen': None, 'identity_checked': False}


def accept_update(root, state, config, control, update, opener, authorized, batch):
    uid = update['update_id']
    if type(uid) is not int or uid < state['next_offset']:
        raise ValueError('Invalid Telegram update order')
    path = root / ('update-' + str(uid) + '.json')
    if path.exists():
        record = json.loads(path.read_bytes())
        if record['update'] != update:
            raise ValueError('Retained update changed')
    else:
        active = authorized()
        if active != control:
            raise Paused()
        forward = (control['mode'] == 'forward' and uid > control['forward_after_update_id']
            and eligible(update, config['allowed_users']))
        record = {'update': update, 'outcome': 'forward_pending' if forward else 'captured', 'batch': batch}
        if forward:
            record['webhook_key'] = webhook_key(config)
        atomic(path, record)
        commit(root, state, retained=state['retained'] + 1)
    if record['outcome'] == 'forward_pending':
        active = authorized()
        if not active or active['mode'] != 'forward':
            raise Paused()
        if (record.get('webhook_key') != webhook_key(config)
                or not eligible(update, config['allowed_users']) or uid <= active['forward_after_update_id']):
            raise ValueError('Forwarding authority changed')
        commit(root, state, pending={'method': 'Shuttle-webhook', 'update_id': uid})
        result = retry_post(opener, config['webhook_url'], update, 'Shuttle-webhook',
            lambda: authorized() == active,
            {'X-Telegram-Bot-Api-Secret-Token': config['webhook_secret']}, timeout=10)
        if result.get('ignored') is not True:
            try:
                uuid.UUID(result['task_id'])
            except (KeyError, TypeError, ValueError, AttributeError):
                raise ValueError('Invalid durable inbox acknowledgment') from None
        record['outcome'] = 'forwarded'
        atomic(path, record)
        commit(root, state, forwarded=state['forwarded'] + 1, pending=None)
    if record['outcome'] not in ('forwarded', 'captured'):
        raise ValueError('Legacy unclassified receipt requires reconciliation')
    commit(root, state, next_offset=uid + 1)


def retained_updates(root, offset):
    # A crash can happen after batch fsync but before the first receipt. Consume
    # retained input before polling; Telegram's upstream retention is finite.
    retained = {}
    def add(update, batch):
        uid = update['update_id']
        if type(uid) is not int:
            raise ValueError('Invalid retained update identifier')
        if uid < offset:
            return
        if uid in retained and retained[uid]['update'] != update:
            raise ValueError('Retained batch changed')
        retained[uid] = {'update': update, 'batch': batch}
    for path in root.glob('batch-*.json'):
        updates = json.loads(path.read_bytes())
        if not isinstance(updates, list) or len(updates) > 100:
            raise ValueError('Invalid retained Telegram batch')
        for update in updates:
            add(update, path.name)
    for path in root.glob('update-*.json'):
        record = json.loads(path.read_bytes())
        add(record['update'], record['batch'])
    return [retained[uid] for uid in sorted(retained)]


def main():
    import fcntl  # The service runs in Linux; pure helpers are testable elsewhere.
    parser = argparse.ArgumentParser()
    parser.add_argument('--state', required=True)
    parser.add_argument('--config', required=True)
    args = parser.parse_args()
    root = Path(args.state)
    lock = (root / 'process.lock').open('a')
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    config = json.loads(Path(args.config).read_bytes())
    assert config['bot_id'] == 6087685431
    assert config['allowed_users'] and all(type(x) is int and 0 < x < 1 << 52 for x in config['allowed_users'])
    assert config['token'].split(':', 1)[0] == str(config['bot_id'])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(),
        urllib.request.HTTPSHandler(context=ssl.create_default_context()))
    base = 'https://api.telegram.org/bot' + config['token'] + '/'
    journal = root / 'journal.json'
    state = json.loads(journal.read_bytes()) if journal.exists() else {
        'next_offset': config['initial_offset'], 'pending': None, 'frozen': None,
        'retained': 0, 'forwarded': 0, 'identity_checked': False}
    state = recover(root, state, config)
    state['identity_checked'] = False
    atomic(journal, state)
    control_path = root / 'authorization.json'
    atomic(control_path, {'armed': False})
    atomic(root / 'startup-ready.json', {'session': config['session'],
        'started_unix': time.time(), 'unbounded_supported': True})

    def authorized():
        control = json.loads(control_path.read_bytes())
        return control if valid_control(control, config['session']) else None

    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(SystemExit(0)))
    while True:
        control = authorized()
        if state['frozen'] or not control:
            time.sleep(.5)
            continue
        try:
            if not state['identity_checked']:
                for method in ('getMe', 'getWebhookInfo'):
                    commit(root, state, pending={'method': method})
                    response = retry_post(opener, base + method, {}, method, lambda: authorized() == control)
                    assert response['ok']
                    assert response['result']['id'] == config['bot_id'] if method == 'getMe' else not response['result']['url']
                    commit(root, state, pending=None)
                commit(root, state, identity_checked=True)
            pending = retained_updates(root, state['next_offset'])
            if pending:
                for record in sorted(pending, key=lambda r: r['update']['update_id']):
                    accept_update(root, state, config, control, record['update'], opener, authorized, record['batch'])
                continue
            commit(root, state, pending={'method': 'getUpdates', 'offset': state['next_offset']})
            response = retry_post(opener, base + 'getUpdates', {'offset': state['next_offset'], 'limit': 100,
                'timeout': 25, 'allowed_updates': []}, 'getUpdates', lambda: authorized() == control)
            assert response['ok'] and isinstance(response['result'], list) and len(response['result']) <= 100
            updates = response['result']
            digest = hashlib.sha256(json.dumps(updates, sort_keys=True).encode()).hexdigest()
            batch = root / ('batch-' + digest + '.json')
            if not batch.exists():
                atomic(batch, updates)
            commit(root, state, pending=None)
            for update in updates:
                accept_update(root, state, config, control, update, opener, authorized, batch.name)
        except Paused:
            time.sleep(.5)
        except Exception as error:
            print(json.dumps({'event': 'receiver_held', 'method': (state.get('pending') or {}).get('method'),
                'error_class': type(error).__name__, 'status': getattr(error, 'code', 0)}), flush=True)
            commit(root, state, frozen='receiver_permanent_requires_inspection')


if __name__ == '__main__':
    main()
