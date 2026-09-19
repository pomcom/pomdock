package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// workspaceSession is the detached tmux session that holds persistent shells,
// VM provisioning jobs and FreeRDP windows. The TUI itself runs directly in
// the caller's terminal and only attaches to this session on demand.
const workspaceSession = "pomdock"

// returnToDashboardScript is appended to job scripts that finish inside a
// workspace window. It brings the user back to the TUI: when they attached
// from the TUI, detaching returns there; when they came from their own tmux
// session (switch-client), it switches back to that session instead.
const returnToDashboardScript = "tmux switch-client -l 2>/dev/null || tmux detach-client 2>/dev/null || true"

func requireTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required for persistent shells")
	}
	return nil
}

func workspaceExists() bool {
	return exec.Command("tmux", "has-session", "-t", "="+workspaceSession).Run() == nil
}

// newWorkspaceWindow creates a detached window running command inside the
// workspace session, creating the session itself if needed, and returns the
// tmux window id.
func newWorkspaceWindow(name, command string) (string, error) {
	if err := requireTmux(); err != nil {
		return "", err
	}
	var args []string
	if workspaceExists() {
		args = []string{"new-window", "-d", "-P", "-F", "#{window_id}",
			"-t", workspaceSession + ":", "-n", name, command}
	} else {
		args = []string{"new-session", "-d", "-P", "-F", "#{window_id}",
			"-s", workspaceSession, "-n", name, command}
	}
	out, err := exec.Command("tmux", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create tmux window: %s", strings.TrimSpace(string(out)))
	}
	windowID := strings.TrimSpace(string(out))
	if windowID == "" {
		return "", fmt.Errorf("create tmux window: tmux returned no window id")
	}
	_ = exec.Command("tmux", "set-option", "-w", "-t", windowID, "automatic-rename", "off").Run()
	return windowID, nil
}

func setWindowOptions(windowID string, options map[string]string) error {
	for key, value := range options {
		if out, err := exec.Command("tmux", "set-option", "-w", "-t", windowID, key, value).CombinedOutput(); err != nil {
			_ = exec.Command("tmux", "kill-window", "-t", windowID).Run()
			return fmt.Errorf("label tmux window: %s", strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// attachWorkspaceWindowCommand returns the tmux command that shows windowID in
// the user's terminal. Outside tmux it attaches to the workspace session and
// returns when the user detaches (Ctrl-b d). Inside tmux it switches the
// current client to the workspace session; Ctrl-b L switches back.
// insideTmux reports whether the TUI itself runs inside a tmux client.
func insideTmux() bool { return os.Getenv("TMUX") != "" }

// returnHint tells the user how to get back to the TUI from a workspace window.
func returnHint() string {
	if insideTmux() {
		return "Ctrl-b L returns here"
	}
	return "Ctrl-b d returns here"
}

func attachWorkspaceWindowCommand(windowID string) *exec.Cmd {
	args := []string{"select-window", "-t", windowID, ";"}
	if insideTmux() {
		args = append(args, "switch-client", "-t", "="+workspaceSession)
	} else {
		args = append(args, "attach-session", "-t", "="+workspaceSession)
	}
	cmd := exec.Command("tmux", args...)
	cmd.Env = usableTerminalEnv(os.Environ())
	return cmd
}
