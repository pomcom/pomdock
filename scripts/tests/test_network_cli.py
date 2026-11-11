"""Network mode selection and input errors, with every external backend mocked."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]


class NetworkCLIContracts(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix='pomdock-network-cli-')
        self.addCleanup(self.folder.cleanup)
        self.base = Path(self.folder.name)
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        self.log = self.base / 'calls.jsonl'
        mock = self.base / 'mock-backend'
        mock.write_text('''#!/usr/bin/env python3
import json, os, pathlib, sys
name, args = pathlib.Path(sys.argv[0]).name, sys.argv[1:]
with open(os.environ['POMDOCK_NETWORK_CALLS'], 'a') as log:
    log.write(json.dumps({'tool':name, 'args':args})+'\\n')
if name == 'docker':
    if args[0] == 'logs':
        print('Public IP address is 192.0.2.2\\nBootstrapped 100%')
    elif args[0] == 'inspect':
        print('true')
    elif args[0] == 'run':
        print('synthetic-container')
    elif args[0] == 'exec' and '-i' in args:
        sys.stdin.buffer.read()
elif name == 'curl':
    print('{"ip":"192.0.2.1","organization":"fixture"}')
elif name == 'virsh':
    if 'domiflist' in args:
        print('vnet0 network default virtio 52:54:00:12:34:56')
    elif 'net-dhcp-leases' in args:
        print('2026-11-06 12:00:00 52:54:00:12:34:56 ipv4 192.0.2.3/24 fixture -')
    elif 'domstate' in args:
        print('running')
elif name == 'ssh':
    sys.stdin.buffer.read()
elif name == 'sleep':
    sys.exit(99)
''')
        mock.chmod(0o755)
        for command in ('docker', 'curl', 'virsh', 'ssh', 'sleep'):
            (self.bin / command).symlink_to(mock)
        self.profile = self.base / 'synthetic.ovpn'
        self.profile.write_text('# synthetic test fixture, never used by OpenVPN\n')
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ['PATH'],
                        HOME=str(self.base), POMDOCK_NETWORK_CALLS=str(self.log))

    def run_network(self, *args):
        self.log.unlink(missing_ok=True)
        return subprocess.run(['bash', str(ROOT / 'test-network.sh'), *args],
                              env=self.env, capture_output=True, text=True, timeout=15)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_invalid_or_missing_inputs_fail_before_any_backend(self):
        for args in (
                ('--mode', 'unknown'), ('--mode',), ('--mode', '--vpn'),
                ('--vpn',), ('--vpn', '--mode', 'direct'), ('--vpn', ''),
                ('--vm',), ('--vm', '--mode', 'direct'), ('--vm', ''),
                ('--mode', 'vpn'), ('--mode', 'tor-vpn'), ('--mode', 'stack'),
                ('--mode', 'vm'), ('--mode', 'vm-whonix'), ('--vm-whonix',),
                ('--vpn', str(self.base / 'missing.ovpn')), ('--unexpected',), ('extra',)):
            with self.subTest(args=args):
                result = self.run_network(*args)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertIn('[!]', result.stderr)
                self.assertEqual(self.calls(), [], 'invalid input touched an external backend')

    def test_canonical_routes_and_legacy_aliases_run_the_requested_checks(self):
        for mode, expected, tor, dns in (
                ('direct', 'direct', 'false', 'false'), ('plain', 'direct', 'false', 'false'),
                ('vpn', 'vpn', 'false', 'true'), ('tor', 'tor', 'true', 'true'),
                ('whonix', 'tor', 'true', 'true'), ('tor-vpn', 'tor-vpn', 'true', 'true'),
                ('stack', 'tor-vpn', 'true', 'true')):
            with self.subTest(mode=mode):
                args = ['--mode', mode]
                if expected in ('vpn', 'tor-vpn'):
                    args += ['--vpn', str(self.profile)]
                result = self.run_network(*args)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                checks = [call['args'] for call in self.calls() if call['tool'] == 'docker'
                          and call['args'][0] == 'run' and '/tmp/netcheck.sh' in call['args']]
                self.assertEqual(len(checks), 1, self.calls())
                self.assertTrue(checks[0][-3].startswith(expected + ' —'), checks)
                self.assertEqual(checks[0][-2:], [tor, dns])
                resolvers = [checks[0][i + 1] for i, arg in enumerate(checks[0]) if arg == '--dns']
                if expected == 'direct':
                    self.assertEqual(resolvers, ['1.1.1.1', '1.0.0.1'])
                    self.assertNotIn('--network', checks[0])
                else:
                    self.assertEqual(resolvers, [], 'shared container networks must inherit sidecar DNS')
                    self.assertTrue(checks[0][checks[0].index('--network') + 1].startswith('container:'))
                self.assertFalse(any(call['tool'] == 'sleep' for call in self.calls()))

    def test_vm_only_modes_use_existing_guest_without_docker_actions(self):
        for mode in ('vm', 'vm-whonix'):
            with self.subTest(mode=mode):
                result = self.run_network('--mode', mode, '--vm', 'lab-fixture')
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                calls = self.calls()
                self.assertFalse(any(call['tool'] == 'docker' for call in calls), calls)
                ssh = [call for call in calls if call['tool'] == 'ssh']
                self.assertEqual(len(ssh), 1, calls)
                self.assertIn('kali@192.0.2.3', ssh[0]['args'])
                self.assertFalse(any('start' in call['args'] or 'attach-interface' in call['args']
                                     for call in calls if call['tool'] == 'virsh'), calls)

    def test_help_and_default_mode_are_consistent(self):
        result = self.run_network('--help')
        self.assertEqual(result.returncode, 0, result.stderr)
        for route in ('--mode direct', '--mode vpn', '--mode tor', '--mode tor-vpn'):
            self.assertIn(route, result.stdout)
        self.assertIn('Historical aliases', result.stdout)
        self.assertEqual(self.calls(), [])
        result = self.run_network()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        checks = [call for call in self.calls() if call['tool'] == 'docker'
                  and '/tmp/netcheck.sh' in call['args']]
        self.assertEqual(len(checks), 1)
        self.assertTrue(checks[0]['args'][-3].startswith('direct —'))

    def test_build_fixture_uses_the_same_direct_resolvers(self):
        result = subprocess.run(['bash', str(ROOT / 'test-build.sh'), '--no-build'],
                                env=self.env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        runs = [call['args'] for call in self.calls()
                if call['tool'] == 'docker' and call['args'][0] == 'run']
        self.assertEqual(len(runs), 1, self.calls())
        resolvers = [runs[0][i + 1] for i, arg in enumerate(runs[0]) if arg == '--dns']
        self.assertEqual(resolvers, ['1.1.1.1', '1.0.0.1'])
        self.assertNotIn('--network', runs[0])


if __name__ == '__main__':
    unittest.main()
