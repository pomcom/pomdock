"""Fallback init tests use isolated homes/PTYs, never a user's history database."""
from contextlib import closing
import errno
import os
from pathlib import Path
import pty
import select
import shlex
import shutil
import signal
import sqlite3
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]


def interactive(shell, text, env):
    args = [shell, '--noprofile', '--norc', '-i'] if Path(shell).name == 'bash' else [shell, '-d', '-f', '-i']
    child, fd = pty.fork()
    if child == 0:
        os.execvpe(shell, args, env)
    output = bytearray()
    reaped = False
    try:
        os.write(fd, text.encode())
        end = time.monotonic() + 15
        while time.monotonic() < end:
            if select.select([fd], [], [], .1)[0]:
                try:
                    data = os.read(fd, 65536)
                    if not data:
                        break
                    output.extend(data)
                except OSError as error:
                    if error.errno == errno.EIO:
                        break
                    raise
            found, status = os.waitpid(child, os.WNOHANG)
            if found:
                reaped = True
                if os.waitstatus_to_exitcode(status) != 0:
                    raise AssertionError(output.decode(errors='replace'))
                return output.decode(errors='replace')
        # PTY EOF can arrive just before the child becomes waitable. Retain
        # the original deadline instead of treating that small race as a hang.
        while not reaped and time.monotonic() < end:
            found, status = os.waitpid(child, os.WNOHANG)
            if found:
                reaped = True
                if os.waitstatus_to_exitcode(status) != 0:
                    raise AssertionError(output.decode(errors='replace'))
                return output.decode(errors='replace')
            time.sleep(.01)
        raise AssertionError('Shell did not exit: ' + output.decode(errors='replace'))
    finally:
        os.close(fd)
        if not reaped:
            os.kill(child, signal.SIGKILL)
            os.waitpid(child, 0)


class AtuinFallbackTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='pomdock-atuin-test-')
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        config = self.home / 'config/atuin'
        config.mkdir(parents=True)
        (config / 'config.toml').write_text('auto_sync = false\nupdate_check = false\n')
        self.env = dict(os.environ, HOME=str(self.home), XDG_CONFIG_HOME=str(self.home / 'config'),
                        XDG_DATA_HOME=str(self.home / 'data'), ATUIN_CONFIG_DIR=str(config),
                        ATUIN_NOBIND='1', TERM='xterm-256color', HISTFILE=str(self.home / 'history'))
        for name in ('ATUIN_SESSION', 'ATUIN_SHLVL', 'ATUIN_HISTORY_ID', 'BASH_ENV', 'ZDOTDIR'):
            self.env.pop(name, None)

    def fallback(self, shell):
        return shlex.quote(str(ROOT / 'scripts/recording' / ('atuin-init.' + shell)))

    def mock(self):
        bindir = self.home / 'bin'
        bindir.mkdir()
        fake = bindir / 'atuin'
        fake.write_text('''#!/bin/sh
printf 'init\\n' >> "$HOME/inits"
if [ "$2" = zsh ]; then
    printf '%s\\n' '_atuin_preexec() { :; }; _atuin_precmd() { :; }'
else
    printf '%s\\n' '__atuin_initialized=true; __atuin_preexec() { :; }; __atuin_precmd() { :; }'
fi
''')
        fake.chmod(0o755)
        self.env['PATH'] = str(bindir) + os.pathsep + self.env['PATH']

    def test_deferred_init_respects_personal_configuration(self):
        self.mock()
        for shell in ('bash', 'zsh'):
            with self.subTest(shell=shell):
                executable = shutil.which(shell)
                if not executable:
                    continue
                prefix = '__atuin' if shell == 'bash' else '_atuin'
                commands = (f'source {self.fallback(shell)}; {prefix}_preexec() {{ :; }}; '
                            f'{prefix}_precmd() {{ :; }}\nexit\n')
                interactive(executable, commands, self.env)
                self.assertFalse((self.home / 'inits').exists())

    def test_fallback_initializes_once_and_preserves_prompt_slots(self):
        self.mock()
        for shell in ('bash', 'zsh'):
            with self.subTest(shell=shell):
                executable = shutil.which(shell)
                if not executable:
                    continue
                if (self.home / 'inits').exists():
                    (self.home / 'inits').unlink()
                if shell == 'bash':
                    # Literal single quotes keep the shell's HOME expansion in the prompt.
                    setup = "PROMPT_COMMAND='printf slot0 >> \"$HOME/slots\"'; PROMPT_COMMAND[101]='printf slot101 >> \"$HOME/slots\"'; "
                else:
                    setup = ''
                interactive(executable, f'{setup}source {self.fallback(shell)}\nsource {self.fallback(shell)}\nprintf ready\\n\nexit\n', self.env)
                self.assertEqual((self.home / 'inits').read_text().splitlines(), ['init'])
                if shell == 'bash':
                    self.assertIn('slot0', (self.home / 'slots').read_text())
                    self.assertIn('slot101', (self.home / 'slots').read_text())

    def test_real_offline_history_when_atuin_is_available(self):
        binary = os.environ.get('POMDOCK_TEST_ATUIN') or shutil.which('atuin')
        if not binary:
            self.skipTest('Set POMDOCK_TEST_ATUIN to run offline Atuin persistence checks')
        bindir = self.home / 'bin'
        bindir.mkdir()
        (bindir / 'atuin').symlink_to(Path(binary).resolve())
        self.env['PATH'] = str(bindir) + os.pathsep + self.env['PATH']
        for shell in ('bash', 'zsh'):
            executable = shutil.which(shell)
            if not executable:
                continue
            with self.subTest(shell=shell):
                marker = f'POMDOCK_ATUIN_OFFLINE_{shell.upper()}'
                output = interactive(executable, f'source {self.fallback(shell)}\nprintf \'{marker}\\n\'\nsh -c \'exit 7\' # {marker}_FAIL\nsleep .2\nexit\n', self.env)
                self.assertIn(marker, output)
                database = self.home / 'data/atuin/history.db'
                end = time.monotonic() + 5
                found = False
                while time.monotonic() < end:
                    if database.exists():
                        with closing(sqlite3.connect(database)) as db:
                            found = bool(db.execute('select command from history where command like ?', ('%' + marker + '%',)).fetchall())
                        if found:
                            break
                    time.sleep(.1)
                self.assertTrue(found, output)
                with closing(sqlite3.connect(database)) as db:
                    exit_codes = db.execute('select exit from history where command like ?',
                                            ('%# ' + marker + '_FAIL%',)).fetchall()
                self.assertEqual(exit_codes, [(7,)], output)
