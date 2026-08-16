package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Container represents a Docker container relevant to pomdock.
type Container struct {
	Name   string
	ID     string
	Status string // "running" | "exited" | ...
	Image  string
	HasVPN bool // a gluetun sidecar is running for this engagement
	HasTor bool // a whonix/tor sidecar is running for this engagement
}

type dockerPS struct {
	Names  string `json:"Names"`
	ID     string `json:"ID"`
	Status string `json:"Status"`
	Image  string `json:"Image"`
	Labels string `json:"Labels"`
}

type ContainerCreateOptions struct {
	Name    string
	VPNFile string
	Whonix  bool
}

// ListContainers returns all Docker containers that look like pomdock pentest containers.
func ListContainers() ([]Container, error) {
	cmd := exec.Command("docker", "ps", "-a",
		"--format", `{"Names":"{{.Names}}","ID":"{{.ID}}","Status":"{{.Status}}","Image":"{{.Image}}","Labels":{{json .Labels}}}`)
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
		if !isPentestContainer(name, ps.Image, ps.Labels) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true

		status := containerRunState(ps.Status)

		c := Container{
			Name:   name,
			ID:     ps.ID,
			Status: status,
			Image:  ps.Image,
		}

		// Check for sidecars
		gluetunName, whonixName := containerSidecarNames(name)
		if g, ok := allByName[gluetunName]; ok && containerRunState(g.Status) == "running" {
			c.HasVPN = true
		}
		if w, ok := allByName[whonixName]; ok && containerRunState(w.Status) == "running" {
			c.HasTor = true
		}

		containers = append(containers, c)
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})
	return containers, nil
}

func isPentestContainer(name, image, labels string) bool {
	return strings.Contains(labels, "io.pomdock.role=pentest") ||
		image == "pcm-kali" ||
		strings.HasPrefix(image, "pcm-kali-pentest") ||
		strings.HasPrefix(name, "pcm-pentest") ||
		strings.HasSuffix(name, "-pentest")
}

func containerSidecarNames(name string) (string, string) {
	if name == "pcm-pentest" {
		return "pcm-gluetun", "pcm-whonix"
	}
	return name + "-gluetun", name + "-whonix"
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

func StartContainer(name string) error {
	if opts, ok := createOptionsFromLabels(name); ok {
		return CreatePentestContainer(opts)
	}
	out, err := exec.Command("docker", "start", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start %s: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}

func createOptionsFromLabels(name string) (ContainerCreateOptions, bool) {
	out, err := exec.Command("docker", "inspect", "-f", "{{json .Config.Labels}}", name).Output()
	if err != nil {
		return ContainerCreateOptions{}, false
	}
	var labels map[string]string
	if err := json.Unmarshal(out, &labels); err != nil {
		return ContainerCreateOptions{}, false
	}
	return createOptionsFromLabelMap(name, labels)
}

func createOptionsFromLabelMap(name string, labels map[string]string) (ContainerCreateOptions, bool) {
	if labels["io.pomdock.role"] != "pentest" {
		return ContainerCreateOptions{}, false
	}
	route := labels["io.pomdock.route"]
	return ContainerCreateOptions{
		Name:    name,
		VPNFile: labels["io.pomdock.vpn-file"],
		Whonix:  route == "tor" || route == "tor-vpn",
	}, true
}

func RemoveContainerStack(name string) error {
	for _, candidate := range []string{name, sidecarName(name, true), sidecarName(name, false)} {
		out, err := exec.Command("docker", "rm", "-f", candidate).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "No such container") {
			return fmt.Errorf("docker rm %s: %s", candidate, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func sidecarName(name string, vpn bool) string {
	vpnName, torName := containerSidecarNames(name)
	if vpn {
		return vpnName
	}
	return torName
}

func CreatePentestContainer(opts ContainerCreateOptions) error {
	script := filepath.Join(repoRoot, "pentest.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("pentest.sh not found at %s", script)
	}
	args := []string{script, "--name", opts.Name}
	if opts.VPNFile != "" {
		args = append(args, "--vpn", opts.VPNFile)
	}
	if opts.Whonix {
		args = append(args, "--whonix")
	}
	args = append(args, "start")
	out, err := exec.Command("bash", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create %s: %s", opts.Name, strings.TrimSpace(string(out)))
	}
	return nil
}

const cwdMarker = "\x1ePOMDOCK_CWD="

func RunContainerCommand(name, cwd, command string) (string, string, error) {
	if cwd == "" {
		cwd = "/home/kali"
	}
	script := `cd -- "$1" 2>/dev/null || cd /home/kali
eval "$2"
status=$?
printf '\n\036POMDOCK_CWD=%s\n' "$PWD"
exit "$status"`
	out, err := exec.Command("docker", "exec", name, "bash", "-lc", script, "pomdock", cwd, command).CombinedOutput()
	clean, nextCWD := parseCommandOutput(string(out), cwd)
	if err != nil {
		return clean, nextCWD, fmt.Errorf("%v", err)
	}
	return clean, nextCWD, nil
}

func parseCommandOutput(output, fallbackCWD string) (string, string) {
	nextCWD := fallbackCWD
	if marker := strings.LastIndex(output, cwdMarker); marker >= 0 {
		tail := output[marker+len(cwdMarker):]
		if line, _, _ := strings.Cut(tail, "\n"); strings.TrimSpace(line) != "" {
			nextCWD = strings.TrimSpace(line)
		}
		output = output[:marker]
	}
	return strings.TrimRight(output, "\r\n"), nextCWD
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
