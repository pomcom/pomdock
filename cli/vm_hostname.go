package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var hostnameSeparators = regexp.MustCompile(`[^a-z0-9-]+`)

// Match Docker and fresh-VM instance labels without changing the libvirt name.
func instanceHostname(name string) string {
	hostname := strings.Trim(hostnameSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(hostname) > 63 {
		hostname = strings.TrimRight(hostname[:63], "-")
	}
	if hostname == "" {
		return "vm"
	}
	return hostname
}

func customizeCloneHostname(name, customizer string) error {
	state, err := GetVMState(name)
	if err != nil {
		return fmt.Errorf("clone %q was created but its stopped state could not be verified: %w", name, err)
	}
	if state != "shut off" {
		return fmt.Errorf("clone %q was created but hostname customization requires it to be shut off (state: %s)", name, state)
	}
	hostname := instanceHostname(name)
	// The clone owns these changes. No seed ISO, source disk, SSH identity,
	// machine-id, account, profile metadata or snapshot is rewritten here.
	cloudConfig := "/etc/cloud/cloud.cfg.d/99-pomdock-hostname.cfg:# Preserve this clone's instance hostname.\npreserve_hostname: true\nhostname: " + hostname + "\n"
	cmd := exec.Command(customizer, "--connect", libvirtURI, "--domain", name, "--no-network",
		"--hostname", hostname, "--mkdir", "/etc/cloud/cloud.cfg.d", "--write", cloudConfig)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone %q was created but hostname customization failed; it remains available for inspection: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
