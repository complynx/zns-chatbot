"""Two literal clean-C phases; this source cannot authorize an installation."""
import sys
if sys.flags.isolated != 1 or getattr(sys.flags,'safe_path',False) is not True:
    raise SystemExit('Isolated safe-path Python (-I) required')
import argparse
import json
import math
import os
from pathlib import Path
import re
import time

# The trusted host authenticates this complete immutable RO delivery before birth.
sys.path.insert(0,str(Path(__file__).resolve().parent))
import bootstrap
import probe_ro
import qualify_source


NATIVE_SHA = 'fbe5118a5981cacf2ccde4b82979e8ca608df30c9977bcff3e30af201a514392'


def authenticate_delivery(runner, approval, native, prior=None):
    """Bind the graph, maintenance and probe to one closed physical delivery."""
    def mounts(plan):
        return {item['Destination']:item for item in plan['invariants']['Mounts']}
    app = mounts(approval['services']['app'])
    probe = mounts(approval['readonly_probe'])
    source = str(Path(app['/etc/zns/runtime.yaml']['Source']).parent)
    private = probe['/private']['Source']
    if (probe['/etc/zns']['Source'] != source
            or probe['/current/zns']['Source'] != app['/usr/local/bin/zns']['Source']
            or probe['/run/registration-clock']['Name'] != app['/run/registration-clock']['Name']
            or approval['bootstrap']['path'] != source+'/bootstrap.json'
            or approval['continuity_sql']['path'] != source+'/bootstrap/read-continuity.sql'):
        raise native.Failure('graph/bootstrap/probe actual input cross-binding')
    required = {approval['compose_file'],approval['bootstrap']['path'],
                approval['continuity_sql']['path'],source,private}
    plans = [*approval['services'].values(),*approval['bootstrap_constructors'],
             approval['readonly_probe']]
    for plan in plans:
        required.update(item['Source'] for item in plan['invariants']['Mounts']
                        if item['Type']=='bind')
    expected_names = sorted([role+'.password' for role in
                             ('postgres','app','meter','inventory','fake','operator')]+
                            [role+'.env' for role in
                             ('app','owner','fake','operator','media','sticker','roles','inventory')])
    roots = {item['path']:item for item in approval['delivery_directories']}
    if (private not in roots or roots[private]['members'] != expected_names
            or roots[private]['directories'] != []):
        raise native.Failure('complete private14 delivery topology')
    postgres = mounts(approval['services']['postgres'])
    for role in ('postgres','app','meter','inventory','fake','operator'):
        if postgres['/run/secrets/'+role+'_password']['Source'] != private+'/'+role+'.password':
            raise native.Failure('graph/private14 credential cross-binding')
    files = {item['path']:item for item in approval['delivery_files']}
    bindings = {'runtime_config':source+'/runtime.yaml','app_env':private+'/app.env',
                'owner_env':private+'/owner.env'}
    for key,path in bindings.items():
        if approval['readonly_inputs'][key] != files[path]['sha256']:
            raise native.Failure('actual graph/probe byte pin cross-binding')
    if files[app['/usr/local/bin/zns']['Source']]['sha256'] != bootstrap.BINARY:
        raise native.Failure('actual current product delivery hash')
    observed = bootstrap.delivery_snapshot(runner,approval,native,required)
    # Compose has expanded env_file into Config.Env. Consume those same pinned
    # app/owner bytes, not independently selected paths with equivalent names.
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        app_env = probe_ro.environment(native.read_bytes_pinned(
            bindings['app_env'],files[bindings['app_env']]['sha256']))
        owner_env = probe_ro.environment(native.read_bytes_pinned(
            bindings['owner_env'],files[bindings['owner_env']]['sha256']))
    actual_env = dict(item.split('=',1) for item in approval['services']['app']['invariants']['Config.Env'])
    if (any(actual_env.get(key)!=value for key,value in app_env.items())
            or any(app_env.get(key)!=value for key,value in owner_env.items())):
        raise native.Failure('actual app/owner constructor credential literals')
    if prior is not None and not native.same(prior,observed):
        raise native.Failure('retained prerequisite delivery changed')
    runner.install_delivery_required = required
    runner.install_delivery_before = observed
    runner.save('delivery-before.json',json.dumps(observed,sort_keys=True).encode())
    return observed


