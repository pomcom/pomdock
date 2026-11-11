"""Exercise the real lifecycle backend with an offline Docker state machine."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
DOCKER = r'''#!/usr/bin/env python3
import json, os, subprocess, sys
from pathlib import Path
p=Path(os.environ['FIXTURE_STATE']); s=json.loads(p.read_text()); a=sys.argv[1:]
with Path(os.environ['FIXTURE_CALLS']).open('a') as f: f.write(json.dumps(a)+'\n')
def save(): p.write_text(json.dumps(s))
def fail(): sys.exit(1)
def obj(name):
 c=s.get(name)
 if c is None: fail()
 return {'Id':name+'-id','Name':'/'+name,'Image':'sha256:fixture',
 'Config':{'Image':'fixture-kali','Hostname':'eng','Labels':c.get('labels',{})},
 'State':{'Running':c['running']},'HostConfig':{'NetworkMode':c.get('net','bridge')},'Mounts':c.get('mounts',[])}
if a[0]=='info': pass
elif a[:2]==['container','inspect']: print(json.dumps([obj(a[2])]))
elif a[:2]==['image','inspect']:
 print(json.dumps([{'Id':'sha256:fixture','Config':{'Labels':{
 'io.pomdock.recording-format':'3','io.pomdock.toolset':'minimal',
 'org.opencontainers.image.revision':'fixture-revision'}}}]))
elif a[0]=='inspect':
 name=a[-1]; c=obj(name)
 if len(a)==2: print(json.dumps([c]))
 else:
  fmt=a[2]
  if '.Config.Labels' in fmt:
   key=fmt.split('"')[1]; print(c['Config']['Labels'].get(key,'<no value>'))
  elif '.Mounts' in fmt:
   target=fmt.split('"')[1]
   print(next((m['Source'] for m in c['Mounts'] if m['Destination']==target),''))
  elif '.State.Health.Status' in fmt: print('healthy')
  elif '.State.Running' in fmt: print(str(c['State']['Running']).lower())
  elif '.HostConfig.NetworkMode' in fmt: print(c['HostConfig']['NetworkMode'])
  elif '.Config.Hostname' in fmt: print(c['Config']['Hostname'])
  elif '.Name' in fmt: print(c['Name'])
  else: fail()
elif a[0]=='run':
 name=a[a.index('--name')+1]; labels={}; mounts=[]
 for i,v in enumerate(a):
  if v=='--label': k,v=a[i+1].split('=',1); labels[k]=v
  if v=='-v': source,dest,*_=a[i+1].split(':'); mounts.append({'Source':source,'Destination':dest})
 s[name]={'running':True,'labels':labels,'net':a[a.index('--network')+1] if '--network' in a else 'bridge','mounts':mounts}
 save(); print(name+'-id')
elif a[0]=='rm':
 name=a[-1]
 if os.environ.get('FIXTURE_FAIL_REMOVE')==name: fail()
 if name not in s: fail()
 del s[name]; save()
elif a[0] in ('start','stop'):
 name=a[-1]; obj(name); s[name]['running']=a[0]=='start'; save(); print(name)
elif a[0]=='logs': print('Bootstrapped 100% (done): Done\nPublic IP address is 192.0.2.123')
elif a[0]=='port': print('127.0.0.1:8888')
elif a[0]=='exec':
 if os.environ.get('FIXTURE_FAIL_EXEC'): fail()
 if os.environ.get('FIXTURE_CONTAINER_HOME') and a[2:4]==['bash','-c']:
  env=dict(os.environ,HOME=os.environ['FIXTURE_CONTAINER_HOME'])
  sys.exit(subprocess.run(a[2:],env=env).returncode)
else: fail()
'''


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.loot = self.root / 'custom loot'
        self.loot.mkdir()
        (self.loot / 'keep.txt').write_text('original data')
        self.vpn = self.root / 'synthetic.ovpn'
        self.vpn.write_text('client\n')
        self.state = self.root / 'state.json'
        self.calls = self.root / 'calls.jsonl'
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        docker = self.bin / 'docker'
        docker.write_text(DOCKER)
        docker.chmod(0o700)
        self.env = {**os.environ, 'HOME': str(self.root), 'XDG_CONFIG_HOME': str(self.root/'config'),
                    'PATH': str(self.bin)+os.pathsep+os.environ['PATH'],
                    'FIXTURE_STATE': str(self.state), 'FIXTURE_CALLS': str(self.calls),
                    'PENTEST_DOTFILES_DIR': str(self.root/'dotfiles'), 'DISPLAY': '', 'TMUX': ''}
        self.env.pop('PENTEST_LOOT_DIR', None)
        self.env.pop('POMDOCK_WORKSPACES_DIR', None)
        self.env.pop('PENTEST_IMAGE', None)
        self.set_state('direct')

    def set_state(self, route, running=True):
        labels = {'io.pomdock.role': 'pentest', 'io.pomdock.profile': 'kali',
                  'io.pomdock.route': route, 'io.pomdock.loot-dir': str(self.loot)}
        if 'vpn' in route:
            labels['io.pomdock.vpn-file'] = str(self.vpn)
        containers = {'eng': {'running': running, 'labels': labels}}
        if route != 'direct':
            containers['eng']['net'] = 'container:eng-'+('whonix' if 'tor' in route else 'gluetun')
        for keyword, sidecar in [('vpn', 'gluetun'), ('tor', 'whonix')]:
            if keyword in route:
                containers['eng-'+sidecar] = {'running': running, 'labels': {}}
        self.state.write_text(json.dumps(containers))
        self.calls.write_text('')

    def run_backend(self, *args, ok=True):
        p = subprocess.run(['bash', str(ROOT/'pentest.sh'), '--name', 'eng', *args],
                           env=self.env, text=True, capture_output=True, timeout=15)
        if ok:
            self.assertEqual(p.returncode, 0, p.stdout+p.stderr)
        else:
            self.assertNotEqual(p.returncode, 0, p.stdout+p.stderr)
        return p

    def commands(self):
        return [json.loads(line) for line in self.calls.read_text().splitlines()]

    def test_running_route_change_is_explicit_failure(self):
        for options in [('--whonix',), ('--route', 'tor'), ('--vpn', str(self.vpn))]:
            with self.subTest(options=options):
                self.set_state('direct')
                p = self.run_backend(*options, 'exec', ok=False)
                self.assertIn('Stop it before changing route', p.stdout)
                self.assertFalse(any(c[0] in ('rm','stop','exec','run') for c in self.commands()))

    def test_existing_route_reused_without_profile_file(self):
        self.set_state('vpn')
        self.vpn.unlink()
        p = self.run_backend('start')
        self.assertIn('route: vpn', p.stdout)
        self.assertEqual(json.loads(self.state.read_text())['eng']['labels']['io.pomdock.route'], 'vpn')

    def dotfiles_home(self):
        repository = Path(self.env['PENTEST_DOTFILES_DIR'])
        repository.mkdir()
        (repository / 'keep.txt').write_text('original dotfiles')
        home = self.root / 'container home'
        home.mkdir()
        (home / 'dotfiles').symlink_to(repository, target_is_directory=True)
        self.env['FIXTURE_CONTAINER_HOME'] = str(home)
        self.state.write_text('{}')
        return home, repository

    def test_repeated_start_keeps_dotfiles_alias_out_of_mounted_repository(self):
        home, repository = self.dotfiles_home()
        self.run_backend('start')
        self.assertEqual((home / 'pcm.dot').readlink(), home / 'dotfiles')
        self.run_backend('stop')
        self.run_backend('start')
        self.assertEqual((home / 'pcm.dot').readlink(), home / 'dotfiles')
        self.assertEqual(sorted(path.name for path in repository.iterdir()), ['keep.txt'])
        self.assertEqual((repository / 'keep.txt').read_text(), 'original dotfiles')

    def test_dotfiles_alias_preserves_an_existing_real_directory(self):
        home, repository = self.dotfiles_home()
        existing = home / 'pcm.dot'
        existing.mkdir()
        (existing / 'keep.txt').write_text('existing user directory')
        self.run_backend('start')
        self.assertFalse(existing.is_symlink())
        self.assertEqual(sorted(path.name for path in existing.iterdir()), ['keep.txt'])
        self.assertEqual((existing / 'keep.txt').read_text(), 'existing user directory')
        self.assertEqual(sorted(path.name for path in repository.iterdir()), ['keep.txt'])

    def test_running_profile_change_rejected(self):
        self.set_state('vpn')
        other = self.root/'other.ovpn'
        other.write_text('client\n')
        self.run_backend('--vpn', str(other), 'start', ok=False)
        self.assertFalse(any(c[0] in ('rm','stop','run') for c in self.commands()))

    def test_stopped_profile_change_replaces_running_sidecars(self):
        for route in ('vpn', 'tor-vpn'):
            with self.subTest(route=route):
                self.set_state(route, running=False)
                state = json.loads(self.state.read_text())
                for name, container in state.items():
                    if name != 'eng': container['running'] = True
                self.state.write_text(json.dumps(state))
                other = self.root/'other.ovpn'
                other.write_text('client\n')
                self.run_backend('--vpn',str(other),'start')
                calls = self.commands()
                self.assertIn(['rm','-f','eng-gluetun'], calls)
                if 'tor' in route:
                    self.assertIn(['rm','-f','eng-whonix'], calls)
                self.assertEqual(json.loads(self.state.read_text())['eng']['labels']['io.pomdock.vpn-file'], str(other))

    def test_failed_runtime_removal_never_reports_start_success(self):
        self.set_state('vpn', running=False)
        self.env['FIXTURE_FAIL_REMOVE'] = 'eng'
        self.run_backend('start', ok=False)
        self.assertFalse(any(c[0] in ('start','run') for c in self.commands()))

    def test_replaced_vpn_also_rebinds_running_tor_gateway(self):
        for missing in (False, True):
            with self.subTest(missing=missing):
                self.set_state('tor-vpn', running=False)
                state = json.loads(self.state.read_text())
                state['eng-whonix']['running'] = True
                if missing: del state['eng-gluetun']
                self.state.write_text(json.dumps(state))
                self.run_backend('start')
                calls = self.commands()
                remove_index = calls.index(['rm','-f','eng-whonix'])
                gateway_runs = [i for i,c in enumerate(calls) if c[0]=='run' and 'eng-whonix' in c]
                self.assertEqual(len(gateway_runs), 1)
                self.assertLess(remove_index, gateway_runs[0])

    def test_stopped_route_transition_matrix_preserves_binding(self):
        for previous in ('direct','vpn','tor','tor-vpn'):
            for requested in ('direct','vpn','tor','tor-vpn'):
                if previous == requested:
                    continue
                with self.subTest(previous=previous, requested=requested):
                    self.set_state(previous, running=False)
                    args = ['--route', requested]
                    if 'vpn' in requested: args += ['--vpn', str(self.vpn)]
                    self.run_backend(*args, 'start')
                    current = json.loads(self.state.read_text())['eng']
                    self.assertEqual(current['labels']['io.pomdock.route'], requested)
                    self.assertEqual(current['labels']['io.pomdock.loot-dir'], str(self.loot))
                    self.assertEqual((self.loot/'keep.txt').read_text(), 'original data')
                    manifest = json.loads((self.loot/'.pomdock/engagement.json').read_text())
                    self.assertEqual(manifest['image']['id'], 'sha256:fixture')

    def test_stop_and_remove_cover_stack_keep_files(self):
        self.set_state('tor-vpn')
        self.run_backend('stop')
        state = json.loads(self.state.read_text())
        self.assertEqual(set(state), {'eng'})
        self.assertFalse(state['eng']['running'])
        self.run_backend('rm')
        self.assertEqual(json.loads(self.state.read_text()), {})
        self.assertTrue((self.loot/'keep.txt').exists())
        events = [json.loads(line) for line in (self.loot/'.pomdock/events.jsonl').read_text().splitlines()]
        self.assertEqual([e['event'] for e in events], ['stop','remove'])

    def test_equivalent_legacy_workspace_alias_reuses_existing_binding(self):
        alias = self.root/'workspace-alias'
        alias.symlink_to(self.loot, target_is_directory=True)
        self.env['PENTEST_LOOT_DIR'] = str(alias)
        self.run_backend('stop')
        self.assertTrue((self.loot/'keep.txt').exists())

    def test_different_loot_override_rejected_before_mutation(self):
        self.env['PENTEST_LOOT_DIR'] = str(self.root/'wrong')
        self.run_backend('stop', ok=False)
        self.assertFalse(any(c[0] in ('rm','stop','run') for c in self.commands()))

    def test_invalid_flags_and_help(self):
        for args in [('stop','--route','tor'), ('start','--fresh'), ('start','--purge'), ('rm','--yes')]:
            self.run_backend(*args, ok=False)
        self.run_backend('--help')

    def test_explicit_purge_only_removes_recorded_binding(self):
        self.run_backend('--purge','--yes','rm')
        self.assertFalse(self.loot.exists())

    def test_new_instances_use_canonical_workspace_root_including_default(self):
        for name in (None, 'eng'):
            with self.subTest(name=name):
                self.state.write_text('{}')
                self.calls.write_text('')
                args = ['--name', name] if name else []
                result = subprocess.run(['bash', str(ROOT/'pentest.sh'), *args, 'start'], env=self.env,
                                        text=True, capture_output=True, timeout=15)
                self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
                instance = name or 'pcm-pentest'
                workspace = self.root/'pomdock/workspaces'/instance
                container = json.loads(self.state.read_text())[instance]
                self.assertIn({'Source':str(workspace),'Destination':'/home/kali/workspace'}, container['mounts'])
                self.assertEqual(container['labels']['io.pomdock.workspace-dir'], str(workspace))
                run = next(call for call in self.commands() if call[0]=='run' and instance in call)
                self.assertIn('POMDOCK_SESSION_DIR=/home/kali/workspace/sessions', run)
                self.assertFalse((self.root/'pentest').exists())

    def test_canonical_root_override_and_exact_legacy_override(self):
        canonical = self.root/'alternative workspaces'
        self.env['POMDOCK_WORKSPACES_DIR'] = str(canonical)
        for legacy in (None, self.root/'exact custom binding'):
            with self.subTest(legacy=legacy):
                self.env['XDG_CONFIG_HOME'] = str(self.root/('legacy-config' if legacy else 'canonical-config'))
                self.state.write_text('{}'); self.calls.write_text('')
                if legacy: self.env['PENTEST_LOOT_DIR'] = str(legacy)
                self.run_backend('start')
                workspace = legacy or canonical/'eng'
                container = json.loads(self.state.read_text())['eng']
                self.assertIn({'Source':str(workspace),'Destination':'/home/kali/workspace'}, container['mounts'])
                self.assertEqual(container['labels']['io.pomdock.loot-dir'], str(workspace))

    def test_canonical_root_expands_home_and_normalizes_trailing_slash(self):
        self.state.write_text('{}')
        self.env['POMDOCK_WORKSPACES_DIR'] = '~/alternate-workspaces/'
        self.run_backend('start')
        workspace = self.root/'alternate-workspaces/eng'
        container = json.loads(self.state.read_text())['eng']
        self.assertEqual(container['labels']['io.pomdock.workspace-dir'], str(workspace))
        self.assertIn({'Source':str(workspace),'Destination':'/home/kali/workspace'}, container['mounts'])

    def test_managed_attach_prefers_workspace_but_accepts_legacy_images(self):
        self.run_backend('exec')
        attach = next(c for c in self.commands() if c[0]=='exec' and '-it' in c)
        self.assertIn('cd /home/kali/workspace', attach[-1])
        self.assertIn('|| cd /home/kali/pentest', attach[-1])
        self.assertIn('POMDOCK_ENGAGEMENT=eng', attach)
        self.assertFalse(any(c[0] in ('run','rm','stop') for c in self.commands()))

    def test_actual_workspace_mount_wins_over_stale_labels(self):
        state = json.loads(self.state.read_text())
        state['eng']['labels']['io.pomdock.loot-dir'] = str(self.root/'stale')
        state['eng']['mounts'] = [{'Source':str(self.loot),'Destination':'/home/kali/workspace'}]
        self.state.write_text(json.dumps(state))
        self.run_backend('stop')
        self.assertTrue((self.loot/'.pomdock/engagement.json').exists())
        self.assertFalse((self.root/'stale').exists())


if __name__ == '__main__':
    unittest.main()
