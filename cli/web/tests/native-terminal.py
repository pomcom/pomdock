#!/usr/bin/env python3
"""Optional offline Docker/PTY smoke test. Uses a private tmux socket and removes
only its uniquely named synthetic container. Never attaches to a user workspace.
Build the web assets first; POMDOCK_TEST_BINARY can reuse a test build.
"""
import json, os, pathlib, pty, re, select, selectors, signal, shlex, shutil, sqlite3, subprocess, tempfile, time, urllib.request, uuid

def run(*args, env=None):
    return subprocess.check_output(args, text=True, env=env, stderr=subprocess.PIPE).strip()
def wait_for(check, label, timeout=12):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        drain_client()
        if check(): return
        time.sleep(.1)
    raise AssertionError(label)

def drain_client():
    if clientfd is None: return
    try:
        while select.select([clientfd],[],[],0)[0]:
            if not os.read(clientfd,65536): break
    except OSError: pass

def reap_client():
    if not client: return
    deadline=time.monotonic()+3
    while time.monotonic()<deadline:
        drain_client()
        try:
            if os.waitpid(client,os.WNOHANG)[0]: return
        except ChildProcessError: return
        time.sleep(.05)
    os.kill(client,signal.SIGKILL)
    os.waitpid(client,0)

name='pomdock-ui-fixture-'+uuid.uuid4().hex[:8]
server=None
client=None
clientfd=None
repo=pathlib.Path(__file__).resolve().parents[3]
with tempfile.TemporaryDirectory(prefix='pomdock-native-ui-') as folder:
    root=pathlib.Path(folder)
    bindir=root/'bin'; bindir.mkdir()
    docker=shutil.which('docker'); tmux=shutil.which('tmux'); socket=root/'tmux.sock'
    (bindir/'tmux').write_text('#!/bin/sh\nexec '+shlex.quote(tmux)+' -S '+shlex.quote(str(socket))+' -f /dev/null "$@"\n')
    (bindir/'docker').write_text('#!/bin/sh\nif [ "$1" = ps ]; then shift; exec '+shlex.quote(docker)+' ps --filter '+shlex.quote('name=^/'+name+'$')+' "$@"; fi\nexec '+shlex.quote(docker)+' "$@"\n')
    for path in bindir.iterdir(): path.chmod(0o700)
    env=dict(os.environ, HOME=str(root), XDG_CONFIG_HOME=str(root/'config'), POMDOCK_WORKSPACES_DIR=str(root/'workspaces'), PATH=str(bindir)+os.pathsep+os.environ['PATH'])
    env.pop('TMUX',None)
    env.pop('PENTEST_LOOT_DIR',None)
    binary=os.environ.get('POMDOCK_TEST_BINARY')
    if not binary:
        binary=str(root/'pomdock')
        subprocess.run(['go', 'build', '-o', binary, '.'], cwd=repo/'cli', check=True)
    def host_mux(*args): return run(tmux, '-S', str(socket), '-f', '/dev/null', *args, env=env)
    (bindir/'pomdock').symlink_to(pathlib.Path(binary).resolve())
    try:
        engagement=root/'workspaces'/name; engagement.mkdir(parents=True); (engagement/'.atuin').mkdir()
        run(docker,'run','-d','--name',name,'--network','none','-v',str(engagement)+':/home/kali/workspace','-v',str(engagement/'.atuin')+':/home/kali/.local/share/atuin','--label','io.pomdock.role=pentest','--label','io.pomdock.profile=kali','--label','io.pomdock.route=direct','-e','POMDOCK_EXITIP=0','-e','POMDOCK_KIND=container','-e','POMDOCK_ROUTE=direct','-e','POMDOCK_MACHINE='+name,os.environ.get('POMDOCK_TEST_IMAGE','pcm-kali-pentest'),'sleep','infinity')
        run(docker, 'exec', name, 'mkdir', '-p', '/home/kali/workspace/.atuin')
        # Install the current recorder into this uniquely named offline fixture;
        # the user's images and running containers are never modified.
        run(docker,'exec','-u','0',name,'mkdir','-p','/usr/local/lib/pomdock/recording')
        run(docker,'cp',str(repo/'scripts/recording')+'/.',name+':/usr/local/lib/pomdock/recording/')
        for helper in ['pomdock-shell', 'pomdock-tmux']:
            run(docker,'cp',str(repo/'scripts'/helper),name+':/usr/local/bin/'+helper)
            run(docker,'exec','-u','0',name,'chmod','0755','/usr/local/bin/'+helper)
        run(docker,'exec','-u','0',name,'sh','-c',"printf '\\n. /usr/local/lib/pomdock/recording/session-log.sh\\n. /usr/local/lib/pomdock/recording/cmdlog.zsh\\n' >> /etc/zsh/zshrc; printf '\\n. /usr/local/lib/pomdock/recording/session-log.sh\\n. /usr/local/lib/pomdock/recording/cmdlog.bash\\n' >> /etc/bash.bashrc")
        tokenfile=root/'token'
        server=subprocess.Popen([binary,'web','--no-open','--port','0','--token-file',str(tokenfile)],cwd=repo,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        reader=selectors.DefaultSelector(); reader.register(server.stdout,selectors.EVENT_READ)
        logs=b''; base=None; end=time.monotonic()+12
        while time.monotonic()<end:
            if reader.select(.2):
                logs+=server.stdout.read1(8192)
                match=re.search(rb'http://127\.0\.0\.1:[0-9]+',logs)
                if match: base=match[0].decode(); break
        assert base, 'Isolated test server did not start'
        token=tokenfile.read_text().strip()
        def api(path, method='GET'):
            request=urllib.request.Request(base+path, data=b'{}' if method=='POST' else None, method=method,headers={'Cookie':'pomdock_web='+token,'Origin':base,'X-Pomdock':'1','Content-Type':'application/json'})
            with urllib.request.urlopen(request,timeout=5) as response: return json.load(response)
        assert [c['name'] for c in api('/api/containers')['containers']]==[name]
        # A legacy host shell remains untouched. Reading the GUI must never
        # create/select a shell or migrate an existing PTY.
        legacy_command='exec docker exec -it -w /home/kali/workspace -e TERM=xterm-256color -e POMDOCK_ENGAGEMENT='+shlex.quote(name)+' '+shlex.quote(name)+' pomdock-shell'
        legacy=host_mux('new-session','-d','-P','-F','#{window_id}','-s','pomdock',legacy_command)
        host_mux('set-option','-w','-t',legacy,'@pomdock_shell_kind','docker')
        host_mux('set-option','-w','-t',legacy,'@pomdock_shell_target',name)
        original=host_mux('display-message','-p','-t',legacy,'#{pane_id}:#{pane_pid}')
        listed=api('/api/containers')['containers'][0]
        assert listed['connect']=="pomdock docker attach '"+name+"'"
        assert not any(sh['location']=='container' for sh in api('/api/shells')['shells'])
        assert run(docker,'exec','-u','kali','-e','POMDOCK_ENGAGEMENT='+name,name,'pomdock-tmux','list')==''
        # Execute the exact displayed connection command in a real terminal.
        client,clientfd=pty.fork()
        if client==0:
            os.execvpe('pomdock',shlex.split(listed['connect']),dict(env,TERM='xterm-256color'))
        def inside(): return [sh for sh in api('/api/shells')['shells'] if sh['location']=='container']
        wait_for(lambda: bool(inside()) and inside()[0]['clients']==1,'CLI did not attach to container tmux')
        shown=inside()[0]; window=shown['windowId']
        assert shown['id']=='docker:'+name+':'+window
        def mux(*args): return run(docker,'exec','-u','kali',name,'tmux','-L','pomdock',*args)
        assert host_mux('display-message','-p','-t',legacy,'#{pane_id}:#{pane_pid}')==original
        assert host_mux('display-message','-p','-t',legacy,'#{session_name}')=='pomdock'
        assert mux('show-option','-v','prefix')=='C-b'
        mux('resize-window','-t',window,'-x','120','-y','40')
        def text(): return mux('capture-pane','-p','-t',window)
        def send(command): mux('send-keys','-t',window,'-l',command); mux('send-keys','-t',window,'Enter')
        def marker(value): return value in text().splitlines()
        send("printf 'POMDOCK_UI_READY\\n'")
        wait_for(lambda: marker('POMDOCK_UI_READY'),'Recorded container terminal was not usable')
        send('sleep 20'); time.sleep(.25); mux('send-keys','-t',window,'C-c')
        send("printf 'POMDOCK_CTRL_C_OK\\n'")
        wait_for(lambda: marker('POMDOCK_CTRL_C_OK'),'Ctrl-C did not interrupt the foreground command',5)
        mux('send-keys','-t',window,'C-r'); time.sleep(1); mux('send-keys','-t',window,'Escape'); time.sleep(.25); mux('send-keys','-t',window,'C-c','C-u')
        send("printf 'POMDOCK_CTRL_R_OK\\n'")
        wait_for(lambda: marker('POMDOCK_CTRL_R_OK'),'History shortcut did not return to usable input',5)
        mux('send-keys','-t',window,'-l',"printf 'SHOULD_BE_COMMENTED\\n'"); mux('send-keys','-t',window,'C-a'); mux('send-keys','-t',window,'-l','# '); mux('send-keys','-t',window,'Enter')
        send("printf 'POMDOCK_CTRL_A_OK\\n'")
        wait_for(lambda: marker('POMDOCK_CTRL_A_OK'),'Line editing did not return usable input',5)
        assert not marker('SHOULD_BE_COMMENTED'),'Ctrl-A line editing did not reach beginning of command'
        mux('copy-mode','-t',window)
        wait_for(lambda: mux('display-message','-p','-t',window,'#{pane_in_mode}')=='1','tmux copy-mode shortcut failed')
        mux('send-keys','-t',window,'C-c')
        wait_for(lambda: mux('display-message','-p','-t',window,'#{pane_in_mode}')=='0','tmux copy mode did not cancel')
        mux('resize-window','-t',window,'-x','100','-y','32')
        expected=mux('display-message','-p','-t',window,'#{pane_height} #{pane_width}')
        send("printf 'PTY_SIZE:'; stty size")
        wait_for(lambda: marker('PTY_SIZE:'+expected),'PTY dimensions did not propagate into container')
        send("printf '\\033[31mPOMDOCK_ANSI_OK\\033[0m\\n'")
        wait_for(lambda: marker('POMDOCK_ANSI_OK'), 'ANSI output did not reach the terminal')
        assert '\x1b[31mPOMDOCK_ANSI_OK' in mux('capture-pane', '-ep', '-t', window), 'ANSI color was lost'
        assert api('/api/containers')['containers'][0]['direct']=="pomdock docker exec --name '"+name+"'"
        # New windows and splits belong to the same container and each record
        # their own shell, independently of any attached client's transcript.
        extra=mux('new-window','-d','-P','-F','#{window_id}','-t','=workspace:')
        split=mux('split-window','-d','-P','-F','#{pane_id}','-t',extra)
        for pane in [extra,split]:
            mux('send-keys','-t',pane,'-l',"printf 'POMDOCK_DEFAULT_SHELL_OK\\n'")
            mux('send-keys','-t',pane,'Enter')
            wait_for(lambda: 'POMDOCK_DEFAULT_SHELL_OK' in mux('capture-pane','-p','-t',pane).splitlines(),'Default pane did not enter recorded shell')
        assert len(inside())==2 and any(sh['panes']==2 for sh in inside())
        mux('select-window','-t',window)
        identity=mux('display-message','-p','-t',window,'#{pane_id}:#{pane_pid}')
        # Detaching and reattaching the client must preserve shell/process and
        # capture identity, without creating another window or recording.
        os.write(clientfd,b'\x02d')
        wait_for(lambda: inside()[0]['clients']==0,'Container detach did not release client')
        reap_client(); client=None
        os.close(clientfd); clientfd=None
        captures_before=set(engagement.glob('sessions/*.log'))
        client,clientfd=pty.fork()
        if client==0:
            os.execvpe('pomdock',shlex.split(shown['attach']),dict(env,TERM='xterm-256color'))
        wait_for(lambda: inside()[0]['clients']==1,'Window-specific connection command did not attach')
        assert mux('display-message','-p','-t',window,'#{pane_id}:#{pane_pid}')==identity
        assert set(engagement.glob('sessions/*.log'))==captures_before, 'Reattach started a duplicate capture'
        assert mux('display-message','-p','-t','=workspace:','#{window_id}')==window
        assert shown['switch']=='', 'Container connections should not expose host switch-client commands'
        send('cd /tmp')
        send("printf 'POMDOCK_INDEXED_OUTPUT\\n'")
        wait_for(lambda: marker('POMDOCK_INDEXED_OUTPUT'),'Indexed output did not reach terminal')
        send('bash'); time.sleep(.7)
        send("printf 'POMDOCK_BASH_INDEX_OK\\n'")
        wait_for(lambda: marker('POMDOCK_BASH_INDEX_OK'),'Nested bash hook did not leave usable input')
        send('exit 3'); time.sleep(.5)
        run(docker,'exec','-e','POMDOCK_ENGAGEMENT='+name,name,'tmux','-L','pomdock-fixture','-f','/dev/null','new-session','-d','-s','nested')
        run(docker,'exec',name,'tmux','-L','pomdock-fixture','send-keys','-t','nested:0.0',"printf 'POMDOCK_NESTED_PANE_OK\\n'",'Enter')
        wait_for(lambda: 'POMDOCK_NESTED_PANE_OK' in run(docker,'exec',name,'tmux','-L','pomdock-fixture','capture-pane','-p','-t','nested:0.0').splitlines(),'Nested tmux pane did not record')
        def timeline(): return api('/api/recordings/timeline?name='+name)
        wait_for(lambda: any(c['cmd']=="printf 'POMDOCK_INDEXED_OUTPUT\\n'" and c['exit']==0 for c in timeline()['commands']),'Structured command completion was not indexed')
        indexed=next(c for c in timeline()['commands'] if c['cmd']=="printf 'POMDOCK_INDEXED_OUTPUT\\n'")
        subprocess.run([docker,'exec','-i','-e','POMDOCK_ENGAGEMENT='+name,name,'script','-q','--return','--command','exec pomdock-shell','/dev/null'],input=b"printf 'POMDOCK_PIPE_SMOKE\\n'\nexit\n",stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,check=True,timeout=15)
        records=[json.loads(line) for line in (engagement/'sessions/commands.jsonl').read_text().splitlines()]
        assert any('POMDOCK_BASH_INDEX_OK' in r.get('cmd','') and r.get('shell')=='bash' for r in records),'Bash commands were not indexed'
        assert any('POMDOCK_NESTED_PANE_OK' in r.get('cmd','') and 'tmux-nested' in r.get('subid','') for r in records),'Nested tmux pane identity was lost'

        assert indexed['cwd']=='/tmp'
        changed=next(c for c in timeline()['commands'] if c['cmd']=='cd /tmp')
        assert changed['cwd']=='/home/kali/workspace','cd must retain the execution directory'
        hits=api('/api/recordings/search?name='+name+'&q=POMDOCK_INDEXED_OUTPUT')
        assert hits['groups'] and any(g['command'] and g['command']['id']==indexed['id'] for g in hits['groups'])
        entries=api('/api/session?name='+name+'&id='+indexed['sub'])['entries']
        assert any(e['id']==indexed['id'] and 'POMDOCK_INDEXED_OUTPUT' in e['output'] for e in entries)
        replay=api('/api/recordings/info?name='+name+'&shell='+indexed['sub'])
        assert replay['markers'] and replay['table']
        def command_completed(command):
            records=[]
            for line in (engagement/'sessions/commands.jsonl').read_text().splitlines():
                try: records.append(json.loads(line))
                except ValueError: pass
            ids={r['id'] for r in records if r.get('cmd')==command}
            return any(r.get('id') in ids and r.get('end') for r in records)
        for i in range(12):
            command="printf 'POMDOCK_FAST_%02d\\n'"%i
            send(command)
            # Wait for prompt readiness, so queued keyboard input is not
            # legitimately echoed into the previous command's terminal output.
            wait_for(lambda: command_completed(command),'Short command did not finish')
        send("printf '%s\\n' \\\n  'POMDOCK_MULTILINE_OK'")
        wait_for(lambda: marker('POMDOCK_MULTILINE_OK'),'Multiline command did not complete')
        wait_for(lambda: sum(c['cmd'].startswith("printf 'POMDOCK_FAST_") and c['exit']==0 for c in timeline()['commands'])==12,'Fast commands were lost from index')
        rapid=api('/api/session?name='+name+'&id='+indexed['sub'])['entries']
        for i in range(12):
            entry=next(e for e in rapid if e['command']=="printf 'POMDOCK_FAST_%02d\\n'"%i)
            assert entry['output'].strip()=='POMDOCK_FAST_%02d'%i, f"Short-command output contained prompt decorations or another command: {i}: {entry['output']!r}"
        multiline=next(e for e in rapid if 'POMDOCK_MULTILINE_OK' in e['command'])
        assert '\n' in multiline['command'] and multiline['output'].strip()=='POMDOCK_MULTILINE_OK'
        atuin=api('/api/rows?name='+name)['rows']
        assert any('POMDOCK_INDEXED_OUTPUT' in row['command'] for row in atuin),'Atuin history stopped recording'
        def atuin_recorded():
            with sqlite3.connect('file:'+str(engagement/'.atuin/history.db')+'?mode=ro',uri=True) as db:
                return db.execute("SELECT COUNT(*) FROM history WHERE command LIKE ?",('%POMDOCK_INDEXED_OUTPUT%',)).fetchone()[0]>0
        wait_for(atuin_recorded,'Atuin database did not retain the original command')
        recorded=run(docker,'exec',name,'sh','-c','find /home/kali/workspace/sessions -maxdepth 1 -name "*.log" -type f | head -n 1')
        assert recorded, 'Native shell did not start a recording'
        # The same displayed CLI works from an existing host tmux client.
        os.write(clientfd,b'\x02d')
        wait_for(lambda: inside()[0]['clients']==0,'Could not detach before nested-host test')
        reap_client(); client=None
        os.close(clientfd); clientfd=None
        nested=host_mux('new-session','-d','-P','-F','#{window_id}','-s','nested-host','exec '+shown['attach'])
        wait_for(lambda: inside()[0]['clients']==1,'Attach from host tmux failed')
        client,clientfd=pty.fork()
        if client==0:
            os.execvpe('tmux',['tmux','attach-session','-t','=nested-host'],dict(env,TERM='xterm-256color'))
        wait_for(lambda: bool(host_mux('list-clients','-t','=nested-host')),'Nested host client did not attach')
        os.write(clientfd,b'\x02\x02d')
        wait_for(lambda: inside()[0]['clients']==0,'Double host prefix did not detach container tmux')
        assert mux('display-message','-p','-t',window,'#{pane_id}:#{pane_pid}')==identity
        # Closing one observed container window preserves the other window and
        # all captures; opaque IDs cannot accidentally address a host window.
        capture_names={p.name for p in engagement.glob('sessions/*.log')}
        close_id='docker:'+name+':'+extra
        api('/api/shells/'+urllib.parse.quote(close_id,safe=''),'DELETE')
        wait_for(lambda: len(inside())==1,'Closing a container window did not update discovery')
        assert mux('display-message','-p','-t',window,'#{pane_id}:#{pane_pid}')==identity
        assert capture_names.issubset({p.name for p in engagement.glob('sessions/*.log')})
        assert host_mux('display-message','-p','-t',legacy,'#{pane_id}:#{pane_pid}')==original
        print('Offline container tmux passed: copy-only discovery, CLI attach/detach/reconnect, preserved legacy host shell, default windows/splits, independent pane recordings, nested host prefix, Bash/Atuin coexistence, complete searchable output, Ctrl-C/Ctrl-R/Ctrl-A, copy mode, resize and ANSI.')
    except BaseException:
        debug=os.environ.get("POMDOCK_TEST_DEBUG_DIR")
        if debug:
            shutil.copytree(engagement,debug)
        raise
    finally:
        subprocess.run([tmux,'-S',str(socket),'kill-server'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        if client:
            reap_client()
        if clientfd is not None: os.close(clientfd)
        if server:
            server.terminate()
            try: server.wait(timeout=5)
            except subprocess.TimeoutExpired: server.kill(); server.wait()
        subprocess.run([docker,'rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
