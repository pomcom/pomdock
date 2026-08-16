package main

import (
	"fmt"
	"hash/fnv"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ShellSession struct {
	ID        string
	Name      string
	Container string
	Active    bool
	Index     int
}

var nonSessionChar = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func shellWindowName(container string) string {
	safe := strings.Trim(nonSessionChar.ReplaceAllString(container, "-"), "-")
	if safe == "" {
		safe = "shell"
	}
	if len(safe) > 28 {
		safe = safe[:28]
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(container))
	return fmt.Sprintf("shell-%s-%04x", safe, hash.Sum32()&0xffff)
}

func ListShellSessions() ([]ShellSession, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux not found")
	}
	if exec.Command("tmux", "has-session", "-t", "="+workspaceSession).Run() != nil {
		return nil, nil
	}
	out, err := exec.Command("tmux", "list-windows", "-t", "="+workspaceSession, "-F",
		"#{window_id}\t#{window_index}\t#{window_active}\t#{window_name}\t#{@pomdock_container}").CombinedOutput()
	if err != nil {
		message := strings.ToLower(string(out))
		if tmuxServerAbsent(message) {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-windows: %s", strings.TrimSpace(string(out)))
	}
	return parseShellSessions(string(out)), nil
}

func tmuxServerAbsent(message string) bool {
	return strings.Contains(message, "no server running") ||
		strings.Contains(message, "no sessions") ||
		strings.Contains(message, "no such file or directory") ||
		strings.Contains(message, "can't find session")
}

func parseShellSessions(output string) []ShellSession {
	var sessions []ShellSession
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[4] == "" {
			continue
		}
		index, _ := strconv.Atoi(fields[1])
		sessions = append(sessions, ShellSession{
			ID: fields[0], Index: index, Active: fields[2] == "1",
			Name: fields[3], Container: fields[4],
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Index < sessions[j].Index
	})
	return sessions
}

func EnsureContainerShell(container string) (string, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return "", fmt.Errorf("tmux is required for persistent shells")
	}
	if ContainerState(container) != "running" {
		return "", fmt.Errorf("container '%s' is not running", container)
	}
	if exec.Command("tmux", "has-session", "-t", "="+workspaceSession).Run() != nil {
		return "", fmt.Errorf("Pomdock tmux workspace is not running")
	}
	if windows, err := ListShellSessions(); err == nil {
		for _, window := range windows {
			if window.Container == container {
				return window.ID, nil
			}
		}
	} else {
		return "", err
	}

	shellCommand := fmt.Sprintf(
		"exec docker exec -it -w /home/kali/pentest -e TERM=xterm-256color -e COLORTERM=truecolor %s zsh -l",
		shellQuote(container),
	)
	out, err := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{window_id}",
		"-t", workspaceSession+":", "-n", shellWindowName(container), shellCommand).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("create shell window: %s", strings.TrimSpace(string(out)))
	}
	windowID := strings.TrimSpace(string(out))
	if out, err := exec.Command("tmux", "set-option", "-w", "-t", windowID,
		"@pomdock_container", container).CombinedOutput(); err != nil {
		_ = exec.Command("tmux", "kill-window", "-t", windowID).Run()
		return "", fmt.Errorf("label shell window: %s", strings.TrimSpace(string(out)))
	}
	_ = exec.Command("tmux", "set-option", "-w", "-t", windowID, "automatic-rename", "off").Run()
	return windowID, nil
}

func SelectShellWindow(windowID string) error {
	out, err := exec.Command("tmux", "select-window", "-t", windowID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("select shell window: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func usableTerminalEnv(env []string) []string {
	term := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "TERM=") {
			term = strings.TrimPrefix(entry, "TERM=")
		}
	}
	if term != "" && term != "dumb" {
		return env
	}

	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "TERM=") {
			result = append(result, entry)
		}
	}
	return append(result, "TERM=xterm-256color")
}

func KillShellSession(windowID string) error {
	out, err := exec.Command("tmux", "kill-window", "-t", windowID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("close shell window: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
