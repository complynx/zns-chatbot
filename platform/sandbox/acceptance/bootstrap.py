"""Literal clean-C bootstrap using the accepted runner's native custody."""
import copy
import hashlib
import json
import math
import os
from pathlib import Path
import re
import stat
import time

PRODUCT = '36264bb8ef6e1a603cc0c4a0cc7e32b412c12cb8'
BINARY = '488a8accc0f4072236a6b779837a28fa6312bf0769573023271ad85d767e1261'
RUNTIME = ['app','evaluator','media-decoder','media-broker','sticker-decoder','sticker-broker']
STEPS = [
    {'service':'tool','command':['migrate'],'environment':{}},
    {'service':'tool','command':['fixture'],'environment':{}},
    {'service':'tool','command':['product-fixture'],'environment':{}},
    {'service':'tool','command':['product-fixture'],'environment':{
        'REGISTRATION_FIXTURE_ACTION':'init',
        'REGISTRATION_FIXTURE_STAND':'synthetic-qa-zns-registration-fixture',
        'REGISTRATION_FIXTURE_OPENS_AT':'2030-10-02T13:00:00Z',
        'REGISTRATION_FIXTURE_CLOCK_ANCHOR':'2030-10-02T12:00:00Z'}},
    {'service':'roles','command':None,'environment':{}},
    {'service':'clock-init','command':None,'environment':{}},
    {'service':'clock-read','command':None,'environment':{}},
]
PREREQUISITES = [
    'current-source-and-executable','genuine-bounded-clock-read',
    'private-config-role-row-and-fresh-data-continuity',
    'six-complete-native-prelaunch-constructors','native-ro-app-owner-bind-probes',
    'separately-bounded-current-provider-delivery',
]

PRIVATE_VOLUME = 'synthetic-qa-c-private-delivery1-20261006'
PRIVATE_ROOT = '/var/lib/docker/volumes/'+PRIVATE_VOLUME+'/_data'
PRIVATE_NAMES = sorted([role+'.password' for role in
                        ('postgres','app','meter','inventory','fake','operator')]+
                       [role+'.env' for role in
                        ('app','owner','fake','operator','media','sticker','roles','inventory')])


