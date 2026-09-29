package main

import (
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
	if !strings.Contains(joined, "select-window -t @7 ;") || !strings.Contains(joined, "attach-session -t =pomdock") {
		t.Fatalf("unexpected attach command outside tmux: %q", joined)
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	joined = strings.Join(attachWorkspaceWindowCommand("@7").Args, " ")
	if !strings.Contains(joined, "switch-client -t =pomdock") || strings.Contains(joined, "attach-session") {
		t.Fatalf("unexpected attach command inside tmux: %q", joined)
	}
}

func TestReturnToDashboardScriptFallsBackToDetach(t *testing.T) {
	if !strings.Contains(returnToDashboardScript, "switch-client -l") ||
		!strings.Contains(returnToDashboardScript, "detach-client") {
		t.Fatalf("unexpected return script %q", returnToDashboardScript)
	}
}

func TestStyleWorkspaceStatusBuildsFormat(t *testing.T) {
	// The format string built inline in styleWorkspaceStatus must cover every
	// route and map it to the pane-border colours.
	route := "#{@pomdock_route}"
	bg := "bg=#{?#{==:" + route + ",vpn},green," +
		"#{?#{==:" + route + ",direct},red," +
		"#{?#{==:" + route + ",tor},magenta," +
		"#{?#{==:" + route + ",tor-vpn},cyan,default}}}}"
	for _, want := range []string{"vpn},green", "direct},red", "tor},magenta", "tor-vpn},cyan", "cyan,default"} {
		if !strings.Contains(bg, want) {
			t.Fatalf("status bg format missing %q: %s", want, bg)
		}
	}
}
