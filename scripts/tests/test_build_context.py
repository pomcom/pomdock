"""Image staging contract; no Docker daemon or external downloads are needed."""
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class BuildContextTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'checkout'
        (self.root / 'scripts/recording').mkdir(parents=True)
        (self.root / 'scripts/prompt').mkdir()
        for name in ('build-image.sh', 'stage-atuin.sh', 'update-runtime-image.sh'):
            shutil.copy(ROOT / 'scripts' / name, self.root / 'scripts' / name)
        for name in ('Dockerfile', 'setup-pentest.sh'):
            (self.root / name).write_text(name)
        for name in ('pomdock-shell', 'pomdock-tmux', 'pomsession', 'pomdock-help', 'pomrdp', 'pomdock-netcontext', 'pomdock-exitip', 'workspace-layout.py'):
            (self.root / 'scripts' / name).write_text(name)
        for name in ('session-log.sh', 'cmdlog.bash', 'cmdlog.zsh'):
            (self.root / 'scripts/recording' / name).write_text(name)
        for name in ('prompt.bash', 'prompt.zsh', 'guard-dotfiles.py'):
            (self.root / 'scripts/prompt' / name).write_text(name)
        vendor = self.root / 'vendor/atuin'
        (vendor / 'crates/client').mkdir(parents=True)
        for name in ('Cargo.toml', 'Cargo.lock', 'README.md', 'LICENSE', 'CONTRIBUTORS',
                     'CHANGELOG.md', 'rust-toolchain.toml'):
            (vendor / name).write_text(name)
        (vendor / 'crates/client/source.rs').write_text('source')
        for name in ('vendor/atuin/target/private', 'vendor/atuin/crates/client/target/cache',
                     'cli/web/node_modules/private', 'kalico/private', '.git/private',
                     'cli/pomdock', 'loot/private', 'scripts/unrelated-secret'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('must not be staged')
        self.dotfiles = self.base / 'dotfiles'
        for name in ('setup-shell.sh', 'zsh/.zshrc', 'starship/.config/starship.toml',
                     'atuin/.config/atuin/config.toml', 'tmux/tmux.conf',
                     '.ssh/id_rsa', '.env', 'tools/private', 'atuin/bin/atuin',
                     'zsh/node_modules/private', 'zsh/secrets.pem', 'nvim/.git/private'):
            path = self.dotfiles / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(name)
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        docker = self.bin / 'docker'
        docker.write_text('''#!/usr/bin/env python3
import json, os, pathlib, shutil, sys
args = sys.argv[1:]
if args[:2] == ['image', 'inspect']:
    print('sha256:' + 'a' * 64)
    sys.exit(0)
if args == ['buildx', 'version']:
    sys.exit(1 if os.environ.get('FAKE_LEGACY') else 0)
pathlib.Path(os.environ['FAKE_LOG']).write_text(json.dumps({'args': args, 'buildkit': os.environ.get('DOCKER_BUILDKIT')}))
shutil.copytree(args[-1], os.environ['FAKE_CONTEXT'], symlinks=True)
''')
        docker.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                        FAKE_LOG=str(self.base / 'docker.json'),
                        FAKE_CONTEXT=str(self.base / 'context'))
        self.env.pop('POMDOCK_BUILD_NETWORK', None)

    def build(self, *args, dotfiles=None, legacy=False, umask=-1):
        env = dict(self.env)
        if legacy:
            env['FAKE_LEGACY'] = '1'
        result = subprocess.run(['bash', str(self.root / 'scripts/build-image.sh'), 'test:isolated',
                                 str(self.dotfiles if dotfiles is None else dotfiles), *args],
                                env=env, text=True, capture_output=True, umask=umask)
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads((self.base / 'docker.json').read_text()), self.base / 'context'

    def test_default_full_build_stages_only_inputs(self):
        invocation, context = self.build()
        self.assertEqual(invocation['args'][:2], ['buildx', 'build'])
        self.assertIn('TOOLSET=full', invocation['args'])
        self.assertIn('--load', invocation['args'])
        self.assertNotIn('--network', invocation['args'])
        self.assertIn('io.pomdock.recording-format=3', invocation['args'])
        self.assertTrue(any(a.startswith('org.opencontainers.image.created=') for a in invocation['args']))
        self.assertTrue((context / 'vendor/atuin/crates/client/source.rs').exists())
        self.assertTrue((context / '.pomdock-dotfiles/zsh/.zshrc').exists())
        self.assertTrue((context / '.pomdock-dotfiles/tmux/tmux.conf').exists())
        self.assertTrue((context / '.pomdock-dotfiles/atuin/.config/atuin/config.toml').exists())
        for forbidden in ('node_modules', 'target', 'kalico', '.git', 'loot', 'id_rsa',
                          '.env', 'private', 'secrets.pem'):
            self.assertFalse(any(p.name == forbidden for p in context.rglob('*')), forbidden)
        self.assertFalse((context / 'cli').exists())
        self.assertFalse((context / 'scripts/unrelated-secret').exists())
        self.assertFalse((context / '.pomdock-dotfiles/atuin/bin').exists())
        # Temporary context is deleted even after a successful build.
        self.assertFalse(Path(invocation['args'][-1]).exists())

    def test_minimal_fresh_legacy_build(self):
        invocation, _ = self.build('--toolset', 'minimal', '--fresh', legacy=True)
        self.assertEqual(invocation['args'][0], 'build')
        self.assertIn('TOOLSET=minimal', invocation['args'])
        self.assertIn('io.pomdock.toolset=minimal', invocation['args'])
        self.assertIn('--no-cache', invocation['args'])
        self.assertNotIn('--load', invocation['args'])
        self.assertEqual(invocation['buildkit'], '0')

    def test_minimal_fresh_buildx_build(self):
        invocation, _ = self.build('--toolset', 'minimal', '--fresh')
        self.assertIn('TOOLSET=minimal', invocation['args'])
        self.assertIn('--no-cache', invocation['args'])
        self.assertIn('--load', invocation['args'])

    def test_dotfiles_are_optional_and_never_created(self):
        missing = self.base / 'absent-dotfiles'
        _, context = self.build(dotfiles=missing)
        self.assertFalse(missing.exists())
        self.assertEqual(list((context / '.pomdock-dotfiles').iterdir()), [])

    def test_explicit_build_network_is_forwarded_to_both_builders(self):
        self.env['POMDOCK_BUILD_NETWORK'] = 'host'
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                invocation, context = self.build(legacy=legacy)
                arguments = invocation['args']
                self.assertEqual(arguments[arguments.index('--network') + 1], 'host')
                shutil.rmtree(context)

    def test_private_umask_staging_stays_readable_to_container_user(self):
        (self.root / 'setup-pentest.sh').chmod(0o755)
        _, context = self.build(umask=0o077)
        setup = context / 'setup-pentest.sh'
        recording = context / 'scripts/recording'
        self.assertEqual(setup.stat().st_mode & 0o777, 0o700)
        self.assertEqual((recording / 'session-log.sh').stat().st_mode & 0o777, 0o600)
        dockerfile = (ROOT / 'Dockerfile').read_text()
        root_setup = dockerfile.split('\nUSER $USERNAME', 1)[0]
        setup_fix = re.search(r'\bchmod\s+0644\s+/tmp/setup-pentest\.sh\b', root_setup)
        hooks_fix = re.search(r'\bchmod\s+-R\s+a\+rX\s+/usr/local/lib/pomdock/recording\b', root_setup)
        self.assertIsNotNone(setup_fix, 'Setup permissions must be normalized before USER Kali')
        self.assertIsNotNone(hooks_fix, 'Recording permissions must be normalized before USER Kali')
        self.assertLess(root_setup.index('COPY scripts/recording/'), hooks_fix.start())
        # Exercise the actual Dockerfile permission commands on the restrictive
        # context, mapping only their absolute image paths into this fixture.
        for match, image_path, staged_path in (
            (setup_fix, '/tmp/setup-pentest.sh', setup),
            (hooks_fix, '/usr/local/lib/pomdock/recording', recording),
        ):
            args = [str(staged_path) if argument == image_path else argument
                    for argument in shlex.split(match.group())]
            subprocess.run(args, check=True)
        self.assertEqual(setup.stat().st_mode & 0o777, 0o644)
        for path in recording.rglob('*'):
            self.assertEqual(path.stat().st_mode & 0o044, 0o044, str(path))
            if path.is_dir():
                self.assertEqual(path.stat().st_mode & 0o011, 0o011, str(path))

    def test_invalid_options_fail_before_docker(self):
        for flags in (['--toolset', 'tiny'], ['--toolset'], ['--no-such-option']):
            result = subprocess.run(['bash', str(self.root / 'scripts/build-image.sh'), 'test:isolated',
                                     str(self.dotfiles), *flags], env=self.env, capture_output=True)
            self.assertEqual(result.returncode, 2)
            self.assertFalse((self.base / 'docker.json').exists())

    def test_runtime_update_pins_local_base_and_stages_only_offline_assets(self):
        self.env['POMDOCK_BUILD_NETWORK'] = 'host'
        result = subprocess.run(['bash', str(self.root / 'scripts/update-runtime-image.sh'),
                                 'source:tag', 'test:runtime'], env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        invocation = json.loads((self.base / 'docker.json').read_text())['args']
        self.assertEqual(invocation[invocation.index('--network') + 1], 'none')
        context = self.base / 'context'
        dockerfile = (context / 'Dockerfile').read_text()
        self.assertTrue(dockerfile.startswith('FROM sha256:' + 'a' * 64 + '\n'))
        self.assertIn('USER kali\nWORKDIR /home/kali/workspace', dockerfile)
        self.assertIn('io.pomdock.container-tmux="1"', dockerfile)
        for name in ('prompt.bash', 'prompt.zsh', 'guard-dotfiles.py'):
            self.assertTrue((context / 'scripts/prompt' / name).is_file())
        self.assertTrue((context / 'scripts/pomdock-netcontext').is_file())
        self.assertTrue((context / 'scripts/pomdock-tmux').is_file())
        self.assertTrue((context / 'scripts/pomdock-exitip').is_file())
        self.assertTrue((context / 'scripts/workspace-layout.py').is_file())
        self.assertFalse((context / 'vendor').exists())
        self.assertNotIn('apt-get', dockerfile)
        self.assertNotIn('go install', dockerfile)


if __name__ == '__main__':
    unittest.main()
