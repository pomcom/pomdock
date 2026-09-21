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
	ID     string
	Name   string
	Kind   string
	Target string
	Active bool
	Index  int
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
		"#{window_id}\t#{window_index}\t#{window_active}\t#{window_name}\t#{@pomdock_shell_kind}\t#{@pomdock_shell_target}\t#{@pomdock_container}\t#{@pomdock_job}\t#{@pomdock_vm}").CombinedOutput()
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
		if len(fields) != 9 {
			continue
		}
		kind, target := fields[4], fields[5]
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
		sessions = append(sessions, ShellSession{
			ID: fields[0], Index: index, Active: fields[2] == "1",
			Name: fields[3], Kind: kind, Target: target,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Index < sessions[j].Index
	})
	return sessions
}

func EnsureContainerShell(container string) (string, error) {
	if err := requireTmux(); err != nil {
		return "", err
	}
	if ContainerState(container) != "running" {
		return "", fmt.Errorf("container '%s' is not running", container)
	}
	if windows, err := ListShellSessions(); err == nil {
		for _, window := range windows {
			if window.Kind == "docker" && window.Target == container {
				return window.ID, nil
			}
		}
	} else {
		return "", err
	}

	// POMDOCK_ENGAGEMENT arms the container's auto-capture hook (records the
	// session into the loot dir); gated on this var, so it's a no-op elsewhere.
	shellCommand := fmt.Sprintf(
		"exec docker exec -it -w /home/kali/pentest -e TERM=xterm-256color -e COLORTERM=truecolor -e POMDOCK_ENGAGEMENT=%s %s zsh -l",
		shellQuote(container), shellQuote(container),
	)
	windowID, err := newWorkspaceWindow(shellWindowName(container), shellCommand)
	if err != nil {
		return "", err
	}
	opts := map[string]string{
		"@pomdock_container":    container,
		"@pomdock_shell_kind":   "docker",
		"@pomdock_shell_target": container,
	}
	// Color the window's pane border by network route so the context is
	// obvious at a glance (green vpn / red direct / magenta tor / cyan tor-vpn).
	if route := containerRouteLabel(container); route != "" {
		color := routeBorderColor(route)
		opts["@pomdock_route"] = route
		opts["pane-border-status"] = "top"
		opts["pane-border-format"] = fmt.Sprintf(" #[fg=%s,bold]%s#[default] %s ", color, route, container)
		opts["pane-active-border-style"] = "fg=" + color + ",bold"
		opts["pane-border-style"] = "fg=" + color
	}
	if err := setWindowOptions(windowID, opts); err != nil {
		return "", err
	}
	return windowID, nil
}

// containerRouteLabel reads the pomdock network route recorded on a container
// (io.pomdock.route: vpn|direct|tor|tor-vpn), or "" if unavailable.
func containerRouteLabel(container string) string {
	out, err := exec.Command("docker", "inspect", "-f",
		`{{index .Config.Labels "io.pomdock.route"}}`, container).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func routeBorderColor(route string) string {
	switch route {
	case "vpn":
		return "green"
	case "direct":
		return "red"
	case "tor":
		return "magenta"
	case "tor-vpn":
		return "cyan"
	default:
		return "white"
	}
}

func EnsureVMShell(vm VM) (string, error) {
	if err := requireTmux(); err != nil {
		return "", err
	}
	if windows, err := ListShellSessions(); err == nil {
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