def readonly_delivery(runner, approval, native, identities):
    """Run one exact UID10001 probe against the actual five protected inputs."""
    expected = approval['readonly_probe']
    project, owner = approval['project'], approval['owner']
    if (expected['name'] != project+'-readonly-probe'
            or expected['invariants']['Config.User'] != '10001:10001'
            or expected['network_contract']['mode'] != 'none'):
        raise native.Failure('literal app/owner read-only probe role')
    mounts = expected['invariants']['Mounts']
    if (len(mounts) != 4 or any(item['RW'] is not False for item in mounts)
            or {item['Destination'] for item in mounts} != {
                '/etc/zns','/current/zns','/private','/run/registration-clock'}):
        raise native.Failure('four complete read-only probe inputs')
    command = expected['invariants']['Config.Cmd']
    pins = approval['readonly_inputs']
    literal = ['-B','/etc/zns/probe_ro.py','--config-sha256',pins['runtime_config'],
               '--app-sha256',pins['app_env'],'--owner-sha256',pins['owner_env']]
    if (expected['invariants']['Config.Entrypoint'] != ['python3']
            or command != literal
            or not all(re.fullmatch('[a-f0-9]{64}',value) for value in pins.values())):
        raise native.Failure('literal protected-byte probe command')
    name = expected['name']
    if runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+re.escape(name)+'$').strip():
        raise native.Failure('fresh read-only probe identity')
    image = 'sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e'
    if expected['image'] != image:
        raise native.Failure('existing exact Linux probe runtime')
    argv = ['create','--pull','never','--name',name,'--user','10001:10001',
            '--label','synthetic.owner='+owner,'--label','com.docker.compose.project='+project,
            '--label','com.docker.compose.service=readonly-probe',
            '--cpus','0.2','--memory','256m','--memory-swap','256m','--pids-limit','64',
            '--read-only','--network','none','--cap-drop','ALL',
            '--security-opt','no-new-privileges','--restart','no','--no-healthcheck',
            '--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777',
            '--entrypoint','python3']
    for mount in sorted(mounts,key=lambda item:item['Destination']):
        if mount['Type'] == 'bind' and mount['Destination']!='/run/registration-clock':
            source = mount['Source']
        elif (mount['Type'] == 'volume' and mount['Destination']=='/run/registration-clock'
              and mount['Name']==approval['volumes']['registration-clock']['name']):
            source = mount['Name']
        else:
            raise native.Failure('exact read-only input source type')
        argv.extend(['--mount','type='+mount['Type']+',source='+source+
                     ',target='+mount['Destination']+',readonly'])
    argv.extend([image,*literal])
    if argv != approval['readonly_probe_create']:
        raise native.Failure('complete literal probe composition before birth')
    bootstrap.check_delivery(runner,approval,native)
    runner.unresolved_resources = True
    raw = runner.docker(*argv)
    identity = raw.decode().strip()
    if not re.fullmatch('[a-f0-9]{64}',identity):
        raise native.Failure('probe birth full-ID custody')
    profile = runner.inspect(identity)
    native.constructor(profile,expected,project,'readonly-probe',owner)
    if (profile['Id'] != identity or profile['State']['Status'] != 'created'
            or profile['State']['Pid'] != 0
            or profile['State']['StartedAt'] != '0001-01-01T00:00:00Z'):
        raise native.Failure('never-started full read-only probe')
    bootstrap.check_delivery(runner,approval,native)
    raw = runner.docker('start','--attach',identity,seconds=bootstrap.attach_seconds(runner))
    final = runner.inspect(identity)
    native.constructor(final,expected,project,'readonly-probe',owner)
    if (final['State']['Status'] != 'exited' or final['State']['Pid'] != 0
            or final['State']['ExitCode'] != 0 or final['State']['OOMKilled']):
        raise native.Failure('actual read-only probe exit')
    observed = json.loads(raw)
    if (set(observed) != {'uid','input_count','native_ro_open_count','binary_sha256','clock_sha256',
                         'private_other_role_denied_count'}
            or observed['uid'] != 10001 or observed['input_count'] != 5
            or observed['native_ro_open_count'] != 5
            or type(observed['private_other_role_denied_count']) is not int
            or observed['private_other_role_denied_count'] != 12
            or observed['binary_sha256'] != bootstrap.BINARY
            or not re.fullmatch('[a-f0-9]{64}',observed['clock_sha256'])):
        raise native.Failure('five genuine bounded native read-only probes')
    runner.docker('rm',identity)
    if (runner.docker('ps','-aq','--no-trunc','--filter','id='+identity).strip()
            or runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+re.escape(name)+'$').strip()):
        raise native.Failure('read-only probe full-ID/name absence')
    runner.unresolved_resources = False
    return observed


