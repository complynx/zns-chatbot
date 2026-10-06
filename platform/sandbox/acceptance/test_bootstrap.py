"""Focused data-contract controls; these do not accept installation runtime."""
import json
import copy
import hashlib
import importlib.util
import io
import os
import sys
import tempfile
import time
import types
from contextlib import nullcontext, redirect_stderr
import unittest
from unittest.mock import patch
from pathlib import Path

import bootstrap
import installation
import probe_ro
import qualify_source


class Native:
    class Failure(Exception):
        pass

    @staticmethod
    def same(left, right):
        return json.dumps(left,sort_keys=True) == json.dumps(right,sort_keys=True)


class Contracts(unittest.TestCase):
    @staticmethod
    def native():
        spec = importlib.util.spec_from_file_location('acceptance_native',sys.argv[2])
        native = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(native)
        return native

    def test_qualifier_compiles_captured_closed_native_body(self):
        raw = Path(sys.argv[2]).read_bytes()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root/'run.py').write_bytes(raw)
            (root/'test_run.py').write_text('')
            (root/'README.md').write_text('')
            original_compile = compile

            def changed_path(source, filename, mode, **kwargs):
                (root/'run.py').write_text('raise RuntimeError("reopened source")')
                return original_compile(source,filename,mode,**kwargs)

            with patch('builtins.compile',side_effect=changed_path):
                native = qualify_source.load_native(root,time.monotonic(),time.time())
            self.assertEqual(native.digest(b'captured'),hashlib.sha256(b'captured').hexdigest())
            for clock in ('monotonic','time'):
                (root/'run.py').write_bytes(raw)
                ticks = {'expired':False}
                def expire_compile(source,filename,mode,**kwargs):
                    code = original_compile(source,filename,mode,**kwargs)
                    ticks['expired'] = True
                    return code
                now_mono,now_utc = time.monotonic(),time.time()
                original_clock = getattr(time,clock)
                def current_clock():
                    return ((now_mono if clock=='monotonic' else now_utc)+91
                            if ticks['expired'] else original_clock())
                with self.subTest(compilation_clock=clock), \
                        patch('builtins.compile',side_effect=expire_compile), \
                        patch.object(time,clock,side_effect=current_clock), \
                        patch('builtins.exec') as execute:
                    with self.assertRaisesRegex(ValueError,'compilation deadline'):
                        qualify_source.load_native(root,now_mono,now_utc)
                    execute.assert_not_called()
            for name in ('guards.py','run.pyc','__pycache__'):
                (root/'run.py').write_bytes(raw)
                extra = root/name
                if name=='__pycache__':
                    extra.mkdir()
                else:
                    extra.write_bytes(b'undeclared')
                with self.subTest(local=name),self.assertRaisesRegex(ValueError,'closed ROOT'):
                    qualify_source.load_native(root,time.monotonic(),time.time())
                if extra.is_dir():
                    extra.rmdir()
                else:
                    extra.unlink()
            (root/'run.py').write_bytes(b'wrong source')
            with self.assertRaisesRegex(ValueError,'exact captured'):
                qualify_source.load_native(root,time.monotonic(),time.time())

    def test_qualifier_final_verification_expiry_is_sticky(self):
        native = self.native()
        for boundary in ('source_read','completed_late'):
            with self.subTest(boundary=boundary),tempfile.TemporaryDirectory() as temporary:
                source = Path(temporary)/'source'
                source.mkdir()
                path = source/'owned'
                path.write_bytes(b'actual delivered bytes')
                approval = {'no_host_source_writers':True,'delivery_files':[{
                    'path':str(path),'bytes':path.stat().st_size,
                    'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}],
                    'delivery_directories':[{'path':str(source),'members':['owned'],'directories':[]}]}
                runner = native.Runner(str(Path(temporary)/'output'),active=1,cleanup=1)
                runner.end = time.monotonic()+.05
                runner.utc_end = time.time()+1

                def delayed(*_args,**_kwargs):
                    time.sleep(.2)
                    return []

                if boundary=='source_read':
                    # The real delivery guard interrupts its actual read boundary.
                    delay = patch.object(native.os,'read',side_effect=delayed)
                    primary = 'source qualification final binding failure'
                else:
                    # A returned-late API hits the real publisher's sticky primary.
                    delay = patch.object(native,'delivery_snapshot',side_effect=delayed)
                    primary = 'original publication deadline'
                with patch.object(native,'DAEMON_SOURCE_PREFIX',str(Path(temporary))+'/'),delay:
                    qualify_source.final_sources(runner,approval,native,[])
                self.assertEqual(runner.first,primary)
                if boundary=='completed_late':
                    self.assertIn('source qualification final binding failure',runner.later_faults)
                self.assertFalse((runner.output/'source-after.json').exists())
                self.assertEqual(runner.finish(),1)
                self.assertEqual(runner.first,primary)

    def test_qualifier_final_verification_uses_real_delivery(self):
        native = self.native()
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)/'source'
            source.mkdir()
            path = source/'owned'
            path.write_bytes(b'actual delivered bytes')
            approval = {'no_host_source_writers':True,'delivery_files':[{
                'path':str(path),'bytes':path.stat().st_size,
                'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}],
                'delivery_directories':[{'path':str(source),'members':['owned'],'directories':[]}]}
            runner = native.Runner(str(Path(temporary)/'output'),sources=(str(path),str(source)))
            # Route the existing physical reader to this owned temporary fixture.
            # The complete delivery implementation and all its timers are real.
            with patch.object(native,'DAEMON_SOURCE_PREFIX',str(Path(temporary))+'/'):
                before = native.delivery_snapshot(runner,approval,{str(path),str(source)})
                qualify_source.final_sources(runner,approval,native,before)
            self.assertIsNone(runner.first)
            self.assertEqual(json.loads((runner.output/'source-after.json').read_bytes()),before)
            self.assertEqual(runner.finish(),0)

    def test_qualifier_original_host_window_forbids_delayed_entry(self):
        raw = Path(sys.argv[3]).read_bytes()
        boot = types.ModuleType('source_window_boot')
        with tempfile.TemporaryDirectory() as temporary:
            boot_path = Path(temporary)/'qa.local/c-installed-f03-successor-20261006/prepared/operator21/boot_operator.py'
            boot_path.parent.mkdir(parents=True)
            boot_path.write_bytes(raw)
            boot.__file__ = str(boot_path)
            exec(compile(raw,boot.__file__,'exec'),boot.__dict__)
            self.assertEqual(boot.ROOT,Path(temporary))
        root_sha = 'a'*64
        boot.M0,boot.U0 = time.monotonic()-89.8,time.time()-89.8
        boot.ACTIVE,boot.TOTAL = boot.M0+90,boot.M0+120
        boot.UTC_ACTIVE,boot.UTC_TOTAL = boot.U0+90,boot.U0+120
        # Real producer, real delay and real consumer clocks; no mocked clock.
        emitted = boot.original_window(root_sha)
        body = json.dumps(emitted,sort_keys=True).encode()
        pin = hashlib.sha256(body).hexdigest()
        mapped = qualify_source.mapped_window(body,pin,root_sha)
        self.assertLessEqual(mapped['active_utc'],emitted['active_utc'])
        self.assertLessEqual(mapped['total_utc'],emitted['total_utc'])
        self.assertGreater(mapped['time_namespace_inode'],0)
        time.sleep(.3)
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)/'window.json'
            path.write_bytes(body)
            args = ['qualify_source.py','--approval','unread-approval','--approval-sha256',
                    root_sha,'--window',str(path),'--window-sha256',pin,
                    '--output',str(Path(temporary)/'must-not-exist')]
            with patch.object(sys,'argv',args),patch.object(qualify_source,'load_native',
                    side_effect=AssertionError('expired entry cannot acquire native dispatcher')) as acquire:
                with self.assertRaisesRegex(ValueError,'expired'):
                    qualify_source.main()
                acquire.assert_not_called()
            self.assertFalse((Path(temporary)/'must-not-exist').exists())
        changed = {**emitted,'clamped_total_utc':emitted['clamped_total_utc']+1}
        wrong = json.dumps(changed).encode()
        with self.assertRaisesRegex(ValueError,'UTC clamp'):
            qualify_source.mapped_window(wrong,hashlib.sha256(wrong).hexdigest(),root_sha)
        self.host_main_controls(raw)
        self.child_profile_controls()

    def child_profile_controls(self):
        source = Path(sys.argv[3]).parent
        approval = self.current_source_approval(
            json.loads((source/'OPERATOR-INPUT.json').read_bytes()),source)
        native = self.native()
        qualify_source.source_contract(approval,native)
        for key in ('NanoCpus','Memory','MemorySwap','PidsLimit'):
            changed = copy.deepcopy(approval)
            changed['helper_profile']['HostConfig'][key] = float(
                changed['helper_profile']['HostConfig'][key])
            with self.subTest(child_resource=key),self.assertRaisesRegex(
                    native.Failure,'literal L2 constructor before birth'):
                qualify_source.source_contract(changed,native)
        identity = 'b'*64
        for started in (False,True):
            for restart in (0,False,0.0):
                with self.subTest(child_started=started,child_restart=repr(restart)):
                    observed = copy.deepcopy(approval['helper_profile'])
                    observed['Config']['Hostname'] = identity[:12]
                    if started:
                        observed['HostConfig']['OomKillDisable'] = None
                    observed.update(Id=identity,Name='/'+approval['helper_name'],
                        Image=qualify_source.IMAGE,Path='/bin/sh',
                        Args=['-c',qualify_source.COMMAND],RestartCount=restart,
                        State={'Status':'exited' if started else 'created',
                            'StartedAt':'2030-10-02T12:00:00Z' if started else '0001-01-01T00:00:00Z',
                            'Running':False,'Paused':False,'Restarting':False,
                            'OOMKilled':False,'Dead':False,'Pid':0,'ExitCode':0},
                        NetworkSettings={'Networks':copy.deepcopy(approval[
                            'terminal_networks' if started else 'created_networks'])})
                    runner = types.SimpleNamespace(inspect=lambda *_args,**_kwargs:observed)
                    dependent = []
                    if type(restart) is int:
                        qualify_source.helper_profile(runner,approval,native,identity,
                            None if started else False,cleanup=started)
                        dependent.append('rm' if started else 'start')
                        self.assertEqual(dependent,['rm' if started else 'start'])
                    else:
                        with self.assertRaisesRegex(native.Failure,'complete owned L2 helper constructor'):
                            qualify_source.helper_profile(runner,approval,native,identity,
                                None if started else False,cleanup=started)
                            dependent.append('rm' if started else 'start')
                        self.assertEqual(dependent,[])

    @staticmethod
    def current_source_approval(original, source):
        approval = copy.deepcopy(original)
        capsule = approval['capsule_path']
        binary = next(item for item in approval['delivery_files']
                      if item['path']==approval['binary_path'])
        approval['delivery_files'] = [binary]+[{
            'path':capsule+'/'+name,'bytes':(source/name).stat().st_size,
            'sha256':hashlib.sha256((source/name).read_bytes()).hexdigest()}
            for name in sorted(qualify_source.MEMBERS)]
        # Retain the supplied topology; only fixture byte pins need reconstruction.
        return approval

    def supplied_topology_controls(self, approval):
        native = self.native()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            files, directories = [], []
            for index, supplied in enumerate(approval['delivery_directories']):
                delivered = root/str(index)
                delivered.mkdir()
                for member in supplied['members']:
                    path = delivered/member
                    path.parent.mkdir(parents=True,exist_ok=True)
                    raw = member.encode()
                    path.write_bytes(raw)
                    files.append({'path':str(path),'bytes':len(raw),
                                  'sha256':hashlib.sha256(raw).hexdigest()})
                for member in supplied['directories']:
                    (delivered/member).mkdir(parents=True,exist_ok=True)
                directories.append({**supplied,'path':str(delivered)})
            fixture = {'no_host_source_writers':True,'delivery_files':files,
                       'delivery_directories':directories}
            runner = types.SimpleNamespace(work_end=time.monotonic()+15,
                work_utc_end=time.time()+15)
            with patch.object(native,'DAEMON_SOURCE_PREFIX',str(root)+'/'):
                self.assertEqual(len(native.delivery_snapshot(runner,fixture,
                    {item['path'] for item in directories})),len(files))
                changed = copy.deepcopy(fixture)
                changed['delivery_directories'][0]['members'].reverse()
                with self.assertRaisesRegex(native.Failure,'exact delivery directory topology'):
                    native.delivery_snapshot(runner,changed,set())

    def host_main_controls(self, raw, installing=False):
        source = Path(sys.argv[3]).parent
        original_profile = json.loads((source/('INSTALL-OPERATOR-PROFILE.json' if installing
                                              else 'OPERATOR-PROFILE.json')).read_bytes())
        original_input = json.loads((source/('INSTALL-INPUT.json' if installing
                                            else 'OPERATOR-INPUT.json')).read_bytes())
        self.supplied_topology_controls(original_input)
        source_input = (original_input if installing else
                        self.current_source_approval(original_input,source))
        substitutions = {
            'config_bool':(('Config','AttachStdin'),0),
            'host_bool':(('HostConfig','ReadonlyRootfs'),1),
            'host_float':(('HostConfig','Memory'),float(original_profile['profile']['HostConfig']['Memory'])),
            'mount_bool':(('Mounts',0,'RW'),1),
            'restart_bool':(('RestartCount',),False),
            'network_bool':(('NetworkSettings','Networks','none','IPPrefixLen'),False),
        }
        cases = ('pass','wrong_name','wrong_argv','before_birth','create',
                 'attach','cleanup','publication','unresolved','no_outcome',
                 'prestarted','missing_domain','unresolved_domain',*substitutions)
        for case in cases:
            with self.subTest(host_main=case),tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                boot = types.ModuleType('actual_host_main_control')
                boot_path = root/('qa.local/c-installed-f03-successor-20261006/prepared/'
                                  + ('install-operator7/boot_installation.py' if installing
                                     else 'operator8/boot_operator.py'))
                boot_path.parent.mkdir(parents=True)
                boot_path.write_bytes(raw)
                boot.__file__ = str(boot_path)
                exec(compile(raw,boot.__file__,'exec'),boot.__dict__)
                self.assertEqual(boot.ROOT,root)
                if case=='pass':
                    dependency = root/'captured.py'
                    dependency.write_bytes(b'BODY_EXECUTED = True\n')
                    dependency_sha = hashlib.sha256(dependency.read_bytes()).hexdigest()
                    original_compile = compile
                    for clock in ('monotonic','time'):
                        ticks = {'expired':False}
                        original_clock = getattr(time,clock)
                        def expire_compile(source,filename,mode,**kwargs):
                            code = original_compile(source,filename,mode,**kwargs)
                            ticks['expired'] = True
                            return code
                        def current_clock():
                            return ((boot.ACTIVE if clock=='monotonic' else boot.UTC_ACTIVE)
                                    if ticks['expired'] else original_clock())
                        with self.subTest(host_compile_clock=clock), \
                                patch('builtins.compile',side_effect=expire_compile), \
                                patch.object(time,clock,side_effect=current_clock), \
                                patch('builtins.exec') as execute:
                            with self.assertRaises(TimeoutError):
                                boot.captured('expired_dependency',dependency,dependency_sha)
                            execute.assert_not_called()
                output = root/('qa.local/c-installed-f03-finish-20261005/clean-linux1/private-offline-render4/c-successor-20261006-1/'
                               + ('install-output7' if installing else 'operator-output1'))
                output.mkdir(parents=True)
                profile = copy.deepcopy(original_profile)
                profile['host_output'] = str(output)
                if case=='wrong_name':
                    profile['name'] = 'synthetic-qa-wrong-operator'
                if case=='wrong_argv':
                    profile['create_argv'].insert(1,'--privileged')
                approval = copy.deepcopy(source_input)
                # This temporary test authority cannot authorize a real dispatch.
                approval.update(authority='ROOT',no_host_source_writers=True,
                                passive_native_custody_allowed=True)
                if not installing:
                    qualify_source.source_contract(approval,self.native())
                approval_raw = json.dumps(approval).encode()
                profile_raw = json.dumps(profile).encode()
                approval_sha = hashlib.sha256(approval_raw).hexdigest()
                profile_sha = hashlib.sha256(profile_raw).hexdigest()
                approval_path = boot.ROOT/boot.APPROVAL_RELATIVE if installing else boot.BASE/'APPROVAL-ROOT.json'
                approval_path.parent.mkdir(parents=True,exist_ok=True)
                approval_path.write_bytes(approval_raw)
                if installing:
                    self.assertEqual(approval_path.parent,output.parent)
                    self.assertEqual(approval_path.name,'install-approval7.json')
                    self.assertFalse((boot.BASE/'APPROVAL-ROOT.json').exists())
                profile_name = 'INSTALL-OPERATOR-PROFILE.json' if installing else 'OPERATOR-PROFILE.json'
                (boot.BASE/profile_name).write_bytes(profile_raw)
                transport_dir = root/'qa.local/c-fileproof-entrypoint-clock-completion-20261004/candidate1/accepted-transport'
                transport_dir.mkdir(parents=True)
                for filename,fixture in (('guards.py','capture_guards.py'),
                                         ('run-qualified.py','capture_transport.py')):
                    (transport_dir/filename).write_bytes((source/fixture).read_bytes())
                identity = 'a'*64
                calls = []
                fake = None
                original_capture = boot.captured

                def captured(name,path,pin):
                    nonlocal fake
                    # Keep immutable dependency diagnostics off unittest's
                    # status stream, but retain them in the helper's raw stdout.
                    diagnostics = io.StringIO()
                    try:
                        with redirect_stderr(diagnostics):
                            module = original_capture(name,path,pin)
                    finally:
                        if diagnostics.getvalue():
                            print('HOST_MAIN_COMPILE_DIAGNOSTICS case='+case+' module='+name)
                            print(diagnostics.getvalue(),end='')
                    if name!='c_trusted_boot_capture':
                        return module
                    exception = module.RecipeFailure
                    native_cli = module.DockerCLI

                    class FakeCLI:
                        def __init__(self,_executable):
                            self.unresolved = []
                            self.last_outcome = None
                            self.phase = 'created'
                            self.failed = False

                        def call(self,argv,end,utc_end):
                            self.assert_cutoffs(end,utc_end)
                            command = argv[2:]
                            calls.append(command)
                            operation = command[0]
                            fail = not self.failed and (
                                (case=='before_birth' and operation=='ps')
                                or (case=='no_outcome' and operation=='image')
                                or (case in ('create','publication','unresolved') and operation=='create')
                                or (case=='attach' and operation=='start')
                                or (case=='cleanup' and operation=='rm'))
                            stdout = b''
                            if operation=='image':
                                stdout = json.dumps([{'Id':boot.IMAGE}]).encode()
                            elif operation=='create':
                                stdout = (identity+'\n').encode()
                                if case=='prestarted':
                                    self.phase = 'exited'
                            elif operation=='start':
                                self.phase = 'exited'
                                if case!='missing_domain':
                                    domain = output/'outcome1'
                                    domain.mkdir()
                                    receipt = {'pass':None,'eligible_before_publication':True,
                                        'publication_status':'pre-publication snapshot; actual exit required',
                                        'unresolved_resources':case=='unresolved_domain',
                                        'utc_start':boot.U0,'work_utc_end':boot.UTC_ACTIVE,
                                        'cleanup_utc_end':boot.UTC_TOTAL,'commands':[{
                                            'released':True,'selector_closed':True,'acquisition':'complete',
                                            'pid':os.getpid(),'exit':0,'reaped':True,
                                            'eof':[True,True],'readers_closed':[True,True]}]}
                                    receipt.update({key:[] for key in ('unresolved',
                                        'unresolved_native_readers','unresolved_native_writers',
                                        'unresolved_acquisition','unresolved_writers')})
                                    (domain/'terminal.json').write_text(json.dumps(receipt))
                                    if installing:
                                        retained = {'state':'OBSERVATIONS_NOT_RUNTIME_ADMISSION',
                                            'identities':{role:('%064x'%number) for number,role
                                                in enumerate(approval['services'],1)},
                                            'product_source':bootstrap.PRODUCT,'binary_sha256':bootstrap.BINARY,
                                            'prerequisites':bootstrap.PREREQUISITES}
                                        (domain/'prerequisites.json').write_text(json.dumps(retained))
                            elif operation=='inspect':
                                observed = copy.deepcopy(profile['profile'])
                                window_sha = hashlib.sha256((output/'WINDOW.json').read_bytes()).hexdigest()
                                observed['Config']['Cmd'] = [approval_sha if item=='ROOT_FINAL_RAW_SHA'
                                    else window_sha if item=='WINDOW_FINAL_RAW_SHA' else item
                                    for item in observed['Config']['Cmd']]
                                observed['Config']['Hostname'] = identity[:12]
                                if self.phase=='exited':
                                    observed['HostConfig']['OomKillDisable'] = None
                                observed.update(Id=identity,Name='/'+boot.NAME,Image=boot.IMAGE,
                                    Path='python3',Args=observed['Config']['Cmd'],RestartCount=0,
                                    State={'Status':self.phase,'Pid':0,'ExitCode':0,
                                        'Running':False,'Paused':False,'Restarting':False,
                                        'OOMKilled':False,'Dead':False,
                                        'StartedAt':'0001-01-01T00:00:00Z',
                                        'FinishedAt':'0001-01-01T00:00:00Z','Error':''},
                                    NetworkSettings={'Networks':copy.deepcopy(profile['created_networks' if self.phase=='created'
                                        else 'terminal_networks'])})
                                if case in substitutions:
                                    keys,value = substitutions[case]
                                    target = observed
                                    for key in keys[:-1]:
                                        target = target[key]
                                    target[keys[-1]] = value
                                stdout = json.dumps(observed).encode()
                            frame = {'pid':os.getpid(),'creation_identity':{
                                'kind':'linux-start-ticks','pid':os.getpid(),'created':1},
                                'reaped':True,'exit':0,'eof':[True,True],'released':True,
                                'original_end':end,'original_end_utc':utc_end,
                                'capture_token':'0'*32,'readers':[], 'recovery_readers':[]}
                            outcome = {'code':0,'stdout':stdout,'stderr':b'',
                                'output_complete':True,'cleanup_errors':[],
                                'custody':native_cli.capture_snapshot(frame),'failure':None}
                            if fail:
                                self.failed = True
                                if case=='no_outcome':
                                    raise exception('Exact native process creation identity unavailable')
                                outcome.update(code=17,stderr=b'causal native stderr',
                                    failure='Docker command timed out')
                                frame['exit'] = 17
                                if case=='unresolved':
                                    frame['released'] = False
                                    outcome['cleanup_errors'] = ['Docker pipe close: OSError']
                                    self.unresolved.append(frame)
                                outcome['custody'] = native_cli.capture_snapshot(frame)
                                self.last_outcome = outcome
                                raise exception(outcome['failure'],outcome=outcome)
                            self.last_outcome = outcome
                            return outcome['code'],outcome['stdout'],outcome['stderr']

                        @staticmethod
                        def assert_cutoffs(end,utc_end):
                            if end>boot.TOTAL or utc_end>boot.UTC_TOTAL:
                                raise AssertionError('original dual cutoffs cannot extend')

                    fake = FakeCLI(None)
                    module.DockerCLI = lambda _executable:fake
                    return module

                class PassiveCustody(Exception):
                    pass

                original_open = Path.open
                def publication(path,*args,**kwargs):
                    if case=='publication' and path.name=='03.stdout' and args==('xb',):
                        raise OSError('causal publication failure')
                    return original_open(path,*args,**kwargs)

                args = ['boot_operator.py','--approval-sha256',approval_sha,
                        '--profile-sha256',profile_sha]
                with patch.dict(sys.modules),patch.object(sys,'argv',args), \
                        patch.object(boot,'captured',side_effect=captured), \
                        patch.object(Path,'open',new=publication), \
                        patch.object(boot.time,'sleep',side_effect=PassiveCustody):
                    if case in ('wrong_name','wrong_argv'):
                        with self.assertRaises(ValueError):
                            boot.main()
                        self.assertIsNone(fake)
                        self.assertFalse((output/'host-receipts1').exists())
                        continue
                    if case in ('cleanup','unresolved','prestarted','missing_domain','unresolved_domain') or case in substitutions:
                        with self.assertRaises(PassiveCustody):
                            boot.main()
                    else:
                        self.assertEqual(boot.main(),0 if case=='pass' else 1)
                receipts = output/'host-receipts1'
                terminal = json.loads((receipts/'terminal.json').read_bytes())
                self.assertIsNone(terminal['pass'])
                window = (output/'WINDOW.json').read_bytes()
                mapped = qualify_source.mapped_window(window,hashlib.sha256(window).hexdigest(),approval_sha)
                self.assertLessEqual(mapped['active_utc'],boot.UTC_ACTIVE)
                self.assertLessEqual(mapped['total_utc'],boot.UTC_TOTAL)
                if len(calls)>2:
                    self.assertEqual(calls[2], [approval_sha if item=='ROOT_FINAL_RAW_SHA'
                        else hashlib.sha256(window).hexdigest() if item=='WINDOW_FINAL_RAW_SHA'
                        else item for item in profile['create_argv']])
                if case=='pass':
                    self.assertIsNone(terminal['first_failure'])
                    self.assertTrue(terminal['eligible_before_publication'])
                    self.assertTrue(terminal['removed_and_absent'])
                    continue
                if case in ('prestarted','missing_domain','unresolved_domain'):
                    self.assertTrue(terminal['unresolved'])
                    self.assertFalse(terminal['removed_and_absent'])
                    self.assertNotIn('rm',[item[0] for item in calls])
                    if case=='prestarted':
                        self.assertNotIn('start',[item[0] for item in calls])
                    self.assertTrue(any('trusted operator release failure:' in item
                                        for item in [terminal['first_failure'],*terminal['later_failures']]
                                        if item is not None))
                    continue
                if case in substitutions:
                    self.assertEqual(terminal['first_failure'],
                        'trusted operator bootstrap failure: '+(
                            'exact trusted operator network' if case=='network_bool'
                            else 'complete trusted operator constructor'))
                    self.assertFalse(terminal['eligible_before_publication'])
                    self.assertTrue(terminal['unresolved'])
                    self.assertFalse(terminal['removed_and_absent'])
                    self.assertEqual(terminal['helper_id'],identity)
                    self.assertEqual([item[0] for item in calls],
                                     ['ps','image','create','inspect','inspect'])
                    self.assertTrue(any('trusted operator release failure:' in item
                                        for item in terminal['later_failures']))
                    continue
                self.assertEqual(terminal['first_failure'],
                    'Exact native process creation identity unavailable' if case=='no_outcome'
                    else 'Docker command timed out')
                self.assertFalse(terminal['eligible_before_publication'])
                failure_index = next(index for index,item in enumerate(terminal['native'],1)
                                     if item['failure'] is not None)
                recorded = terminal['native'][failure_index-1]
                self.assertEqual(recorded['failure'],terminal['first_failure'])
                self.assertEqual((receipts/('%02d.stderr'%failure_index)).read_bytes(),
                                 b'' if case=='no_outcome' else b'causal native stderr')
                if case=='no_outcome':
                    self.assertIsNone(recorded['custody'])
                    self.assertIsNone(recorded['code'])
                else:
                    self.assertEqual(recorded['cleanup_errors'],
                        ['Docker pipe close: OSError'] if case=='unresolved' else [])
                    self.assertEqual(recorded['custody']['exit'],17)
                    self.assertEqual(recorded['custody']['eof'],[True,True])
                if case in ('create','publication','unresolved'):
                    self.assertEqual(terminal['helper_id'],identity)
                if case=='publication':
                    self.assertFalse((receipts/'03.stdout').exists())
                    self.assertTrue((receipts/'03.stderr').is_file())
                    self.assertTrue((receipts/'03.json').is_file())
                    self.assertTrue(any('03.stdout: OSError' in item for item in terminal['later_failures']))
                if case in ('cleanup','unresolved'):
                    self.assertTrue(terminal['unresolved'])
                    self.assertFalse(terminal['removed_and_absent'])
                if case=='unresolved':
                    self.assertEqual([item[0] for item in calls],['ps','image','create'])

    def test_qualifier_terminal_sync_expiry_cannot_accept(self):
        native = self.native()
        with tempfile.TemporaryDirectory() as temporary:
            runner = native.Runner(str(Path(temporary)/'output'),active=1,cleanup=1)
            runner.end = time.monotonic()+.05
            runner.utc_end = time.time()+1

            def delayed(_fd):
                time.sleep(.2)

            with patch.object(native.os,'fsync',side_effect=delayed):
                self.assertEqual(runner.finish(),1)
            self.assertIsNotNone(runner.first)
            terminal = runner.output/'terminal.json'
            if terminal.exists():
                self.assertIsNone(json.loads(terminal.read_bytes())['pass'])

    def test_actual_bootstrap_artifact_mutations_stop_before_lifecycle(self):
        native = self.native()
        original = json.loads(Path(sys.argv[1]).read_bytes())
        mutations = [
            ('project','foreign'),('product_source','a'*40),
            ('product_binary_sha256','a'*64),('sole_writer','foreign'),
            ('runtime_services',bootstrap.RUNTIME[:-1]),('prerequisite_services',['fake']),
            ('steps',original['steps'][:-1]),
            ('prerequisite_start_order',{'before_steps':['fake']}),
        ]
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            def docker(self,*args,**kwargs):
                raise AssertionError('Unauthenticated artifact cannot dispatch')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'bootstrap.json'
            for field,value in mutations:
                with self.subTest(field=field):
                    wrong = copy.deepcopy(original)
                    wrong[field] = value
                    raw = json.dumps(wrong).encode()
                    path.write_bytes(raw)
                    approval = {'project':original['project'],'bootstrap':{
                        'path':str(path),'sha256':native.digest(raw)}}
                    with self.assertRaises(native.Failure):
                        installation.prerequisites(Runner(),approval,native)
            # Artifact notes do not grant ROOT authority; main validates the
            # actual anchor. These six fields are the executable window contract.
            for key in ('prerequisites_before_arm','startup_seconds',
                        'guard_deadline_offset_seconds','readiness_seconds',
                        'child_deadline_offset_seconds','outer_deadline_offset_seconds'):
                with self.subTest(window=key):
                    wrong = copy.deepcopy(original)
                    wrong['installation_windows'][key] = [] if isinstance(
                        wrong['installation_windows'][key],list) else 1
                    raw = json.dumps(wrong).encode()
                    path.write_bytes(raw)
                    with self.assertRaises(native.Failure):
                        bootstrap.bootstrap_contract(Runner(),{'project':original['project'],
                            'bootstrap':{'path':str(path),'sha256':native.digest(raw)}},native)
            path.write_bytes(json.dumps(original).encode())
            with self.assertRaises(native.Failure):
                bootstrap.bootstrap_contract(Runner(),{'project':original['project'],
                    'bootstrap':{'path':str(path),'sha256':'a'*64}},native)

    def test_installation_main_authenticates_raw_authority_and_anchor_before_output(self):
        native = self.native()
        anchor = {'authority':'ROOT','prerequisites_passed':bootstrap.PREREQUISITES,
                  'product_source':bootstrap.PRODUCT,'product_binary_sha256':bootstrap.BINARY,
                  'monotonic':time.monotonic(),'utc':time.time(),
                  'boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
                  'time_namespace_inode':os.stat('/proc/self/ns/time').st_ino,
                  'windows':{'startup':30,'guard':600,'readiness':90,'child':22332,'outer':22344}}
        approval = {'authority':'ROOT','passive_native_custody_allowed':True,
                    'no_host_source_writers':True,'operation':'C_INSTALL_RUNTIME',
                    'project':'synthetic-qa-c-current','owner':'c_installed_f03_finish',
                    'installation_anchor':anchor,'delivery_files':[],'delivery_directories':[],
                    'prerequisite_observations':{'path':'unused'}}
        mutations = [(key,value) for key,value in (
            ('authority','PENDING_ROOT'),('passive_native_custody_allowed',1),
            ('no_host_source_writers',False),('operation','C_RENDER'),
            ('project','foreign'),('owner','foreign'))]
        anchor_mutations = [('authority','PENDING_ROOT'),('prerequisites_passed',[]),
            ('product_source','a'*40),('product_binary_sha256','a'*64),('boot_id','foreign'),
            ('time_namespace_inode',-1),('windows',{'startup':30}),
            ('monotonic',True),('monotonic',float('nan')),('utc',float('inf')),
            ('monotonic',time.monotonic()+1000),('utc',time.time()+1000)]
        anchor_mutations.extend([('monotonic',time.monotonic()-91),('utc',time.time()-91)])
        read_bytes = native.read_bytes_pinned
        native_path = sys.argv[2]
        def mounted_native(path,pin,*args,**kwargs):
            return read_bytes(native_path if path=='/runner/run.py' else path,pin,*args,**kwargs)
        with tempfile.TemporaryDirectory() as directory, patch.dict(sys.modules,{'run':native}), \
                patch.object(native,'read_bytes_pinned',side_effect=mounted_native), \
                patch.object(native,'Runner',side_effect=AssertionError('No output may be created')) as created:
            path = Path(directory)/'approval.json'
            output = Path(directory)/'output'
            variants = []
            for key,value in mutations:
                wrong = copy.deepcopy(approval)
                wrong[key] = value
                variants.append((key,wrong))
            for key,value in anchor_mutations:
                wrong = copy.deepcopy(approval)
                wrong['installation_anchor'][key] = value
                variants.append(('anchor.'+key,wrong))
            for label,wrong in variants:
                with self.subTest(field=label):
                    raw = json.dumps(wrong).encode()
                    path.write_bytes(raw)
                    argv = ['installation.py','--approval',str(path),'--approval-sha256',
                            native.digest(raw),'--output',str(output),'--operation','C_INSTALL_RUNTIME']
                    with patch.object(sys,'argv',argv), self.assertRaises(native.Failure):
                        installation.main()
                    created.assert_not_called()
                    self.assertFalse(output.exists())
            path.write_bytes(json.dumps(approval).encode())
            with patch.object(sys,'argv',['installation.py','--approval',str(path),
                    '--approval-sha256','a'*64,'--output',str(output),
                    '--operation','C_INSTALL_RUNTIME']), self.assertRaises(native.Failure):
                installation.main()
            created.assert_not_called()

        self.installation_window_controls()

    def installation_window_controls(self):
        source = Path(sys.argv[3]).parent
        raw = (source/'boot_installation.py').read_bytes()
        boot = types.ModuleType('installation_window_boot')
        approval = json.loads((source/'INSTALL-INPUT.json').read_bytes())
        self.assertEqual(len(approval['delivery_files']),33)
        self.assertEqual([len(item['members']) for item in approval['delivery_directories']],[17,14])
        self.assertEqual(approval['authority'],'PENDING_ROOT')
        approval.update(authority='ROOT',no_host_source_writers=True,
                        passive_native_custody_allowed=True)
        body = json.dumps(approval).encode()
        root_sha = hashlib.sha256(body).hexdigest()
        native = self.native()
        read_bytes = native.read_bytes_pinned
        native_path = sys.argv[2]

        def mounted_native(path,pin,*args,**kwargs):
            return read_bytes(native_path if path=='/runner/run.py' else path,pin,*args,**kwargs)

        with tempfile.TemporaryDirectory() as temporary,patch.dict(sys.modules,{'run':native}), \
                patch.object(native,'read_bytes_pinned',side_effect=mounted_native):
            root = Path(temporary)
            boot_path = root/'qa.local/c-installed-f03-successor-20261006/prepared/install-operator7/boot_installation.py'
            boot_path.parent.mkdir(parents=True)
            boot_path.write_bytes(raw)
            boot.__file__ = str(boot_path)
            exec(compile(raw,boot.__file__,'exec'),boot.__dict__)
            self.assertEqual(boot.ROOT,root)
            grant,window,output = root/'approval.json',root/'window.json',root/'output'
            grant.write_bytes(body)
            calls,parameters,saved = [],[],[]

            def acquire(_output,**kwargs):
                parameters.append(kwargs)
                return types.SimpleNamespace(end=kwargs['start']+120,
                    utc_end=kwargs['utc_start']+120,
                    work_end=kwargs['start']+90,work_utc_end=kwargs['utc_start']+90,
                    save=lambda name,body:saved.append((name,body)),finish=lambda:0,
                    fault=lambda reason:self.fail('unexpected prerequisite fixture fault: '+reason))

            def invoke(emitted):
                window_body = json.dumps(emitted,sort_keys=True).encode()
                window.write_bytes(window_body)
                return ['installation.py','--approval',str(grant),'--approval-sha256',root_sha,
                        '--output',str(output),'--window',str(window),
                        '--window-sha256',hashlib.sha256(window_body).hexdigest()]

            emitted = boot.original_window(root_sha)
            with patch.object(sys,'argv',invoke(emitted)), \
                    patch.object(native,'Runner',side_effect=acquire), \
                    patch.object(installation,'prerequisites',side_effect=lambda runner,_approval,_native:
                        calls.append(('prerequisites',runner.end,runner.utc_end,
                                      runner.work_end,runner.work_utc_end))):
                self.assertEqual(installation.main(),0)
            self.assertEqual(parameters[0]['active'],90)
            self.assertEqual(parameters[0]['cleanup'],30)
            self.assertEqual(calls[0][0],'prerequisites')
            mapped = json.loads(saved[0][1])
            self.assertLessEqual(calls[0][1],mapped['total_end'])
            self.assertLessEqual(calls[0][3],mapped['active_end'])
            self.assertLessEqual(parameters[0]['utc_start']+90,emitted['clamped_active_utc'])
            self.assertLessEqual(calls[0][2],emitted['clamped_total_utc'])
            self.assertLessEqual(calls[0][4],emitted['clamped_active_utc'])
            self.assertIn(str(window),parameters[0]['sources'])
            self.assertEqual([name for name,_body in saved],['original-window.json'])
            self.assertFalse(output.exists())
            with patch.object(native,'Runner',side_effect=AssertionError('invalid binding precedes output')) as created:
                missing = ['installation.py','--approval',str(grant),'--approval-sha256',root_sha,
                           '--output',str(output)]
                original_import = __import__

                def before_native(name,*args,**kwargs):
                    if name=='run':
                        self.fail('missing window precedes native import')
                    return original_import(name,*args,**kwargs)

                with patch.object(sys,'argv',missing), \
                        patch('builtins.__import__',side_effect=before_native), \
                        patch.object(native,'read_pinned',side_effect=AssertionError('missing window precedes grant intake')) as intake, \
                        self.assertRaisesRegex(ValueError,'original prerequisite window'):
                    installation.main()
                intake.assert_not_called()
                with patch.object(sys,'argv',missing+['--window',str(window)]),self.assertRaisesRegex(
                        ValueError,'complete original prerequisite window'):
                    installation.main()
                wrong = {**emitted,'root_sha256':'a'*64}
                with patch.object(sys,'argv',invoke(wrong)),self.assertRaisesRegex(
                        ValueError,'typed authenticated original host window'):
                    installation.main()
                created.assert_not_called()
            self.assertFalse(output.exists())

            boot.M0,boot.U0 = time.monotonic()-89.8,time.time()-89.8
            boot.ACTIVE,boot.TOTAL = boot.M0+90,boot.M0+120
            boot.UTC_ACTIVE,boot.UTC_TOTAL = boot.U0+90,boot.U0+120
            delayed = boot.original_window(root_sha)
            argv = invoke(delayed)
            time.sleep(.3)
            with patch.object(sys,'argv',argv), \
                    patch.object(native,'read_pinned',side_effect=AssertionError('expired window precedes grant intake')) as intake, \
                    patch.object(native,'Runner',side_effect=AssertionError('expired window precedes output')) as created:
                with self.assertRaisesRegex(ValueError,'expired'):
                    installation.main()
                intake.assert_not_called()
                created.assert_not_called()
            self.assertFalse(output.exists())
        self.host_main_controls(raw,installing=True)
        self.installation_compose_controls(boot,source)

    def installation_compose_controls(self,boot,source):
        profile = json.loads((source/'INSTALL-OPERATOR-PROFILE.json').read_bytes())
        literal = boot.environment(profile['profile']['Config']['Env'])
        inputs = boot.compose_environment()
        self.assertEqual({key:value for key,value in literal.items()
                          if key.startswith('C_ACCEPTANCE_')},inputs)
        mounts = {item['Source']:item['Destination'] for item in profile['profile']['Mounts']}
        for key in ('C_ACCEPTANCE_SOURCE_DIR','C_ACCEPTANCE_PRIVATE_DIR','C_ACCEPTANCE_ZNS_BINARY'):
            self.assertEqual(mounts[inputs[key]],inputs[key])
        body = (source/'compose.acceptance.yaml').read_bytes()
        native = self.native()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            private = root/'private'
            private.mkdir()
            fixture = json.loads((source/'INSTALL-INPUT.json').read_bytes())
            for item in fixture['delivery_directories'][1]['members']:
                (private/item).write_bytes(b'FIXTURE=synthetic\n' if item.endswith('.env') else b'synthetic\n')
            compose = root/'compose.acceptance.yaml'
            compose.write_bytes(body)
            # Rebind only the three mount paths to owned no-socket fixture paths.
            # Image inputs and the actual Compose body/CLI remain literal.
            env = dict(literal)
            env.update(C_ACCEPTANCE_SOURCE_DIR=str(source),
                       C_ACCEPTANCE_PRIVATE_DIR=str(private),
                       C_ACCEPTANCE_ZNS_BINARY='/usr/local/bin/zns')
            runner = native.Runner(root/'render-output',active=15,cleanup=5)
            actual = runner.docker
            calls = []
            def docker(*args,**kwargs):
                calls.append(args)
                if args==('info','--format','{{.ID}}'):
                    return b'synthetic-no-socket-daemon\n'
                self.assertEqual(args[:1],('compose',))
                return actual(*args,**kwargs)
            approval = {'project':'synthetic-qa-c-current','daemon_id':'synthetic-no-socket-daemon',
                        'compose_file':str(compose),'compose_sha256':hashlib.sha256(body).hexdigest(),
                        'operation':'C_RENDER','services':{}}
            with patch.dict(os.environ,env,clear=True),patch.object(runner,'docker',side_effect=docker):
                self.assertIsNone(native.compose_config(runner,approval))
            rendered = json.loads((runner.output/'rendered-compose.json').read_bytes())
            self.assertEqual(len(rendered['services']),12)
            expected = {'app':'APP_BASE','fake':'APP_BASE','evaluator':'SCRIPT_IMAGE',
                        'postgres':'POSTGRES_IMAGE','media-decoder':'MEDIA_DECODER_IMAGE',
                        'media-broker':'MEDIA_BROKER_IMAGE','sticker-decoder':'STICKER_DECODER_IMAGE',
                        'sticker-broker':'STICKER_BROKER_IMAGE','tool':'APP_BASE',
                        'roles':'POSTGRES_IMAGE','clock-init':'CLOCK_IMAGE','clock-read':'CLOCK_IMAGE'}
            for service,key in expected.items():
                self.assertEqual(rendered['services'][service]['image'],inputs['C_ACCEPTANCE_'+key])
            self.assertEqual(len(calls),2)
            self.assertTrue(all(item['released'] for item in runner.records))
            self.assertEqual(runner.finish(),0)

    def test_runtime_actual_observation_mutations_precede_start_and_publication(self):
        native = self.native()
        names = ['postgres','fake']+bootstrap.RUNTIME
        identities = {name:('%064x' % index) for index,name in enumerate(names,1)}
        observed = {'state':'OBSERVATIONS_NOT_RUNTIME_ADMISSION',
                    'product_source':bootstrap.PRODUCT,'binary_sha256':bootstrap.BINARY,
                    'prerequisites':bootstrap.PREREQUISITES,'identities':identities,
                    'delivery':[],'clock':{},'continuity':{}}
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            def inspect(self,*args):
                raise AssertionError('Invalid observations forbid dependent inspection')
            def docker(self,*args,**kwargs):
                raise AssertionError('Invalid observations forbid START')
            def save(self,*args):
                raise AssertionError('Invalid observations forbid publication')
        mutations = [('state','pass'),('product_source','a'*40),('binary_sha256','a'*64),
                     ('prerequisites',bootstrap.PREREQUISITES[:-1]),
                     ('identities',{key:value for key,value in identities.items() if key!='fake'}),
                     ('identities',{**identities,'app':'a'*12}),
                     ('identities',{**identities,'app':identities['postgres']})]
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(installation,'authenticate_delivery',return_value=[]):
            path = Path(directory)/'prerequisites.json'
            for key,value in mutations:
                with self.subTest(field=key):
                    raw = json.dumps({**observed,key:value}).encode()
                    path.write_bytes(raw)
                    approval = {'installation_anchor':{'monotonic':time.monotonic(),'utc':time.time()},
                                'prerequisite_observations':{'path':str(path),'sha256':native.digest(raw)}}
                    with self.assertRaises(native.Failure):
                        installation.runtime(Runner(),approval,native)
            path.write_bytes(json.dumps(observed).encode())
            approval['prerequisite_observations']['sha256'] = 'a'*64
            with self.assertRaises(native.Failure):
                installation.runtime(Runner(),approval,native)
            path.unlink()
            with self.assertRaises((native.Failure,OSError)):
                installation.runtime(Runner(),approval,native)

    def test_original_startup_and_readiness_expiry_forbid_dependent_work(self):
        native = self.native()
        names = ['postgres','fake']+bootstrap.RUNTIME
        identities = {name:('%064x' % index) for index,name in enumerate(names,1)}
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            calls = []
            def inspect(self,identity):
                return {'Id':identity,'State':{'Status':'created','Pid':0,
                                              'StartedAt':'0001-01-01T00:00:00Z'}}
            def docker(self,*args,**kwargs):
                self.calls.append(args)
                raise AssertionError('Expired F30/readiness cannot dispatch')
        native.constructor = lambda *args:None
        anchor = {'authority':'ROOT','prerequisites_passed':bootstrap.PREREQUISITES,
                  'product_source':bootstrap.PRODUCT,'product_binary_sha256':bootstrap.BINARY,
                  'monotonic':time.monotonic()-31,'utc':time.time()-31,
                  'boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
                  'time_namespace_inode':os.stat('/proc/self/ns/time').st_ino,
                  'windows':{'startup':30,'guard':600,'readiness':90,'child':22332,'outer':22344}}
        approval = {'authority':'ROOT','installation_anchor':anchor,'services':dict.fromkeys(names,{}),
                    'project':'synthetic-qa-c-current','owner':'c_installed_f03_finish'}
        with self.assertRaises(native.Failure):
            bootstrap.start_runtime(Runner(),approval,native,identities)
        self.assertEqual(Runner.calls,[])
        anchor.update(monotonic=time.monotonic()-91,utc=time.time()-91)
        with self.assertRaises(native.Failure):
            bootstrap.runtime_readiness(Runner(),approval,native,identities)
        self.assertEqual(Runner.calls,[])

    def test_actual_create_failure_retains_unknown_birth_without_start(self):
        native = self.native()
        names = ['postgres','fake']+bootstrap.RUNTIME
        services = {name:{'name':'synthetic-qa-c-current-'+name+'-1'} for name in names}
        class Runner:
            unresolved_resources = False
            create_compose_pin = 'a'*64
            calls = []
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            def docker(self,*args,**kwargs):
                self.calls.append(args)
                if args[0]=='compose':
                    raise native.Failure('CREATE returned no complete identity')
                if args[0]=='start':
                    raise AssertionError('Unknown births cannot START')
                return b''
        runner = Runner()
        runner.calls = []
        approval = {'retain_owned_stand':True,'project':'synthetic-qa-c-current'}
        with patch.object(native,'compose_config',return_value=(approval['project'],
                ['compose','--file','fixture'],services,[])), \
                patch.object(native,'read_pinned',return_value={}), \
                patch.object(bootstrap,'check_delivery',return_value=[]):
            with self.assertRaises(native.Failure):
                bootstrap.create_graph(runner,approval,native)
        self.assertTrue(runner.unresolved_resources)
        self.assertFalse(any(call[0]=='start' for call in runner.calls))

    def test_provider_requires_complete_results_clock_rows_and_full_identity(self):
        native = self.native()
        names = ['postgres','fake']+bootstrap.RUNTIME
        identities = {name:('%064x' % index) for index,name in enumerate(names,1)}
        results = [{'step':index,'service':step['service'],'raw':b'{}'}
                   for index,step in enumerate(bootstrap.STEPS,1)]
        approval = {'services':dict.fromkeys(names,{}),'project':'synthetic-qa-c-current',
                    'owner':'c_installed_f03_finish'}
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            calls = []
            mismatch = False
            def inspect(self,identity):
                return {'Id':'f'*64 if self.mismatch else identity,'State':{
                    'Status':'created','Pid':0,'StartedAt':'0001-01-01T00:00:00Z'}}
            def docker(self,*args,**kwargs):
                self.calls.append(args)
                raise AssertionError('Rejected provider cannot START')
        native.constructor = lambda *args:None
        runner = Runner()
        runner.calls = []
        with self.assertRaises(native.Failure):
            bootstrap.start_provider(runner,approval,native,identities,results[:-1],b'{}')
        with self.assertRaises(native.Failure):
            bootstrap.start_provider(runner,approval,native,identities,results,b'{}')
        with patch.object(bootstrap,'clock_readback',return_value={}):
            with self.assertRaises(native.Failure):
                bootstrap.start_provider(runner,approval,native,identities,results,b'{}')
        with patch.object(bootstrap,'clock_readback',return_value={}), \
                patch.object(bootstrap,'continuity_readback',return_value={}), \
                patch.object(bootstrap,'check_delivery',return_value=[]):
            runner.mismatch = True
            with self.assertRaises(native.Failure):
                bootstrap.start_provider(runner,approval,native,identities,results,b'{}')
        self.assertEqual(runner.calls,[])

    def test_actual_delivery_rejection_precedes_all_lifecycle(self):
        spec = importlib.util.spec_from_file_location('delivery_native',sys.argv[2])
        native = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(native)
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            calls = []
            def docker(self,*args,**kwargs):
                self.calls.append(args)
                raise AssertionError('CREATE and START forbidden')
            def save(self,*args):
                pass
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source,private = root/'source',root/'private'
            source.mkdir()
            private.mkdir()
            (source/'bootstrap').mkdir()
            contents = {'source/runtime.yaml':b'config','source/bootstrap.json':b'{}',
                        'source/bootstrap/read-continuity.sql':b'query','binary':b'fixture binary'}
            for role in ('postgres','app','meter','inventory','fake','operator'):
                contents['private/'+role+'.password'] = b'fixture\n'
            for role in ('app','owner','fake','operator','media','sticker','roles','inventory'):
                contents['private/'+role+'.env'] = b'FIXTURE=literal\n'
            for path,raw in contents.items():
                (root/path).write_bytes(raw)
            files = [{'path':str(root/path),'bytes':len(raw),
                      'sha256':hashlib.sha256(raw).hexdigest()} for path,raw in contents.items()]
            def bind(target,path):
                return {'Destination':target,'Type':'bind','Source':str(path),'RW':False}
            clock = {'Destination':'/run/registration-clock','Type':'volume','Name':'clock','RW':False}
            app = {'invariants':{'Mounts':[bind('/etc/zns/runtime.yaml',source/'runtime.yaml'),
                    bind('/usr/local/bin/zns',root/'binary'),clock],'Config.Env':['FIXTURE=literal']}}
            pg = {'invariants':{'Mounts':[bind('/run/secrets/'+role+'_password',private/(role+'.password'))
                    for role in ('postgres','app','meter','inventory','fake','operator')]}}
            probe = {'invariants':{'Mounts':[bind('/etc/zns',source),bind('/current/zns',root/'binary'),
                                             bind('/private',private),copy.deepcopy(clock)]}}
            roots = [{'path':str(source),'members':['bootstrap.json','bootstrap/read-continuity.sql',
                                                   'runtime.yaml'],'directories':['bootstrap']},
                     {'path':str(private),'members':sorted(path.split('/',1)[1] for path in contents
                                                          if path.startswith('private/')),'directories':[]}]
            pins = {item['path']:item['sha256'] for item in files}
            approval = {'services':{'app':app,'postgres':pg},'readonly_probe':probe,
                        'bootstrap_constructors':[],'compose_file':str(source/'bootstrap.json'),
                        'bootstrap':{'path':str(source/'bootstrap.json')},
                        'continuity_sql':{'path':str(source/'bootstrap/read-continuity.sql')},
                        'delivery_files':files,'delivery_directories':roots,'no_host_source_writers':True,
                        'readonly_inputs':{'runtime_config':pins[str(source/'runtime.yaml')],
                            'app_env':pins[str(private/'app.env')],'owner_env':pins[str(private/'owner.env')]}}
            with patch.object(native,'DAEMON_SOURCE_PREFIX',str(root)+'/' ), \
                    patch.object(bootstrap,'BINARY',pins[str(root/'binary')]), \
                    patch.object(bootstrap,'bootstrap_contract',return_value={}):
                baseline = installation.authenticate_delivery(Runner(),approval,native)
                for target in ('binary','source/runtime.yaml','extra'):
                    with self.subTest(target=target):
                        changed = source/'extra' if target=='extra' else root/target
                        previous = changed.read_bytes() if changed.exists() else None
                        changed.write_bytes(b'changed')
                        runner = Runner()
                        runner.calls = []
                        with self.assertRaises(native.Failure):
                            installation.prerequisites(runner,approval,native)
                        self.assertEqual(runner.calls,[])
                        if previous is None:
                            changed.unlink()
                        else:
                            changed.write_bytes(previous)
                mutations = [
                    lambda value:value['bootstrap'].update(path=str(source/'runtime.yaml')),
                    lambda value:value['continuity_sql'].update(path=str(source/'runtime.yaml')),
                    lambda value:value['readonly_inputs'].update(runtime_config='a'*64),
                    lambda value:value['readonly_inputs'].update(app_env='a'*64),
                    lambda value:value['readonly_inputs'].update(owner_env='a'*64),
                    lambda value:value['readonly_probe']['invariants']['Mounts'][0].update(Source=str(private)),
                    lambda value:value['readonly_probe']['invariants']['Mounts'][1].update(Source=str(source/'runtime.yaml')),
                    lambda value:value['readonly_probe']['invariants']['Mounts'][3].update(Name='foreign-clock'),
                    lambda value:value['services']['postgres']['invariants']['Mounts'][0].update(Source=str(private/'app.password')),
                    lambda value:value['services']['app']['invariants'].update(**{'Config.Env':['FIXTURE=foreign']}),
                    lambda value:value['delivery_directories'][1].update(members=['app.env']),
                    lambda value:value['delivery_files'].pop(),
                ]
                for index,mutate in enumerate(mutations):
                    with self.subTest(cross_binding=index):
                        wrong = copy.deepcopy(approval)
                        mutate(wrong)
                        runner = Runner()
                        runner.calls = []
                        with self.assertRaises(native.Failure):
                            installation.authenticate_delivery(runner,wrong,native)
                        self.assertEqual(runner.calls,[])
                with self.assertRaises(native.Failure):
                    installation.authenticate_delivery(Runner(),approval,native,baseline+[{'changed':True}])

    def test_c_attachments_never_override_native15(self):
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
        self.assertLessEqual(bootstrap.attach_seconds(Runner()),15)
        with patch.object(bootstrap.time,'monotonic',return_value=Runner.work_end-3), \
                patch.object(bootstrap.time,'time',return_value=Runner.work_utc_end-4):
            self.assertEqual(bootstrap.attach_seconds(Runner()),3)
        # Both actual callers must dispatch this bound, not remaining active90.
        import ast
        for path in (Path(bootstrap.__file__),Path(installation.__file__)):
            tree = ast.parse(path.read_bytes())
            attachments = [node for node in ast.walk(tree) if isinstance(node,ast.Call)
                           and isinstance(node.func,ast.Attribute) and node.func.attr=='docker'
                           and len(node.args)>1 and isinstance(node.args[1],ast.Constant)
                           and node.args[1].value=='--attach']
            self.assertEqual(len(attachments),1)
            seconds = next(item.value for item in attachments[0].keywords if item.arg=='seconds')
            self.assertIsInstance(seconds,ast.Call)
            self.assertEqual(seconds.func.attr if isinstance(seconds.func,ast.Attribute)
                             else seconds.func.id,'attach_seconds')
        class BootstrapRunner(Runner):
            identity = 'a'*64
            live = False
            started = False
            attachments = []
            def docker(self,*args,**kwargs):
                if args[:2]==('image','inspect'):
                    return b'[{}]'
                if args[0]=='ps':
                    return self.identity.encode() if self.live else b''
                if args[0]=='compose':
                    self.live,self.started = True,False
                elif args[:2]==('start','--attach'):
                    self.attachments.append(kwargs['seconds'])
                    self.started = True
                elif args[0]=='rm':
                    self.live = False
                return b'{}'
            def inspect(self,identity):
                return {'Id':identity,'State':{'Status':'exited' if self.started else 'created',
                        'Pid':0,'ExitCode':0,'OOMKilled':False}}
            def save(self,name,raw):
                (self.output/name).write_bytes(raw)
        native = Native()
        native.read_pinned = lambda *args: None
        native.intake_deadline = lambda *args: nullcontext()
        native.digest = lambda raw: hashlib.sha256(raw).hexdigest()
        native.literal_compose = lambda value:value
        native.owned_profile = lambda *args:True
        native.rendered_guard = lambda *args:None
        native.constructor = lambda *args:None
        approval = {'project':'synthetic-qa-c-current','owner':'owner',
                    'bootstrap_constructors':[{'name':'synthetic-qa-c-current-'+step['service']+'-1',
                                               'image':'fixture'} for step in bootstrap.STEPS]}
        rendered = {'services':{step['service']:{'profiles':['maintenance'],'restart':'no'}
                                for step in bootstrap.STEPS}}
        with tempfile.TemporaryDirectory() as directory, \
                patch.object(bootstrap,'bootstrap_contract',return_value={}), \
                patch.object(bootstrap,'check_delivery',return_value=[]), \
                patch.object(bootstrap,'clock_readback',return_value={}):
            runner = BootstrapRunner()
            runner.output = Path(directory)
            bootstrap.run_bootstrap(runner,approval,native,rendered)
            self.assertEqual(len(runner.attachments),7)
            self.assertTrue(all(0<seconds<=15 for seconds in runner.attachments))

    def test_readiness_rejects_exited_helper_while_app_healthy(self):
        names = ['postgres','fake']+bootstrap.RUNTIME
        identities = {name:('%064x' % index) for index,name in enumerate(names,1)}
        class Runner:
            work_end = time.monotonic()+90
            work_utc_end = time.time()+90
            publications = []
            def save(self,name,raw):
                self.publications.append(name)
            def inspect(self,identity):
                service = next(name for name,value in identities.items() if value==identity)
                return {'Id':identity,'Config':{'Healthcheck':{'Test':['CMD','health']}},
                        'State':{'Running':service!='media-decoder','Pid':0 if service=='media-decoder' else 1,
                                 'OOMKilled':False,'Health':{'Status':'healthy'}}}
        native = Native()
        checked = []
        native.constructor = lambda profile,expected,project,service,owner: checked.append(service)
        approval = {'services':dict.fromkeys(names,{}),'project':'synthetic-qa-c-current','owner':'owner',
                    'installation_anchor':{'monotonic':time.monotonic(),'utc':time.time()}}
        with self.assertRaises(Native.Failure):
            bootstrap.runtime_readiness(Runner(),approval,native,identities)
        self.assertIn('app',checked)
        self.assertIn('media-decoder',checked)
        observed = {'state':'OBSERVATIONS_NOT_RUNTIME_ADMISSION',
                    'product_source':bootstrap.PRODUCT,'binary_sha256':bootstrap.BINARY,
                    'prerequisites':bootstrap.PREREQUISITES,'identities':identities,
                    'delivery':[],'clock':{},'continuity':{}}
        approval['prerequisite_observations'] = {'path':'fixture','sha256':'a'*64}
        native.read_pinned = lambda *args:observed
        native.intake_deadline = lambda *args:nullcontext()
        with patch.object(installation,'authenticate_delivery',return_value=[]), \
                patch.object(bootstrap,'clock_readback',return_value={}), \
                patch.object(bootstrap,'continuity_readback',return_value={}), \
                patch.object(bootstrap,'start_runtime',return_value=dict.fromkeys(bootstrap.RUNTIME,{})):
            runner = Runner()
            with self.assertRaises(Native.Failure):
                installation.runtime(runner,approval,native)
            self.assertNotIn('startup-readiness.json',runner.publications)

    def test_bootstrap_rejection_precedes_all_lifecycle(self):
        with patch.object(bootstrap,'bootstrap_contract',side_effect=Native.Failure('rejected artifact')):
            with patch.object(bootstrap,'create_graph',side_effect=AssertionError('CREATE is forbidden')) as create:
                with self.assertRaises(Native.Failure):
                    installation.prerequisites(object(),{},Native)
                create.assert_not_called()

    def test_readonly_probe_rejects_changed_argv_before_birth(self):
        class Runner:
            calls = []
            def docker(self,*argv):
                self.calls.append(argv)
                if argv[0]!='ps':
                    raise AssertionError('No CREATE or START is permitted')
                return b''
        pins = dict.fromkeys(('runtime_config','app_env','owner_env'),'a'*64)
        expected = {
            'name':'synthetic-qa-c-current-readonly-probe',
            'image':'sha256:c7776d61291b599bb4dd2f30eb6e7f33f2336e3b88ae800cb1d580e01661467e',
            'network_contract':{'mode':'none'},
            'invariants':{'Config.User':'10001:10001','Config.Entrypoint':['python3'],
                'Config.Cmd':['-B','/etc/zns/probe_ro.py','--config-sha256','a'*64,
                              '--app-sha256','a'*64,'--owner-sha256','a'*64],
                'Mounts':[{'Type':'bind','Source':'/owned/'+str(index),
                           'Destination':target,'RW':False} for index,target in enumerate(
                    ('/etc/zns','/current/zns','/private','/run/registration-clock'))]},
        }
        expected['invariants']['Mounts'][-1] = {
            'Type':'volume','Name':'synthetic-qa-c-current_registration-clock',
            'Destination':'/run/registration-clock','RW':False}
        approval = {'readonly_probe':expected,'readonly_inputs':pins,
                    'project':'synthetic-qa-c-current','owner':'c_installed_f03_finish',
                    'readonly_probe_create':['create','--privileged'],
                    'volumes':{'registration-clock':{'name':'synthetic-qa-c-current_registration-clock'}}}
        runner = Runner()
        with self.assertRaises(Native.Failure):
            installation.readonly_delivery(runner,approval,Native,{})
        self.assertEqual(len(runner.calls),1)
        self.assertEqual(runner.calls[0][0],'ps')
        argv = ['create','--pull','never','--name',expected['name'],'--user','10001:10001',
                '--label','synthetic.owner='+approval['owner'],
                '--label','com.docker.compose.project='+approval['project'],
                '--label','com.docker.compose.service=readonly-probe','--cpus','0.2',
                '--memory','256m','--memory-swap','256m','--pids-limit','64',
                '--read-only','--network','none','--cap-drop','ALL',
                '--security-opt','no-new-privileges','--restart','no','--no-healthcheck',
                '--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777','--entrypoint','python3']
        for mount in sorted(expected['invariants']['Mounts'],key=lambda item:item['Destination']):
            argv.extend(['--mount','type='+mount['Type']+',source='+mount.get('Source',mount.get('Name'))+
                         ',target='+mount['Destination']+',readonly'])
        approval['readonly_probe_create'] = argv+[expected['image'],*expected['invariants']['Config.Cmd']]
        class PartialRunner:
            calls = []
            unresolved_resources = False
            def docker(self,*args,**kwargs):
                self.calls.append(args)
                if args[0]=='ps':
                    return b''
                if args[0]=='create':
                    return self.birth
                raise AssertionError('Unidentified probe cannot START')
        with patch.object(bootstrap,'check_delivery',return_value=[]):
            for birth in (b'',b'a'*12,b'a'*64+b'\n'+b'b'*64):
                with self.subTest(birth_length=len(birth)):
                    partial = PartialRunner()
                    partial.calls,partial.birth = [],birth
                    with self.assertRaises(Native.Failure):
                        installation.readonly_delivery(partial,approval,Native,{})
                    self.assertTrue(partial.unresolved_resources)
                    self.assertEqual([call[0] for call in partial.calls],['ps','create'])

    def test_actual_readonly_and_writable_open_controls(self):
        self.assertEqual((os.getuid(),os.geteuid()),(10001,10001))
        probe_ro.readonly_open(__file__)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'owned-control'
            path.write_bytes(b'preserved')
            with self.assertRaises(ValueError):
                probe_ro.readonly_open(path)
            self.assertEqual(path.read_bytes(),b'preserved')

    def test_initial_clock_exact_state(self):
        state = {'version':1,'installation':'010400000204',
                 'case':'c-registration-clock-20261001-v1',
                 'stand':'synthetic-qa-zns-registration-fixture',
                 'database_address':'postgres:5432/synthetic_qa_zns_registration_fixture',
                 'anchor':'2030-10-02T12:00:00Z','current':'2030-10-02T12:00:00Z','revision':1}
        self.assertEqual(bootstrap.clock_readback(Native,json.dumps(state)),state)
        for field, value in [('revision',0),('revision',True),('current','2030-10-02T12:00:01Z'),
                             ('database_address','postgres:5432/other')]:
            wrong = {**state,field:value}
            with self.assertRaises(Native.Failure):
                bootstrap.clock_readback(Native,json.dumps(wrong))
        with self.assertRaises(Native.Failure):
            bootstrap.clock_readback(Native,json.dumps({**state,'extra':1}))

    def test_literal_bootstrap_and_windows(self):
        contract = json.loads(Path(sys.argv[1]).read_bytes())
        self.assertEqual(contract['steps'],bootstrap.STEPS)
        self.assertEqual(contract['runtime_services'],bootstrap.RUNTIME)
        self.assertEqual(contract['installation_windows']['prerequisites_before_arm'],
                         bootstrap.PREREQUISITES)
        self.assertEqual([contract['installation_windows'][key] for key in (
            'startup_seconds','guard_deadline_offset_seconds','readiness_seconds',
            'child_deadline_offset_seconds','outer_deadline_offset_seconds')],
            [30,600,90,22332,22344])

    def test_complete_created_cohort_before_start(self):
        class Runner:
            def inspect(self, identity):
                return {'Id':identity,'State':{'Status':'created','Pid':0,'Running':False,
                                              'StartedAt':'0001-01-01T00:00:00Z'}}
        native = Native()
        observed = []
        native.constructor = lambda profile,expected,project,service,owner: observed.append(service)
        names = ['postgres','fake']+bootstrap.RUNTIME
        ids = {name:('%064x' % index) for index,name in enumerate(names,1)}
        approval = {'services':dict.fromkeys(names,{}),'project':'synthetic-qa-c-current',
                    'owner':'c_installed_f03_finish'}
        bootstrap.graph_constructors(Runner(),approval,native,ids)
        self.assertEqual(observed,names)
        wrong = dict(ids)
        wrong['app'] = wrong['postgres']
        with self.assertRaises(Native.Failure):
            bootstrap.graph_constructors(Runner(),approval,native,wrong)
        with self.assertRaises(Native.Failure):
            bootstrap.graph_constructors(Runner(),approval,native,{key:value for key,value in ids.items()
                                                                  if key!='fake'})
        for field,value in [('Status','running'),('Pid',1),('Pid',False),('Running',True),
                            ('Running',0),('StartedAt','2030-10-02T12:00:00Z')]:
            with self.subTest(state=field,value=value):
                runner = Runner()
                original = runner.inspect(ids['postgres'])
                original['State'][field] = value
                with patch.object(runner,'inspect',return_value=original), self.assertRaises(Native.Failure):
                    bootstrap.graph_constructors(runner,approval,native,ids)
        runner = Runner()
        with patch.object(runner,'inspect',side_effect=AssertionError('Prefix IDs cannot be inspected')):
            with self.assertRaises(Native.Failure):
                bootstrap.graph_constructors(runner,approval,native,{**ids,'postgres':'a'*12})

    def test_fresh_role_and_row_contract(self):
        state = {
            'database':'synthetic_qa_zns_registration_fixture',
            'session_user':'zns_registration_operator','database_owner':'zns_app',
            'fixture_owner':'zns_app',
            'actors':[{'id':'alice','telegram_id':101},{'id':'bob','telegram_id':202},
                      {'id':'visitor','telegram_id':303}],
            'product_markers':['product-passport-v1','product-v1','registration-fqa-v1'],
            'booking_count':0,'intent_count':0,'ingress_count':0,
            'booking_admins':['visitor'],
            'payment_admins':[{'event_id':'registration-fixture-a','owner':'bob'}],
            'registration_tiers':[
                {'event_id':'registration-fixture-a','position':0,'starts_at':'2030-10-02T13:00:00+00:00'},
                {'event_id':'registration-fixture-a','position':1,'starts_at':'2030-10-03T13:00:00+00:00'},
                {'event_id':'registration-fixture-b','position':0,'starts_at':'2030-10-02T13:00:00+00:00'}],
            'allocated_roles':['zns_app','zns_fake','zns_inventory','zns_meter','zns_registration_operator'],
            'role_memberships':[{'member':'zns_inventory','role':'pg_read_all_stats','admin':False}],
            'privileged_roles':0,
        }
        self.assertEqual(bootstrap.continuity_readback(Native,json.dumps(state)),state)
        for field,value in [('session_user','postgres'),('database_owner','postgres'),
                            ('booking_count',1),('role_memberships',[]),
                            ('booking_admins',['bob']),('privileged_roles',1)]:
            with self.assertRaises(Native.Failure):
                bootstrap.continuity_readback(Native,json.dumps({**state,field:value}))


if __name__ == '__main__':
    # Keep the one exact original public contract argument outside unittest flags.
    unittest.main(argv=[sys.argv[0]],verbosity=2)
