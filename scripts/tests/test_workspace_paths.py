"""Workspace defaults are used by recording and RDP without touching real data."""
import errno
import json
import os
from pathlib import Path
import pty
import select
import shlex
import signal
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]


class WorkspaceRuntimeTests(unittest.TestCase):
    def test_recorder_writes_under_the_users_workspace(self):
        with tempfile.TemporaryDirectory(prefix='pomdock-workspace-') as folder:
            home = Path(folder)
            env = dict(os.environ, HOME=folder, POMDOCK_ENGAGEMENT='synthetic', TERM='xterm')
            for name in ('POMDOCK_SESSION_DIR', 'POMDOCK_PTY', 'POMDOCK_CAPTURE', 'POMDOCK_SESSION_LOG',
                         'POMDOCK_CAPTURE_ACTIVE', 'POMDOCK_SHELL_CONTEXT', 'TMUX'):
                env.pop(name, None)
            pid, fd = pty.fork()
            if pid == 0:
                os.execvpe('bash', ['bash', '--noprofile', '--norc', '-ic', 'source ' + shlex.quote(
                    str(ROOT/'scripts/recording/session-log.sh'))], env)
            output, status = b'', None
            try:
                os.write(fd, b"printf 'SYNTHETIC_WORKSPACE_RECORDING\\n'\nexit\n")
                deadline = time.monotonic() + 8
                while time.monotonic() < deadline:
                    if select.select([fd], [], [], .05)[0]:
                        try:
                            data = os.read(fd, 65536)
                            if not data:
                                break
                            output += data
                        except OSError as error:
                            if error.errno == errno.EIO:
                                break
                            raise
                    done, status = os.waitpid(pid, os.WNOHANG)
                    if done:
                        break
                if status is None or done == 0:
                    done, status = os.waitpid(pid, os.WNOHANG)
                    self.assertEqual(done, pid, 'recording shell did not exit')
                self.assertEqual(os.waitstatus_to_exitcode(status), 0, output.decode(errors='replace'))
                recordings = list((home/'workspace/sessions').glob('*.log'))
                self.assertEqual(len(recordings), 1, output.decode(errors='replace'))
                self.assertIn('SYNTHETIC_WORKSPACE_RECORDING', recordings[0].read_text(errors='replace'))
                self.assertTrue(recordings[0].with_suffix('.timing').is_file())
                self.assertTrue(recordings[0].with_suffix('.start').is_file())
                self.assertFalse((home/'pentest').exists(), 'recorder created the historical directory')
            finally:
                os.close(fd)
                try:
                    remaining, _ = os.waitpid(pid, os.WNOHANG)
                    if remaining == 0:
                        os.kill(pid, signal.SIGKILL)
                        os.waitpid(pid, 0)
                except (ProcessLookupError, ChildProcessError):
                    pass

    def test_rdp_drive_uses_workspace_and_keeps_the_shared_drive_name(self):
        with tempfile.TemporaryDirectory(prefix='pomdock-rdp-workspace-') as folder:
            home = Path(folder)
            bindir = home/'bin'; bindir.mkdir()
            args = home/'args.json'
            client = bindir/'xfreerdp3'
            client.write_text('#!/usr/bin/env python3\nimport json, os, sys\n'
                              'open(os.environ["POMDOCK_RDP_ARGS"],"w").write(json.dumps(sys.argv[1:]))\n')
            client.chmod(0o700)
            env = dict(os.environ, HOME=folder, DISPLAY=':synthetic', POMDOCK_RDP_ARGS=str(args),
                       PATH=str(bindir)+os.pathsep+os.environ['PATH'])
            result = subprocess.run(['bash', str(ROOT/'scripts/pomrdp'), '-u', 'fixture', '192.0.2.10'],
                                    env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('/drive:loot,'+str(home/'workspace'), json.loads(args.read_text()))


if __name__ == '__main__':
    unittest.main()
