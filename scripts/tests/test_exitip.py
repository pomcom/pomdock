"""Observed HTTPS exit tests never invoke real curl or external network traffic."""
from contextlib import redirect_stdout
from importlib.machinery import SourceFileLoader
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
HELPER = ROOT / 'scripts/pomdock-exitip'
SPEC = importlib.util.spec_from_loader('pomdock_exitip', SourceFileLoader('pomdock_exitip', str(HELPER)))
EXIT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EXIT)


class ExitIPUnitTests(unittest.TestCase):
    def test_only_global_ipv4_unicast_is_accepted(self):
        for value in ('127.0.0.1', '10.8.0.1', '100.64.1.1', '192.0.2.1', '224.0.0.1', '::1', 'not-an-ip', None):
            self.assertIsNone(EXIT.global_ipv4(value), value)
        self.assertEqual(EXIT.global_ipv4('8.8.4.4'), '8.8.4.4')

    def test_signature_changes_for_route_interfaces_and_namespace(self):
        route = [{'dev': 'eth0', 'gateway': '192.0.2.1', 'cache': [{'expires': 1}]}]
        interfaces = [(2, 'eth0', 1, '192.0.2.3')]
        before = EXIT.state_signature(route, interfaces, 'net:[1]')
        # Dynamic route-cache counters do not trigger request storms.
        route[0]['cache'] = [{'expires': 0}]
        self.assertEqual(before, EXIT.state_signature(route, interfaces, 'net:[1]'))
        for new_route, new_interfaces, namespace in (
            ([{'dev': 'tun0'}], interfaces, 'net:[1]'),
            (route, [(2, 'eth0', 1, '192.0.2.4')], 'net:[1]'),
            (route, interfaces, 'net:[2]'),
        ):
            self.assertNotEqual(before, EXIT.state_signature(new_route, new_interfaces, namespace))

    def test_expired_future_route_mismatch_and_optout_are_suppressed(self):
        value = dict(signature='route', checked_at=100, ip='8.8.4.4')
        self.assertTrue(EXIT.fresh(value, 'route', 129.9))
        for signature, now in (('route', 130), ('route', 99), ('changed', 110)):
            self.assertFalse(EXIT.fresh(value, signature, now))
        with patch.dict(os.environ, {'POMDOCK_EXITIP': '0'}), patch.object(EXIT, 'network_signature') as local:
            with redirect_stdout(io.StringIO()) as output:
                EXIT.main(['show'])
            local.assert_not_called()
            self.assertEqual(output.getvalue(), '')

    def test_uid_home_cache_isolation_and_validation(self):
        with tempfile.TemporaryDirectory() as folder, patch.dict(os.environ, {'XDG_CACHE_HOME': folder}):
            uid, home = os.geteuid(), str(Path(folder) / 'home')
            directory = EXIT.cache_directory(uid, home)
            EXIT.write_cache(directory, uid, home, 'route', '8.8.4.4')
            self.assertIsNotNone(EXIT.read_cache(directory, uid, home))
            self.assertIsNone(EXIT.read_cache(directory, uid + 1, home))
            self.assertIsNone(EXIT.read_cache(directory, uid, home + '-other'))
            other = EXIT.cache_directory(uid, home + '-other')
            self.assertNotEqual(directory, other)
            document = json.loads((directory / 'state.json').read_text())
            document['ip'] = '192.168.1.1'
            (directory / 'state.json').write_text(json.dumps(document))
            self.assertIsNone(EXIT.read_cache(directory, uid, home))
            self.assertEqual((directory / 'state.json').stat().st_mode & 0o777, 0o600)

    def test_curl_contract_bypasses_proxies_and_has_one_bounded_https_endpoint(self):
        process = subprocess.CompletedProcess([], 0, 'ip=8.8.4.4\n', '')
        with patch.object(EXIT.subprocess, 'run', return_value=process) as run:
            self.assertEqual(EXIT.observe_exit(), '8.8.4.4')
        args = run.call_args.args[0]
        self.assertEqual(args[:2], ['curl', '-q'])
        self.assertEqual(args[-1], 'https://1.1.1.1/cdn-cgi/trace')
        self.assertEqual(args[args.index('--noproxy') + 1], '*')
        self.assertIn('--ipv4', args)
        self.assertEqual(args[args.index('--max-time') + 1], '3')
        self.assertEqual(run.call_args.kwargs['timeout'], EXIT.CURL_TIMEOUT)
        with patch.object(EXIT.subprocess, 'run', side_effect=subprocess.TimeoutExpired('curl', 3.5)):
            self.assertIsNone(EXIT.observe_exit())


class ExitIPProcessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='pomdock-exitip-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.home = self.root / 'home'; self.home.mkdir()
        bindir = self.root / 'bin'; bindir.mkdir()
        self.route = self.root / 'route.json'
        self.route.write_text('[{"dev":"eth0","gateway":"192.0.2.1"}]')
        self.answer = self.root / 'answer'
        self.answer.write_text('ip=8.8.4.4\n')
        self.calls = self.root / 'curl-calls'
        self.delay = self.root / 'delay'; self.delay.write_text('0')
        ip = bindir / 'ip'
        ip.write_text(f'#!{sys.executable}\nimport os,pathlib,sys,time\ntime.sleep(float(os.environ.get("FIXTURE_IP_DELAY","0")))\nprint(pathlib.Path(os.environ["FIXTURE_ROUTE"]).read_text())\n')
        curl = bindir / 'curl'
        curl.write_text(f'''#!{sys.executable}
import os,pathlib,sys,time
with pathlib.Path(os.environ['FIXTURE_CALLS']).open('a') as stream: stream.write('curl\\n')
time.sleep(float(pathlib.Path(os.environ['FIXTURE_DELAY']).read_text()))
sys.stdout.write(pathlib.Path(os.environ['FIXTURE_ANSWER']).read_text())
''')
        ip.chmod(0o755); curl.chmod(0o755)
        self.env = dict(os.environ, HOME=str(self.home), XDG_CACHE_HOME=str(self.root / 'cache'),
                        PATH=str(bindir) + os.pathsep + os.environ['PATH'], POMDOCK_EXITIP='1',
                        FIXTURE_ROUTE=str(self.route), FIXTURE_ANSWER=str(self.answer),
                        FIXTURE_CALLS=str(self.calls), FIXTURE_DELAY=str(self.delay))

    def call(self, action='show'):
        start = time.monotonic()
        result = subprocess.run([sys.executable, str(HELPER), action], env=self.env,
                                text=True, capture_output=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, '')
        return result.stdout.strip(), time.monotonic() - start

    def document(self):
        files = list((self.root / 'cache').rglob('state.json')) if (self.root / 'cache').exists() else []
        return json.loads(files[0].read_text()) if files else None

    def wait_for(self, check):
        end = time.monotonic() + 5
        while time.monotonic() < end:
            if check(): return
            time.sleep(.025)
        self.fail('Background refresh did not complete')

    def test_cold_show_returns_before_slow_lookup_then_reuses_fresh_cache(self):
        self.delay.write_text('.7')
        value, elapsed = self.call('--show')
        self.assertEqual(value, '')
        self.assertLess(elapsed, .35)
        self.wait_for(lambda: self.document() is not None)
        self.assertEqual(self.call()[0], '8.8.4.4')
        self.assertEqual(self.calls.read_text().splitlines(), ['curl'])

    def test_expired_failed_cache_never_prints_old_ip_or_retries_on_every_prompt(self):
        self.call('refresh')
        path = next((self.root / 'cache').rglob('state.json'))
        document = json.loads(path.read_text()); document['checked_at'] = time.time() - 31
        path.write_text(json.dumps(document))
        self.answer.write_text('bad response\n')
        self.assertEqual(self.call()[0], '')
        self.wait_for(lambda: self.document()['ip'] is None)
        for _ in range(3): self.assertEqual(self.call()[0], '')
        self.assertEqual(self.calls.read_text().splitlines(), ['curl', 'curl'])

    def test_route_switch_hides_cached_ip_and_discards_inflight_old_route_result(self):
        self.delay.write_text('.4')
        self.assertEqual(self.call()[0], '')
        self.wait_for(lambda: self.calls.exists())
        self.route.write_text('[{"dev":"tun0","prefsrc":"198.51.100.9"}]')
        self.wait_for(lambda: self.document() is not None)
        self.assertIsNone(self.document()['ip'])
        self.answer.write_text('ip=9.9.9.9\n')
        self.assertEqual(self.call()[0], '')
        self.wait_for(lambda: self.document()['ip'] == '9.9.9.9')
        self.assertEqual(self.call()[0], '9.9.9.9')

    def test_parallel_prompts_share_single_refresh_lock(self):
        self.delay.write_text('.5')
        processes = [subprocess.Popen([sys.executable, str(HELPER), 'show'], env=self.env,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) for _ in range(8)]
        for process in processes:
            stdout, stderr = process.communicate(timeout=3)
            self.assertEqual(process.returncode, 0)
            self.assertIn(stdout, ('', '8.8.4.4\n'))
            self.assertEqual(stderr, '')
        self.wait_for(lambda: self.document() is not None)
        self.assertEqual(self.calls.read_text().splitlines(), ['curl'])

    def test_slow_local_route_query_is_bounded_and_does_not_launch_lookup(self):
        self.env['FIXTURE_IP_DELAY'] = '1'
        value, elapsed = self.call()
        self.assertEqual(value, '')
        self.assertLess(elapsed, .35)
        self.assertFalse(self.calls.exists())
