"""Bootstrap must refuse an initialized stand before any external effect."""
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

previous_address = socket.getaddrinfo
import prepare
socket.getaddrinfo = previous_address


class PreparationTests(unittest.TestCase):
    def test_fresh_check_allows_missing_state_and_preserves_ca_only_state(self):
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory) / 'stand'
            with patch.object(prepare, 'STATE', state):
                prepare.require_fresh()
                self.assertFalse(state.exists())
                state.mkdir()
                (state / 'root-ca.cer').write_bytes(b'synthetic CA')
                prepare.require_fresh()
                self.assertEqual([p.name for p in state.iterdir()], ['root-ca.cer'])
                self.assertEqual((state / 'root-ca.cer').read_bytes(), b'synthetic CA')

    def test_existing_runtime_marker_refuses_without_changing_files_or_services(self):
        for marker in ('identity.json', 'loom.yaml', 'shuttle.yaml'):
            with self.subTest(marker=marker), tempfile.TemporaryDirectory() as directory:
                state = Path(directory) / 'stand'
                state.mkdir()
                (state / marker).write_bytes(b'preserve existing synthetic settings')
                tls = SimpleNamespace(issue=Mock())
                with patch.object(prepare, 'STATE', state), patch.object(prepare, 'ENV', state / 'compose.env'), \
                        patch.object(prepare, 'compose', side_effect=RuntimeError('Docker mutation attempted')) as docker, \
                        patch.dict(sys.modules, {'tls': tls}):
                    with self.assertRaisesRegex(RuntimeError, 'already initialized'):
                        prepare.prepare()
                    docker.assert_not_called()
                    tls.issue.assert_not_called()
                self.assertEqual([p.name for p in state.iterdir()], [marker])
                self.assertEqual((state / marker).read_bytes(), b'preserve existing synthetic settings')

    @unittest.skipUnless(shutil.which('pwsh'), 'PowerShell wrapper requires pwsh')
    def test_start_refuses_before_build_or_provisioning(self):
        with tempfile.TemporaryDirectory(prefix='zinusya-start-guard-') as directory:
            root = Path(directory)
            scripts = root / 'deploy/shuttle/test'
            scripts.mkdir(parents=True)
            for name in ('run.ps1', 'prepare.py'):
                shutil.copyfile(Path(__file__).with_name(name), scripts / name)
            (root / 'config').mkdir()
            (root / 'config/shuttle.test.env').write_text('SYNTHETIC_TEST_ONLY=1\n')
            state = root / '.cache/shuttle-live-test'
            state.mkdir(parents=True)
            (state / 'identity.json').write_text('preserve identity')
            record = root / 'docker-was-invoked'
            commands = root / 'commands'
            commands.mkdir()
            if os.name == 'nt':
                (commands / 'python.cmd').write_text(f'@"{sys.executable}" %*\r\n')
                (commands / 'docker.cmd').write_text(f'@echo invoked>"{record}"\r\n@exit /b 86\r\n')
            else:
                import shlex
                (commands / 'python').write_text('#!/bin/sh\nexec ' + shlex.quote(sys.executable) + ' "$@"\n')
                (commands / 'docker').write_text('#!/bin/sh\nprintf invoked >' + shlex.quote(str(record)) + '\nexit 86\n')
                for command in commands.iterdir():
                    command.chmod(0o700)
            result = subprocess.run([shutil.which('pwsh'), '-NoProfile', '-File', str(scripts / 'run.ps1'), 'Start'],
                                    cwd=root, env=dict(os.environ, PATH=str(commands)),
                                    capture_output=True, text=True, timeout=15)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(record.exists(), 'Start called Docker on an initialized stand')
            self.assertIn('already initialized', result.stdout + result.stderr)
            self.assertEqual((state / 'identity.json').read_text(), 'preserve identity')


if __name__ == '__main__':
    unittest.main()
