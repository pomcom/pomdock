package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Real, isolated host tmux plus a synthetic Docker backend. No user terminals or
// containers are accessed, and legacy panes keep their original processes.
func legacyHostTmuxFixture(t *testing.T) func(...string) string {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "server")
	wrapper := "#!/bin/sh\nexec " + shellQuote(real) + " -S " + shellQuote(socket) + " -f /dev/null \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	docker := `#!/bin/sh
case "$1" in
ps)
 [ "${POMDOCK_TEST_DOCKER_DOWN:-}" != 1 ] || exit 7
 printf '%s\n' '{"Names":"lab-kali","ID":"synthetic","Status":"Up 1 minute","Image":"pcm-kali-pentest","Labels":"io.pomdock.role=pentest"}' ;;
exec)
 case "$*" in
 *'pomdock-tmux ensure') printf '@3\n';;
 *'pomdock-tmux list') printf '@3\t0\t1\tshell\tworkspace\t0\t1\n';;
 *) exit 0;;
 esac;;
*) exit 9;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	t.Cleanup(func() { _ = exec.Command(real, "-S", socket, "kill-server").Run() })
	return func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("private tmux: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
}

func TestContainerTmuxCreationPreservesLegacyHostWindows(t *testing.T) {
	run := legacyHostTmuxFixture(t)
	legacy := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "pomdock", "sleep 120")
	run("set-option", "-w", "-t", legacy, "@pomdock_container", "lab-kali")
	other := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "unrelated", "sleep 120")
	before := run("display-message", "-p", "-t", legacy, "#{pane_pid}:#{session_name}")
	otherBefore := run("display-message", "-p", "-t", other, "#{pane_pid}:#{session_name}")
	id, err := EnsureContainerShell("lab-kali")
	if err != nil || id != "docker:lab-kali:@3" {
		t.Fatalf("container ensure: %s %v", id, err)
	}
	if got := run("display-message", "-p", "-t", legacy, "#{pane_pid}:#{session_name}"); got != before {
		t.Fatalf("legacy process moved/restarted: %s", got)
	}
	if got := run("display-message", "-p", "-t", other, "#{pane_pid}:#{session_name}"); got != otherBefore {
		t.Fatalf("unrelated host window changed: %s", got)
	}
	rows, warnings, err := DiscoverShellSessions()
	if err != nil || len(warnings) != 0 || len(rows) != 2 {
		t.Fatalf("combined discovery: %+v %q %v", rows, warnings, err)
	}
	for _, row := range rows {
		if row.Location == "host" && (row.ID != legacy || row.WindowID != legacy || row.Panes != 1) {
			t.Fatalf("wrong host metadata: %+v", row)
		}
	}
}

func TestLegacyHostCloseAndAttachWorkWithDockerUnavailable(t *testing.T) {
	run := legacyHostTmuxFixture(t)
	legacy := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "pomdock", "sleep 120")
	run("set-option", "-w", "-t", legacy, "@pomdock_shell_kind", "vm")
	run("set-option", "-w", "-t", legacy, "@pomdock_shell_target", "lab-vm")
	other := run("new-session", "-d", "-P", "-F", "#{window_id}", "-s", "unrelated", "sleep 120")
	t.Setenv("POMDOCK_TEST_DOCKER_DOWN", "1")
	rows, warnings, err := DiscoverShellSessions()
	if err != nil || len(rows) != 1 || rows[0].ID != legacy || len(warnings) != 1 {
		t.Fatalf("host fallback lost: %+v %q %v", rows, warnings, err)
	}
	cmd, err := attachShellWindowCommand(legacy)
	if err != nil || !strings.Contains(strings.Join(cmd.Args, " "), "attach-session -t "+legacy) {
		t.Fatalf("legacy attach: %v %+v", err, cmd)
	}
	if err := KillShellSession(other); err == nil {
		t.Fatal("closed unmanaged host window")
	}
	if err := KillShellSession(legacy); err != nil {
		t.Fatal(err)
	}
	if got := run("list-windows", "-a", "-F", "#{window_id}"); got != other {
		t.Fatalf("unrelated window removed: %s", got)
	}
}
