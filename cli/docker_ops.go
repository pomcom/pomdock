package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Container represents a Docker container relevant to pomdock.
type Container struct {
	Name    string
	ID      string
	Status  string // "running" | "exited" | ...
	Image   string
	HasVPN  bool // a gluetun sidecar is running for this engagement
	HasTor  bool // a whonix/tor sidecar is running for this engagement
}

type dockerPS struct {
	Names  string `json:"Names"`
	ID     string `json:"ID"`
	Status string `json:"Status"`
	Image  string `json:"Image"`
}

// ListContainers returns all Docker containers that look like pomdock pentest containers.
func ListContainers() ([]Container, error) {
	cmd := exec.Command("docker", "ps", "-a",
		"--format", `{"Names":"{{.Names}}","ID":"{{.ID}}","Status":"{{.Status}}","Image":"{{.Image}}"}`)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}

	// Collect all containers first
	allByName := map[string]dockerPS{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ps dockerPS
		if err := json.Unmarshal([]byte(line), &ps); err != nil {
			continue
		}
		allByName[ps.Names] = ps
	}

	// Extract pentest containers (image pcm-kali or name *-pentest / pcm-pentest)
	var containers []Container
	seen := map[string]bool{}
	for name, ps := range allByName {
		if !isPentestContainer(name, ps.Image) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true

		engagement := engagementName(name)
		status := containerRunState(ps.Status)

		c := Container{
			Name:   name,
			ID:     ps.ID,
			Status: status,
			Image:  ps.Image,
		}

		// Check for sidecars
		gluetunName := engagement + "-gluetun"
		if engagement == "pcm" {
			gluetunName = "pcm-gluetun"
		}
		whonixName := engagement + "-whonix"
		if engagement == "pcm" {
			whonixName = "pcm-whonix"
		}
		if g, ok := allByName[gluetunName]; ok && containerRunState(g.Status) == "running" {
			c.HasVPN = true
		}
		if w, ok := allByName[whonixName]; ok && containerRunState(w.Status) == "running" {
			c.HasTor = true
		}

		containers = append(containers, c)
	}
	return containers, nil
}

func isPentestContainer(name, image string) bool {
	return image == "pcm-kali" ||
		strings.HasPrefix(name, "pcm-pentest") ||
		strings.HasSuffix(name, "-pentest")
}

func engagementName(name string) string {
	if name == "pcm-pentest" {
		return "pcm"
	}
	return strings.TrimSuffix(name, "-pentest")
}

func containerRunState(status string) string {
	s := strings.ToLower(status)
	if strings.HasPrefix(s, "up") {
		return "running"
	}
	return "exited"
}

func ExecInContainer(name string) error {
	cmd := exec.Command("docker", "exec", "-it", name, "bash", "-l")
	return runInteractive(cmd)
}

func StopContainer(name string) error {
	out, err := exec.Command("docker", "stop", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop %s: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

func RemoveContainer(name string) error {
	out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm %s: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

func ContainerState(name string) string {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", name).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func containerNames() []string {
	out, err := exec.Command("docker", "ps", "-a", "--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, n := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}
