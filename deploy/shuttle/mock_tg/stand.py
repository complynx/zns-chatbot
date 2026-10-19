"""Create isolated mock-Telegram QA projects. State and credentials stay ignored."""
import argparse
import base64
import importlib.util
import json
import re
import secrets
import socket
import ssl
import subprocess
import time
import urllib.request
import uuid
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
HERE = Path(__file__).resolve().parent
CACHE = REPO / '.cache/shuttle-mock'
PROVIDER_LAYOUT = 'project-network-tls-v1'


def private(path, text):
    path.write_text(text, encoding='utf-8')
    path.chmod(0o600)


def state_path(name):
    if not re.fullmatch('[a-z][a-z0-9-]{0,23}', name):
        raise ValueError('Name must be 1..24 lowercase letters/digits/hyphens, starting with a letter')
    path = (CACHE / name).resolve()
    if path.parent != CACHE.resolve():
        raise ValueError('State path escapes mock cache')
    return path


def run(args, state):
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode:
        private(state / 'last-error.private.log', result.stdout + result.stderr)
        raise RuntimeError('Command failed; inspect ignored last-error.private.log; do not blindly replay an uncertain operation')
    return result.stdout.strip()


def save(state, manifest):
    temp = state / 'manifest.tmp'
    temp.write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
    temp.replace(state / 'manifest.json')


def load(name):
    state = state_path(name)
    value = json.loads((state / 'manifest.json').read_text())
    assert value['name'] == name and value['project'] == 'zinusya-mock-' + name
    return state, value


def compose(state, manifest):
    return ['docker', 'compose', '--project-name', manifest['project'], '--env-file', str(state / 'compose.env'), '-f', str(HERE / 'compose.yaml')]


def ownership(state, manifest):
    ids = run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + manifest['project']], state).splitlines()
    for cid in ids:
        item = json.loads(run(['docker', 'inspect', cid], state))[0]
        assert item['Config']['Labels'].get('zinusya.qa.owner') == manifest['owner'], 'Foreign container in project; refusing mutation'
    # Volumes and networks carry project ownership from Compose, not a global name.
    for kind in ('volume', 'network'):
        names = run(['docker', kind, 'ls', '-q', '--filter', 'label=com.docker.compose.project=' + manifest['project']], state).splitlines()
        for name in names:
            item = json.loads(run(['docker', kind, 'inspect', name], state))[0]
            assert item.get('Labels', {}).get('com.docker.compose.project') == manifest['project']
            assert item.get('Labels', {}).get('zinusya.qa.owner') == manifest['owner'], 'Foreign volume/network in project; refusing mutation'
    return ids


