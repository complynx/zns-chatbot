"""Focused data-contract controls; these do not accept installation runtime."""
import json
import os
import sys
import tempfile
import unittest
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
