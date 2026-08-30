package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	snapshotName  = "post-setup"
	imageDir      = "/var/lib/libvirt/images"
	libvirtURI    = "qemu:///system"
	whonixNetwork = "Whonix-Internal"
)

type VM struct {
	Name      string
	State     string
	IP        string
	ProfileID string
	HasWhonix bool
}

func virsh(args ...string) (string, error) {
	cmd := exec.Command("virsh", append([]string{"--connect", libvirtURI}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func ListVMs() ([]VM, error) {
	out, err := virsh("list", "--all", "--name")
	if err != nil {
		return nil, fmt.Errorf("virsh list: %w", err)
	}
	var vms []VM
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		state, _ := GetVMState(name)
		ip := ""
		if state == "running" {
			ip, _ = GetVMIP(name)
		}
		vms = append(vms, VM{
			Name:      name,
			State:     state,
			IP:        ip,
			ProfileID: GetVMProfileID(name),
			HasWhonix: vmHasWhonixNIC(name),
		})
	}
	return vms, nil
}

func GetVMState(name string) (string, error) {
	out, err := virsh("domstate", name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func GetVMIP(name string) (string, error) {
	if out, err := virsh("domifaddr", name); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "ipv4") {
				for _, f := range strings.Fields(line) {
					if strings.Contains(f, ".") && strings.Contains(f, "/") {
						return strings.Split(f, "/")[0], nil
					}
				}
			}
		}
	}
	mac := vmMAC(name)
	if mac != "" {
		if ip := dhcpSearch(mac); ip != "" {
			return ip, nil
		}
		if out, err := exec.Command("ip", "neigh").CombinedOutput(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(strings.ToLower(line), strings.ToLower(mac)) {
					if f := strings.Fields(line); len(f) > 0 {
						return f[0], nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("no IP for %s", name)
}

func vmMAC(name string) string {
	out, _ := virsh("domiflist", name)
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 5 && strings.Contains(f[4], ":") {
			return f[4]
		}
	}
	return ""
}

func dhcpSearch(mac string) string {
	out, err := virsh("net-list", "--name")
	if err != nil {
		return ""
	}
	for _, net := range strings.Split(out, "\n") {
		net = strings.TrimSpace(net)
		if net == "" {
			continue
		}
		leases, err := virsh("net-dhcp-leases", net)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(leases, "\n") {
			if strings.Contains(strings.ToLower(line), strings.ToLower(mac)) {
				for _, f := range strings.Fields(line) {
					if strings.Contains(f, ".") && strings.Contains(f, "/") {
						return strings.Split(f, "/")[0]
					}
				}
			}
		}
	}
	return ""
}

func vmHasWhonixNIC(name string) bool {
	out, _ := virsh("domiflist", name)
	return strings.Contains(out, whonixNetwork)
}

func VMExists(name string) bool {
	_, err := virsh("dominfo", name)
	return err == nil
}

func NetworkExists(net string) bool {
	_, err := virsh("net-info", net)
	return err == nil
}

// WhonixGatewayIP returns the Whonix Gateway's internal IP.
// This is hardcoded in all Whonix versions — the firewall blocks ARP from the host,
// so runtime detection via virbr2 doesn't work.
func WhonixGatewayIP() string {
	return "10.152.152.10"
}

func StartVM(name string) error {
	state, _ := GetVMState(name)
	if state == "paused" {
		_, err := virsh("resume", name)
		return err
	}
	_, err := virsh("start", name)
	return err
}
func StopVM(name string) error     { _, err := virsh("shutdown", name); return err }
func ForceOffVM(name string) error { _, err := virsh("destroy", name); return err }

func DeleteVM(name string) error {
	_, _ = virsh("destroy", name)

	// --remove-all-storage only works for storage libvirt itself manages
	// (a storage pool volume). Our disks are plain qcow2 files created
	// directly by the provisioning scripts, so libvirt refuses to touch
	// them ("not managed by libvirt") — remove the disk file ourselves.
	// --snapshots-metadata is required too: undefine otherwise refuses any
	// domain with a snapshot, and every profile leaves a post-setup one.
	if _, err := virsh("undefine", name, "--snapshots-metadata", "--nvram"); err != nil {
		return err
	}

	disk := filepath.Join(imageDir, name+".qcow2")
	if err := os.Remove(disk); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		// Disk is chown'd to libvirt-qemu at creation time, so a plain
		// remove needs elevated rights. Only try non-interactively —
		// this can run from the TUI, which has no terminal to prompt on.
		if _, sudoErr := exec.Command("sudo", "-n", "rm", "-f", disk).CombinedOutput(); sudoErr != nil {
			return fmt.Errorf("undefined '%s' but could not remove disk %s (owned by libvirt-qemu): remove it manually with sudo", name, disk)
		}
	}
	return nil
}

func RevertSnapshot(name, snap string) error {
	_, err := virsh("snapshot-revert", name, snap)
	return err
}

func FinalizeWindowsVM(name, snap string) error {
	state, err := GetVMState(name)
	if err != nil {
		return err
	}
	if state != "shut off" {
		return fmt.Errorf("shut down Windows before finalizing (current state: %s)", state)
	}
	if snapshots, _ := virsh("snapshot-list", name, "--name"); slicesContainLine(snapshots, snap) {
		return fmt.Errorf("snapshot %q already exists", snap)
	}

	blocks, err := virsh("domblklist", name, "--details")
	if err != nil {
		return fmt.Errorf("list VM media: %w", err)
	}
	for _, line := range strings.Split(blocks, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[1] != "cdrom" {
			continue
		}
		if out, err := virsh("detach-disk", name, fields[2], "--config"); err != nil {
			return fmt.Errorf("detach installer media %s: %w: %s", fields[2], err, out)
		}
	}
	if out, err := virsh("snapshot-create-as", name, snap,
		"--description", "Windows installation ready", "--atomic"); err != nil {
		return fmt.Errorf("create snapshot: %w: %s", err, out)
	}
	if err := StartVM(name); err != nil {
		return fmt.Errorf("snapshot created, but VM could not be restarted: %w", err)
	}
	return nil
}

func slicesContainLine(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func CloneVM(src, dst string) error {
	cmd := exec.Command("virt-clone", "--connect", libvirtURI,
		"--original", src, "--name", dst,
		"--file", fmt.Sprintf("%s/%s.qcow2", imageDir, dst))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("virt-clone: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func AttachWhonixNIC(name string) error {
	state, _ := GetVMState(name)
	args := []string{"attach-interface", name, "network", whonixNetwork,
		"--model", "virtio", "--persistent"}
	if state != "running" {
		args = append(args, "--config")
	}
	_, err := virsh(args...)
	return err
}

func DetachWhonixNIC(name string) error {
	out, _ := virsh("domiflist", name)
	var mac string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, whonixNetwork) {
			if f := strings.Fields(line); len(f) >= 5 {
				mac = f[4]
				break
			}
		}
	}
	if mac == "" {
		return fmt.Errorf("no %s NIC on %s", whonixNetwork, name)
	}
	state, _ := GetVMState(name)
	args := []string{"detach-interface", name, "network", "--mac", mac, "--persistent"}
	if state != "running" {
		args = append(args, "--config")
	}
	_, err := virsh(args...)
	return err
}

func WaitForVMIP(name string, timeout time.Duration) (string, error) {
	dl := time.Now().Add(timeout)
	for time.Now().Before(dl) {
		if ip, err := GetVMIP(name); err == nil && ip != "" {
			return ip, nil
		}
		time.Sleep(3 * time.Second)
	}
	return "", fmt.Errorf("timeout waiting for IP of %s", name)
}

func WaitForTCP(ip string, port int, timeout time.Duration) error {
	return waitForTCP(ip, port, timeout, func(address string, dialTimeout time.Duration) error {
		conn, err := net.DialTimeout("tcp", address, dialTimeout)
		if err != nil {
			return err
		}
		return conn.Close()
	})
}

func waitForTCP(ip string, port int, timeout time.Duration, probe func(string, time.Duration) error) error {
	address := net.JoinHostPort(ip, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := probe(address, 2*time.Second); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s", address)
}

func PrepareVMRDP(name string, timeout time.Duration) (*exec.Cmd, string, error) {
	profile := GuestProfileByID(GetVMProfileID(name))
	if !profile.SupportsRDP {
		return nil, "", fmt.Errorf("%s profile does not provide RDP", profile.Label)
	}
	state, err := GetVMState(name)
	if err != nil {
		return nil, "", err
	}
	if state != "running" {
		if err := StartVM(name); err != nil {
			return nil, "", fmt.Errorf("start VM: %w", err)
		}
	}
	ip, err := WaitForVMIP(name, timeout)
	if err != nil {
		return nil, "", err
	}
	if err := WaitForTCP(ip, 3389, timeout); err != nil {
		return nil, "", fmt.Errorf("RDP service is not ready: %w", err)
	}
	cmd, err := rdpClientCommand(profile, ip)
	return cmd, ip, err
}

func rdpClientCommand(profile GuestProfile, ip string) (*exec.Cmd, error) {
	preference := strings.ToLower(strings.TrimSpace(os.Getenv("POMDOCK_RDP_CLIENT")))
	if preference != "" && preference != "xfreerdp" && preference != "xfreerdp3" && preference != "remmina" {
		return nil, fmt.Errorf("unsupported POMDOCK_RDP_CLIENT %q", preference)
	}

	clients := []string{"xfreerdp3", "xfreerdp", "remmina"}
	if preference != "" {
		clients = []string{preference}
	}
	for _, client := range clients {
		if _, err := exec.LookPath(client); err != nil {
			continue
		}
		if client == "remmina" {
			return exec.Command("remmina", "--no-tray-icon", "--disable-news", "--disable-stats",
				"-c", fmt.Sprintf("rdp://%s@%s", profile.RDPUser, ip)), nil
		}
		return exec.Command(client, "/v:"+ip, "/u:"+profile.RDPUser,
			"/dynamic-resolution", "/gfx:avc444", "+clipboard", "/cert:tofu", "/log-level:ERROR"), nil
	}
	if preference != "" {
		return nil, fmt.Errorf("configured RDP client %q not found", preference)
	}
	return nil, fmt.Errorf("no RDP client found; install FreeRDP or Remmina")
}

func isFreeRDPCommand(cmd *exec.Cmd) bool {
	name := filepath.Base(cmd.Path)
	return name == "xfreerdp" || name == "xfreerdp3"
}

func launchDesktopClient(cmd *exec.Cmd, startupTimeout time.Duration) error {
	logFile, err := os.CreateTemp("", "pomdock-rdp-*.log")
	if err != nil {
		return err
	}
	logPath := logFile.Name()
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		_ = os.Remove(logPath)
		return err
	}

	done := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		_ = logFile.Close()
		if waitErr != nil {
			output, _ := os.ReadFile(logPath)
			message := strings.TrimSpace(string(output))
			if message != "" {
				waitErr = fmt.Errorf("%w: %s", waitErr, message)
			}
		}
		_ = os.Remove(logPath)
		done <- waitErr
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(startupTimeout):
		return nil
	}
}

func vmNames() []string {
	out, err := virsh("list", "--all", "--name")
	if err != nil {
		return nil
	}
	var names []string
	for _, n := range strings.Split(out, "\n") {
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}