def create(args):
    state = state_path(args.name)
    assert not state.exists(), 'Preserve existing evidence; use its start/status/destroy commands or choose a new name'
    assert 20000 <= args.base_port <= 60000 and args.base_port % 10 == 0
    assert args.loom_template.is_file()
    # Pin actual local immutable image IDs; do not build or pull another checkout.
    images = {}
    for key, image in (('bot', args.bot_image), ('loom', args.loom_image), ('file_worker', args.file_worker_image)):
        value = json.loads(subprocess.check_output(['docker', 'image', 'inspect', image], text=True))[0]
        images[key] = value['Id']
    ports = dict(zip(('bot', 'internal', 'mock', 'iam', 'loom', 'app'), range(args.base_port, args.base_port + 6)))
    held = []
    try:
        for port in ports.values():
            sock = socket.socket()
            sock.bind(('127.0.0.1', port))
            held.append(sock)
    finally:
        for sock in held:
            sock.close()
    state.mkdir(parents=True)
    for child in ('data', 'telegram', 'mailbox'):
        (state / child).mkdir()
    owner = uuid.uuid4().hex
    value = dict(name=args.name, project='zinusya-mock-' + args.name, owner=owner, ports=ports, images=images, stage='created', provider_layout=PROVIDER_LAYOUT,
                 urls={'bot': f'http://127.0.0.1:{ports["bot"]}', 'internal': f'http://127.0.0.1:{ports["internal"]}',
                       'mock': f'http://127.0.0.1:{ports["mock"]}', 'loom': f'http://127.0.0.1:{ports["loom"]}', 'miniapp': f'https://localhost:{ports["app"]}'})
    assert not subprocess.check_output(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + value['project']], text=True).strip()
    for kind in ('volume', 'network'):
        assert not subprocess.check_output(['docker', kind, 'ls', '-q', '--filter', 'label=com.docker.compose.project=' + value['project']], text=True).strip(), 'Existing resources: choose a fresh project name'
    credentials = dict(db_password=secrets.token_hex(24), masterkey=secrets.token_hex(16), human_password='Fixture-' + secrets.token_hex(24) + '!A1',
                       bot_token='9900000001:' + secrets.token_urlsafe(32), qa_token=secrets.token_hex(32), provider_key=secrets.token_hex(32), webhook_secret=secrets.token_hex(32))
    private(state / 'credentials.json', json.dumps(credentials))
    private(state / 'auth.key', base64.b64encode(secrets.token_bytes(32)).decode())
    private(state / 'registration.key', base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip('='))
    (state / 'loom.template.yaml').write_bytes(args.loom_template.read_bytes())
    env = {'QA_OWNER': owner, 'QA_NAME': args.name, 'QA_STATE': state.as_posix(), 'QA_SOURCE': REPO.as_posix(),
           'QA_DB_PASSWORD': credentials['db_password'], 'QA_MASTERKEY': credentials['masterkey'], 'QA_HUMAN_PASSWORD': credentials['human_password'],
           'QA_BOT_IMAGE': images['bot'], 'QA_LOOM_IMAGE': images['loom'], 'QA_FILE_WORKER_IMAGE': images['file_worker'], 'QA_PROVIDER_KEY': credentials['provider_key']}
    env.update({'QA_' + k.upper() + '_PORT': str(v) for k, v in ports.items()})
    private(state / 'compose.env', '\n'.join(k + '=' + v for k, v in env.items()) + '\n')
    private(state / 'telegram/mock.json', json.dumps(dict(token=credentials['bot_token'], qa_token=credentials['qa_token'],
            webhook_url='http://shuttle:8080/telegram/webhook', webhook_secret=credentials['webhook_secret'],
            database='/state/telegram.sqlite3', app_url='http://shuttle:8080', tls_cert='/tls/tls.crt', tls_key='/tls/tls.key')))
    save(state, value)
    print(json.dumps({'name': args.name, 'stage': value['stage'], 'urls': value['urls']}))


def certificates(state, manifest):
    # Reuse the reviewed local-CA issuer in a distinct state directory. Never
    # install browser trust and never read the live stand's CA or credentials.
    spec = importlib.util.spec_from_file_location('mock_tls', REPO / 'deploy/shuttle/test/tls.py')
    tls = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(tls)
    name = manifest['project'] + '-certificates'
    run(['docker', 'run', '--rm', '--name', name, '--label', 'zinusya.qa.owner=' + manifest['owner'],
         '--memory', '128m', '--memory-swap', '128m', '--cpus', '1', '--pids-limit', '64',
         '--mount', 'type=bind,source=' + str(state) + ',target=/certs', tls.IMAGE, 'sh', '-ceu', tls.ISSUE + '\nchown 1000:1000 /certs/mailbox\nchmod 700 /certs/mailbox\n'], state)


class IAM:
    def __init__(self, state, port):
        self.state = state
        self.issuer = 'https://iam-' + state.name + '.zinusya.localhost:8080'
        self.port = port
        self.context = ssl.create_default_context(cafile=str(state / 'ca-bundle.pem'))
        self.pat = None

    def request(self, path, body=None, method=None):
        # Reach the loopback published port without global DNS monkey-patching.
        # localhost is on the stand certificate; Host retains the configured issuer.
        headers = {'Content-Type': 'application/json', 'Host': self.issuer.removeprefix('https://')}
        if self.pat:
            headers['Authorization'] = 'Bearer ' + self.pat
        req = urllib.request.Request('https://localhost:' + str(self.port) + '/' + path, headers=headers,
            data=None if body is None else json.dumps(body).encode(), method=method or ('GET' if body is None else 'POST'))
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=self.context))
        with opener.open(req, timeout=15) as response:
            return json.load(response)

    def ready(self):
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                discovery = self.request('.well-known/openid-configuration')
                assert discovery['issuer'] == self.issuer
                self.pat = (self.state / 'data/admin.pat').read_text().strip()
                return
            except OSError:
                time.sleep(.5)
        raise RuntimeError('IAM readiness observation timed out; inspect same project, do not recreate on timeout alone')


