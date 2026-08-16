package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestShellWindowNameIsStableAndSafe(t *testing.T) {
	first := shellWindowName("client.example-pentest")
	if first != shellWindowName("client.example-pentest") {
		t.Fatal("window name is not deterministic")
	}
	if first == shellWindowName("different") {
		t.Fatal("different containers produced the same window name")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(first) {
		t.Fatalf("unsafe window name %q", first)
	}
}

func TestParseShellSessions(t *testing.T) {
	sessions := parseShellSessions("@1\t0\t1\tdashboard\t\n@3\t2\t0\tshell-bravo\tbravo\n@2\t1\t1\tshell-alpha\talpha\n")
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions", len(sessions))
	}
	if sessions[0].Container != "alpha" || sessions[1].Container != "bravo" {
		t.Fatalf("sessions not sorted by container: %#v", sessions)
	}
	if !sessions[0].Active || sessions[0].ID != "@2" {
		t.Fatalf("active window not parsed: %#v", sessions[0])
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'"'"'s'` {
		t.Fatalf("shellQuote = %q", got)
	}
}

func TestTmuxServerAbsent(t *testing.T) {
	for _, message := range []string{
		"no server running on /tmp/tmux-1000/default",
		"no sessions",
		"error connecting to /tmp/tmux-1000/default (No such file or directory)",
	} {
		if !tmuxServerAbsent(strings.ToLower(message)) {
			t.Fatalf("expected absent tmux server for %q", message)
		}
	}
	if tmuxServerAbsent("permission denied") {
		t.Fatal("permission errors must not be treated as an absent server")
	}
}

func TestUsableTerminalEnv(t *testing.T) {
	got := usableTerminalEnv([]string{"PATH=/bin", "TERM=dumb"})
	if strings.Join(got, "|") != "PATH=/bin|TERM=xterm-256color" {
		t.Fatalf("unexpected terminal environment: %v", got)
	}
	original := []string{"TERM=screen-256color", "PATH=/bin"}
	got = usableTerminalEnv(original)
	if strings.Join(got, "|") != strings.Join(original, "|") {
		t.Fatalf("usable TERM was changed: %v", got)
	}
}
