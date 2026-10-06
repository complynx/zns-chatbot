"""Focused data-contract controls; these do not accept installation runtime."""
import json
import hashlib
import importlib.util
import os
import sys
import tempfile
import time
from contextlib import nullcontext
import unittest
from unittest.mock import patch
from pathlib import Path

import bootstrap
import installation
import probe_ro


class Native:
    class Failure(Exception):
        pass

    @staticmethod
    def same(left, right):
        return json.dumps(left,sort_keys=True) == json.dumps(right,sort_keys=True)


class Contracts(unittest.TestCase):
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
                                             bind('/private',private),clock]}}
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
                installation.authenticate_delivery(Runner(),approval,native)
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
