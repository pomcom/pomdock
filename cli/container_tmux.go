package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var containerShellIDRe = regexp.MustCompile(`^docker:([a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}):(@[0-9]+)$`)

func parseContainerShellID(id string) (string, string, error) {
	parts := containerShellIDRe.FindStringSubmatch(id)
	if parts == nil {
		return "", "", fmt.Errorf("invalid container window ID %q", id)
	}
	return parts[1], parts[2], nil
}

func requireRunningManagedContainer(name string) error {
	if !containerNameRe.MatchString(name) {
		return fmt.Errorf("invalid container name %q", name)
	}
	containers, err := ListContainers()
	if err != nil {
		return err
	}
	for _, container := range containers {
		if container.Name != name {
			continue
		}
		if container.Status != "running" {
			return fmt.Errorf("container %q is not running; start it with pomdock docker start %s", name, shellQuote(name))
		}
		return nil
	}
	return fmt.Errorf("no managed pomdock container named %q", name)
}

func runContainerTmux(name, action string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := []string{"exec", "-u", "kali", "-e", "POMDOCK_ENGAGEMENT=" + name, name, "pomdock-tmux", action}
	out, err := exec.CommandContext(ctx, "docker", append(command, args...)...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("container %s tmux %s: %w", name, action, ctx.Err())
		}
		return "", fmt.Errorf("container %s tmux %s: %w (%s); an older image may need rebuilding", name, action, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func parseContainerShellSessions(name, output string) ([]ShellSession, error) {
	if !containerNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid container name")
	}
	rows := []ShellSession{}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(output, "\r\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(strings.TrimSuffix(line, "\r"), "\t")
		if len(fields) != 7 || !tmuxWindowIDRe.MatchString(fields[0]) || (fields[2] != "0" && fields[2] != "1") || fields[4] == "" || seen[fields[0]] {
			return nil, fmt.Errorf("invalid tmux window listing for container %s", name)
		}
		index, e1 := strconv.Atoi(fields[1])
		clients, e2 := strconv.Atoi(fields[5])
		panes, e3 := strconv.Atoi(fields[6])
		if e1 != nil || e2 != nil || e3 != nil || index < 0 || clients < 0 || panes < 1 {
			return nil, fmt.Errorf("invalid tmux window metadata for container %s", name)
		}
		seen[fields[0]] = true
		rows = append(rows, ShellSession{ID: "docker:" + name + ":" + fields[0], WindowID: fields[0], Session: fields[4], Name: fields[3], Kind: "docker", Target: name, Active: fields[2] == "1", Index: index, Location: "container", Clients: clients, Panes: panes})
	}
	return rows, nil
}

// Discovery is read-only: it never ensures a session, selects a window, or
// contacts stopped containers. Successful targets remain visible with warnings.
func discoverShellSessions(containers []Container) ([]ShellSession, []string) {
	rows, hostErr := listHostShellSessions()
	warnings := []string{}
	if hostErr != nil {
		warnings = append(warnings, "Host terminals: "+hostErr.Error())
	}
	type result struct {
		rows    []ShellSession
		warning string
	}
	results := make([]result, len(containers))
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, container := range containers {
		if container.Status != "running" {
			continue
		}
		workers.Add(1)
		go func(i int, name string) {
			defer workers.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			output, err := runContainerTmux(name, "list")
			if err == nil {
				results[i].rows, err = parseContainerShellSessions(name, output)
			}
			if err != nil {
				results[i].warning = err.Error()
			}
		}(i, container.Name)
	}
	workers.Wait()
	for _, result := range results {
		rows = append(rows, result.rows...)
		if result.warning != "" {
			warnings = append(warnings, result.warning)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Location != rows[j].Location {
			return rows[i].Location < rows[j].Location
		}
		if rows[i].Target != rows[j].Target {
			return rows[i].Target < rows[j].Target
		}
		if rows[i].Session != rows[j].Session {
			return rows[i].Session < rows[j].Session
		}
		return rows[i].Index < rows[j].Index
	})
	return rows, warnings
}

func DiscoverShellSessions() ([]ShellSession, []string, error) {
	containers, err := ListContainers()
	if err != nil {
		rows, hostErr := listHostShellSessions()
		warnings := []string{"Container terminals: " + err.Error()}
		if hostErr != nil {
			warnings = append(warnings, "Host terminals: "+hostErr.Error())
		}
		return rows, warnings, nil
	}
	rows, warnings := discoverShellSessions(containers)
	return rows, warnings, nil
}

func ListShellSessions() ([]ShellSession, error) {
	rows, warnings, err := DiscoverShellSessions()
	if err != nil {
		return rows, err
	}
	issues := make([]error, 0, len(warnings))
	for _, warning := range warnings {
		issues = append(issues, errors.New(warning))
	}
	return rows, errors.Join(issues...)
}

func EnsureContainerShell(name string) (string, error) {
	if err := requireRunningManagedContainer(name); err != nil {
		return "", err
	}
	window, err := runContainerTmux(name, "ensure")
	if err != nil {
		return "", err
	}
	if !tmuxWindowIDRe.MatchString(window) {
		return "", fmt.Errorf("container %s returned an invalid tmux window ID", name)
	}
	return "docker:" + name + ":" + window, nil
}

func containerAttachShellCommand(name, window string) string {
	command := "pomdock docker attach " + shellQuote(name)
	if window != "" {
		command += " --window " + shellQuote(window)
	}
	return command
}

func containerAttachCommand(name, window string) (*exec.Cmd, error) {
	if window != "" && !tmuxWindowIDRe.MatchString(window) {
		return nil, fmt.Errorf("invalid tmux window ID %q", window)
	}
	if err := requireRunningManagedContainer(name); err != nil {
		return nil, err
	}
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		term = "xterm-256color"
	}
	color := os.Getenv("COLORTERM")
	if color == "" {
		color = "truecolor"
	}
	args := []string{"exec", "-it", "-u", "kali", "-e", "TERM=" + term, "-e", "COLORTERM=" + color, "-e", "POMDOCK_ENGAGEMENT=" + name, name, "pomdock-tmux", "attach"}
	if window != "" {
		args = append(args, window)
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = usableTerminalEnv(os.Environ())
	return cmd, nil
}

func attachShellWindowCommand(id string) (*exec.Cmd, error) {
	if strings.HasPrefix(id, "docker:") {
		name, window, err := parseContainerShellID(id)
		if err != nil {
			return nil, err
		}
		return containerAttachCommand(name, window)
	}
	if !tmuxWindowIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid host window ID %q", id)
	}
	rows, err := listHostShellSessions()
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.ID == id {
			return attachWorkspaceWindowCommand(id), nil
		}
	}
	return nil, fmt.Errorf("no managed host terminal %s", id)
}

func KillShellSession(id string) error {
	if strings.HasPrefix(id, "docker:") {
		name, window, err := parseContainerShellID(id)
		if err != nil {
			return err
		}
		if err := requireRunningManagedContainer(name); err != nil {
			return err
		}
		_, err = runContainerTmux(name, "close", window)
		return err
	}
	if !tmuxWindowIDRe.MatchString(id) {
		return fmt.Errorf("invalid host window ID %q", id)
	}
	rows, err := listHostShellSessions()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ID == id {
			return killHostShellSession(id)
		}
	}
	return fmt.Errorf("no managed host terminal %s", id)
}

func shellReturnHint(id string) string {
	if containerShellIDRe.MatchString(id) {
		if insideTmux() {
			return "Ctrl-b Ctrl-b d detaches container tmux"
		}
		return "Ctrl-b d detaches container tmux"
	}
	return returnHint()
}
