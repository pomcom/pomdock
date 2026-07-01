package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	snapshotName = "post-setup"
	imageDir     = "/var/lib/libvirt/images"
	libvirtURI   = "qemu:///system"
)

type VM struct {
	Name      string
	State     string
	IP        string
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
	return strings.Contains(out, "Whonix-Internal")
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
func StopVM(name string) error    { _, err := virsh("shutdown", name); return err }
func ForceOffVM(name string) error { _, err := virsh("destroy", name); return err }

func DeleteVM(name string) error {
	_, _ = virsh("destroy", name)
	if _, err := virsh("undefine", name, "--remove-all-storage"); err != nil {
		_, err2 := virsh("undefine", name)
		return err2
	}
	return nil
}

func RevertSnapshot(name, snap string) error {
	_, err := virsh("snapshot-revert", name, snap)
	return err
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
	args := []string{"attach-interface", name, "network", "Whonix-Internal",
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
		if strings.Contains(line, "Whonix-Internal") {
			if f := strings.Fields(line); len(f) >= 5 {
				mac = f[4]
				break
			}
		}
	}
	if mac == "" {
		return fmt.Errorf("no Whonix-Internal NIC on %s", name)
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
