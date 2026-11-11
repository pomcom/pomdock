"""Installer failures must reach Docker/callers after remaining tools are tried."""
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class SetupFailureTests(unittest.TestCase):
    def run_setup(self, *arguments, failures=''):
        with tempfile.TemporaryDirectory(prefix='pomdock-setup-failure-') as directory:
            root = Path(directory)
            script = (ROOT / 'setup-pentest.sh').read_text()
            fixtures = {
                'PENTEST_APT': ['fixture-apt-first', 'fixture-apt-last'],
                'PENTEST_GO': ['fixture-go-first example.invalid/first@v1',
                               'fixture-go-last example.invalid/last@v1'],
                'PENTEST_BINS': [],
                'PENTEST_PIP': ['fixture-pipx-first', 'fixture-pipx-last'],
            }
            for name, entries in fixtures.items():
                replacement = name + '=(\n' + '\n'.join(json.dumps(entry) for entry in entries) + '\n)'
                script, count = re.subn(r'^' + name + r'=\([\s\S]*?^\)', replacement,
                                        script, count=1, flags=re.MULTILINE)
                self.assertEqual(count, 1)
            installer = root / 'setup.sh'
            installer.write_text(script)
            bindir = root / 'bin'
            bindir.mkdir()
            log = root / 'calls.jsonl'
            mock = f'''#!{sys.executable}
import json, os, pathlib, sys
name=pathlib.Path(sys.argv[0]).name
args=sys.argv[1:]
with pathlib.Path(os.environ['SETUP_FIXTURE_LOG']).open('a') as stream:
    stream.write(json.dumps([name, *args])+'\\n')
if name == 'id':
    print('0')
    sys.exit(0)
failures=os.environ.get('SETUP_FIXTURE_FAILURES','').split(',')
if name in failures and any(('first' in argument) for argument in args):
    sys.exit(1)
'''
            for name in ('apt-get', 'go', 'pipx', 'id'):
                path = bindir / name
                path.write_text(mock)
                path.chmod(0o755)
            for name in ('awk', 'grep'):
                (bindir / name).symlink_to(shutil.which(name))
            env = dict(os.environ, HOME=str(root), GOPATH=str(root / 'go'), PATH=str(bindir),
                       SETUP_FIXTURE_LOG=str(log), SETUP_FIXTURE_FAILURES=failures)
            result = subprocess.run(['/bin/bash', str(installer), *arguments], env=env,
                                    text=True, capture_output=True)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            return result, calls

    def test_apt_only_attempts_remaining_packages_and_fails(self):
        result, calls = self.run_setup('--apt-only', failures='apt-get')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(['apt-get', 'install', '-y', 'fixture-apt-last'], calls)
        self.assertFalse(any(call[0] in ('go', 'pipx') for call in calls))

    def test_full_setup_reports_all_failed_stages_after_attempts(self):
        result, calls = self.run_setup(failures='apt-get,go,pipx')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(['apt-get', 'install', '-y', 'fixture-apt-last'], calls)
        self.assertIn(['go', 'install', 'example.invalid/last@v1'], calls)
        self.assertIn(['pipx', 'install', 'fixture-pipx-last'], calls)
        self.assertIn('3 install(s) failed', result.stdout)

    def test_successful_setup_and_apt_only_exit_zero(self):
        for flags in ((), ('--apt-only',)):
            with self.subTest(flags=flags):
                result, _ = self.run_setup(*flags)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
