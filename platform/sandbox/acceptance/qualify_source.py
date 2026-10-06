"""Fixed Linux L2 caller; the child has no daemon socket or writable source."""
import argparse
import copy
import hashlib
import json
import math
import os
from pathlib import Path
import re
import time
import types

NATIVE_SHA = 'fbe5118a5981cacf2ccde4b82979e8ca608df30c9977bcff3e30af201a514392'
IMAGE = 'sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e'
BINARY = '488a8accc0f4072236a6b779837a28fa6312bf0769573023271ad85d767e1261'
COMMAND = ('python3 -B -m tabnanny /source/bootstrap.py /source/installation.py '
           '/source/qualify_source.py /source/test_bootstrap.py && '
           'python3 -B /source/test_bootstrap.py /source/bootstrap.json /source/native_run.py /source/boot_operator.py && '
           'python3 -B /source/test_private.py -v && '
           'python3 -B /source/test_health.py --rendered /source/rendered.json '
           '--binary /usr/local/bin/zns')
MEMBERS = {'bootstrap.py','installation.py','qualify_source.py','probe_ro.py',
           'prepare_private.py','test_bootstrap.py','test_private.py','test_health.py',
           'bootstrap.json','native_run.py','rendered.json','boot_operator.py',
           'OPERATOR-PROFILE.json','OPERATOR-INPUT.json','capture_guards.py','capture_transport.py',
           'boot_installation.py','INSTALL-OPERATOR-PROFILE.json','INSTALL-INPUT.json'}


def load_native(directory, start, utc_start):
    """Compile one captured native body from ROOT's immutable closed RO mount."""
    directory = Path(directory)
    if (directory.is_symlink()
            or {item.name for item in directory.iterdir()} != {'run.py','test_run.py','README.md'}):
        raise ValueError('closed ROOT native source directory')
    for item in directory.iterdir():
        if item.is_symlink() or not item.is_file() or item.stat().st_nlink!=1:
            raise ValueError('regular ROOT native sources without bytecode')
    source = directory/'run.py'
    if source.stat().st_size > 1048576:
        raise ValueError('bounded ROOT native body')
    with source.open('rb') as stream:
        raw = stream.read(1048577)
    if len(raw)>1048576:
        raise ValueError('bounded captured native body')
    if hashlib.sha256(raw).hexdigest() != NATIVE_SHA:
        raise ValueError('exact captured native body')
    if time.monotonic() >= start+90 or time.time() >= utc_start+90:
        raise ValueError('original source intake deadline')
    native = types.ModuleType('c_source_native')
    native.__file__ = str(source)
    exec(compile(raw,str(source),'exec'),native.__dict__)
    if time.monotonic() >= start+90 or time.time() >= utc_start+90:
        raise ValueError('original source compilation deadline')
    return native


def validate_result(stdout, stderr, render_sha):
    """Require all original22 and three caller-boundary controls, without skips."""
    groups = {
        'Contracts': (
            'actual_bootstrap_artifact_mutations_stop_before_lifecycle',
            'actual_create_failure_retains_unknown_birth_without_start',
            'actual_delivery_rejection_precedes_all_lifecycle',
            'actual_readonly_and_writable_open_controls',
            'bootstrap_rejection_precedes_all_lifecycle',
            'c_attachments_never_override_native15','complete_created_cohort_before_start',
            'fresh_role_and_row_contract','initial_clock_exact_state',
            'installation_main_authenticates_raw_authority_and_anchor_before_output',
            'literal_bootstrap_and_windows',
            'original_startup_and_readiness_expiry_forbid_dependent_work',
            'provider_requires_complete_results_clock_rows_and_full_identity',
            'qualifier_compiles_captured_closed_native_body',
            'qualifier_final_verification_expiry_is_sticky',
            'qualifier_final_verification_uses_real_delivery',
            'qualifier_original_host_window_forbids_delayed_entry',
            'qualifier_terminal_sync_expiry_cannot_accept',
            'readiness_rejects_exited_helper_while_app_healthy',
            'readonly_probe_rejects_changed_argv_before_birth',
            'runtime_actual_observation_mutations_precede_start_and_publication'),
        'PrivateInputsTest': ('existing_input_is_not_overwritten',
            'fresh_inputs_have_exact_shared_and_separate_bindings',
            'storage_fault_removes_only_owned_partial_inputs','symlink_directory_is_not_followed'),
        'HealthTest': ('real_http_200_with_parent_clock_unchanged','real_http_503_is_not_healthy'),
    }
    expected = {(group,'test_'+name) for group,names in groups.items() for name in names}
    result = stdout.decode('utf-8')+'\n'+stderr.decode('utf-8')
    observed = []
    for line in result.splitlines():
        if line.lstrip().startswith('test_'):
            match = re.fullmatch(r'(test_[a-z0-9_]+) \(__main__\.([A-Za-z]+)\.(test_[a-z0-9_]+)\) \.\.\. ok',line)
            if match is None or match[1]!=match[3]:
                raise ValueError('actual unittest error, skip or malformed status')
            observed.append((match[2],match[1]))
    if len(observed)!=27 or len(set(observed))!=27 or set(observed)!=expected:
        raise ValueError('complete actual bootstrap/private/health membership')
    counts = re.findall(r'^Ran ([0-9]+) tests in [0-9.]+s$',result,re.MULTILINE)
    if [int(value) for value in counts]!=[21,4,2] or result.count('\nOK\n')!=3:
        raise ValueError('complete successful zero-skip suite summaries')
    if re.search(r'^OK \(.*\)|^FAILED\b|^ERROR:|^FAIL:',result,re.MULTILINE):
        raise ValueError('unittest failure or positive skip summary')
    health = [line for line in stdout.decode('utf-8').splitlines() if line.startswith('HEALTH_CONTROLS')]
    render = [line for line in stdout.decode('utf-8').splitlines() if line.startswith('HEALTH_RENDER')]
    if health!=['HEALTH_CONTROLS count=2 skipped=0 pass=true']:
        raise ValueError('typed zero-skip health result')
    if render!=['HEALTH_RENDER services=12 sha256='+render_sha]:
        raise ValueError('actual complete pinned health render')


