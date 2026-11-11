package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ShellSession struct {
	ID        string
	Session   string
	SessionID string
	Name      string
	Kind      string
	Target    string
	Active    bool
	Index     int
	Location  string
	WindowID  string
	Clients   int
	Panes     int
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

func listHostShellSessions() ([]ShellSession, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, fmt.Errorf("tmux not found")
	}
	out, err := exec.Command("tmux", "list-windows", "-a", "-F",
		"#{window_id}\t#{window_index}\t#{window_active}\t#{window_name}\t#{@pomdock_shell_kind}\t#{@pomdock_shell_target}\t#{@pomdock_container}\t#{@pomdock_job}\t#{@pomdock_vm}\t#{session_name}\t#{session_id}\t#{@pomdock_container_session}\t#{session_attached}\t#{window_panes}").CombinedOutput()
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
	for _, line := range strings.Split(strings.TrimRight(output, "\r\n"), "\n") {
		line = strings.TrimSuffix(line, "\r")
		fields := strings.Split(line, "\t")
		if len(fields) != 12 && len(fields) != 14 {
			continue
		}
		session, sessionID, container := fields[9], fields[10], fields[11]
		// Only explicitly managed container sessions and the legacy jobs workspace.
		if session != workspaceSession && container == "" {
			continue
		}
		kind, target := fields[4], fields[5]
		if container != "" {
			kind, target = "docker", container
		}
		if kind == "" && fields[6] != "" {
			kind, target = "docker", fields[6]
		}
		if kind == "" && fields[7] != "" {
			kind, target = fields[7], fields[8]
		}
		if kind == "" || target == "" {
			continue
		}
		index, _ := strconv.Atoi(fields[1])
		clients, panes := 0, 0
		if len(fields) == 14 {
			var e1, e2 error
			clients, e1 = strconv.Atoi(fields[12])
			panes, e2 = strconv.Atoi(fields[13])
			if e1 != nil || e2 != nil || clients < 0 || panes < 1 {
				continue
			}
		}
		sessions = append(sessions, ShellSession{
			ID: fields[0], Session: session, SessionID: sessionID, Index: index, Active: fields[2] == "1",
			Name: fields[3], Kind: kind, Target: target, Location: "host", WindowID: fields[0], Clients: clients, Panes: panes,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Session != sessions[j].Session {
			return sessions[i].Session < sessions[j].Session
		}
		return sessions[i].Index < sessions[j].Index
	})
	// A linked window is still one terminal. Listing never changes its links.
	unique := sessions[:0]
	seen := make(map[string]bool)
	for _, window := range sessions {
		if !seen[window.ID] {
			unique = append(unique, window)
			seen[window.ID] = true
		}
	}
	return unique
}

func containerDirectShellCommand(container string) string {
	return "pomdock docker exec --name " + shellQuote(container)
}

func EnsureVMShell(vm VM) (string, error) {
	if err := requireTmux(); err != nil {
		return "", err
	}
	if windows, err := listHostShellSessions(); err == nil {
		for _, window := range windows {
			if window.Kind == "vm" && window.Target == vm.Name {
				return window.ID, nil
			}
		}
	} else {
		return "", err
	}

	profile := GuestProfileByID(vm.ProfileID)
	if !profile.SupportsSSH {
		return "", fmt.Errorf("%s profile does not provide SSH", profile.Label)
	}
	ip := vm.IP
	if ip == "" {
		var err error
		ip, err = WaitForVMIP(vm.Name, 30*time.Second)
		if err != nil {
			return "", err
		}
	}
	args := vmSSHArgs(profile, ip)
	shellCommand := "exec " + shellJoin(append([]string{"ssh"}, args...))
	windowID, err := newWorkspaceWindow(shellWindowName("ssh-"+vm.Name), shellCommand)
	if err != nil {
		return "", err
	}
	if err := setWindowOptions(windowID, map[string]string{
		"@pomdock_shell_kind":   "vm",
		"@pomdock_shell_target": vm.Name,
		"@pomdock_vm":           vm.Name,
	}); err != nil {
		return "", err
	}
	return windowID, nil
}

func vmSSHArgs(profile GuestProfile, ip string) []string {
	args := []string{}
	if keyPath := profileSSHKey(profile); keyPath != "" {
		if _, err := os.Stat(keyPath); err == nil {
			args = append(args, "-i", keyPath)
		}
	}
	return append(args,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		profile.SSHUser+"@"+ip)
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

func killHostShellSession(windowID string) error {
	out, err := exec.Command("tmux", "kill-window", "-t", windowID).CombinedOutput()
	if err != nil {
		return fmt.Errorf("close shell window: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