def delivery_snapshot(runner, approval, native, required):
    """Keep legacy public delivery guards and admit one exact native private root."""
    binding = approval.get('native_private_delivery')
    if binding is None:
        return native.delivery_snapshot(runner,approval,required)
    if (not native.same(binding,{'volume':PRIVATE_VOLUME,'path':PRIVATE_ROOT,
                                'driver':'local','scope':'local'})
            or approval.get('no_host_source_writers') is not True or os.geteuid()!=0):
        raise native.Failure('exact admitted native private volume/root')
    private_paths = {PRIVATE_ROOT+'/'+name for name in PRIVATE_NAMES}
    files = approval['delivery_files']
    directories = approval['delivery_directories']
    if (not files or len(files)>4096 or len(directories)>256
            or len({item['path'] for item in files})!=len(files)
            or len({item['path'] for item in directories})!=len(directories)):
        raise native.Failure('unique complete delivery inventory')
    private = [item for item in files if item['path'] in private_paths]
    roots = [item for item in directories if item['path']==PRIVATE_ROOT]
    if (len(private)!=14 or len(roots)!=1
            or not native.same(roots[0]['members'],PRIVATE_NAMES)
            or not native.same(roots[0]['directories'],[])):
        raise native.Failure('exact native private14 topology')
    public = dict(approval,delivery_files=[item for item in files if item['path'] not in private_paths],
                  delivery_directories=[item for item in directories if item['path']!=PRIVATE_ROOT])
    observed = native.delivery_snapshot(runner,public,required-private_paths-{PRIVATE_ROOT})
    identities = {(item['device'],item['inode']) for item in observed}
    with native.intake_deadline(runner.work_end,runner.work_utc_end) as check, \
            native.physical_open(PRIVATE_ROOT,directory=True,check=check) as root:
        info = os.fstat(root)
        if (info.st_uid!=0 or info.st_gid!=10001 or stat.S_IMODE(info.st_mode)!=0o750
                or sorted(os.listdir(root))!=PRIVATE_NAMES):
            raise native.Failure('physical native private root/access')
        for item in private:
            name = Path(item['path']).name
            group = 10001 if name in {'app.env','owner.env'} else 70 if name.endswith('.password') else 0
            with native.physical_open(item['path'],check=check) as fd:
                before = os.fstat(fd)
                if (before.st_uid!=0 or before.st_gid!=group
                        or stat.S_IMODE(before.st_mode)!=(0o640 if group else 0o600)
                        or type(item['bytes']) is not int or not 0<item['bytes']<=1048576
                        or before.st_size!=item['bytes'] or (before.st_dev,before.st_ino) in identities):
                    raise native.Failure('exact owned bounded native private file')
                check()
                raw = os.read(fd,1048577)
                check()
                after = os.fstat(fd)
                if (any(getattr(before,key)!=getattr(after,key) for key in
                        ('st_dev','st_ino','st_size','st_mtime_ns','st_ctime_ns'))
                        or len(raw)!=item['bytes'] or hashlib.sha256(raw).hexdigest()!=item['sha256']):
                    raise native.Failure('unchanged exact native private bytes/identity')
                identities.add((before.st_dev,before.st_ino))
                observed.append({'path':item['path'],'bytes':before.st_size,'sha256':item['sha256'],
                                 'device':before.st_dev,'inode':before.st_ino,
                                 'mtime_ns':before.st_mtime_ns,'ctime_ns':before.st_ctime_ns})
        check()
        after = os.fstat(root)
        if (any(getattr(info,key)!=getattr(after,key) for key in
                ('st_dev','st_ino','st_mode','st_uid','st_gid','st_mtime_ns','st_ctime_ns'))
                or sorted(os.listdir(root))!=PRIVATE_NAMES):
            raise native.Failure('unchanged physical native private directory/topology')
        observed.append({'path':PRIVATE_ROOT,'type':'directory','device':info.st_dev,'inode':info.st_ino,
                         'mode':info.st_mode,'uid':info.st_uid,'gid':info.st_gid,
                         'mtime_ns':info.st_mtime_ns,'ctime_ns':info.st_ctime_ns})
    return observed


def check_delivery(runner, approval, native):
    """Recheck the authenticated physical delivery before dependent operations."""
    observed = delivery_snapshot(runner,approval,native,runner.install_delivery_required)
    if not native.same(runner.install_delivery_before,observed):
        raise native.Failure('installation delivery changed')
    return observed


def attach_seconds(runner):
    return min(15,runner.work_end-time.monotonic(),runner.work_utc_end-time.time())


def postgres_password_mounts(profile, approval, native):
    """Keep propagation in actual observations outside the frozen mount projection."""
    planned=approval['services']['postgres']['invariants']['Mounts']
    for role in ('postgres','app','meter','inventory','fake','operator'):
        target='/run/secrets/'+role+'_password'
        expected=[item for item in planned if item['Destination']==target]
        actual=[item for item in profile['Mounts'] if item['Destination']==target]
        if len(expected)!=1 or len(actual)!=1:
            raise native.Failure('six distinct actual PG password binds')
        wanted,observed=expected[0],actual[0]
        propagation='rslave' if wanted['Source']==PRIVATE_ROOT+'/'+role+'.password' else 'rprivate'
        if (wanted['Type']!='bind' or wanted['RW'] is not False
                or wanted['Propagation']!=propagation
                or observed['Type']!='bind' or observed['Source']!=wanted['Source']
                or observed['RW'] is not False or observed['Propagation']!=propagation):
            raise native.Failure('actual PG password source/read-only/propagation')


