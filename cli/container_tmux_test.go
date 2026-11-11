package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type tmuxBackendCall struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
}

func internalTmuxBackend(t *testing.T) func() []tmuxBackendCall {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	source := `#!/usr/bin/env python3
import json, os, pathlib, sys
name=pathlib.Path(sys.argv[0]).name;args=sys.argv[1:]
with open(os.environ['POMDOCK_TEST_TMUX_CALLS'],'a') as log: log.write(json.dumps({'tool':name,'args':args})+'\n')
if name=='tmux':
 if os.environ.get('POMDOCK_TEST_HOST_ERROR'): print('synthetic permission denied',file=sys.stderr);sys.exit(6)
 rows=os.environ.get('POMDOCK_TEST_HOST_ROWS','')
 if rows: print(rows,end='')
 else: print('no server running',file=sys.stderr);sys.exit(1)
elif args[0]=='ps':
 if os.environ.get('POMDOCK_TEST_DOCKER_ERROR'):sys.exit(7)
 for container,state,image,labels in [('alpha','Up 1 minute','pcm-kali-pentest','io.pomdock.role=pentest'),('beta','Up 1 minute','pcm-kali-pentest','io.pomdock.role=pentest'),('broken','Up 1 minute','pcm-kali-pentest','io.pomdock.role=pentest'),('stopped','Exited (0)','pcm-kali-pentest','io.pomdock.role=pentest'),('unrelated','Up 1 minute','busybox','')]:
  print(json.dumps({'Names':container,'Status':state,'Image':image,'Labels':labels}))
elif args[0]=='exec':
 pos=args.index('pomdock-tmux');container=args[pos-1];action=args[pos+1]
 if container in json.loads(os.environ.get('POMDOCK_TEST_HELPER_ERRORS') or '[]'):print('pomdock-tmux: unavailable',file=sys.stderr);sys.exit(127)
 if action=='ensure':print('invalid' if os.environ.get('POMDOCK_TEST_BAD_ENSURE') else '@7')
 elif action=='list':print(json.loads(os.environ.get('POMDOCK_TEST_CONTAINER_ROWS') or '{}').get(container,''),end='')
 elif action not in ('attach','close'):sys.exit(91)
else:sys.exit(92)
`
	for _, name := range []string{"docker", "tmux"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("POMDOCK_TEST_TMUX_CALLS", log)
	for _, name := range []string{"POMDOCK_TEST_HOST_ERROR", "POMDOCK_TEST_HOST_ROWS", "POMDOCK_TEST_DOCKER_ERROR", "POMDOCK_TEST_HELPER_ERRORS", "POMDOCK_TEST_BAD_ENSURE", "POMDOCK_TEST_CONTAINER_ROWS"} {
		t.Setenv(name, "")
	}
	return func() []tmuxBackendCall {
		data, _ := os.ReadFile(log)
		var calls []tmuxBackendCall
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var call tmuxBackendCall
			if err := json.Unmarshal([]byte(line), &call); err != nil {
				t.Fatal(err)
			}
			calls = append(calls, call)
		}
		return calls
	}
}

func TestContainerShellParserKeepsGlobalIdentityAndFacts(t *testing.T) {
	data := "@7\t2\t1\tworking shell\tworkspace\t3\t2\n"
	alpha, err := parseContainerShellSessions("alpha", data)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := parseContainerShellSessions("beta", data)
	if err != nil {
		t.Fatal(err)
	}
	if alpha[0].ID == beta[0].ID || alpha[0].ID != "docker:alpha:@7" || alpha[0].WindowID != "@7" || alpha[0].Clients != 3 || alpha[0].Panes != 2 || alpha[0].Location != "container" || !alpha[0].Active {
		t.Fatalf("lost identity/metadata: %+v %+v", alpha, beta)
	}
	for _, bad := range []string{"@7\t2", strings.Replace(data, "\t3\t2", "\t-1\t2", 1), strings.Replace(data, "\t3\t2", "\t3\t0", 1), strings.Replace(data, "@7", "bad", 1), data + data} {
		if _, err := parseContainerShellSessions("alpha", bad); err == nil {
			t.Fatalf("malformed listing accepted: %q", bad)
		}
	}
}