def final_sources(runner, approval, native, before):
    try:
        runner.publication_deadline(cleanup=True)
        after = native.delivery_snapshot(runner,approval,set(),cleanup=True)
        runner.publication_deadline(cleanup=True)
        if not native.same(before,after):
            raise native.Failure('source qualification delivery changed')
        runner.save('source-after.json',json.dumps(after,sort_keys=True).encode(),cleanup=True)
    except Exception:
        runner.fault('source qualification final binding failure')


def mapped_window(raw, sha, root_sha):
    """Clamp local endpoints to the authenticated original host UTC cutoffs."""
    if len(raw)>16384 or hashlib.sha256(raw).hexdigest()!=sha:
        raise ValueError('captured original window hash')
    window = json.loads(raw)
    fields = ('host_monotonic','host_utc','active_monotonic','total_monotonic',
              'active_utc','total_utc','sample_monotonic','sample_utc',
              'clamped_active_utc','clamped_total_utc')
    if (set(window)!={'authority','root_sha256','clock_domain',*fields}
            or window['authority']!='ROOT_BOOTSTRAP' or window['root_sha256']!=root_sha
            or window['clock_domain']!='host_original_dual_cutoffs_v1'
            or any(type(window[key]) not in (int,float) or not math.isfinite(window[key])
                   for key in fields)):
        raise ValueError('typed authenticated original host window')
    for endpoint,origin,seconds in (
            ('active_monotonic','host_monotonic',90),('total_monotonic','host_monotonic',120),
            ('active_utc','host_utc',90),('total_utc','host_utc',120)):
        if not math.isclose(window[endpoint]-window[origin],seconds,rel_tol=0,abs_tol=1e-6):
            raise ValueError('unchanged original host offsets')
    if window['sample_monotonic']<window['host_monotonic'] or window['sample_utc']<window['host_utc']:
        raise ValueError('causal host clock sample')
    for phase in ('active','total'):
        intended = min(window[phase+'_utc'],window['sample_utc']+
                       window[phase+'_monotonic']-window['sample_monotonic'])
        if not math.isclose(window['clamped_'+phase+'_utc'],intended,rel_tol=0,abs_tol=1e-6):
            raise ValueError('original dual-clock UTC clamp')
    mono,utc = time.monotonic(),time.time()
    if utc<window['sample_utc'] or utc>=window['clamped_active_utc']:
        raise ValueError('expired or incompatible original host window')
    active_remaining = min(window['active_monotonic']-window['sample_monotonic'],
                           window['clamped_active_utc']-utc)
    total_remaining = min(window['total_monotonic']-window['sample_monotonic'],
                          window['clamped_total_utc']-utc)
    if active_remaining<=0 or total_remaining<=active_remaining:
        raise ValueError('original host remaining window')
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
    namespace = os.stat('/proc/self/ns/time').st_ino
    if not re.fullmatch('[0-9a-f-]{36}',boot) or type(namespace) is not int or namespace<=0:
        raise ValueError('actual Linux boot/time namespace')
    return {'active_end':mono+active_remaining,'total_end':mono+total_remaining,
            'active_utc':window['clamped_active_utc'],'total_utc':window['clamped_total_utc'],
            'local_monotonic':mono,'local_utc':utc,'boot_id':boot,
            'time_namespace_inode':namespace,'window_sha256':sha,'host_window':window}