def graph_constructors(runner, approval, native, identities):
    """Verify the complete owned cohort before starting PG or any product role."""
    names = ['postgres', 'fake'] + RUNTIME
    if set(identities) != set(names) or set(approval['services']) != set(names):
        raise native.Failure('complete eight-role installation cohort')
    if len(set(identities.values())) != len(names):
        raise native.Failure('distinct full installation identities')
    profiles = {}
    for service in names:
        identity = identities[service]
        if not re.fullmatch('[a-f0-9]{64}', identity):
            raise native.Failure('full installation identity')
        profile = runner.inspect(identity)
        if (profile['Id'] != identity or profile['State']['Status'] != 'created'
                or type(profile['State']['Pid']) is not int
                or type(profile['State']['Running']) is not bool
                or profile['State']['Pid'] != 0 or profile['State']['Running']
                or profile['State']['StartedAt'] != '0001-01-01T00:00:00Z'):
            raise native.Failure('complete never-started installation cohort')
        native.constructor(profile, approval['services'][service],
                           approval['project'], service, approval['owner'])
        if service=='postgres':
            postgres_password_mounts(profile,approval,native)
        profiles[service] = profile
    return profiles


def create_graph(runner, approval, native):
    """Create only the eight literal roles from the validated saved producer."""
    project,command,services,resources = native.compose_config(runner,approval)
    names = ['postgres','fake']+RUNTIME
    if set(services)!=set(names) or approval.get('retain_owned_stand') is not True:
        raise native.Failure('literal owned installation graph retention')
    if runner.docker('ps','-aq','--filter','label=com.docker.compose.project='+project).strip():
        raise native.Failure('fresh isolated installation project')
    for expected in services.values():
        if runner.docker('ps','-aq','--filter','name=^/'+re.escape(expected['name'])+'$').strip():
            raise native.Failure('fresh declared installation name')
    for kind,expected in resources:
        if expected['name'] in native.resource_names(runner,kind,expected['name']):
            raise native.Failure('fresh declared installation resource')
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        native.read_pinned(command[-1],runner.create_compose_pin)
    check_delivery(runner,approval,native)
    runner.owned = names
    runner.unresolved_resources = True
    runner.docker(*command,'create','--no-build','--pull','never',*names)
    discovered = runner.docker('ps','-aq','--no-trunc','--filter',
                               'label=com.docker.compose.project='+project).decode().split()
    if len(discovered)!=8:
        raise native.Failure('full installation birth discovery custody')
    identities = {}
    for identity in discovered:
        if not re.fullmatch('[a-f0-9]{64}',identity):
            raise native.Failure('full installation birth identity')
        profile = runner.inspect(identity)
        service = profile['Config']['Labels'].get('com.docker.compose.service')
        if (profile['Id']!=identity or service not in services or service in identities
                or not native.owned_profile(profile,project,service,approval['owner'])):
            raise native.Failure('complete distinct owned installation births')
        identities[service] = identity
    graph_constructors(runner,approval,native,identities)
    for kind,expected in resources:
        profile = json.loads(runner.docker(kind,'inspect',expected['name']))[0]
        native.resource_guard(profile,expected,project,approval['owner'],kind)
    # Only a complete full-ID cohort and independently verified owned resources
    # retire unknown-birth custody; known stand ownership persists for F03.
    runner.installation_ids = identities
    runner.installation_resources = resources
    runner.unresolved_resources = False
    return identities