func TestInternalEnsureAttachAndCloseNeverCreateHostSessions(t *testing.T) {
	calls := internalTmuxBackend(t)
	id, err := EnsureContainerShell("alpha")
	if err != nil || id != "docker:alpha:@7" {
		t.Fatalf("ensure: %s %v", id, err)
	}
	if again, err := EnsureContainerShell("alpha"); err != nil || again != id {
		t.Fatalf("ensure identity changed: %s %v", again, err)
	}
	t.Setenv("TERM", "dumb")
	t.Setenv("TMUX", "/synthetic/host,1,0")
	command, err := attachShellWindowCommand(id)
	if err != nil {
		t.Fatal(err)
	}
	if command.Path == "tmux" || command.Args[0] != "docker" || !slices.Contains(command.Args, "TERM=xterm-256color") || !slices.Contains(command.Args, "kali") || !slices.Contains(command.Args, "-it") {
		t.Fatalf("wrong backend/terminal: %v", command.Args)
	}
	before := len(calls())
	if got := containerAttachShellCommand("alpha", "@7"); got != "pomdock docker attach 'alpha' --window '@7'" {
		t.Fatalf("wrong copy command: %s", got)
	}
	if len(calls()) != before {
		t.Fatal("generating copy command executed a backend")
	}
	if err := KillShellSession(id); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls() {
		if call.Tool == "tmux" {
			t.Fatalf("container action touched host tmux: %+v", call)
		}
		if call.Args[0] == "exec" && (!slices.Contains(call.Args, "POMDOCK_ENGAGEMENT=alpha") || !slices.Contains(call.Args, "kali")) {
			t.Fatalf("wrong target/user: %+v", call)
		}
		if slices.Contains([]string{"run", "start", "stop", "rm"}, call.Args[0]) {
			t.Fatalf("unexpected container lifecycle action: %+v", call)
		}
	}
}

func TestContainerTmuxRejectsUnmanagedStoppedAndInvalidTargets(t *testing.T) {
	calls := internalTmuxBackend(t)
	for _, name := range []string{"unrelated", "stopped", "missing", "bad:name"} {
		if _, err := EnsureContainerShell(name); err == nil {
			t.Fatalf("unsafe target accepted: %s", name)
		}
		if _, err := containerAttachCommand(name, ""); err == nil {
			t.Fatalf("unsafe attach accepted: %s", name)
		}
	}
	for _, id := range []string{"docker:alpha:@7:extra", "docker:alpha:7", "docker:alpha;bad:@7", "docker:unrelated:@7", "docker:stopped:@7"} {
		if err := KillShellSession(id); err == nil {
			t.Fatalf("unsafe close accepted: %s", id)
		}
	}
	if _, err := containerAttachCommand("alpha", "@7; bad"); err == nil {
		t.Fatal("invalid window accepted")
	}
	for _, call := range calls() {
		if call.Args[0] == "exec" {
			t.Fatalf("invalid target caused helper mutation: %+v", call)
		}
	}
	t.Setenv("POMDOCK_TEST_BAD_ENSURE", "1")
	if _, err := EnsureContainerShell("alpha"); err == nil {
		t.Fatal("invalid ensure ID reported success")
	}
}