def prerequisites(runner, approval, native):
    bootstrap.bootstrap_contract(runner,approval,native)
    authenticate_delivery(runner,approval,native)
    identities = bootstrap.create_graph(runner,approval,native)
    bootstrap.start_postgres(runner,approval,native,identities)
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        rendered = native.read_pinned(runner.output/'rendered-compose.json',
                                      approval['rendered_config_sha256'])
    results = bootstrap.run_bootstrap(runner,approval,native,rendered)
    continuity_raw,continuity = bootstrap.read_continuity(runner,approval,native,identities)
    readonly = readonly_delivery(runner,approval,native,identities)
    bootstrap.start_provider(runner,approval,native,identities,results,continuity_raw)
    delivery = bootstrap.check_delivery(runner,approval,native)
    runner.save('delivery-after.json',json.dumps(delivery,sort_keys=True).encode())
    # The actual source and constructor predicates were consumed above. Only
    # ROOT can accept these observations and admit the separate original M0.
    runner.save('prerequisites.json',json.dumps({
        'state':'OBSERVATIONS_NOT_RUNTIME_ADMISSION','identities':identities,
        'product_source':bootstrap.PRODUCT,'binary_sha256':bootstrap.BINARY,
        'clock':bootstrap.clock_readback(native,results[-1]['raw']),
        'continuity':continuity,'readonly':readonly,'delivery':delivery,
        'prerequisites':bootstrap.PREREQUISITES,
    },sort_keys=True).encode())