def start_postgres(runner, approval, native, identities):
    """Start only the verified fresh PG; reuse the original active deadline."""
    graph_constructors(runner, approval, native, identities)
    check_delivery(runner,approval,native)
    identity = identities['postgres']
    runner.docker('start', identity)
    while True:
        if min(runner.work_end-time.monotonic(), runner.work_utc_end-time.time()) <= 0:
            raise native.Failure('original PG prerequisite deadline')
        profile = runner.inspect(identity)
        native.constructor(profile, approval['services']['postgres'],
                           approval['project'], 'postgres', approval['owner'])
        postgres_password_mounts(profile,approval,native)
        state = profile['State']
        if profile['Id'] != identity or not state['Running'] or state['Pid'] <= 0 or state['OOMKilled']:
            raise native.Failure('actual PG prerequisite process')
        health = state.get('Health', {}).get('Status')
        if health == 'healthy':
            return profile
        if health not in ('starting', 'unhealthy'):
            raise native.Failure('actual PG healthcheck required')
        remaining = min(runner.work_end-time.monotonic(), runner.work_utc_end-time.time())
        if remaining <= 0:
            raise native.Failure('original PG prerequisite deadline')
        time.sleep(min(.1, remaining))


def clock_readback(native, raw):
    """Validate the real clock-read result before any runtime role can start."""
    state = json.loads(raw)
    expected = {
        'version': 1, 'installation': '010400000204',
        'case': 'c-registration-clock-20261001-v1',
        'stand': 'synthetic-qa-zns-registration-fixture',
        'database_address': 'postgres:5432/synthetic_qa_zns_registration_fixture',
        'anchor': '2030-10-02T12:00:00Z',
        'current': '2030-10-02T12:00:00Z', 'revision': 1,
    }
    if not native.same(state, expected):
        raise native.Failure('genuine initial registration clock readback')
    return state


def continuity_readback(native, raw):
    """Check actual read-only DB facts, without contacts, passwords or names."""
    state = json.loads(raw)
    expected = {
        'database': 'synthetic_qa_zns_registration_fixture',
        'session_user': 'zns_registration_operator',
        'database_owner': 'zns_app', 'fixture_owner': 'zns_app',
        'actors': [{'id':'alice','telegram_id':101}, {'id':'bob','telegram_id':202},
                   {'id':'visitor','telegram_id':303}],
        'product_markers': ['product-passport-v1','product-v1','registration-fqa-v1'],
        'booking_count': 0, 'intent_count': 0, 'ingress_count': 0,
        'booking_admins': ['visitor'],
        'payment_admins': [{'event_id':'registration-fixture-a','owner':'bob'}],
        'registration_tiers': [
            {'event_id':'registration-fixture-a','position':0,'starts_at':'2030-10-02T13:00:00+00:00'},
            {'event_id':'registration-fixture-a','position':1,'starts_at':'2030-10-03T13:00:00+00:00'},
            {'event_id':'registration-fixture-b','position':0,'starts_at':'2030-10-02T13:00:00+00:00'}],
        'allocated_roles': ['zns_app','zns_fake','zns_inventory','zns_meter',
                            'zns_registration_operator'],
        'role_memberships': [{'member':'zns_inventory','role':'pg_read_all_stats','admin':False}],
        'privileged_roles': 0,
    }
    if not native.same(state, expected):
        raise native.Failure('genuine fresh database/role/actor continuity')
    return state


def read_continuity(runner, approval, native, identities):
    """Use one literal read-only query with the isolated operator credential."""
    binding = approval['continuity_sql']
    if binding['container_path'] != '/bootstrap/read-continuity.sql':
        raise native.Failure('literal continuity SQL mount')
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        native.read_bytes_pinned(binding['path'],binding['sha256'])
    identity = identities['postgres']
    profile = runner.inspect(identity)
    native.constructor(profile,approval['services']['postgres'],
                       approval['project'],'postgres',approval['owner'])
    postgres_password_mounts(profile,approval,native)
    if (profile['Id'] != identity
            or not native.owned_profile(profile,approval['project'],'postgres',approval['owner'])
            or profile['Image'] != approval['services']['postgres']['image']
            or not profile['State']['Running'] or profile['State']['OOMKilled']):
        raise native.Failure('owned current PG for continuity read')
    # The child has the PG constructor's root UID only to read its existing RO
    # secret. SQL authenticates as the narrow registration operator, not postgres.
    command = ('set -eu; PGPASSWORD=$(cat /run/secrets/operator_password); '
               'export PGPASSWORD; exec psql --no-psqlrc -q -A -t '
               '--host=127.0.0.1 --username=zns_registration_operator '
               '--dbname=synthetic_qa_zns_registration_fixture '
               '-v ON_ERROR_STOP=1 -v ECHO=none -f /bootstrap/read-continuity.sql')
    raw = runner.docker('exec','--user','0',identity,'/bin/sh','-c',command)
    result = continuity_readback(native,raw)
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        native.read_bytes_pinned(binding['path'],binding['sha256'])
    return raw, result


