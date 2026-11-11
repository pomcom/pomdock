"""Application histories belong in the engagement; existing user data survives."""
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ApplicationHistoryTests(unittest.TestCase):
    def test_paths_are_persistent_and_existing_msf_history_is_preserved(self):
        with tempfile.TemporaryDirectory(prefix='pomdock-apphist-') as folder:
            home = Path(folder)
            sessions = home / 'loot/sessions'
            msf = home / '.msf4/history'
            msf.parent.mkdir()
            msf.write_text('existing user history')
            env = dict(os.environ, HOME=str(home), POMDOCK_ENGAGEMENT='synthetic',
                       POMDOCK_SESSION_DIR=str(sessions))
            command = ('source ' + shlex.quote(str(ROOT / 'scripts/recording/app-history.sh'))
                       + '; printf "%s\\n" "$PYTHON_HISTORY" "$SQLITE_HISTORY" "$PSQL_HISTORY" "$MYSQL_HISTFILE"')
            result = subprocess.run(['bash', '--noprofile', '--norc', '-ic', command],
                                    env=env, capture_output=True, text=True, check=True)
            self.assertEqual(result.stdout.splitlines(), [str(sessions / 'apphist' / name) for name in
                             ('python_history', 'sqlite_history', 'psql_history', 'mysql_history')])
            self.assertEqual((sessions / 'apphist').stat().st_mode & 0o777, 0o700)
            self.assertEqual(msf.read_text(), 'existing user history')
            self.assertFalse(msf.is_symlink())

    def test_new_msf_history_points_into_engagement(self):
        with tempfile.TemporaryDirectory(prefix='pomdock-apphist-') as folder:
            home = Path(folder)
            env = dict(os.environ, HOME=str(home), POMDOCK_ENGAGEMENT='synthetic')
            subprocess.run(['bash', '--noprofile', '--norc', '-ic', 'source ' + shlex.quote(
                str(ROOT / 'scripts/recording/app-history.sh'))], env=env, capture_output=True, check=True)
            self.assertEqual((home / '.msf4/history').readlink(),
                             home / 'workspace/sessions/apphist/msfconsole_history')
