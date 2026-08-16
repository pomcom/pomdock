package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	workspaceSession = "pomdock"
	dashboardWindow  = "dashboard"
	workspaceEnv     = "POMDOCK_TMUX_WORKSPACE"
)

func enterPomdockWorkspace() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required for the Pomdock workspace")
	}
	command, err := dashboardCommand()
	if err != nil {
		return err
	}

	if exec.Command("tmux", "has-session", "-t", "="+workspaceSession).Run() != nil {
		out, createErr := exec.Command("tmux", "new-session", "-d", "-s", workspaceSession,
			"-n", dashboardWindow, command).CombinedOutput()
		if createErr != nil {
			return fmt.Errorf("create Pomdock workspace: %s", strings.TrimSpace(string(out)))
		}
	}

	dashboardID, err := ensureDashboardWindow(command)
	if err != nil {
		return err
	}
	_ = exec.Command("tmux", "set-option", "-t", "="+workspaceSession, "base-index", "0").Run()
	_ = exec.Command("tmux", "set-option", "-w", "-t", dashboardID, "automatic-rename", "off").Run()
	_ = moveDashboardToZero(dashboardID)
	_ = exec.Command("tmux", "select-window", "-t", dashboardID).Run()

	if os.Getenv("TMUX") != "" {
		out, switchErr := exec.Command("tmux", "switch-client", "-t", "="+workspaceSession).CombinedOutput()
		if switchErr != nil {
			return fmt.Errorf("switch to Pomdock workspace: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	cmd := exec.Command("tmux", "attach-session", "-t", "="+workspaceSession)
	cmd.Env = usableTerminalEnv(os.Environ())
	return runInteractive(cmd)
}

func dashboardCommand() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve Pomdock executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	parts := []string{"env", workspaceEnv + "=1"}
	if repoRoot != "" {
		parts = append(parts, "POMDOCK_ROOT="+repoRoot)
	}
	parts = append(parts, executable, "tui")
	for i := range parts {
		parts[i] = shellQuote(parts[i])
	}
	return "exec " + strings.Join(parts, " "), nil
}

func ensureDashboardWindow(command string) (string, error) {
	out, err := exec.Command("tmux", "list-windows", "-t", "="+workspaceSession,
		"-F", "#{window_id}\t#{@pomdock_dashboard}\t#{window_name}").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("list Pomdock windows: %s", strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 3 && fields[1] == "1" {
			return fields[0], nil
		}
		if len(fields) == 3 && fields[2] == dashboardWindow {
			if labelOut, labelErr := exec.Command("tmux", "set-option", "-w", "-t", fields[0],
				"@pomdock_dashboard", "1").CombinedOutput(); labelErr != nil {
				return "", fmt.Errorf("label dashboard window: %s", strings.TrimSpace(string(labelOut)))
			}
			return fields[0], nil
		}
	}

	out, err = exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", workspaceSession+":", "-n", dashboardWindow, command).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create dashboard window: %s", strings.TrimSpace(string(out)))
	}
	id := strings.TrimSpace(string(out))
	if labelOut, labelErr := exec.Command("tmux", "set-option", "-w", "-t", id,
		"@pomdock_dashboard", "1").CombinedOutput(); labelErr != nil {
		_ = exec.Command("tmux", "kill-window", "-t", id).Run()
		return "", fmt.Errorf("label dashboard window: %s", strings.TrimSpace(string(labelOut)))
	}
	return id, nil
}

func moveDashboardToZero(dashboardID string) error {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", dashboardID, "#{window_index}").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) == "0" {
		return err
	}
	if exec.Command("tmux", "display-message", "-p", "-t", workspaceSession+":0", "#{window_id}").Run() == nil {
		return exec.Command("tmux", "swap-window", "-s", dashboardID, "-t", workspaceSession+":0").Run()
	}
	return exec.Command("tmux", "move-window", "-s", dashboardID, "-t", workspaceSession+":0").Run()
}
