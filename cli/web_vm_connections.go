package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type vmConnectionView struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	Profile      string `json:"profile"`
	ProfileLabel string `json:"profileLabel"`
	IP           string `json:"ip,omitempty"`
	RDPUser      string `json:"rdpUser,omitempty"`
	RDPCommand   string `json:"rdpCommand,omitempty"`
	RDPClient    string `json:"rdpClient,omitempty"`
	RDPReason    string `json:"rdpReason,omitempty"`
	Connect      string `json:"connect,omitempty"`
}

type vmConnectionsResponse struct {
	VMs      []vmConnectionView `json:"vms"`
	Warnings []string           `json:"warnings"`
}

type vmConnectionReader func(args ...string) (string, error)

func (s *webServer) handleVMConnections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, discoverVMConnections(r.Context()))
}

// Read-only, bounded discovery is deliberately separate from ListVMs and
// PrepareVMRDP: displaying a connection must never start a guest or probe RDP.
func discoverVMConnections(parent context.Context) vmConnectionsResponse {
	result := vmConnectionsResponse{VMs: []vmConnectionView{}, Warnings: []string{}}
	binary, err := exec.LookPath("virsh")
	if err != nil {
		result.Warnings = append(result.Warnings, "VM connections unavailable: virsh is not installed")
		return result
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	read := func(args ...string) (string, error) {
		commandCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		cmd := exec.CommandContext(commandCtx, binary, append([]string{"--connect", libvirtURI}, args...)...)
		cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
		out, err := cmd.CombinedOutput()
		text := strings.TrimSpace(string(out))
		if commandCtx.Err() != nil {
			return text, commandCtx.Err()
		}
		if err != nil {
			return text, fmt.Errorf("virsh %s: %w (%s)", args[0], err, text)
		}
		return text, nil
	}
	names, err := read("list", "--all", "--name")
	if err != nil {
		result.Warnings = append(result.Warnings, "VM connections unavailable: "+err.Error())
		return result
	}
	type item struct {
		view    *vmConnectionView
		warning string
	}
	guests := vmDeleteLines(names)
	items := make([]item, len(guests))
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, name := range guests {
		if strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n") {
			result.Warnings = append(result.Warnings, "A VM name cannot be inspected safely")
			continue
		}
		workers.Add(1)
		go func(i int, name string) {
			defer workers.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			items[i].view, items[i].warning = readVMConnection(name, read)
		}(i, name)
	}
	workers.Wait()
	for _, item := range items {
		if item.view != nil {
			result.VMs = append(result.VMs, *item.view)
		}
		if item.warning != "" {
			result.Warnings = append(result.Warnings, item.warning)
		}
	}
	sort.Slice(result.VMs, func(i, j int) bool { return result.VMs[i].Name < result.VMs[j].Name })
	sort.Strings(result.Warnings)
	return result
}

func readVMConnection(name string, read vmConnectionReader) (*vmConnectionView, string) {
	metadata, err := read("metadata", name, guestMetadataURI, "--config")
	if err != nil {
		message := strings.ToLower(metadata)
		if strings.Contains(message, "metadata not found") || strings.Contains(message, "metadata element is not present") {
			return nil, ""
		}
		return nil, "Some VM profile metadata could not be read: " + err.Error()
	}
	var assigned guestMetadata
	if xml.Unmarshal([]byte(metadata), &assigned) != nil || assigned.XMLName.Space != guestMetadataURI || assigned.Profile == "" || assigned.Profile == fallbackGuestProfileID {
		return nil, ""
	}
	view := &vmConnectionView{Name: name, State: "unknown", Profile: assigned.Profile, ProfileLabel: "Unknown profile"}
	state, stateErr := read("domstate", name)
	warning := ""
	if stateErr == nil && state != "" {
		view.State = state
	} else {
		warning = "VM state unavailable for " + name
	}
	profile, known := guestProfiles[assigned.Profile]
	if !known {
		view.RDPReason = "Unknown assigned profile; RDP connection is unavailable"
		return view, warning
	}
	view.ProfileLabel = profile.Label
	if !profile.SupportsRDP {
		view.RDPReason = "This profile does not provide RDP"
		return view, warning
	}
	view.RDPUser = profile.RDPUser
	view.Connect = "pomdock vm rdp " + shellQuote(name)
	if stateErr != nil || state == "" {
		view.RDPReason = "VM state is unavailable"
		return view, warning
	}
	if view.State != "running" {
		view.RDPReason = "VM is not running; no current management address is available"
		return view, warning
	}
	if profile.RDPUser == "" {
		view.RDPReason = "The profile has no RDP username"
		return view, warning
	}
	ip, reason := readVMManagementIPv4(name, read)
	if ip == "" {
		view.RDPReason = reason
		return view, warning
	}
	view.IP = ip
	command, err := rdpClientCommand(profile, ip)
	if err != nil {
		view.RDPReason = err.Error()
		return view, warning
	}
	view.RDPClient = command.Args[0]
	view.RDPCommand = shellJoin(command.Args)
	return view, warning
}

func managementInterface(output string) (mac, network string) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "Interface" || strings.HasPrefix(fields[0], "---") {
			continue
		}
		if len(fields) < 5 {
			return "", ""
		}
		parsed, err := net.ParseMAC(fields[len(fields)-1])
		if err != nil || len(parsed) != 6 {
			return "", ""
		}
		if fields[1] == "network" && len(fields) == 5 && fields[2] != "-" && !strings.HasPrefix(fields[2], "-") {
			network = fields[2]
		}
		return parsed.String(), network
	}
	return "", ""
}

// Matching the management MAC avoids choosing an internal Whonix/tunnel NIC.
func managementIPv4s(output, mac string) []string {
	unique := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		matching := false
		for _, field := range fields {
			if parsed, err := net.ParseMAC(field); err == nil && parsed.String() == mac {
				matching = true
				break
			}
		}
		if !matching {
			continue
		}
		for i, field := range fields {
			if field != "ipv4" || i+1 >= len(fields) {
				continue
			}
			address := strings.SplitN(fields[i+1], "/", 2)[0]
			ip := net.ParseIP(address)
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() {
				unique[ip.String()] = true
			}
		}
	}
	addresses := []string{}
	for ip := range unique {
		addresses = append(addresses, ip)
	}
	sort.Strings(addresses)
	return addresses
}

func readVMManagementIPv4(name string, read vmConnectionReader) (string, string) {
	interfaces, err := read("domiflist", name)
	if err != nil {
		return "", "Management interface metadata is unavailable"
	}
	mac, network := managementInterface(interfaces)
	if mac == "" {
		return "", "No unambiguous management interface is available"
	}
	queries := [][]string{{"domifaddr", name}}
	if network != "" {
		queries = append(queries, []string{"net-dhcp-leases", network, "--mac", mac})
	}
	queries = append(queries, []string{"domifaddr", name, "--source", "agent"})
	for _, query := range queries {
		output, err := read(query...)
		if err != nil {
			continue
		}
		addresses := managementIPv4s(output, mac)
		if len(addresses) > 1 {
			return "", "Multiple management IPv4 addresses were reported; choose the address manually"
		}
		if len(addresses) == 1 {
			return addresses[0], ""
		}
	}
	return "", "No current IPv4 address was reported for the management interface"
}
