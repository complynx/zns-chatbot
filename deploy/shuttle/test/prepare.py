"""Provision only the named local test project; secrets stay in ignored runtime files."""
import argparse
import base64
import json
import secrets
import socket
import ssl
import subprocess
import time
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
STATE = ROOT / '.cache/shuttle-live-test'
ENV = STATE / 'compose.env'
original_address = socket.getaddrinfo

def local_address(host, *args, **kwargs):
    return original_address('127.0.0.1' if host == 'host.docker.internal' else host, *args, **kwargs)

socket.getaddrinfo = local_address

def open_request(request, timeout):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(STATE / 'ca-bundle.pem'))))
    return opener.open(request, timeout=timeout)


def save(path, text):
    path.write_text(text, encoding='utf-8')
    path.chmod(0o600)


class InitializedStandError(RuntimeError):
    """Bootstrap cannot safely reconstruct an existing stand's Compose model."""


def require_fresh():
    if any((STATE / name).exists() for name in ('identity.json', 'loom.yaml', 'shuttle.yaml')):
        raise InitializedStandError(
            'Stand already initialized; preserve its configuration and restart services '
            'with the complete Compose model used for that stand, including overlays. '
            'Do not rerun bootstrap or delete runtime state as a retry.')


def compose(*args):
    subprocess.run(['docker', 'compose', '--env-file', str(ENV), '-f', str(HERE / 'compose.yaml'), *args], check=True)


def api(path, body, pat, method=None):
    request = urllib.request.Request('https://host.docker.internal:18443/' + path,
        data=None if body is None else json.dumps(body).encode(), headers={'Authorization': 'Bearer ' + pat, 'Content-Type': 'application/json', 'Host': 'host.docker.internal:18443', 'X-Forwarded-Proto': 'https'},
        method=method or ('PUT' if path in ('admin/v1/policies/security',) or path.endswith('/secret') else 'POST'))
    with open_request(request, timeout=15) as response:
        return json.load(response)