def provision(state, manifest):
    iam = IAM(state, manifest['ports']['iam'])
    iam.ready()
    # This flag precedes the first non-idempotent creation. Unknown outcomes must
    # be inspected, or the disposable project destroyed, rather than replayed.
    manifest['stage'] = 'identity_creation_started'
    save(state, manifest)
    project = iam.request('management/v1/projects', {'name': manifest['project'] + '-loom', 'projectRoleAssertion': True})
    app = iam.request(f'management/v1/projects/{project["id"]}/apps/api', {'name': 'Loom', 'authMethod': 'API_AUTH_METHOD_BASIC'})
    exchange = iam.request(f'management/v1/projects/{project["id"]}/apps/oidc', {'name': 'Exchange', 'redirectUris': [manifest['urls']['bot'] + '/unused'],
        'responseTypes': ['OIDC_RESPONSE_TYPE_CODE'], 'grantTypes': ['OIDC_GRANT_TYPE_AUTHORIZATION_CODE', 'OIDC_GRANT_TYPE_TOKEN_EXCHANGE'],
        'appType': 'OIDC_APP_TYPE_WEB', 'authMethodType': 'OIDC_AUTH_METHOD_TYPE_BASIC', 'version': 'OIDC_VERSION_1_0', 'accessTokenType': 'OIDC_TOKEN_TYPE_JWT'})
    api_project = iam.request('management/v1/projects', {'name': manifest['project'] + '-api', 'projectRoleAssertion': True})
    intro = iam.request(f'management/v1/projects/{api_project["id"]}/apps/api', {'name': 'API', 'authMethod': 'API_AUTH_METHOD_BASIC'})
    for project_id, role in ((project['id'], 'zinusya.service'), (project['id'], 'zinusya.admin'), (api_project['id'], 'zinusya.admin')):
        iam.request(f'management/v1/projects/{project_id}/roles', {'roleKey': role, 'displayName': role})
    machine = iam.request('management/v1/users/machine', {'userName': 'mock-service', 'name': 'Mock QA Service', 'accessTokenType': 'ACCESS_TOKEN_TYPE_JWT'})
    secret = iam.request(f'management/v1/users/{machine["userId"]}/secret', {}, method='PUT')
    iam.request(f'management/v1/users/{machine["userId"]}/grants', {'projectId': project['id'], 'roleKeys': ['zinusya.service']})
    iam.request('admin/v1/policies/security', {'enableImpersonation': True}, method='PUT')
    iam.request('management/v1/orgs/me/members', {'userId': machine['userId'], 'roles': ['ORG_OWNER', 'ORG_END_USER_IMPERSONATOR']})
    private(state / 'identity.json', json.dumps(dict(project=project, app=app, exchange=exchange, api_project=api_project, intro=intro, machine=machine, secret=secret)))
    credentials = json.loads((state / 'credentials.json').read_text())
    organization = machine['details']['resourceOwner']
    template = (state / 'loom.template.yaml').read_text()
    replacements = {
        '${CLX_DATABASE_DSN}': f'postgres://postgres:{credentials["db_password"]}@postgres:5432/loom?sslmode=disable',
        '${CLX_ZITADEL_ISSUER}': iam.issuer, '${CLX_ZITADEL_AUDIENCE}': project['id'], '${CLX_ZITADEL_CLIENT_ID}': app['clientId'],
        '${CLX_ZITADEL_CLIENT_SECRET}': app['clientSecret'], '${CLX_ADMIN_ORGANIZATION_ID}': organization,
        '/run/secrets/clx-auth-key': '/run/secrets/auth.key', '["loom-dev_default"]': '["' + manifest['project'] + '_default"]',
        'poll_seconds: 300': 'poll_seconds: 2', 'max_workers: 4': 'max_workers: 2', 'initial_credits: 0': 'initial_credits: 1000000000',
        'external_disclosure_default: false': 'external_disclosure_default: true',
        '  openai:\n    endpoint: "https://api.openai.com/v1"\n    api_key: "${CLX_OPENAI_API_KEY}"': '  openai:\n    endpoint: "https://provider.zinusya.localhost:18089/v1"\n    api_key: "' + credentials['provider_key'] + '"',
        '    harness-user:': '    zinusya.service: [runs.create, runs.control, chat.read, chat.events.append, artifacts.read, artifacts.write]\n    zinusya.admin: [zinusya.admin]\n    harness-user:',
    }
    for old, new in replacements.items():
        if old not in template:
            raise RuntimeError('Supplied Loom template is incompatible; inspect owned fixture config')
        template = template.replace(old, new)
    # Explicit migration job works with both pre- and post-migrate-on-start images.
    template = re.sub(r'^  migrate_on_start:.*\n', '', template, flags=re.M)
    if '\nfile_tools:' in template:
        raise RuntimeError('Reviewed mock template must leave file_tools to the stand wrapper')
    template += '\nfile_tools:\n  enabled: true\n  socket: /run/loom-files/processor.sock\n  timeout_ms: 30000\n'
    private(state / 'loom.yaml', template)
    bot = dict(database_url=f'postgres://postgres:{credentials["db_password"]}@postgres:5432/shuttle?sslmode=disable',
        http_address=':8080', internal_address=':9090', shutdown_timeout='1s',
        registration_details_key=(state / 'registration.key').read_text(),
        mini_app_origin=manifest['urls']['miniapp'],
        telegram=dict(token=credentials['bot_token'], webhook_secret=credentials['webhook_secret'], base_url='https://telegram.zinusya.localhost:8443'),
        loom=dict(base_url='http://loom:8080'), identity=dict(issuer=iam.issuer, organization_id=organization, audience=project['id'], api_audience=api_project['id'],
            actor_id=machine['userId'], actor_client_id=secret['clientId'], actor_client_secret=secret['clientSecret'], client_id=exchange['clientId'], client_secret=exchange['clientSecret'],
            introspection_client_id=intro['clientId'], introspection_client_secret=intro['clientSecret']), otel=dict(service_name=manifest['project'], sample_ratio=1))
    private(state / 'shuttle.yaml', json.dumps(bot, indent=2))
    manifest['stage'] = 'configs_ready'
    save(state, manifest)