def source_contract(approval, native):
    capsule,binary = approval['capsule_path'],approval['binary_path']
    for path in (capsule,binary):
        if (not path.startswith(native.DAEMON_SOURCE_PREFIX) or ',' in path
                or '\n' in path or '\r' in path or '..' in Path(path).parts
                or Path(path).as_posix()!=path):
            raise native.Failure('canonical owned qualification inputs')
    files = approval['delivery_files']
    directories = approval['delivery_directories']
    if (len(files)!=len(MEMBERS)+1 or len(directories)!=1
            or directories[0]['path']!=capsule or directories[0]['directories']
            or set(directories[0]['members'])!=MEMBERS
            or {item['path'] for item in files}!={binary,*[capsule+'/'+name for name in MEMBERS]}):
        raise native.Failure('complete closed qualification delivery')
    pins = {item['path']:item['sha256'] for item in files}
    if (pins[binary]!=BINARY or pins[capsule+'/native_run.py']!=NATIVE_SHA
            or pins[capsule+'/rendered.json']!=approval['render_sha256']):
        raise native.Failure('current binary/native/render cross-binding')
    expected = approval['helper_profile']
    mounts = [{'Type':'bind','Source':capsule,'Target':'/source','ReadOnly':True},
              {'Type':'bind','Source':binary,'Target':'/usr/local/bin/zns','ReadOnly':True}]
    if (expected['Config']['User']!='10001:10001'
            or expected['Config']['Cmd']!=['-c',COMMAND]
            or expected['Config']['Entrypoint']!=['/bin/sh']
            or expected['Config']['Labels'].get('synthetic.owner')!=approval['owner']
            or not native.same(expected['HostConfig']['Mounts'],mounts)
            or not native.same(expected['HostConfig']['NanoCpus'],200000000)
            or not native.same(expected['HostConfig']['Memory'],268435456)
            or not native.same(expected['HostConfig']['MemorySwap'],268435456)
            or not native.same(expected['HostConfig']['PidsLimit'],64)
            or expected['HostConfig']['NetworkMode']!='none'
            or expected['HostConfig']['CapDrop']!=['ALL']
            or expected['HostConfig']['CapAdd'] not in ([],None)
            or expected['HostConfig']['Privileged'] is not False
            or expected['HostConfig']['ReadonlyRootfs'] is not True
            or expected['HostConfig']['SecurityOpt']!=['no-new-privileges']):
        raise native.Failure('literal L2 constructor before birth')


def helper_profile(runner, approval, native, identity, started, cleanup=False):
    profile = runner.inspect(identity,cleanup=cleanup)
    state = profile['State']
    if started is None:
        if state['Status']=='created' and state['StartedAt']=='0001-01-01T00:00:00Z':
            started = False
        elif (state['Status']=='exited' and isinstance(state['StartedAt'],str)
              and state['StartedAt']!='0001-01-01T00:00:00Z'):
            started = True
        else:
            raise native.Failure('observed quiescent L2 phase')
    expected = copy.deepcopy(approval['helper_profile'])
    expected['Config']['Hostname'] = identity[:12]
    if started:
        expected['HostConfig']['OomKillDisable'] = None
    if (profile['Id']!=identity or profile['Name']!='/'+approval['helper_name']
            or profile['Image']!=IMAGE or profile['Path']!='/bin/sh'
            or profile['Args']!=['-c',COMMAND] or not native.same(profile['RestartCount'],0)
            or not native.same(profile['Config'],expected['Config'])
            or not native.same(profile['HostConfig'],expected['HostConfig'])
            or not native.same(profile['Mounts'],expected['Mounts'])):
        raise native.Failure('complete owned L2 helper constructor')
    flags = ('Running','Paused','Restarting','OOMKilled','Dead')
    if (any(type(state[key]) is not bool or state[key] for key in flags)
            or type(state['Pid']) is not int or state['Pid']!=0
            or state['Status']!=('exited' if started else 'created')
            or type(state['ExitCode']) is not int
            or (not started and state['StartedAt']!='0001-01-01T00:00:00Z')):
        raise native.Failure('quiescent L2 helper state')
    networks = copy.deepcopy(approval['terminal_networks' if started else 'created_networks'])
    actual = profile['NetworkSettings']['Networks']
    if started and re.fullmatch('[a-f0-9]{64}',actual.get('none',{}).get('NetworkID','')):
        networks['none']['NetworkID'] = actual['none']['NetworkID']
    if not native.same(actual,networks):
        raise native.Failure('complete networknone L2 helper')
    return profile


