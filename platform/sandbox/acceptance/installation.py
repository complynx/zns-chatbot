"""Two literal clean-C phases; this source cannot authorize an installation."""
import argparse
import json
import math
import os
from pathlib import Path
import re
import sys
import time

import bootstrap


NATIVE_SHA = 'dbb506b94f55d6b8b3f9c05750ad5b8eae680374ba9e8bdbd68b3d6cc2f513f2'


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
    remaining = min(runner.work_end-time.monotonic(),runner.work_utc_end-time.time())
    raw = runner.docker('start','--attach',identity,seconds=remaining)
    final = runner.inspect(identity)
    native.constructor(final,expected,project,'readonly-probe',owner)
    if (final['State']['Status'] != 'exited' or final['State']['Pid'] != 0
            or final['State']['ExitCode'] != 0 or final['State']['OOMKilled']):
        raise native.Failure('actual read-only probe exit')
    observed = json.loads(raw)
    if (set(observed) != {'uid','input_count','native_ro_open_count','binary_sha256','clock_sha256'}
            or observed['uid'] != 10001 or observed['input_count'] != 5
            or observed['native_ro_open_count'] != 5
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
    identities = bootstrap.create_graph(runner,approval,native)
    bootstrap.start_postgres(runner,approval,native,identities)
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        rendered = native.read_pinned(runner.output/'rendered-compose.json',
                                      approval['rendered_config_sha256'])
    results = bootstrap.run_bootstrap(runner,approval,native,rendered)
    continuity_raw,continuity = bootstrap.read_continuity(runner,approval,native,identities)
    readonly = readonly_delivery(runner,approval,native,identities)
    bootstrap.start_provider(runner,approval,native,identities,results,continuity_raw)
    # The actual source and constructor predicates were consumed above. Only
    # ROOT can accept these observations and admit the separate original M0.
    runner.save('prerequisites.json',json.dumps({
        'state':'OBSERVATIONS_NOT_RUNTIME_ADMISSION','identities':identities,
        'product_source':bootstrap.PRODUCT,'binary_sha256':bootstrap.BINARY,
        'clock':bootstrap.clock_readback(native,results[-1]['raw']),
        'continuity':continuity,'readonly':readonly,
        'prerequisites':bootstrap.PREREQUISITES,
    },sort_keys=True).encode())


def runtime(runner, approval, native):
    """Start the retained graph on ROOT's already armed original Linux M0."""
    binding = approval['prerequisite_observations']
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        observed = native.read_pinned(binding['path'],binding['sha256'])
    if (observed['state'] != 'OBSERVATIONS_NOT_RUNTIME_ADMISSION'
            or observed['product_source'] != bootstrap.PRODUCT
            or observed['binary_sha256'] != bootstrap.BINARY
            or observed['prerequisites'] != bootstrap.PREREQUISITES):
        raise native.Failure('actual authenticated current prerequisite observations')
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
    profiles = bootstrap.start_runtime(runner,approval,native,identities)
    ready = bootstrap.runtime_readiness(runner,approval,native,identities)
    runner.save('startup-readiness.json',json.dumps({
        'state':'STARTUP_READINESS_ONLY_NOT_FULL_GUARD_OR_FUNCTIONAL_QA',
        'identities':identities,'runtime_process_count':len(profiles),
        'app_health':ready['State']['Health']['Status'],
        'installation_anchor':approval['installation_anchor'],
    },sort_keys=True).encode())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--approval',required=True)
    parser.add_argument('--approval-sha256',required=True)
    parser.add_argument('--output',required=True)
    args = parser.parse_args()
    start,utc_start = time.monotonic(),time.time()
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
            or approval.get('operation') not in ('C_INSTALL_PREREQUISITES','C_INSTALL_RUNTIME')
            or approval.get('no_host_source_writers') is not True
            or approval.get('project') != 'synthetic-qa-c-current'
            or approval.get('owner') != 'c_installed_f03_finish'):
        raise native.Failure('literal clean-C prerequisite authority')
    sources = [args.approval,'/runner/run.py']
    sources.extend(item['path'] for item in approval['delivery_files'])
    sources.extend(item['path'] for item in approval['delivery_directories'])
    active,cleanup = 90,30
    if approval['operation']=='C_INSTALL_RUNTIME':
        anchor = approval['installation_anchor']
        # This is not a new clock. The bootstrap startup validator checks boot,
        # namespace, six predicates and all original offsets before START.
        start,utc_start = anchor['monotonic'],anchor['utc']
        if (anchor['authority']!='ROOT' or anchor['prerequisites_passed']!=bootstrap.PREREQUISITES
                or type(start) not in (int,float) or type(utc_start) not in (int,float)
                or not math.isfinite(start) or not math.isfinite(utc_start)
                or start>time.monotonic() or utc_start>time.time()
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
    try:
        if approval['operation']=='C_INSTALL_PREREQUISITES':
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
