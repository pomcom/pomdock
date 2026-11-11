"""Managed Bash uses the shared help reference without losing builtin topics."""
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SHELL_HELP = ROOT / 'scripts/recording/shell-help.sh'


class ShellHelpTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix='pomdock-help-')
        self.addCleanup(self.folder.cleanup)
        self.home = Path(self.folder.name)
        self.bin = self.home / 'bin'
        self.bin.mkdir()
        (self.bin / 'pomdock-help').symlink_to(ROOT / 'scripts/pomdock-help')
        self.env = dict(os.environ, HOME=str(self.home), POMDOCK_ENGAGEMENT='synthetic',
                        POMDOCK_ROUTE='direct', PATH=str(self.bin) + os.pathsep + os.environ['PATH'])

    def bash(self, command, *, managed=True, interactive=True):
        env = dict(self.env)
        if not managed:
            env.pop('POMDOCK_ENGAGEMENT', None)
        return subprocess.run(['bash', '--noprofile', '--norc', '-ic' if interactive else '-c',
                               'source ' + shlex.quote(str(SHELL_HELP)) + '; ' + command],
                              env=env, capture_output=True, text=True)

    def test_bare_help_uses_shared_reference_in_nested_managed_bash(self):
        command = 'bash --noprofile --rcfile ' + shlex.quote(str(SHELL_HELP)) + ' -ic help'
        result = self.bash(command)
        self.assertEqual(result.returncode, 0, result.stderr)
        for expected in ('Pomdock · synthetic', 'Sessions', 'Networking', 'netinfo', 'RDP', 'pomrdp'):
            self.assertIn(expected, result.stdout)

    def test_topics_and_options_keep_the_bash_builtin(self):
        for topic in ('cd', '-s cd'):
            result = self.bash('help ' + topic)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('cd:', result.stdout)
            self.assertNotIn('Pomdock', result.stdout)
        missing = self.bash('help pomdock_nonexistent_builtin')
        self.assertNotEqual(missing.returncode, 0)

    def test_unmanaged_and_noninteractive_shells_keep_builtin(self):
        for settings in ({'managed': False}, {'interactive': False}):
            result = self.bash('type -t help', **settings)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), 'builtin')

    def test_missing_helper_falls_back_and_prompt_state_is_untouched(self):
        (self.bin / 'pomdock-help').unlink()
        # Avoid a separately installed helper on the developer's PATH.
        empty_path = self.home / 'empty'
        empty_path.mkdir()
        command = ('PROMPT_COMMAND=original; trap "true" DEBUG; source '
                   + shlex.quote(str(SHELL_HELP))
                   + '; PATH=' + shlex.quote(str(empty_path))
                   + '; help -s cd; help >/dev/null; '
                   'printf "PROMPT=%s\\n" "$PROMPT_COMMAND"; trap -p DEBUG')
        result = self.bash(command)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('cd:', result.stdout)
        self.assertIn('PROMPT=original', result.stdout)
        self.assertIn("trap -- 'true' DEBUG", result.stdout)


if __name__ == '__main__':
    unittest.main()
