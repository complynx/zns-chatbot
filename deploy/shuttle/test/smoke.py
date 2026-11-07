"""Real IAM/Loom smoke: delegated roleless user, run deduplication and model mailbox."""
import base64
import json
import socket
import ssl
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
import prepare

STATE = prepare.STATE
original_address = socket.getaddrinfo

def local_address(host, *args, **kwargs):
    return original_address('127.0.0.1' if host == 'host.docker.internal' else host, *args, **kwargs)

socket.getaddrinfo = local_address
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
    urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(STATE / 'ca-bundle.pem'))))


def call(url, data=None, headers=None, form=False):
    body = None if data is None else (urllib.parse.urlencode(data).encode() if form else json.dumps(data).encode())
    request = urllib.request.Request(url, data=body, headers={
        'Content-Type': 'application/x-www-form-urlencoded' if form else 'application/json', **(headers or {})})
    try:
        with opener.open(request, timeout=15) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        print("HTTP probe rejected", urllib.parse.urlsplit(url).path, error.code)
        raise


def main():
    fixture = json.loads((STATE / 'identity.json').read_text())
    user_file = STATE / 'probe-user.json'
    if user_file.exists():
        user = json.loads(user_file.read_text())
    else:
        user = prepare.api('management/v1/users/human', {
            'userName': 'shuttle-probe', 'profile': {'firstName': 'Shuttle', 'lastName': 'Probe'},
            'email': {'email': 'shuttle-probe@telegram.invalid', 'isEmailVerified': True},
            'initialPassword': 'Fixture-' + uuid.uuid4().hex + '!A1'}, (STATE / 'data/admin.pat').read_text().strip())
        prepare.save(user_file, json.dumps(user))
    issuer = 'https://host.docker.internal:18443'
    discovery = call(issuer + '/.well-known/openid-configuration')
    assert discovery['issuer'] == issuer
    scope = 'openid urn:zitadel:iam:user:resourceowner urn:zitadel:iam:org:projects:roles ' + ' '.join(
        f"urn:zitadel:iam:org:project:id:{fixture[key]['id']}:aud" for key in ('project', 'api_project'))
    machine = call(issuer + '/oauth/v2/token', {
        'grant_type': 'client_credentials', 'client_id': fixture['secret']['clientId'],
        'client_secret': fixture['secret']['clientSecret'], 'scope': scope + ' urn:zitadel:iam:org:project:id:zitadel:aud'}, form=True)
    basic = base64.b64encode((fixture['exchange']['clientId'] + ':' + fixture['exchange']['clientSecret']).encode()).decode()
    delegated = call(issuer + '/oauth/v2/token', {
        'grant_type': 'urn:ietf:params:oauth:grant-type:token-exchange', 'subject_token': user['userId'],
        'subject_token_type': 'urn:zitadel:params:oauth:token-type:user_id', 'actor_token': machine['access_token'],
        'actor_token_type': 'urn:ietf:params:oauth:token-type:access_token',
        'requested_token_type': 'urn:ietf:params:oauth:token-type:jwt', 'scope': scope},
        {'Authorization': 'Basic ' + basic}, form=True)
    claims = json.loads(base64.urlsafe_b64decode(delegated['access_token'].split('.')[1] + '=='))
    assert claims['sub'] == user['userId'] and claims['act']['sub'] == fixture['machine']['userId']
    call(issuer + '/v2/users', {'query': {'limit': 1}}, {'Authorization': 'Bearer ' + machine['access_token']})
    print('PASS TLS discovery, IAM user API and real user delegation')
    auth = {'Authorization': 'Bearer ' + machine['access_token'], 'X-Subject-Token': delegated['access_token']}
    session = call('http://127.0.0.1:18381/v1/auth/sessions', {}, auth)
    auth.pop('X-Subject-Token')
    auth['X-Auth-Session-Id'] = session['auth_session_id']
    operation = str(uuid.uuid4())
    body = {'input': {'text': 'Reply exactly: Real IAM and Loom smoke passed.'},
            'interaction': {'questions': 'choices_and_text', 'approvals': True},
            'disconnect_policy': 'continue', 'delivery': {'mode': 'async'}}
    before = set((STATE / 'mailbox').glob('*.request.json'))
    run = call('http://127.0.0.1:18381/v1/runs', body, {**auth, 'Idempotency-Key': operation})
    replay = call('http://127.0.0.1:18381/v1/runs', body, {**auth, 'Idempotency-Key': operation})
    assert replay['run_id'] == run['run_id']
    print('PASS real auth session and idempotent run creation')
    run_id = str(uuid.UUID(run['run_id']))
    for _ in range(60):
        step_ids = set(subprocess.check_output([
            'docker', 'exec', 'shuttle-live-test-postgres-1', 'psql', '-U', 'postgres', '-d', 'loom', '-At',
            '-c', f"SELECT id FROM steps WHERE run_id='{run_id}'",
        ], text=True).splitlines())
        pending = {file for file in set((STATE / 'mailbox').glob('*.request.json')) - before
                   if json.loads(file.read_text())['ClientRequestID'] in step_ids}
        if pending:
            break
        time.sleep(.25)
    else:
        raise RuntimeError('Provider request not observed')
    assert len(pending) == 1
    request = pending.pop()
    inference = json.loads(request.read_text())
    assert inference['ClientRequestID'] and inference['Input'] and inference['Tools']
    response = {'ID': 'smoke-' + operation, 'Text': 'Real IAM and Loom smoke passed.', 'Calls': [],
                'Usage': {'InputTokens': 100, 'OutputTokens': 10, 'CachedInputTokens': 0, 'CacheWriteTokens': 0}}
    destination = request.with_name(request.name.replace('.request.json', '.response.json'))
    temporary = destination.with_suffix('.tmp')
    prepare.save(temporary, json.dumps(response))
    temporary.replace(destination)
    for _ in range(60):
        result = call('http://127.0.0.1:18381/v1/runs/' + run['run_id'], headers=auth)
        if result['status'] == 'completed':
            assert result['result'] == response['Text']
            print('PASS real agent loop and final result; mock usage is scenario data')
            return
        if result['status'] in ('failed', 'cancelled'):
            raise RuntimeError('Real run failed')
        time.sleep(.25)
    raise RuntimeError('Run completion not observed')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        raise SystemExit('Smoke failed: ' + type(error).__name__ + '; credential details withheld') from None

# Written by Codex (gpt-6/Codex desktop)
# on behalf of Daniel Drizhuk