def runtime(runner, approval, native):
    """Start the retained graph on ROOT's already armed original Linux M0."""
    anchor = approval['installation_anchor']
    # This phase publishes readiness only. G600 remains a separate mandatory
    # observation; no intake, native call or receipt may extend readiness90.
    runner.work_end = min(runner.work_end,anchor['monotonic']+90)
    runner.work_utc_end = min(runner.work_utc_end,anchor['utc']+90)
    binding = approval['prerequisite_observations']
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        observed = native.read_pinned(binding['path'],binding['sha256'])
    if (observed['state'] != 'OBSERVATIONS_NOT_RUNTIME_ADMISSION'
            or observed['product_source'] != bootstrap.PRODUCT
            or observed['binary_sha256'] != bootstrap.BINARY
            or observed['prerequisites'] != bootstrap.PREREQUISITES):
        raise native.Failure('actual authenticated current prerequisite observations')
    authenticate_delivery(runner,approval,native,observed['delivery'])
    identities = observed['identities']
    if set(identities) != {'postgres','fake',*bootstrap.RUNTIME}:
        raise native.Failure('complete retained installation identities')
    if (len(set(identities.values()))!=8
            or not all(re.fullmatch('[a-f0-9]{64}',value) for value in identities.values())):
        raise native.Failure('distinct retained full installation identities')
    runner.installation_ids = identities
    bootstrap.clock_readback(native,json.dumps(observed['clock']))
    bootstrap.continuity_readback(native,json.dumps(observed['continuity']))
    # Retained prerequisite roles are checked again; no START, reset or fixture
    # rewrite is performed for either prerequisite process in this phase.
    for service in ('postgres','fake'):
        profile = runner.inspect(identities[service])
        native.constructor(profile,approval['services'][service],
                           approval['project'],service,approval['owner'])
        state = profile['State']
        if (profile['Id']!=identities[service] or not state['Running']
                or state['Pid']<=0 or state['OOMKilled']
                or state.get('Health',{}).get('Status')!='healthy'):
            raise native.Failure('actual retained prerequisite delivery')
    bootstrap.start_runtime(runner,approval,native,identities)
    ready = bootstrap.runtime_readiness(runner,approval,native,identities)
    delivery = bootstrap.check_delivery(runner,approval,native)
    runner.save('delivery-after.json',json.dumps(delivery,sort_keys=True).encode())
    runner.save('startup-readiness.json',json.dumps({
        'state':'STARTUP_READINESS_ONLY_NOT_FULL_GUARD_OR_FUNCTIONAL_QA',
        'identities':identities,'runtime_process_count':len(bootstrap.RUNTIME),
        'cohort_process_count':len(ready),'app_health':ready['app']['State']['Health']['Status'],
        'installation_anchor':approval['installation_anchor'],
    },sort_keys=True).encode())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--approval',required=True)
    parser.add_argument('--approval-sha256',required=True)
    parser.add_argument('--output',required=True)
    parser.add_argument('--operation',choices=('C_INSTALL_PREREQUISITES','C_INSTALL_RUNTIME'),
                        default='C_INSTALL_PREREQUISITES')
    parser.add_argument('--window')
    parser.add_argument('--window-sha256')
    args = parser.parse_args()
    start,utc_start = time.monotonic(),time.time()
    window = None
    if args.operation=='C_INSTALL_PREREQUISITES' and args.window is None and args.window_sha256 is None:
        raise ValueError('authenticated original prerequisite window before native intake')
    if args.operation=='C_INSTALL_RUNTIME' and (args.window is not None or args.window_sha256 is not None):
        raise ValueError('runtime uses its separately authenticated Linux anchor')
    if args.window is not None or args.window_sha256 is not None:
        if args.window is None or args.window_sha256 is None:
            raise ValueError('complete original prerequisite window binding')
        path = Path(args.window)
        if path.is_symlink() or not path.is_file() or path.stat().st_size>16384:
            raise ValueError('bounded original prerequisite window')
        with path.open('rb') as stream:
            raw = stream.read(16385)
        window = qualify_source.mapped_window(raw,args.window_sha256,args.approval_sha256)
        start,utc_start = window['active_end']-90,window['active_utc']-90
    # ROOT binds the complete closed /runner directory RO before this trusted
    # interpreter starts. No caller-selected module path or unbounded loader
    # read is introduced; subsequent byte intake uses the accepted primitive.
    sys.path.insert(0,'/runner')
    import run as native
    with native.intake_deadline(start+90,utc_start+90):
        native.read_bytes_pinned('/runner/run.py',NATIVE_SHA)
        approval = native.read_pinned(args.approval,args.approval_sha256)
    if (approval.get('authority') != 'ROOT'
            or approval.get('passive_native_custody_allowed') is not True
            or approval.get('operation') != args.operation
            or approval.get('no_host_source_writers') is not True
            or approval.get('project') != 'synthetic-qa-c-current'
            or approval.get('owner') != 'c_installed_f03_finish'):
        raise native.Failure('literal clean-C prerequisite authority')
    sources = [args.approval,'/runner/run.py']
    sources.extend(item['path'] for item in approval['delivery_files'])
    sources.extend(item['path'] for item in approval['delivery_directories'])
    active,cleanup = 90,30
    if approval['operation']=='C_INSTALL_PREREQUISITES':
        if window is None or approval.get('clock_domain')!='host_original_dual_cutoffs_v1':
            raise native.Failure('authenticated original prerequisite window before output')
        sources.append(args.window)
    else:
        if window is not None:
            raise native.Failure('runtime uses its separately authenticated Linux anchor')
        anchor = approval['installation_anchor']
        # This is not a new clock. The bootstrap startup validator checks boot,
        # namespace, six predicates and all original offsets before START.
        start,utc_start = anchor['monotonic'],anchor['utc']
        if (anchor['authority']!='ROOT' or anchor['prerequisites_passed']!=bootstrap.PREREQUISITES
                or type(start) not in (int,float) or type(utc_start) not in (int,float)
                or not math.isfinite(start) or not math.isfinite(utc_start)
                or start>time.monotonic() or utc_start>time.time()
                or start+90<=time.monotonic() or utc_start+90<=time.time()
                or anchor['product_source']!=bootstrap.PRODUCT
                or anchor['product_binary_sha256']!=bootstrap.BINARY
                or anchor['boot_id']!=Path('/proc/sys/kernel/random/boot_id').read_text().strip()
                or anchor['time_namespace_inode']!=os.stat('/proc/self/ns/time').st_ino
                or anchor['windows']!={'startup':30,'guard':600,'readiness':90,
                                      'child':22332,'outer':22344}):
            raise native.Failure('original authenticated runtime anchor before output')
        active,cleanup = 600,120
        sources.append(approval['prerequisite_observations']['path'])
    runner = native.Runner(args.output,start=start,utc_start=utc_start,
                           active=active,cleanup=cleanup,sources=sources)
    if window is not None:
        runner.end = min(runner.end,window['total_end'])
        runner.utc_end = min(runner.utc_end,window['total_utc'])
    try:
        if approval['operation']=='C_INSTALL_PREREQUISITES':
            runner.save('original-window.json',json.dumps(window,sort_keys=True).encode())
            prerequisites(runner,approval,native)
        else:
            runtime(runner,approval,native)
    except Exception as error:
        runner.fault(str(error) if isinstance(error,native.Failure) else 'installation prerequisite failure')
        # Already known retained stand ownership does not disappear on a later
        # failure. Keep it explicit for exact operator-controlled retirement.
        if getattr(runner,'installation_ids',None):
            runner.unresolved_resources = True
    return runner.finish()


if __name__ == '__main__':
    raise SystemExit(main())