def start_provider(runner, approval, native, identities, bootstrap_results, continuity_raw):
    """Start the current Fake only after actual bootstrap and readback checks."""
    if [(item['step'], item['service']) for item in bootstrap_results] != [
            (index, step['service']) for index, step in enumerate(STEPS, 1)]:
        raise native.Failure('complete actual bootstrap result order')
    clock_readback(native, bootstrap_results[-1]['raw'])
    continuity_readback(native, continuity_raw)
    project, owner = approval['project'], approval['owner']
    for service in ['fake'] + RUNTIME:
        profile = runner.inspect(identities[service])
        native.constructor(profile, approval['services'][service], project, service, owner)
        if (profile['Id'] != identities[service]
                or profile['State']['Status'] != 'created' or profile['State']['Pid'] != 0
                or profile['State']['StartedAt'] != '0001-01-01T00:00:00Z'):
            raise native.Failure('provider/runtime prelaunch state')
    identity = identities['fake']
    check_delivery(runner,approval,native)
    runner.docker('start', identity)
    while True:
        remaining = min(runner.work_end-time.monotonic(), runner.work_utc_end-time.time())
        if remaining <= 0:
            raise native.Failure('original current provider deadline')
        profile = runner.inspect(identity)
        native.constructor(profile, approval['services']['fake'], project, 'fake', owner)
        state = profile['State']
        if profile['Id'] != identity or not state['Running'] or state['Pid'] <= 0 or state['OOMKilled']:
            raise native.Failure('actual current provider process')
        if state.get('Health', {}).get('Status') == 'healthy':
            return profile
        time.sleep(min(.1, remaining))


def start_runtime(runner, approval, native, identities):
    """Consume a separately admitted Linux M0; never arm or extend a window.

    ROOT authenticates the six actual prerequisite receipts before this phase.
    Returning process startup is not health, G600, full/final or Functional QA.
    """
    anchor = approval['installation_anchor']
    if (approval['authority'] != 'ROOT' or anchor['authority'] != 'ROOT'
            or anchor['prerequisites_passed'] != PREREQUISITES
            or anchor['product_source'] != PRODUCT
            or anchor['product_binary_sha256'] != BINARY
            or anchor['boot_id'] != Path('/proc/sys/kernel/random/boot_id').read_text().strip()
            or anchor['time_namespace_inode'] != os.stat('/proc/self/ns/time').st_ino):
        raise native.Failure('same Linux anchor and all six admitted prerequisites')
    mono, utc = anchor['monotonic'], anchor['utc']
    if (type(mono) not in (int,float) or type(utc) not in (int,float)
            or not math.isfinite(mono) or not math.isfinite(utc)
            or mono > time.monotonic() or utc > time.time()
            or anchor['windows'] != {'startup':30,'guard':600,'readiness':90,
                                     'child':22332,'outer':22344}):
        raise native.Failure('original absolute installation windows')
    startup_end, startup_utc = mono+30, utc+30
    project, owner = approval['project'], approval['owner']
    for service in RUNTIME:
        profile = runner.inspect(identities[service])
        native.constructor(profile, approval['services'][service], project, service, owner)
        if (profile['State']['Status'] != 'created' or profile['State']['Pid'] != 0
                or profile['State']['StartedAt'] != '0001-01-01T00:00:00Z'):
            raise native.Failure('six exact never-started runtime constructors')
    remaining = min(startup_end-time.monotonic(), startup_utc-time.time())
    if remaining <= 0:
        raise native.Failure('original F30 startup deadline')
    check_delivery(runner,approval,native)
    remaining = min(startup_end-time.monotonic(), startup_utc-time.time())
    if remaining <= 0:
        raise native.Failure('original F30 authenticated delivery deadline')
    runner.docker('start', *(identities[service] for service in RUNTIME),
                  seconds=min(15,remaining))
    profiles = {}
    for service in RUNTIME:
        if min(startup_end-time.monotonic(),startup_utc-time.time()) <= 0:
            raise native.Failure('original F30 process startup deadline')
        profile = runner.inspect(identities[service])
        if (profile['Id'] != identities[service]
                or not native.owned_profile(profile,project,service,owner)
                or profile['Image'] != approval['services'][service]['image']
                or not profile['State']['Running'] or profile['State']['Pid'] <= 0
                or profile['State']['OOMKilled']):
            raise native.Failure('six actual owned runtime processes')
        profiles[service] = profile
    if min(startup_end-time.monotonic(),startup_utc-time.time()) <= 0:
        raise native.Failure('original F30 final process observation deadline')
    return profiles


