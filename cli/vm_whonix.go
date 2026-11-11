package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const whonixAllocationURI = "https://pomdock.dev/xmlns/whonix/1.0"

type whonixAllocation struct {
	XMLName xml.Name `xml:"https://pomdock.dev/xmlns/whonix/1.0 route"`
	Address string   `xml:"address,attr"`
	MAC     string   `xml:"mac,attr,omitempty"`
	Network string   `xml:"network,attr"`
}

type whonixInterface struct {
	MAC struct {
		Address string `xml:"address,attr"`
	} `xml:"mac"`
	Source struct {
		Network string `xml:"network,attr"`
	} `xml:"source"`
	IPs []struct {
		Address string `xml:"address,attr"`
	} `xml:"ip"`
}

type whonixDomain struct {
	Name     string `xml:"name"`
	Metadata struct {
		Route *whonixAllocation `xml:"https://pomdock.dev/xmlns/whonix/1.0 route"`
	} `xml:"metadata"`
	Interfaces []whonixInterface `xml:"devices>interface"`
}

func readWhonixDomain(name string) (whonixDomain, error) {
	var domain whonixDomain
	out, err := virsh("dumpxml", name, "--inactive")
	if err != nil {
		return domain, fmt.Errorf("read %s interfaces: %w", name, err)
	}
	if err = xml.Unmarshal([]byte(out), &domain); err != nil {
		return domain, err
	}
	// Include transient live NICs as well as persistent configuration. Domain
	// metadata is read from --inactive because reservations are stored in config.
	live, err := virsh("dumpxml", name)
	if err != nil {
		return domain, fmt.Errorf("read %s live interfaces: %w", name, err)
	}
	var current whonixDomain
	if err = xml.Unmarshal([]byte(live), &current); err != nil {
		return domain, err
	}
	for _, nic := range current.Interfaces {
		found := false
		for _, saved := range domain.Interfaces {
			if strings.EqualFold(saved.MAC.Address, nic.MAC.Address) && saved.Source.Network == nic.Source.Network {
				found = true
				break
			}
		}
		if !found {
			domain.Interfaces = append(domain.Interfaces, nic)
		}
	}
	return domain, nil
}

func whonixNICs(domain whonixDomain) (management, tor string, err error) {
	for _, nic := range domain.Interfaces {
		if _, parseErr := net.ParseMAC(nic.MAC.Address); parseErr != nil {
			continue
		}
		if nic.Source.Network == whonixNetwork {
			if tor != "" {
				return "", "", fmt.Errorf("%s has multiple Whonix NICs", domain.Name)
			}
			tor = strings.ToLower(nic.MAC.Address)
		} else if management == "" {
			management = strings.ToLower(nic.MAC.Address)
		}
	}
	if management == "" {
		return "", "", fmt.Errorf("%s has no management NIC", domain.Name)
	}
	return management, tor, nil
}

func whonixObservedAddresses(output, mac string) []string {
	addresses := []string{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.EqualFold(fields[1], mac) && fields[2] == "ipv4" {
			address, _, err := net.ParseCIDR(fields[3])
			if err == nil {
				addresses = append(addresses, address.String())
			}
		}
	}
	return addresses
}

func chooseWhonixAddress(name, gateway, preferred string, used map[string]string) (string, error) {
	gw := net.ParseIP(gateway).To4()
	if gw == nil {
		return "", fmt.Errorf("invalid Whonix IPv4 gateway")
	}
	free := func(address string) bool { return address != gateway && (used[address] == "" || used[address] == name) }
	if preferred != "" {
		ip := net.ParseIP(preferred).To4()
		if ip == nil || ip[0] != gw[0] || ip[1] != gw[1] || ip[2] != gw[2] || ip[3] < 100 || ip[3] > 254 {
			return "", fmt.Errorf("invalid saved Whonix address %q for %s", preferred, name)
		}
		if !free(preferred) {
			return "", fmt.Errorf("Whonix address %s is also used by %s", preferred, used[preferred])
		}
		return preferred, nil
	}
	start := int(crc32.ChecksumIEEE([]byte(name)) % 155)
	for n := 0; n < 155; n++ {
		address := fmt.Sprintf("%d.%d.%d.%d", gw[0], gw[1], gw[2], 100+(start+n)%155)
		if free(address) {
			return address, nil
		}
	}
	return "", fmt.Errorf("Whonix workstation address range is full")
}

