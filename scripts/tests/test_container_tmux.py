"""Private-socket tmux tests never connect to a host's default tmux server."""
import json
import os
from pathlib import Path
import pty
import select
import shlex
import shutil
import signal
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]
HELPER = ROOT / 'scripts/pomdock-tmux'
TMUX = shutil.which('tmux')


@unittest.skipUnless(TMUX and shutil.which('zsh'), 'tmux and zsh are required')
class ContainerTmuxTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='pomdock-container-tmux-')
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        (self.home / 'workspace').mkdir()
        (self.home / 'bin').mkdir()
        (self.home / 'rc').mkdir()
        self.sessions = self.home / 'workspace/sessions'
        self.sessions.mkdir()
        id_command = self.home / 'bin/id'
        id_command.write_text('#!/bin/sh\nif test "$1" = -un; then echo kali; else echo ' + str(os.getuid()) + '; fi\n')
        id_command.chmod(0o755)
        wrapper = self.home / 'bin/pomdock-shell'
        wrapper.write_text('''#!/bin/bash
printf '%s|%s|%s|%s\\n' "$POMDOCK_ENGAGEMENT" "$PWD" "${POMDOCK_CAPTURE:-}" "${POMDOCK_PTY:-}" >> "$HOME/shell-starts"
exec zsh -d -i
''')
        wrapper.chmod(0o755)
        (self.home / 'rc/.zshrc').write_text(
            'source ' + shlex.quote(str(ROOT / 'scripts/recording/session-log.sh')) + '\n'
            'source ' + shlex.quote(str(ROOT / 'scripts/recording/cmdlog.zsh')) + '\n'
            'bindkey -e\nPS1="fixture> "\n')
        self.env = dict(os.environ, HOME=str(self.home), TMUX_TMPDIR=str(self.home),
                        ZDOTDIR=str(self.home / 'rc'), POMDOCK_ENGAGEMENT='synthetic',
                        POMDOCK_SESSION_DIR=str(self.sessions), TERM='xterm-256color',
                        PATH=str(self.home / 'bin') + os.pathsep + os.environ['PATH'])
        for key in ('TMUX', 'TMUX_PANE'):
            self.env.pop(key, None)
        self.clients = []
        self.addCleanup(self.cleanup)

    def cleanup(self):
        for child, fd in self.clients:
            try: os.kill(child, signal.SIGTERM)
            except ProcessLookupError: pass
            try: os.waitpid(child, 0)
            except ChildProcessError: pass
            try: os.close(fd)
            except OSError: pass
        for label in ('pomdock', 'fixture-outer'):
            subprocess.run([TMUX, '-L', label, 'kill-server'], env=self.env, capture_output=True)

    def mux(self, *args, label='pomdock', check=True):
        result = subprocess.run([TMUX, '-L', label, '-f', '/dev/null', *args], env=self.env,
                                text=True, capture_output=True, timeout=5)
        if check: self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout.strip()

    def helper(self, *args, ok=True, env=None):
        result = subprocess.run(['bash', str(HELPER), *args], env=env or self.env,
                                text=True, capture_output=True, timeout=5)
        if ok: self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else: self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    def wait_for(self, check):
        end = time.monotonic() + 6
        while time.monotonic() < end:
            for _, fd in self.clients:
                try:
                    while select.select([fd], [], [], 0)[0]:
                        if not os.read(fd, 65536): break
                except OSError: pass
            if check(): return
            time.sleep(.025)
        self.fail('tmux fixture did not reach expected state')

    def attach_client(self, command=None):
        child, fd = pty.fork()
        if child == 0:
            if command:
                os.execvpe(TMUX, [TMUX, '-L', 'fixture-outer', '-f', '/dev/null', 'attach-session', '-t', 'outer'], self.env)
            os.execvpe('/bin/bash', ['bash', str(HELPER), 'attach'], self.env)
        self.clients.append((child, fd))
        return child, fd

    def test_missing_server_is_empty_but_other_session_is_failure(self):
        self.assertEqual(self.helper('list').stdout, '')
        self.mux('new-session', '-d', '-s', 'unrelated', 'sleep 60')
        self.helper('list', ok=False)

    def test_empty_existing_server_is_not_reported_as_absent(self):
        self.mux('new-session', '-d', '-s', 'unrelated', 'sleep 60')
        self.mux('set-option', '-s', 'exit-empty', 'off')
        self.mux('kill-session', '-t', 'unrelated')
        self.helper('list', ok=False)

    def test_failed_configuration_never_marks_a_partial_session_owned(self):
        wrapper = self.home / 'bin/tmux'
        wrapper.write_text('#!/bin/bash\nfor argument in "$@"; do\n'
                           'if [[ "$argument" == prefix && -n "${FAIL_PREFIX:-}" ]]; then echo fixture-error >&2; exit 1; fi\n'
                           'done\nexec ' + shlex.quote(TMUX) + ' "$@"\n')
        wrapper.chmod(0o755)
        self.helper('ensure', env=dict(self.env, FAIL_PREFIX='1'), ok=False)
        self.helper('ensure', ok=False)

    def test_unowned_workspace_cannot_be_claimed_or_closed(self):
        window = self.mux('new-session', '-d', '-P', '-F', '#{window_id}', '-s', 'workspace', 'sleep 60')
        self.helper('ensure', ok=False)
        self.helper('close', window, ok=False)
        self.assertEqual(self.mux('list-windows', '-t', '=workspace:', '-F', '#{window_id}'), window)

    def test_concurrent_ensure_creates_one_window_and_releases_lock(self):
        children = [subprocess.Popen(['bash', str(HELPER), 'ensure'], env=self.env,
                                     text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE) for _ in range(8)]
        windows = []
        for child in children:
            output, error = child.communicate(timeout=10)
            self.assertEqual(child.returncode, 0, error)
            windows.append(output.strip())
        self.assertEqual(len(set(windows)), 1)
        self.assertRegex(windows[0], r'^@[0-9]+$')
        self.assertEqual(self.helper('ensure').stdout.strip(), windows[0])
        self.assertEqual(len(self.helper('list').stdout.splitlines()), 1)

    def test_ensure_preserves_active_window_and_list_fields(self):
        original = self.helper('ensure').stdout.strip()
        second = self.mux('new-window', '-d', '-P', '-F', '#{window_id}', '-t', '=workspace:', '-n', 'second')
        self.assertEqual(self.helper('ensure').stdout.strip(), original)
        self.mux('select-window', '-t', '=workspace:' + second)
        self.assertEqual(self.helper('ensure').stdout.strip(), second)
        self.mux('rename-window', '-t', '=workspace:' + original, 'named shell')
        rows = [line.split('\t') for line in self.helper('list').stdout.splitlines()]
        self.assertEqual(len(rows), 2)
        self.assertTrue(all(len(row) == 7 for row in rows))
        self.assertEqual(rows[0][3], 'named shell')
        self.assertTrue(all(row[4:] == ['workspace', '0', '1'] for row in rows))
        self.assertEqual(self.mux('show-option', '-v', '-t', '=workspace:', 'prefix'), 'C-b')
        self.assertEqual(self.mux('show-option', '-v', '-t', '=workspace:', 'prefix2'), 'None')
        self.assertEqual(self.mux('show-option', '-v', '-t', '=workspace:', 'default-command'), 'exec pomdock-shell')
        self.assertIn('bg=#62c7d5', self.mux('show-options', '-gwv', 'window-status-current-style'))
        self.assertIn('clipboard', self.mux('show-options', '-sv', 'terminal-features'))

    def test_each_pane_records_once_and_clears_inherited_capture(self):
        env = dict(self.env, POMDOCK_CAPTURE='old', POMDOCK_PTY='/dev/pts/old',
                   POMDOCK_CAPTURE_ACTIVE='1', POMDOCK_SESSION_LOG='/not/a/transcript')
        window = self.helper('ensure', env=env).stdout.strip()
        self.wait_for(lambda: len(list(self.sessions.glob('*.log'))) == 1)
        self.mux('split-window', '-d', '-t', '=workspace:' + window, '-c', str(self.home / 'workspace'))
        self.wait_for(lambda: len(list(self.sessions.glob('*.log'))) == 2)
        starts = (self.home / 'shell-starts').read_text().splitlines()
        self.assertEqual(len(starts), 2)
        self.assertTrue(all(line == 'synthetic|' + str(self.home / 'workspace') + '||' for line in starts))
        self.assertTrue(all(row.split('\t')[-1] == '2' for row in self.helper('list').stdout.splitlines()))

    def test_attach_detach_keeps_pane_and_ctrl_a_editing(self):
        window = self.helper('ensure').stdout.strip()
        pane = self.mux('display-message', '-p', '-t', '=workspace:' + window, '#{pane_id}:#{pane_pid}')
        self.wait_for(lambda: len(list(self.sessions.glob('*.log'))) == 1)
        _, fd = self.attach_client()
        self.wait_for(lambda: self.mux('display-message', '-p', '-t', '=workspace:', '#{session_attached}') == '1')
        os.write(fd, b"printf 'TAIL\\n'")
        os.write(fd, b"\x01printf 'HEAD\\n'; \r")
        commands = self.sessions / 'commands.jsonl'
        self.wait_for(lambda: commands.exists() and "printf 'HEAD" in commands.read_text())
        records = [json.loads(line) for line in commands.read_text().splitlines()]
        self.assertTrue(any(row.get('cmd', '').startswith("printf 'HEAD\\n'; printf 'TAIL") for row in records))
        os.write(fd, b'\x02d')
        self.wait_for(lambda: self.mux('display-message', '-p', '-t', '=workspace:', '#{session_attached}') == '0')
        self.assertEqual(self.mux('display-message', '-p', '-t', '=workspace:' + window, '#{pane_id}:#{pane_pid}'), pane)
        self.assertEqual(len(list(self.sessions.glob('*.log'))), 1)

    def test_nested_host_prefix_only_reaches_container_when_doubled(self):
        window = self.helper('ensure').stdout.strip()
        self.mux('new-session', '-d', '-s', 'outer', 'bash ' + shlex.quote(str(HELPER)) + ' attach', label='fixture-outer')
        self.mux('set-option', '-w', '-t', 'outer', 'remain-on-exit', 'on', label='fixture-outer')
        _, fd = self.attach_client(command='outer')
        self.wait_for(lambda: self.mux('display-message', '-p', '-t', '=workspace:', '#{session_attached}') == '1')
        os.write(fd, b'\x02\x02c')
        self.wait_for(lambda: len(self.helper('list').stdout.splitlines()) == 2)
        os.write(fd, b'\x02\x02d')
        self.wait_for(lambda: self.mux('display-message', '-p', '-t', '=workspace:', '#{session_attached}') == '0')
        self.assertEqual(self.mux('show-option', '-gv', 'prefix', label='fixture-outer'), 'C-b')
        self.assertTrue(window in self.helper('list').stdout)

    def test_close_rejects_other_session_and_invalid_ids_and_root(self):
        self.helper('ensure')
        other = self.mux('new-session', '-d', '-P', '-F', '#{window_id}', '-s', 'other', 'sleep 60')
        for window in (other, '@0;kill-server', '0'):
            self.helper('close', window, ok=False)
        self.assertTrue(self.mux('has-session', '-t', 'other') == '')
        (self.home / 'bin/id').write_text('#!/bin/sh\necho root\n')
        self.helper('ensure', ok=False)
