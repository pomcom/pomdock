package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Container represents a Docker container relevant to pomdock.
type Container struct {
	Name   string
	ID     string
	Status string // "running" | "exited" | ...
	Image  string
	HasVPN bool // a gluetun sidecar is running for this engagement
	HasTor bool // a whonix/tor sidecar is running for this engagement
	Legacy bool // created before contextual prompt metadata was introduced
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
			Legacy: isLegacyContainer(ps.Labels),
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

func isLegacyContainer(labels string) bool {
	return !strings.Contains(labels, "io.pomdock.profile=kali")
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
		// The original default container predates Pomdock labels and was direct-routed.
		if name == "pcm-pentest" {
			return ContainerCreateOptions{Name: name}, true
		}
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

func RunContainerTool(name, tool string) (string, error) {
	var script string
	switch tool {
	case "identity":
		script = `printf 'Hostname: '; hostname
printf 'Addresses: '; hostname -I 2>/dev/null || true
printf 'Public egress: '
curl -fsS --max-time 10 https://ifconfig.me/ip || printf 'unavailable'
printf '\n'`
	case "ports":
		script = `printf '%-8s %s\n' PROTOCOL PORT
for entry in tcp:/proc/net/tcp tcp6:/proc/net/tcp6; do
  protocol=${entry%%:*}; file=${entry#*:}
  while read -r _ local _ state _; do
    [ "$state" = "0A" ] || continue
    port_hex=${local##*:}
    printf '%-8s %d\n' "$protocol" "$((16#$port_hex))"
  done < <(tail -n +2 "$file")
done
for entry in udp:/proc/net/udp udp6:/proc/net/udp6; do
  protocol=${entry%%:*}; file=${entry#*:}
  while read -r _ local _ state _; do
    [ "$state" = "07" ] || continue
    port_hex=${local##*:}
    printf '%-8s %d\n' "$protocol" "$((16#$port_hex))"
  done < <(tail -n +2 "$file")
done`
	case "tor":
		script = `response=$(curl -fsS --max-time 12 https://check.torproject.org/api/ip) || exit $?
if command -v jq >/dev/null 2>&1; then
  printf '%s\n' "$response" | jq -r '"Tor: \(.IsTor)\nExit IP: \(.IP)"'
else
  printf '%s\n' "$response"
fi`
	default:
		return "", fmt.Errorf("unknown engagement check %q", tool)
	}
	out, err := commandOutputWithTimeout(20*time.Second, "docker", "exec", name, "bash", "-lc", script)
	output := strings.TrimSpace(string(out))
	if tool == "identity" {
		networks, networkErr := commandOutputWithTimeout(3*time.Second, "docker", "inspect", "-f",
			`{{range $name, $net := .NetworkSettings.Networks}}{{printf "%s: %s via %s\n" $name $net.IPAddress $net.Gateway}}{{end}}`, name)
		if networkErr == nil && strings.TrimSpace(string(networks)) != "" {
			output += "\nDocker networks:\n" + strings.TrimSpace(string(networks))
		}
	}
	if tool == "ports" {
		published, portErr := commandOutputWithTimeout(3*time.Second, "docker", "port", name)
		if portErr == nil && strings.TrimSpace(string(published)) != "" {
			output += "\n\nPublished by Docker:\n" + strings.TrimSpace(string(published))
		} else if portErr != nil {
			output += "\n\nPublished by Docker: query unavailable"
		} else {
			output += "\n\nPublished by Docker: none"
		}
	}
	if err != nil {
		return output, fmt.Errorf("%s check: %w", tool, err)
	}
	return output, nil
}

func commandOutputWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return out, err
}

func CopyToContainer(name, hostSource, containerDestination string) error {
	hostSource = expandHome(hostSource)
	if _, err := os.Stat(hostSource); err != nil {
		return fmt.Errorf("source %s: %w", hostSource, err)
	}
	out, err := exec.Command("docker", "cp", hostSource, name+":"+containerDestination).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker cp: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func CopyFromContainer(name, containerSource, hostDestination string) error {
	hostDestination = expandHome(hostDestination)
	if strings.HasSuffix(hostDestination, string(os.PathSeparator)) {
		if err := os.MkdirAll(hostDestination, 0o750); err != nil {
			return fmt.Errorf("create destination: %w", err)
		}
	}
	out, err := exec.Command("docker", "cp", name+":"+containerSource, hostDestination).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker cp: %s", strings.TrimSpace(string(out)))
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
