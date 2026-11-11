package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// workspaceSession holds VM shells, provisioning jobs and FreeRDP windows.
// New Docker shells use tmux inside their container. Legacy host terminals stay
// in their existing sessions; the TUI attaches to host workspaces on demand.
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
	styleWorkspaceStatus()
	return windowID, nil
}

// styleWorkspaceStatus removes Pomdock's old status colour overrides. The bar
// and window tabs inherit the user's theme; route colours stay on pane borders.
func styleWorkspaceStatus() {
	_ = exec.Command("tmux", "set-option", "-u", "-t", workspaceSession, "status-style").Run()
	out, err := exec.Command("tmux", "list-windows", "-t", workspaceSession, "-F", "#{window_id}").Output()
	if err != nil {
		return
	}
	for _, windowID := range strings.Fields(string(out)) {
		_ = exec.Command("tmux", "set-option", "-wu", "-t", windowID, "window-status-current-style").Run()
	}
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

// insideTmux reports whether the TUI itself runs inside a tmux client.
func insideTmux() bool { return os.Getenv("TMUX") != "" }

// returnHint tells the user how to get back to the TUI from a workspace window.
func returnHint() string {
	if insideTmux() {
		return "Ctrl-b L returns here"
	}
	return "Ctrl-b d returns here"
}

// Attach directly to the window target, so tmux resolves its owning session.
// Inside tmux switch the existing client instead of nesting a second client.
func attachWorkspaceWindowCommand(windowID string) *exec.Cmd {
	args := []string{"select-window", "-t", windowID, ";"}
	if insideTmux() {
		args = append(args, "switch-client", "-t", windowID)
	} else {
		args = append(args, "attach-session", "-t", windowID)
	}
	cmd := exec.Command("tmux", args...)
	cmd.Env = usableTerminalEnv(os.Environ())
	return cmd
}