def prepare():
    require_fresh()
    STATE.mkdir(parents=True, exist_ok=True)
    for child in ('data', 'mailbox', 'poller'):
        (STATE / child).mkdir(exist_ok=True)
    import tls
    tls.issue()
    if not ENV.exists():
        save(ENV, '\n'.join([
            'TEST_DB_PASSWORD=' + secrets.token_hex(24),
            'TEST_MASTERKEY=' + secrets.token_hex(16),
            'TEST_HUMAN_PASSWORD=Fixture-' + secrets.token_hex(16) + '!A1',
            'TEST_WEBHOOK_SECRET=' + secrets.token_hex(32), 'TEST_TELEGRAM_BOT_ID=6087685431', '',
        ]))
    if not (STATE / 'auth.key').exists():
        save(STATE / 'auth.key', base64.b64encode(secrets.token_bytes(32)).decode())
    compose('up', '-d', 'postgres', 'zitadel')
    for _ in range(90):
        try:
            with open_request('https://host.docker.internal:18443/.well-known/openid-configuration', timeout=2) as response:
                discovery = json.load(response)
            break
        except (OSError, ValueError):
            time.sleep(1)
    else:
        raise RuntimeError('Test Zitadel discovery not ready')
    if discovery['issuer'] != 'https://host.docker.internal:18443':
        raise RuntimeError('Unexpected test issuer')
    saved = STATE / 'identity.json'
    if saved.exists():
        identity = json.loads(saved.read_text())
    else:
        pat = (STATE / 'data/admin.pat').read_text().strip()
        project = api('management/v1/projects', {'name': 'Shuttle Test Loom', 'projectRoleAssertion': True}, pat)
        app = api(f"management/v1/projects/{project['id']}/apps/api", {'name': 'Loom Introspection', 'authMethod': 'API_AUTH_METHOD_BASIC'}, pat)
        exchange = api(f"management/v1/projects/{project['id']}/apps/oidc", {
            'name': 'Shuttle Exchange', 'redirectUris': ['http://localhost:18382/unused'],
            'responseTypes': ['OIDC_RESPONSE_TYPE_CODE'],
            'grantTypes': ['OIDC_GRANT_TYPE_AUTHORIZATION_CODE', 'OIDC_GRANT_TYPE_TOKEN_EXCHANGE'],
            'appType': 'OIDC_APP_TYPE_WEB', 'authMethodType': 'OIDC_AUTH_METHOD_TYPE_BASIC',
            'version': 'OIDC_VERSION_1_0', 'accessTokenType': 'OIDC_TOKEN_TYPE_JWT'}, pat)
        api_project = api('management/v1/projects', {'name': 'Shuttle Test API', 'projectRoleAssertion': True}, pat)
        intro = api(f"management/v1/projects/{api_project['id']}/apps/api", {'name': 'Shuttle Introspection', 'authMethod': 'API_AUTH_METHOD_BASIC'}, pat)
        api(f"management/v1/projects/{project['id']}/roles", {'roleKey': 'zinusya.service', 'displayName': 'Test Shuttle Service'}, pat)
        api(f"management/v1/projects/{api_project['id']}/roles", {'roleKey': 'zinusya.admin', 'displayName': 'Test Shuttle Admin'}, pat)
        machine = api('management/v1/users/machine', {'userName': 'shuttle-test-service', 'name': 'Test Shuttle Service', 'accessTokenType': 'ACCESS_TOKEN_TYPE_JWT'}, pat)
        secret = api(f"management/v1/users/{machine['userId']}/secret", {}, pat)
        api(f"management/v1/users/{machine['userId']}/grants", {'projectId': project['id'], 'roleKeys': ['zinusya.service']}, pat)
        api('admin/v1/policies/security', {'enableImpersonation': True}, pat)
        api('management/v1/orgs/me/members', {'userId': machine['userId'], 'roles': ['ORG_OWNER', 'ORG_END_USER_IMPERSONATOR']}, pat)
        identity = {'project': project, 'app': app, 'exchange': exchange, 'api_project': api_project, 'intro': intro, 'machine': machine, 'secret': secret}
        save(saved, json.dumps(identity))
    # Expand existing owned fixtures before changing the caller/permission namespace.
    pat = (STATE / 'data/admin.pat').read_text().strip()
    for project_key, role, display in [('project', 'zinusya.service', 'Zinusya service'),
                                     ('project', 'zinusya.admin', 'Zinusya administrator'),
                                     ('api_project', 'zinusya.admin', 'Zinusya administrator')]:
        project_id = identity[project_key]['id']
        found = api(f"management/v1/projects/{project_id}/roles/_search",
                    {'queries': [{'keyQuery': {'key': role, 'method': 'TEXT_QUERY_METHOD_EQUALS'}}]}, pat)
        if not any(item['key'] == role for item in found.get('result', [])):
            api(f"management/v1/projects/{project_id}/roles", {'roleKey': role, 'displayName': display}, pat)
    user_id = identity['machine']['userId']
    grants = api("management/v1/users/grants/_search",
                 {"queries": [{"userIdQuery": {"userId": user_id}}]}, pat).get('result', [])
    matches = [grant for grant in grants if grant['projectId'] == identity['project']['id']]
    if len(matches) != 1:
        raise RuntimeError('Expected one owned service grant')
    grant = matches[0]
    roles = sorted(set(grant.get('roleKeys', [])) | {'zinusya.service'})
    if set(roles) != set(grant.get('roleKeys', [])):
        api(f"management/v1/users/{user_id}/grants/{grant['id']}", {'roleKeys': roles}, pat, method='PUT')
    env = dict(line.split('=', 1) for line in ENV.read_text().splitlines() if '=' in line)
    organization = identity['machine']['details']['resourceOwner']
    config = (ROOT.parent / 'clx-scripter/deploy/config.example.yaml').read_text()
    replacements = {
        '${CLX_DATABASE_DSN}': f"postgres://postgres:{env['TEST_DB_PASSWORD']}@postgres:5432/loom?sslmode=disable",
        '${CLX_ZITADEL_ISSUER}': 'https://host.docker.internal:18443',
        '${CLX_ZITADEL_AUDIENCE}': identity['project']['id'],
        '${CLX_ZITADEL_CLIENT_ID}': identity['app']['clientId'],
        '${CLX_ZITADEL_CLIENT_SECRET}': identity['app']['clientSecret'],
        '${CLX_ADMIN_ORGANIZATION_ID}': organization,
        '/run/secrets/clx-auth-key': '/run/secrets/auth.key',
        '["loom-dev_default"]': '["shuttle-live-test_default"]',
        'poll_seconds: 300': 'poll_seconds: 2',
        'max_workers: 4': 'max_workers: 2',
        'initial_credits: 0': 'initial_credits: 1000000000',
        'external_disclosure_default: false': 'external_disclosure_default: true',
        '  openai:\n    endpoint: "https://api.openai.com/v1"\n    api_key: "${CLX_OPENAI_API_KEY}"': '  openai:\n    kind: mock\n    mailbox: /mailbox',
        '    harness-user:': f"    zinusya.service: [runs.create, runs.control, chat.read, chat.events.append, artifacts.read, artifacts.write]\n    zinusya.admin: [zinusya.admin]\n    harness-user:",
    }
    for old, new in replacements.items():
        if old not in config:
            raise RuntimeError('Loom template changed; test config replacement unavailable')
        config = config.replace(old, new)
    save(STATE / 'loom.yaml', config)
    shuttle = {
        'database_url': f"postgres://postgres:{env['TEST_DB_PASSWORD']}@postgres:5432/shuttle?sslmode=disable",
        'http_address': ':8080', 'internal_address': ':9090', 'shutdown_timeout': '1s',
        'telegram': {'base_url': 'https://api.telegram.org'}, 'loom': {'base_url': 'http://loom:8080'},
        'identity': {'issuer': 'https://host.docker.internal:18443', 'organization_id': organization,
            'audience': identity['project']['id'], 'api_audience': identity['api_project']['id'],
            'actor_id': identity['machine']['userId'], 'actor_client_id': identity['secret']['clientId'],
            'actor_client_secret': identity['secret']['clientSecret'], 'client_id': identity['exchange']['clientId'],
            'client_secret': identity['exchange']['clientSecret'], 'introspection_client_id': identity['intro']['clientId'],
            'introspection_client_secret': identity['intro']['clientSecret']},
        'otel': {'service_name': 'zinusya-test', 'sample_ratio': 1},
    }
    save(STATE / 'shuttle.yaml', json.dumps(shuttle, indent=2))
    print('Real test IAM provisioned; generated configs and credentials remain ignored.')


if __name__ == '__main__':
    try:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument('--check-fresh', action='store_true', help='Check bootstrap eligibility without changing state')
        args = parser.parse_args()
        if args.check_fresh:
            require_fresh()
        else:
            prepare()
    except InitializedStandError as error:
        raise SystemExit(str(error)) from None
    except Exception as error:
        # Do not print URLs, request bodies or credential-bearing subprocess args.
        raise SystemExit('Test preparation failed: ' + type(error).__name__ + '; inspect local service health') from None

# Written by Codex (gpt-6/Codex desktop)
# on behalf of Daniel Drizhuk