// Reservations are separate from guest-profile metadata. The local lock also
// serializes simultaneous CLI calls before their domain metadata is committed.
func reserveWhonixAddress(name string) (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(config, "pomdock")
	if err = os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	lock, err := os.OpenFile(filepath.Join(directory, "whonix-allocation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	names, err := virsh("list", "--all", "--name")
	if err != nil {
		return "", err
	}
	used := map[string]string{WhonixGatewayIP(): "Whonix-Gateway"}
	preferred := ""
	claim := func(address, owner string) error {
		if net.ParseIP(address).To4() == nil {
			return fmt.Errorf("invalid Whonix address metadata on %s", owner)
		}
		if other := used[address]; other != "" && other != owner {
			return fmt.Errorf("Whonix address collision: %s is claimed by %s and %s", address, other, owner)
		}
		used[address] = owner
		return nil
	}
	for _, other := range strings.Fields(names) {
		domain, err := readWhonixDomain(other)
		if err != nil {
			return "", err
		}
		allocation := domain.Metadata.Route
		reserved := false
		if allocation != nil && allocation.Network == whonixNetwork && allocation.Address != "" {
			if err = claim(allocation.Address, other); err != nil {
				return "", err
			}
			reserved = true
			if other == name {
				preferred = allocation.Address
			}
		}
		torCount := 0
		for _, nic := range domain.Interfaces {
			if nic.Source.Network == whonixNetwork {
				torCount++
			}
		}
		if torCount > 1 {
			return "", fmt.Errorf("%s has multiple Whonix NICs; cannot verify its addressing", other)
		}
		for _, nic := range domain.Interfaces {
			if nic.Source.Network != whonixNetwork {
				continue
			}
			known := reserved && (allocation.MAC == "" || strings.EqualFold(allocation.MAC, nic.MAC.Address))
			for _, ip := range nic.IPs {
				if err = claim(ip.Address, other); err != nil {
					return "", err
				}
				known = true
			}
			// The agent may be absent, particularly for stopped VMs. Saved allocation
			// metadata then remains authoritative; unknown legacy NICs are not guessed.
			out, _ := virsh("domifaddr", other, "--source", "agent")
			for _, address := range whonixObservedAddresses(out, nic.MAC.Address) {
				if err = claim(address, other); err != nil {
					return "", err
				}
				known = true
			}
			if other != name && other != "Whonix-Gateway" && !known {
				return "", fmt.Errorf("cannot verify %s's existing Whonix address; restore/detach its legacy Whonix configuration before allocating another VM", other)
			}
		}
	}
	address, err := chooseWhonixAddress(name, WhonixGatewayIP(), preferred, used)
	if err != nil {
		return "", err
	}
	if err = saveWhonixAllocation(name, address, ""); err != nil {
		return "", err
	}
	return address, nil
}

func saveWhonixAllocation(name, address, mac string) error {
	allocation := whonixAllocation{XMLName: xml.Name{Space: whonixAllocationURI, Local: "route"}, Address: address, MAC: mac, Network: whonixNetwork}
	payload, err := xml.Marshal(allocation)
	if err != nil {
		return err
	}
	out, err := virsh("metadata", name, whonixAllocationURI, "--config", "--key", "pomdock-whonix", "--set", string(payload))
	if err != nil {
		return fmt.Errorf("save Whonix address for %s: %w: %s", name, err, out)
	}
	return nil
}

func whonixManagementIP(name, mac string) (string, error) {
	if out, err := virsh("domifaddr", name, "--source", "agent"); err == nil {
		if addresses := whonixObservedAddresses(out, mac); len(addresses) > 0 {
			return addresses[0], nil
		}
	}
	if address := dhcpSearch(mac); address != "" {
		return address, nil
	}
	if out, err := exec.Command("ip", "neigh").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.Contains(strings.ToLower(line), strings.ToLower(mac)) && net.ParseIP(fields[0]).To4() != nil {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("management IPv4 address unavailable for %s", name)
}

func whonixSSH(ctx context.Context, profile GuestProfile, ip, script string, stdout, stderr io.Writer) error {
	args := []string{}
	binary := "ssh"
	if key := profileSSHKey(profile); regularFile(key) {
		args = append(args, "-i", key, "-o", "BatchMode=yes")
	} else if profile.SSHUser == "kali" {
		if path, err := exec.LookPath("sshpass"); err == nil {
			binary = path
			args = append(args, "-p", "kali", "ssh")
		}
	}
	args = append(args, "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=10", "-o", "ServerAliveCountMax=2", profile.SSHUser+"@"+ip, "bash", "-s")
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("guest SSH/configuration failed for %s: %w", ip, err)
	}
	return nil
}

func prepareWhonixManagement(ctx context.Context, name string) (GuestProfile, string, string, error) {
	profile := GuestProfileByID(GetVMProfileID(name))
	if !profile.SupportsWhonix {
		return profile, "", "", fmt.Errorf("Whonix routing is not supported for the %s profile", profile.Label)
	}
	domain, err := readWhonixDomain(name)
	if err != nil {
		return profile, "", "", err
	}
	management, _, err := whonixNICs(domain)
	if err != nil {
		return profile, "", "", err
	}
	state, err := GetVMState(name)
	if err != nil {
		return profile, "", "", err
	}
	if state != "running" {
		if err = StartVM(name); err != nil {
			return profile, "", "", err
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var last error
	for {
		address, err := whonixManagementIP(name, management)
		if err == nil {
			probe, cancel := context.WithTimeout(waitCtx, 8*time.Second)
			err = whonixSSH(probe, profile, address, "true\n", io.Discard, io.Discard)
			cancel()
			if err == nil {
				return profile, address, management, nil
			}
		}
		last = err
		select {
		case <-waitCtx.Done():
			return profile, "", "", fmt.Errorf("waiting for management SSH: %w (%v)", waitCtx.Err(), last)
		case <-time.After(3 * time.Second):
		}
	}
}

func configureVMWhonix(ctx context.Context, name string, stdout, stderr io.Writer) error {
	if !NetworkExists(whonixNetwork) {
		return fmt.Errorf("%s not found — run: pomdock vm whonix-gateway", whonixNetwork)
	}
	state, _ := GetVMState("Whonix-Gateway")
	if state != "running" {
		return fmt.Errorf("Whonix-Gateway is not running; start it: pomdock vm start Whonix-Gateway")
	}
	profile, managementIP, managementMAC, err := prepareWhonixManagement(ctx, name)
	if err != nil {
		return err
	}
	address, err := reserveWhonixAddress(name)
	if err != nil {
		return err
	}
	domain, err := readWhonixDomain(name)
	if err != nil {
		return err
	}
	_, torMAC, err := whonixNICs(domain)
	if err != nil {
		return err
	}
	if torMAC == "" {
		if err = AttachWhonixNIC(name); err != nil {
			return err
		}
	}
	domain, err = readWhonixDomain(name)
	if err != nil {
		return err
	}
	_, torMAC, err = whonixNICs(domain)
	if err != nil {
		return err
	}
	if torMAC == "" {
		return fmt.Errorf("Whonix attachment did not expose a NIC MAC")
	}
	if err = saveWhonixAllocation(name, address, torMAC); err != nil {
		return err
	}
	script, err := buildWhonixRoutingScript(WhonixGatewayIP(), address, torMAC, managementMAC, managementIP)
	if err != nil {
		return err
	}
	configCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err = whonixSSH(configCtx, profile, managementIP, script, stdout, stderr); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Whonix IPv4 default route verified for %s (%s via %s); no IPv6 default route. Management: %s\n", name, address, WhonixGatewayIP(), managementIP)
	return nil
}

func restoreVMWhonix(ctx context.Context, name string, stdout, stderr io.Writer) error {
	domain, err := readWhonixDomain(name)
	if err != nil {
		return err
	}
	_, torMAC, err := whonixNICs(domain)
	if err != nil {
		return err
	}
	if torMAC == "" {
		fmt.Fprintf(stdout, "%s has no Whonix NIC.\n", name)
		return nil
	}
	profile, managementIP, managementMAC, err := prepareWhonixManagement(ctx, name)
	if err != nil {
		return err
	}
	script, err := buildWhonixRestoreScript(managementMAC)
	if err != nil {
		return err
	}
	configCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err = whonixSSH(configCtx, profile, managementIP, script, stdout, stderr); err != nil {
		return err
	}
	if err = DetachWhonixNIC(name); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Whonix NIC removed from %s; management routing restored.\n", name)
	return nil
}
