package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSessionNameIsStable(t *testing.T) {
	if workspaceSession != "pomdock" {
		t.Fatalf("unexpected workspace session name %q", workspaceSession)
	}
}

func TestAttachWorkspaceWindowCommand(t *testing.T) {
	t.Setenv("TMUX", "")
	cmd := attachWorkspaceWindowCommand("@7")
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "select-window -t @7 ;") || !strings.Contains(joined, "attach-session -t @7") {
		t.Fatalf("unexpected attach command outside tmux: %q", joined)
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	joined = strings.Join(attachWorkspaceWindowCommand("@7").Args, " ")
	if !strings.Contains(joined, "switch-client -t @7") || strings.Contains(joined, "attach-session") {
		t.Fatalf("unexpected attach command inside tmux: %q", joined)
	}
}

func TestReturnToDashboardScriptFallsBackToDetach(t *testing.T) {
	if !strings.Contains(returnToDashboardScript, "switch-client -l") ||
		!strings.Contains(returnToDashboardScript, "detach-client") {
		t.Fatalf("unexpected return script %q", returnToDashboardScript)
	}
}

func TestWorkspaceStatusInheritsThemeWithoutChangingOtherSessions(t *testing.T) {
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux is not installed")
	}
	dir := t.TempDir()
	socket := filepath.Join(dir, "server")
	wrapper := "#!/bin/sh\nexec " + shellQuote(realTmux) + " -S " + shellQuote(socket) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %s (%v)", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	run("-f", "/dev/null", "new-session", "-d", "-s", "pomdock", "sleep 60")
	t.Cleanup(func() { _ = exec.Command(realTmux, "-S", socket, "kill-server").Run() })
	run("new-session", "-d", "-s", "other", "sleep 60")
	other := run("display-message", "-p", "-t", "other:", "#{window_id}")
	run("set-option", "-w", "-t", other, "window-status-current-style", "fg=yellow,bg=blue")
	theme := "fg=#e8e3f2,bg=#33213f,bold"
	run("set-option", "-g", "window-status-current-style", theme)
	run("set-option", "-g", "status-style", "fg=#e8e3f2,bg=#0e0e1a")
	run("set-option", "-t", "pomdock", "status-style", "fg=black,bg=red")
	run("new-window", "-d", "-t", "pomdock:", "sleep 60")
	windows := strings.Fields(run("list-windows", "-t", "pomdock", "-F", "#{window_id}"))
	for _, id := range windows {
		run("set-option", "-w", "-t", id, "window-status-current-style", "fg=black,bg=red,bold")
	}
	styleWorkspaceStatus()
	for _, id := range windows {
		if got := run("show-options", "-Av", "-w", "-t", id, "window-status-current-style"); got != theme {
			t.Fatalf("tab %s does not inherit theme: %s", id, got)
		}
	}
	if got := run("show-options", "-Av", "-t", "pomdock", "status-style"); got != "fg=#e8e3f2,bg=#0e0e1a" {
		t.Fatalf("bar does not inherit theme: %s", got)
	}
	if got := run("show-options", "-v", "-w", "-t", other, "window-status-current-style"); got != "fg=yellow,bg=blue" {
		t.Fatalf("other session changed: %s", got)
	}
	run("set-option", "-g", "window-status-current-style", "fg=white,bg=black")
	for _, id := range windows {
		if got := run("show-options", "-Av", "-w", "-t", id, "window-status-current-style"); got != "fg=white,bg=black" {
			t.Fatalf("tab %s no longer follows theme: %s", id, got)
		}
	}
}