def runtime_fingerprints(state, manifest):
    required = {'postgres', 'zitadel', 'telegram', 'provider', 'docker-api-proxy', 'sandbox', 'file-worker', 'loom', 'shuttle'}
    ids = run(['docker', 'ps', '-aq', '--filter', 'label=com.docker.compose.project=' + manifest['project']], state).splitlines()
    if not ids:
        raise RuntimeError('QA services are absent')
    items = json.loads(run(['docker', 'inspect', *ids], state))
    result = {}
    for item in items:
        labels = item['Config']['Labels']
        assert labels.get('zinusya.qa.owner') == manifest['owner']
        service = labels.get('com.docker.compose.service')
        if service not in required or service in result:
            raise RuntimeError('Unexpected QA service instance')
        status = item['State']
        if not status['Running'] or status.get('Restarting') or status['OOMKilled']:
            raise RuntimeError('QA dependency is not running: ' + service + '; inspect its existing container')
        result[service] = dict(id=item['Id'], image=item['Image'], started_at=status['StartedAt'], restarts=item['RestartCount'])
    if set(result) != required:
        raise RuntimeError('QA service set is incomplete')
    return result


def start(name):
    state, manifest = load(name)
    assert manifest.get('provider_layout') == PROVIDER_LAYOUT, 'Legacy provider layout: preserve evidence and create a fresh named stand; stop/status/destroy remain available'
    assert manifest['stage'] in ('created', 'configs_ready', 'migrated', 'running', 'stopped'), 'Inspect uncertain stage; do not replay provisioning/migration'
    ownership(state, manifest)
    cmd = compose(state, manifest)
    if manifest['stage'] == 'created':
        certificates(state, manifest)
        run(cmd + ['up', '-d', '--no-build', '--pull', 'never', 'postgres', 'zitadel'], state)
        provision(state, manifest)
    # Resume infrastructure before any --no-deps migration, including a stand
    # deliberately stopped after config provisioning.
    run(cmd + ['up', '-d', '--no-build', '--pull', 'never', 'postgres', 'zitadel'], state)
    IAM(state, manifest['ports']['iam']).ready()
    if manifest['stage'] == 'configs_ready':
        manifest['stage'] = 'migration_started'
        save(state, manifest)
        run(cmd + ['run', '--rm', '--no-deps', 'loom', 'migrate'], state)
        run(cmd + ['run', '--rm', '--no-deps', 'shuttle', '-config', '/etc/shuttle/config.yaml', '-migrate'], state)
        manifest['stage'] = 'migrated'
        save(state, manifest)
    run(cmd + ['up', '-d', '--no-build', '--pull', 'never'], state)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    deadline = time.monotonic() + 90
    stable = None
    stable_since = None
    while time.monotonic() < deadline:
        current = runtime_fingerprints(state, manifest)
        if current != stable:
            stable, stable_since = current, time.monotonic()
        try:
            for url in (manifest['urls']['bot'] + '/readyz', manifest['urls']['loom'] + '/readyz', manifest['urls']['mock'] + '/health'):
                with opener.open(url, timeout=2) as response:
                    assert response.status == 200
            if time.monotonic() - stable_since >= 2:
                break
        except OSError:
            pass
        time.sleep(.5)
    else:
        raise RuntimeError('Readiness timed out; inspect same containers, do not restart on timeout alone')
    manifest['runtime'] = stable
    manifest['stage'] = 'running'
    save(state, manifest)
    print(json.dumps({'name': name, 'stage': 'running', 'urls': manifest['urls']}))


