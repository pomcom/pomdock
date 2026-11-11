"""Prompt state comes from local facts; synthetic interfaces never make probes."""
from contextlib import closing, redirect_stdout
from importlib.machinery import SourceFileLoader
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import sqlite3
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from datetime import datetime
from zoneinfo import ZoneInfo
from test_atuin_fallback import interactive

ROOT = Path(__file__).resolve().parents[2]


def module(name, path):
    spec = importlib.util.spec_from_loader(name, SourceFileLoader(name, str(path)))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


NET = module('pomdock_netcontext', ROOT / 'scripts/pomdock-netcontext')
LAYOUT = module('pomdock_workspace', ROOT / 'scripts/workspace-layout.py')
GUARD = module('pomdock_prompt_guard', ROOT / 'scripts/prompt/guard-dotfiles.py')


class NetworkContextTests(unittest.TestCase):
    def test_only_actual_up_tunnel_ipv4_addresses_are_shown(self):
        def interface(name, kind, address='192.0.2.9', flags=None):
            return dict(ifname=name, flags=['UP'] if flags is None else flags,
                        linkinfo={'info_kind': kind}, addr_info=[{'family': 'inet', 'local': address}])
        interfaces = [interface('tun0', 'tun'), interface('customer-lab', 'wireguard', '198.51.100.8'),
                      interface('eth0', 'veth'), interface('tun-fake', 'veth'),
                      interface('tap0', 'tun', flags=[]), interface('wg0', 'wireguard', '::1')]
        self.assertEqual(NET.context(interfaces), 'tun0 192.0.2.9 · customer-lab 198.51.100.8')
        self.assertEqual(NET.context([]), '')

    def test_helper_only_queries_local_ip_state_and_fails_quietly(self):
        with patch.object(NET.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout='[]')) as run:
            output = io.StringIO()
            with redirect_stdout(output): NET.main()
            self.assertEqual(output.getvalue(), '')
            self.assertEqual(run.call_args.args[0], ['ip', '-j', '-d', '-4', 'address', 'show'])
        for error in (FileNotFoundError(), subprocess.TimeoutExpired('ip', .5)):
            with patch.object(NET.subprocess, 'run', side_effect=error), redirect_stdout(io.StringIO()) as output:
                NET.main()
                self.assertEqual(output.getvalue(), '')


class WorkspaceLayoutTests(unittest.TestCase):
    def test_old_image_artifacts_move_without_loss_and_link_is_idempotent(self):
        with tempfile.TemporaryDirectory() as folder:
            home = Path(folder)
            legacy = home / 'pentest'; legacy.mkdir()
            (legacy / 'keep.txt').write_text('synthetic base artifact')
            LAYOUT.normalize(home, os.getuid(), os.getgid())
            LAYOUT.normalize(home, os.getuid(), os.getgid())
            self.assertEqual(legacy.readlink(), Path('workspace'))
            self.assertEqual((home / 'workspace/keep.txt').read_text(), 'synthetic base artifact')

    def test_conflicting_populated_directories_are_never_merged_or_deleted(self):
        with tempfile.TemporaryDirectory() as folder:
            home = Path(folder)
            for name in ('pentest', 'workspace'):
                (home / name).mkdir(); (home / name / 'keep').write_text(name)
            with self.assertRaisesRegex(ValueError, 'refusing to merge'):
                LAYOUT.normalize(home, os.getuid(), os.getgid())
            self.assertEqual((home / 'pentest/keep').read_text(), 'pentest')
            self.assertEqual((home / 'workspace/keep').read_text(), 'workspace')


class ShellPromptTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='pomdock-prompt-')
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        (self.home / 'workspace').mkdir()
        bindir = self.home / 'bin'; bindir.mkdir()
        helper = bindir / 'pomdock-netcontext'
        helper.write_text('#!/bin/sh\nif test -f "$HOME/tunnels"; then cat "$HOME/tunnels"; fi\n')
        helper.chmod(0o755)
        exitip = bindir / 'pomdock-exitip'
        exitip.write_text('#!/bin/sh\nif test -f "$HOME/exit-ip"; then cat "$HOME/exit-ip"; fi\n')
        exitip.chmod(0o755)
        self.atuin = os.environ.get('POMDOCK_TEST_ATUIN')
        if not self.atuin:
            vendored = ROOT / 'vendor/atuin/target/release/atuin'
            self.atuin = str(vendored) if vendored.is_file() else shutil.which('atuin')
        if self.atuin:
            (bindir / 'atuin').symlink_to(Path(self.atuin).resolve())
        self.env = dict(os.environ, HOME=str(self.home), POMDOCK_KIND='container', POMDOCK_MACHINE='lab',
                        PATH=str(bindir)+os.pathsep+os.environ['PATH'], TERM='xterm-256color')

    def test_failure_multiline_duration_and_recorder_hooks(self):
        for shell in ('bash', 'zsh'):
            executable = shutil.which(shell)
            if not executable: continue
            with self.subTest(shell=shell):
                sessions = self.home / ('sessions-' + shell); sessions.mkdir()
                log = sessions / 'fixture.log'; log.touch()
                config = self.home / ('config-' + shell); config.mkdir()
                (config / 'config.toml').write_text('auto_sync=false\nupdate_check=false\n')
                data = self.home / ('data-' + shell)
                env = dict(self.env, XDG_DATA_HOME=str(data), ATUIN_CONFIG_DIR=str(config), ATUIN_NOBIND='1',
                           POMDOCK_ENGAGEMENT='lab', POMDOCK_CAPTURE='fixture',
                           POMDOCK_SESSION_DIR=str(sessions), POMDOCK_SESSION_LOG=str(log))
                prompt = shlex.quote(str(ROOT / 'scripts/prompt' / ('prompt.' + shell)))
                recorder = shlex.quote(str(ROOT / 'scripts/recording' / ('cmdlog.' + shell)))
                atuin = shlex.quote(str(ROOT / 'scripts/recording' / ('atuin-init.' + shell)))
                commands = (f'source {atuin}; source {recorder}; source {prompt}\n'
                            "sh -c 'exit 7' # PROMPT_FAILURE\n"
                            "printf '%s\\n' 'MULTILINE\nVALUE'\n"
                            "sleep 2.1\nexit 0\n")
                output = interactive(executable, commands, env)
                self.assertIn('· exit 7', output)
                self.assertIn('· 2s', output)
                records = [json.loads(line) for line in (sessions / 'commands.jsonl').read_text().splitlines()]
                failed = next(row for row in records if 'PROMPT_FAILURE' in row.get('cmd', ''))
                self.assertTrue(any(row.get('id') == failed['id'] and row.get('exit') == 7 for row in records))
                self.assertTrue(any('MULTILINE\nVALUE' in row.get('cmd', '') for row in records))
                if self.atuin:
                    with closing(sqlite3.connect(data / 'atuin/history.db')) as db:
                        exits = db.execute("select exit from history where command like '%# PROMPT_FAILURE%'").fetchall()
                    self.assertEqual(exits, [(7,)])

    def test_tunnel_disappears_on_next_prompt_and_legacy_poll_is_never_started(self):
        if not shutil.which('zsh'): self.skipTest('zsh unavailable')
        for name in ('curl', 'starship'):
            fake = self.home / 'bin' / name
            fake.write_text('#!/bin/sh\necho called >> "$HOME/' + name + '-calls"\n')
            fake.chmod(0o755)
        original = '''if command -v starship &> /dev/null; then
    eval "$(starship init zsh)"
fi
_pomdock_exitip() { curl https://example.invalid; }
autoload -Uz add-zsh-hook 2>/dev/null && add-zsh-hook precmd _pomdock_exitip
'''
        guarded = GUARD.guard(original)
        self.assertEqual(GUARD.guard(guarded), guarded)
        dotfiles = self.home / 'legacy.zsh'; dotfiles.write_text(guarded)
        (self.home / 'tunnels').write_text('tun0 192.0.2.9\n')
        prompt = shlex.quote(str(ROOT / 'scripts/prompt/prompt.zsh'))
        commands = (f'source {prompt}; source {shlex.quote(str(dotfiles))}\n'
                    'printf "BEFORE_REMOVE\\n"\n'
                    'command rm "$HOME/tunnels"\n'
                    'printf "AFTER_REMOVE\\n"\nexit 0\n')
        output = interactive(shutil.which('zsh'), commands, self.env)
        self.assertIn('tun0 192.0.2.9', output)
        self.assertNotIn('tun0 192.0.2.9', output.rsplit('AFTER_REMOVE', 1)[-1])
        self.assertFalse((self.home / 'curl-calls').exists())
        self.assertFalse((self.home / 'starship-calls').exists())

    def test_clock_exit_ip_and_live_reload_preserve_prompt_hooks(self):
        for shell in ('bash', 'zsh'):
            executable = shutil.which(shell)
            if not executable:
                continue
            with self.subTest(shell=shell):
                (self.home / 'exit-ip').write_text('1.1.1.1\n')
                prompt = shlex.quote(str(ROOT / 'scripts/prompt' / ('prompt.' + shell)))
                if shell == 'zsh':
                    inspect = ('print -rl -- "${precmd_functions[@]}" > "$HOME/prompt-callbacks"\n'
                               'print -r -- "$PROMPT" > "$HOME/prompt-definition"\n')
                else:
                    inspect = ('printf "%s\\n" "${PROMPT_COMMAND[@]}" > "$HOME/prompt-callbacks"\n'
                               'printf "%s\\n" "$PS1" > "$HOME/prompt-definition"\n')
                commands = (f'cd "$HOME/workspace"\nsource {prompt}\nsource {prompt}\n'
                            "sh -c 'exit 7'\n" + inspect +
                            'command rm "$HOME/exit-ip"\nprintf "AFTER_IP_REMOVED\\n"\nexit 0\n')
                before = datetime.now(ZoneInfo('Europe/Berlin')).strftime('%H:')
                output = interactive(executable, commands, dict(self.env, TZ='Pacific/Honolulu'))
                after = datetime.now(ZoneInfo('Europe/Berlin')).strftime('%H:')
                self.assertRegex(output, r'\d{2}:\d{2}:\d{2} .*lab')
                self.assertTrue(before in output or after in output)
                self.assertIn('· exit 1.1.1.1', re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]', '', output))
                self.assertIn('· exit 7', output)
                self.assertRegex((self.home / 'prompt-definition').read_text(), r'^\d{2}:\d{2}:\d{2} ')
                self.assertNotIn('1.1.1.1', output.rsplit('AFTER_IP_REMOVED', 1)[-1])
                callbacks = (self.home / 'prompt-callbacks').read_text().splitlines()
                self.assertEqual(callbacks.count('_pomdock_prompt_precmd'), 1)
