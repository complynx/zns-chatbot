"""Operate one named QA stand without exposing its fake-bot/control credentials."""
import argparse
import base64
import json
import os
import re
import tempfile
import urllib.error
import urllib.request
import uuid
from pathlib import Path

import stand

LIMIT = 8 * 1024 * 1024


def read_json(path):
    raw = path.read_bytes()
    if len(raw) > LIMIT:
        raise ValueError('JSON file exceeds 8 MiB')
    return json.loads(raw)


def request(state, manifest, action, body=None, after=0, snapshots=True):
    credentials = read_json(state / 'credentials.json')
    path = '/qa/state?after=' + str(after) + '&snapshots=' + ('1' if snapshots else '0') if action == 'state' else '/qa/' + action
    req = urllib.request.Request(manifest['urls']['mock'] + path,
        headers={'Authorization': 'Bearer ' + credentials['qa_token'], 'Content-Type': 'application/json'},
        data=None if body is None else json.dumps(body).encode())
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(req, timeout=25) as response:
        raw = response.read(LIMIT + 1)
    if len(raw) > LIMIT:
        raise ValueError('QA response exceeds 8 MiB; consume a smaller transcript page')
    return json.loads(raw)


def model_request(state, request_id):
    if not re.fullmatch('[a-f0-9]{64}', request_id):
        if str(uuid.UUID(request_id)) != request_id:
            raise ValueError('Expected the exact mailbox UUID or SHA-256 request ID')
    path = state / 'mailbox' / (request_id + '.request.json')
    return read_json(path)


def answer_model(state, request_id, response_file):
    original = model_request(state, request_id)
    value = read_json(response_file)
    usage = value.get('Usage', {})
    if not value.get('ID') or any(type(usage.get(k)) is not int or usage[k] < 0 for k in ('InputTokens', 'OutputTokens', 'CachedInputTokens', 'CacheWriteTokens')):
        raise ValueError('Response requires ID and four nonnegative synthetic Usage counters')
    if usage['CachedInputTokens'] + usage['CacheWriteTokens'] > usage['InputTokens'] or usage['OutputTokens'] > original['MaxOutputTokens']:
        raise ValueError('Synthetic counters exceed request limits')
    known = {t['name'] for t in original.get('Tools') or [] if t.get('type') == 'function'}
    ids = set()
    for call in value.get('Calls') or []:
        if not call.get('ID') or call['ID'] in ids or call.get('Name') not in known or not isinstance(call.get('Arguments'), dict):
            raise ValueError('Tool call must use a unique ID, an offered tool and object Arguments')
        ids.add(call['ID'])
    raw = json.dumps(value, ensure_ascii=False).encode()
    if len(raw) > 2 * 1024 * 1024:
        raise ValueError('Response exceeds the counted provider 2 MiB limit')
    # Publish atomically without overwriting an answer or exposing partial JSON.
    fd, name = tempfile.mkstemp(prefix='.qa-answer-', dir=state / 'mailbox')
    try:
        with os.fdopen(fd, 'wb') as out:
            out.write(raw)
        target = state / 'mailbox' / (request_id + '.response.json')
        os.link(name, target)
    finally:
        Path(name).unlink(missing_ok=True)
    return {'answered': request_id, 'synthetic_provider': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--name', required=True)
    sub = parser.add_subparsers(dest='action', required=True)
    send = sub.add_parser('send')
    send.add_argument('--user', type=int, required=True)
    send.add_argument('--first-name', default='QA User')
    send.add_argument('--language', default='ru')
    send.add_argument('--chat', type=int)
    send.add_argument('--chat-type', choices=('private', 'group', 'supergroup'), default='private')
    send.add_argument('--text', required=True)
    send.add_argument('--update-id', type=int)
    for action in ('update', 'click', 'media', 'fault', 'release', 'init-data'):
        sub.add_parser(action).add_argument('--json', type=Path, required=True)
    upload = sub.add_parser('upload')
    upload.add_argument('--file', type=Path, required=True)
    upload.add_argument('--mime-type', default='application/octet-stream')
    upload.add_argument('--file-id')
    sub.add_parser('transcript').add_argument('--after', type=int, default=0)
    pending = sub.add_parser('pending-model')
    pending.add_argument('--request')
    answer = sub.add_parser('answer-model')
    answer.add_argument('--request', required=True)
    answer.add_argument('--json', type=Path, required=True)
    sub.add_parser('evidence').add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    state, manifest = stand.load(args.name)
    if args.action == 'send':
        body = {'user': dict(id=args.user, first_name=args.first_name, language_code=args.language), 'text': args.text}
        if args.chat is not None:
            body['chat'] = {'id': args.chat, 'type': args.chat_type}
        if args.update_id is not None:
            body['update_id'] = args.update_id
        value = request(state, manifest, 'message', body)
    elif args.action in ('update', 'click', 'media', 'fault', 'release', 'init-data'):
        value = request(state, manifest, args.action, read_json(args.json))
    elif args.action == 'upload':
        if args.file.stat().st_size > 4 * 1024 * 1024:
            raise ValueError('File fixture exceeds 4 MiB')
        body = dict(name=args.file.name, mime_type=args.mime_type, data_base64=base64.b64encode(args.file.read_bytes()).decode())
        if args.file_id:
            body['file_id'] = args.file_id
        value = request(state, manifest, 'file', body)
    elif args.action == 'transcript':
        value = request(state, manifest, 'state', after=args.after)
    elif args.action == 'pending-model':
        value = model_request(state, args.request) if args.request else {'pending': [p.name.removesuffix('.request.json') for p in sorted((state / 'mailbox').glob('*.request.json')) if not p.with_name(p.name.replace('.request.json', '.response.json')).exists()]}
    elif args.action == 'answer-model':
        value = answer_model(state, args.request, args.json)
    else:
        events, cursor = [], 0
        while True:
            page = request(state, manifest, 'state', after=cursor, snapshots=False)
            events.extend(page['journal'])
            if not page['truncated']:
                break
            assert page['cursor'] > cursor, 'Journal cursor must advance'
            cursor = page['cursor']
        output = {'manifest': manifest, 'telegram': dict(page, journal=events, snapshots_included=False), 'scope': 'Synthetic Telegram and manual model fixture; not real transport/model acceptance'}
        args.output.write_text(json.dumps(output, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        value = {'evidence': str(args.output), 'journal_entries': len(events)}
    print(json.dumps(value, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    try:
        main()
    except urllib.error.HTTPError as error:
        raise SystemExit('Mock control HTTP error ' + str(error.code) + '; inspect stand health and request schema') from None
    except Exception as error:
        raise SystemExit(type(error).__name__ + ': QA operation failed; inspect inputs and stand evidence, do not blindly resend') from None

# Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
# on behalf of Daniel Drizhuk