def runtime_readiness(runner, approval, native, identities):
    """Observe genuine app HTTP health under the same original M0+90 cutoff."""
    anchor = approval['installation_anchor']
    limit, utc_limit = anchor['monotonic']+90, anchor['utc']+90
    while True:
        remaining = min(limit-time.monotonic(),utc_limit-time.time(),
                        runner.work_end-time.monotonic(),runner.work_utc_end-time.time())
        if remaining <= 0:
            raise native.Failure('original readiness deadline')
        profiles, healthy = {}, True
        for service in ['postgres','fake']+RUNTIME:
            if min(limit-time.monotonic(),utc_limit-time.time()) <= 0:
                raise native.Failure('original readiness cohort deadline')
            profile = runner.inspect(identities[service])
            native.constructor(profile,approval['services'][service],
                               approval['project'],service,approval['owner'])
            if service=='postgres':
                postgres_password_mounts(profile,approval,native)
            state = profile['State']
            if (profile['Id'] != identities[service] or not state['Running']
                    or state['Pid'] <= 0 or state['OOMKilled']):
                raise native.Failure('actual readiness complete cohort process')
            healthcheck = profile['Config'].get('Healthcheck')
            if healthcheck and healthcheck.get('Test') != ['NONE']:
                healthy = healthy and state.get('Health',{}).get('Status') == 'healthy'
            profiles[service] = profile
        healthy = healthy and profiles['app']['State'].get('Health',{}).get('Status') == 'healthy'
        if healthy:
            check_delivery(runner,approval,native)
            if min(limit-time.monotonic(),utc_limit-time.time()) <= 0:
                raise native.Failure('original readiness final observation deadline')
            return profiles
        time.sleep(min(.1,remaining))


def bootstrap_contract(runner, approval, native):
    """Read the authenticated actual artifact, not caller-selected commands."""
    binding = approval['bootstrap']
    with native.intake_deadline(runner.work_end, runner.work_utc_end):
        contract = native.read_pinned(binding['path'], binding['sha256'])
    if (contract['project'] != 'synthetic-qa-c-current'
            or approval['project'] != contract['project']
            or contract['product_source'] != PRODUCT
            or contract['product_binary_sha256'] != BINARY
            or contract['sole_writer'] != 'c_installed_f03_finish'
            or contract['steps'] != STEPS or contract['runtime_services'] != RUNTIME
            or contract['prerequisite_services'] != ['postgres','fake']):
        raise native.Failure('literal current C bootstrap contract')
    windows = contract['installation_windows']
    if (windows['prerequisites_before_arm'] != PREREQUISITES
            or [windows[key] for key in ('startup_seconds','guard_deadline_offset_seconds',
                'readiness_seconds','child_deadline_offset_seconds','outer_deadline_offset_seconds')]
                != [30,600,90,22332,22344]):
        raise native.Failure('original C prerequisites and absolute windows')
    if contract['prerequisite_start_order'] != {
            'before_steps':['postgres'],'after_steps':['fake'],
            'runtime_start_requires':'all six prerequisites passed and ROOT admitted the original Linux anchor'}:
        raise native.Failure('PG/bootstrap/provider/runtime start order')
    return contract