func TestShellDiscoveryReturnsPartialRowsAndActionableWarnings(t *testing.T) {
	calls := internalTmuxBackend(t)
	t.Setenv("POMDOCK_TEST_HOST_ROWS", "@7\t0\t1\tlegacy\tvm\told-vm\t\t\t\tpomdock\t$1\t\t1\t2\n")
	data, _ := json.Marshal(map[string]string{"alpha": "@7\t0\t1\tshell\tworkspace\t2\t3\n", "beta": "@7\t0\t1\tshell\tworkspace\t0\t1\n"})
	t.Setenv("POMDOCK_TEST_CONTAINER_ROWS", string(data))
	t.Setenv("POMDOCK_TEST_HELPER_ERRORS", `["broken"]`)
	rows, warnings, err := DiscoverShellSessions()
	if err != nil || len(rows) != 3 || len(warnings) != 1 || !strings.Contains(warnings[0], "broken") {
		t.Fatalf("partial discovery lost: %+v %q %v", rows, warnings, err)
	}
	for _, call := range calls() {
		if call.Tool == "docker" && call.Args[0] == "exec" && (slices.Contains(call.Args, "stopped") || call.Args[len(call.Args)-1] != "list") {
			t.Fatalf("discovery changed state: %+v", call)
		}
	}
	partial, err := ListShellSessions()
	if err == nil || len(partial) != 3 {
		t.Fatalf("compatibility wrapper hid warning/rows: %+v %v", partial, err)
	}
	t.Setenv("POMDOCK_TEST_DOCKER_ERROR", "1")
	rows, warnings, err = DiscoverShellSessions()
	if err != nil || len(rows) != 1 || rows[0].Location != "host" || rows[0].Clients != 1 || rows[0].Panes != 2 || len(warnings) != 1 {
		t.Fatalf("host fallback lost: %+v %q %v", rows, warnings, err)
	}
	t.Setenv("POMDOCK_TEST_HOST_ERROR", "1")
	rows, warnings, err = DiscoverShellSessions()
	if err != nil || len(rows) != 0 || len(warnings) != 2 {
		t.Fatalf("both unavailable looked empty-success: %+v %q %v", rows, warnings, err)
	}
}

func TestDockerAttachCLIUsesContainerHelper(t *testing.T) {
	calls := internalTmuxBackend(t)
	cmd := newRootCommand()
	cmd.SetArgs([]string{"docker", "attach", "alpha", "--window", "@7"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range calls() {
		if call.Tool == "docker" && call.Args[0] == "exec" {
			found = true
			if !slices.Equal(call.Args[len(call.Args)-3:], []string{"pomdock-tmux", "attach", "@7"}) {
				t.Fatalf("wrong helper argv: %+v", call)
			}
		}
		if call.Tool == "tmux" {
			t.Fatal("CLI touched host tmux")
		}
	}
	if !found {
		t.Fatal("attach command did not run helper")
	}
}

func TestContainerReturnHintsPreserveReadlineAndNestedPrefix(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := shellReturnHint("docker:alpha:@7"); got != "Ctrl-b d detaches container tmux" {
		t.Fatal(got)
	}
	t.Setenv("TMUX", "/synthetic/host,1,0")
	if got := shellReturnHint("docker:alpha:@7"); got != "Ctrl-b Ctrl-b d detaches container tmux" {
		t.Fatal(got)
	}
	if strings.Contains(shellReturnHint("@7"), "Ctrl-a") {
		t.Fatal("host shortcut changed")
	}
}

func TestTUIDiscoveryWarningsDoNotClaimEmptySuccess(t *testing.T) {
	m := newTUI()
	m.width = 100
	m.shellWarnings = []string{"synthetic helper unavailable"}
	view := m.shellsView()
	if !strings.Contains(view, "Discovery incomplete") || strings.Contains(view, "No shell windows") {
		t.Fatal(view)
	}
	m.shells = []ShellSession{{ID: "docker:beta:@7", Name: "shell"}, {ID: "docker:alpha:@7", Name: "shell"}}
	m.shellCursor = 1
	updated, _ := m.Update(shellsMsg{sessions: []ShellSession{{ID: "docker:alpha:@7", Name: "shell"}, {ID: "docker:beta:@7", Name: "shell"}}})
	if updated.(tuiModel).shellCursor != 0 {
		t.Fatal("selection followed duplicate name instead of opaque ID")
	}
}