def qualify(runner, approval, native):
    source_contract(approval,native)
    before = native.delivery_snapshot(runner,approval,{approval['binary_path'],approval['capsule_path']})
    runner.save('source-before.json',json.dumps(before,sort_keys=True).encode())
    capsule,binary = approval['capsule_path'],approval['binary_path']
    with native.intake_deadline(runner.work_end,runner.work_utc_end):
        if {p.name for p in Path(capsule).iterdir()}!=MEMBERS:
            raise native.Failure('exact closed L2 capsule')
    identity = None
    name = approval['helper_name']
    try:
        if runner.docker('info','--format','{{.ID}}').decode().strip()!=approval['daemon_id']:
            raise native.Failure('exact admitted daemon')
        if runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+name+'$').strip():
            raise native.Failure('fresh L2 helper')
        if json.loads(runner.docker('image','inspect',IMAGE))[0]['Id']!=IMAGE:
            raise native.Failure('exact local L2 image')
        argv = ['create','--pull','never','--name',name,'--label',
                'synthetic.owner='+approval['owner'],'--user','10001:10001',
                '--cpus','0.2','--memory','256m','--memory-swap','256m','--pids-limit','64',
                '--network','none','--read-only','--cap-drop','ALL','--security-opt',
                'no-new-privileges','--restart','no','--no-healthcheck','--workdir','/',
                '--entrypoint','/bin/sh','--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777',
                '--mount','type=bind,source='+capsule+',target=/source,readonly',
                '--mount','type=bind,source='+binary+',target=/usr/local/bin/zns,readonly',
                IMAGE,'-c',COMMAND]
        runner.unresolved_resources = True
        raw = runner.docker(*argv)
        candidate = raw.decode().strip()
        if not re.fullmatch('[a-f0-9]{64}',candidate):
            raise native.Failure('full L2 birth identity')
        identity = candidate
        helper_profile(runner,approval,native,identity,False)
        runner.docker('start','-a',identity)
        validate_result(*runner.records[-1]['raw'],approval['render_sha256'])
    except Exception as error:
        runner.fault(str(error) if isinstance(error,native.Failure) else 'L2 source qualification failure')
        if identity is None and runner.records:
            record = runner.records[-1]
            if record['argv'][1:2]==['create']:
                candidate = record['raw'][0].decode(errors='replace').strip()
                if re.fullmatch('[a-f0-9]{64}',candidate):
                    identity = candidate
    finally:
        if identity is not None and not runner.pending and not runner.pending_writers:
            try:
                helper_profile(runner,approval,native,identity,None,cleanup=True)
                runner.docker('rm',identity,cleanup=True)
                by_id = runner.docker('ps','-aq','--no-trunc','--filter','id='+identity,cleanup=True)
                by_name = runner.docker('ps','-aq','--no-trunc','--filter','name=^/'+name+'$',cleanup=True)
                if by_id.strip() or by_name.strip():
                    raise native.Failure('independent L2 ID/name absence')
                runner.unresolved_resources = False
            except Exception:
                runner.fault('L2 helper release failure')
        if not runner.pending and not runner.pending_writers and not runner.unresolved_resources:
            final_sources(runner,approval,native,before)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--approval',required=True)
    parser.add_argument('--approval-sha256',required=True)
    parser.add_argument('--output',required=True)
    parser.add_argument('--window',required=True)
    parser.add_argument('--window-sha256',required=True)
    args = parser.parse_args()
    window_path = Path(args.window)
    if window_path.is_symlink() or not window_path.is_file() or window_path.stat().st_size>16384:
        raise ValueError('bounded original host window')
    with window_path.open('rb') as stream:
        window_raw = stream.read(16385)
    window = mapped_window(window_raw,args.window_sha256,args.approval_sha256)
    start,utc = window['active_end']-90,window['active_utc']-90
    native = load_native('/runner',start,utc)
    with native.intake_deadline(start+90,utc+90):
        approval = native.read_pinned(args.approval,args.approval_sha256)
    if (approval.get('authority')!='ROOT' or approval.get('operation')!='C_SOURCE_QUALIFICATION'
            or approval.get('owner')!='c_installed_f03_finish'
            or approval.get('passive_native_custody_allowed') is not True
            or approval.get('no_host_source_writers') is not True
            or approval.get('clock_domain')!='host_original_dual_cutoffs_v1'
            or not re.fullmatch('synthetic-qa-c-l2-linux-[a-z0-9-]+',approval.get('helper_name',''))):
        raise native.Failure('literal ROOT source qualification authority')
    source_contract(approval,native)
    sources = [args.approval,args.window,'/runner/run.py']
    sources.extend(item['path'] for item in approval['delivery_files'])
    sources.extend(item['path'] for item in approval['delivery_directories'])
    runner = native.Runner(args.output,start=start,utc_start=utc,sources=sources)
    runner.end = min(runner.end,window['total_end'])
    runner.utc_end = min(runner.utc_end,window['total_utc'])
    try:
        runner.save('original-window.json',json.dumps(window,sort_keys=True).encode())
        qualify(runner,approval,native)
    except Exception:
        runner.fault('source qualification intake failure')
    return runner.finish()


if __name__=='__main__':
    raise SystemExit(main())