def run_bootstrap(runner, approval, native, rendered):
    """Execute only the seven admitted bootstrap constructors, one at a time.

    Caller owns the verified running PG and existing graph. This function cannot
    start Fake or any runtime service, arm M0 or certify the six prerequisites.
    """
    bootstrap_contract(runner, approval, native)
    plans = approval['bootstrap_constructors']
    if len(plans) != len(STEPS):
        raise native.Failure('seven complete bootstrap native constructors required')
    project, owner = approval['project'], approval['owner']
    results = []
    for index, (step, expected) in enumerate(zip(STEPS, plans), 1):
        check_delivery(runner,approval,native)
        service = step['service']
        if expected['name'] != project+'-'+service+'-1':
            raise native.Failure('literal bootstrap constructor name')
        config = copy.deepcopy(rendered)
        role = config['services'][service]
        if role.get('profiles') != ['maintenance'] or role.get('restart') != 'no':
            raise native.Failure('explicit inactive maintenance constructor')
        if step['command'] is not None:
            role['command'] = step['command']
        role.setdefault('environment',{}).update(step['environment'])
        image = json.loads(runner.docker('image','inspect',expected['image']))[0]
        native.rendered_guard(role,expected,image,config)
        path = runner.output / ('bootstrap-%02d.compose.json' % index)
        raw = json.dumps(native.literal_compose(config),sort_keys=True).encode()
        runner.save(path.name,raw)
        with native.intake_deadline(runner.work_end,runner.work_utc_end):
            native.read_pinned(path,native.digest(raw))
        name = expected['name']
        if runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+re.escape(name)+'$').strip():
            raise native.Failure('fresh serial bootstrap helper required')
        # An empty CREATE response cannot retire discovery responsibility.
        runner.unresolved_resources = True
        runner.docker('compose','--project-name',project,'--file',str(path),
                      'create','--no-build','--pull','never',service)
        ids = runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+re.escape(name)+'$').decode().split()
        if len(ids)!=1 or not re.fullmatch('[a-f0-9]{64}',ids[0]):
            raise native.Failure('bootstrap full-ID discovery custody')
        identity = ids[0]
        profile = runner.inspect(identity)
        if (profile['Id'] != identity or not native.owned_profile(profile,project,service,owner)
                or profile['State']['Status']!='created' or profile['State']['Pid']!=0):
            raise native.Failure('owned never-started bootstrap helper')
        native.constructor(profile,expected,project,service,owner)
        check_delivery(runner,approval,native)
        output = runner.docker('start','--attach',identity,seconds=attach_seconds(runner))
        final = runner.inspect(identity)
        native.constructor(final,expected,project,service,owner)
        if (final['State']['Status']!='exited' or final['State']['Pid']!=0
                or final['State']['ExitCode']!=0 or final['State']['OOMKilled']):
            raise native.Failure('actual bootstrap exit/reap state')
        runner.docker('rm',identity)
        if (runner.docker('ps','-aq','--no-trunc','--filter','id='+identity).strip()
                or runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+re.escape(name)+'$').strip()):
            raise native.Failure('bootstrap full-ID/name absence')
        runner.unresolved_resources = False
        results.append({'step':index,'service':service,'id':identity,'raw':output})
    clock_readback(native, results[-1]['raw'])
    return results
