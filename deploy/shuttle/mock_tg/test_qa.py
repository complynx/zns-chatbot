"""Behavioral guardrails for QA mailbox publication and stand identity."""
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import qa
import stand


class OperatorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'mailbox').mkdir()
        self.request_id = 'a' * 64
        self.request = self.root / 'mailbox' / (self.request_id + '.request.json')
        self.request.write_text(json.dumps({'MaxOutputTokens': 50, 'Tools': [{'type': 'function', 'name': 'run_script'}]}))
        self.response = self.root / 'answer.json'
        self.body = {'ID': 'synthetic-answer', 'Text': '', 'Calls': [{'ID': 'call-1', 'Name': 'run_script', 'Arguments': {'script': 'return 1'}}],
                     'Usage': {'InputTokens': 10, 'OutputTokens': 2, 'CachedInputTokens': 0, 'CacheWriteTokens': 0}}

    def write_answer(self):
        self.response.write_text(json.dumps(self.body))
        return qa.answer_model(self.root, self.request_id, self.response)

    def test_answer_is_complete_and_cannot_overwrite_previous_receipt(self):
        self.assertEqual(self.write_answer()['answered'], self.request_id)
        target = self.request.with_name(self.request_id + '.response.json')
        original = target.read_bytes()
        self.body['ID'] = 'different'
        with self.assertRaises(FileExistsError):
            self.write_answer()
        self.assertEqual(target.read_bytes(), original)
        self.assertFalse(list((self.root / 'mailbox').glob('.qa-answer-*')))

    def test_unoffered_tool_and_bad_synthetic_usage_publish_nothing(self):
        self.body['Calls'][0]['Name'] = 'not_offered'
        with self.assertRaises(ValueError):
            self.write_answer()
        self.body['Calls'] = []
        self.body['Usage']['OutputTokens'] = 51
        with self.assertRaises(ValueError):
            self.write_answer()
        self.assertFalse(self.request.with_name(self.request_id + '.response.json').exists())

    def test_stop_preserves_incomplete_and_uncertain_initialization(self):
        for stage in ('created', 'configs_ready', 'identity_creation_started', 'migration_started'):
            manifest = dict(name='alpha', project='zinusya-mock-alpha', owner='fixture', stage=stage)
            with self.subTest(stage=stage), patch.object(stand, 'load', return_value=(self.root, manifest)), patch.object(stand, 'ownership', return_value=[]), patch.object(stand, 'run', return_value=''):
                stand.control('alpha', 'stop')
                saved = json.loads((self.root / 'manifest.json').read_text())
                self.assertEqual(saved['stage'], stage)
                self.assertTrue(saved['containers_stopped'])
                if stage.endswith('_started'):
                    with self.assertRaises(AssertionError):
                        stand.start('alpha')

    def test_request_id_cannot_read_outside_mailbox(self):
        for value in ('../credentials', 'a' * 63, 'A' * 64):
            with self.subTest(value=value), self.assertRaises(ValueError):
                qa.model_request(self.root, value)

    def test_stand_names_and_manifest_cannot_select_live_project(self):
        with patch.object(stand, 'CACHE', self.root):
            for value in ('../shuttle-live-test', 'A', '', '/tmp'):
                with self.subTest(value=value), self.assertRaises(ValueError):
                    stand.state_path(value)
            state = self.root / 'alpha'
            state.mkdir()
            (state / 'manifest.json').write_text(json.dumps({'name': 'alpha', 'project': 'shuttle-live-test'}))
            with self.assertRaises(AssertionError):
                stand.load('alpha')


if __name__ == '__main__':
    unittest.main()

# Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
# on behalf of Daniel Drizhuk