def control(name, action):
    state, manifest = load(name)
    ids = ownership(state, manifest)
    if action == 'status':
        summary = []
        for cid in ids:
            item = json.loads(run(['docker', 'inspect', cid], state))[0]
            summary.append(dict(service=item['Config']['Labels'].get('com.docker.compose.service'), running=item['State']['Running'], oom=item['State']['OOMKilled']))
        print(json.dumps(dict(name=name, stage=manifest['stage'], urls=manifest['urls'], containers=summary)))
        return
    assert action in ('stop', 'destroy')
    run(compose(state, manifest) + (['stop', '-t', '5'] if action == 'stop' else ['down', '--volumes', '--remove-orphans', '--timeout', '5']), state)
    if action == 'destroy':
        manifest['stage'] = 'destroyed'
    elif manifest['stage'] in ('migrated', 'running', 'stopped'):
        manifest['stage'] = 'stopped'
    # Preserve created/configs_ready and uncertain initialization stages. Stopping
    # containers is not proof that provisioning or migrations completed.
    manifest['containers_stopped'] = True
    save(state, manifest)
    print(json.dumps(dict(name=name, stage=manifest['stage'], evidence_retained=True)))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='action', required=True)
    new = sub.add_parser('create')
    new.add_argument('--name', required=True)
    new.add_argument('--base-port', type=int, required=True)
    new.add_argument('--bot-image', required=True)
    new.add_argument('--loom-image', required=True)
    new.add_argument('--file-worker-image', required=True)
    new.add_argument('--loom-template', type=Path, required=True)
    for action in ('start', 'status', 'stop', 'destroy'):
        sub.add_parser(action).add_argument('--name', required=True)
    args = parser.parse_args()
    if args.action == 'create':
        create(args)
    elif args.action == 'start':
        start(args.name)
    else:
        control(args.name, args.action)


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        raise SystemExit(type(error).__name__ + ': mock stand operation failed; inspect ignored manifest/private diagnostics') from None

# Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
# on behalf of Daniel Drizhuk
