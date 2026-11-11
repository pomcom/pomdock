package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type vmConnectionFixture struct {
	Profile    string `json:"profile"`
	State      string `json:"state"`
	IP         string `json:"ip"`
	MAC        string `json:"mac"`
	Namespace  string `json:"namespace,omitempty"`
	Interfaces string `json:"interfaces,omitempty"`
	Addresses  string `json:"addresses,omitempty"`
	Leases     string `json:"leases,omitempty"`
	Agent      string `json:"agent,omitempty"`
	StateError bool   `json:"stateError,omitempty"`
}

func vmConnectionsFixture(t *testing.T, guests map[string]vmConnectionFixture, clients ...string) (func() [][]string, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.jsonl")
	ran := filepath.Join(bin, "client-ran")
	for name, guest := range guests {
		if guest.MAC == "" {
			guest.MAC = "52:54:00:00:00:01"
		}
		guests[name] = guest
	}
	data, _ := json.Marshal(guests)
	t.Setenv("POMDOCK_VM_CONNECTION_FIXTURES", string(data))
	t.Setenv("POMDOCK_VM_CONNECTION_CALLS", log)
	t.Setenv("POMDOCK_VM_CONNECTION_CLIENT_RAN", ran)
	t.Setenv("POMDOCK_VM_CONNECTION_STALL", "")
	t.Setenv("POMDOCK_RDP_CLIENT", "")
	source := `import json, os, sys, time
args=sys.argv[1:]
assert args[:2]==['--connect','qemu:///system'];args=args[2:]
with open(os.environ['POMDOCK_VM_CONNECTION_CALLS'],'a') as log:log.write(json.dumps(args)+'\n')
guests=json.loads(os.environ['POMDOCK_VM_CONNECTION_FIXTURES'])
command=args[0]
if command=='list':print('\n'.join(sorted(guests)));sys.exit(0)
if command=='net-dhcp-leases':
 mac=args[args.index('--mac')+1]
 for guest in guests.values():
  if guest['mac']==mac and guest.get('leases'):print(guest['leases'])
 sys.exit(0)
guest=guests[args[1]]
if command=='metadata':
 if os.environ.get('POMDOCK_VM_CONNECTION_STALL'):time.sleep(3)
 if not guest['profile']:print('error: metadata not found: Requested metadata element is not present',file=sys.stderr);sys.exit(1)
 namespace=guest.get('namespace','https://pomdock.dev/xmlns/domain/1.0')
 print('<guest xmlns="'+namespace+'" profile="'+guest['profile']+'"/>')
elif command=='domstate':
 if guest.get('stateError'):sys.exit(4)
 print(guest['state'])
elif command=='domiflist':print(guest.get('interfaces') or ('Interface Type Source Model MAC\n-------------------------------\nvnet0 network default virtio '+guest['mac']))
elif command=='domifaddr':
 if '--source' in args:print(guest.get('agent',''))
 elif 'addresses' in guest:print(guest['addresses'])
 elif guest['ip']:print('Name MAC Protocol Address\n-------------------------\nvnet0 '+guest['mac']+' ipv4 '+guest['ip']+'/24')
elif command not in ('metadata','domstate','domiflist','domifaddr'):sys.exit(99)
`
	if err := os.WriteFile(filepath.Join(bin, "virsh"), []byte("#!"+python+"\n"+source), 0700); err != nil {
		t.Fatal(err)
	}
	for _, client := range clients {
		if err := os.WriteFile(filepath.Join(bin, client), []byte("#!/bin/sh\nprintf executed > \"$POMDOCK_VM_CONNECTION_CLIENT_RAN\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return func() [][]string {
		raw, _ := os.ReadFile(log)
		var calls [][]string
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var call []string
			if err := json.Unmarshal([]byte(line), &call); err != nil {
				t.Fatal(err)
			}
			calls = append(calls, call)
		}
		return calls
	}, ran
}

func TestVMConnectionsAssignedProfilesUseSharedClientWithoutExecuting(t *testing.T) {
	calls, ran := vmConnectionsFixture(t, map[string]vmConnectionFixture{
		"assigned-kali":   {Profile: "kali", State: "running", IP: "192.0.2.10", MAC: "52:54:00:00:00:10"},
		"assigned-win11":  {Profile: "windows-11-enterprise", State: "running", IP: "192.0.2.11", MAC: "52:54:00:00:00:11"},
		"assigned-server": {Profile: "windows-server-2022", State: "running", IP: "192.0.2.12", MAC: "52:54:00:00:00:12"},
		"GOAD-unrelated":  {State: "running", IP: "192.0.2.99"},
		"unassigned":      {Profile: "unassigned", State: "running", IP: "192.0.2.98"},
	}, "xfreerdp3", "xfreerdp", "remmina")
	result := discoverVMConnections(context.Background())
	if len(result.VMs) != 3 || len(result.Warnings) != 0 {
		t.Fatalf("unexpected discovery: %+v", result)
	}
	wantUsers := map[string]string{"kali": "kali", "windows-11-enterprise": "pomdock", "windows-server-2022": "Administrator"}
	for _, vm := range result.VMs {
		if vm.RDPUser != wantUsers[vm.Profile] || vm.RDPClient != "xfreerdp3" || vm.IP == "" || vm.Connect != "pomdock vm rdp "+shellQuote(vm.Name) {
			t.Fatalf("wrong connection metadata: %+v", vm)
		}
		command, err := rdpClientCommand(GuestProfileByID(vm.Profile), vm.IP)
		if err != nil || vm.RDPCommand != shellJoin(command.Args) {
			t.Fatalf("CLI/GUI command drift: %+v %v", vm, err)
		}
		for _, flag := range []string{"/dynamic-resolution", "/gfx:avc444", "+clipboard", "/cert:tofu", "/log-level:ERROR"} {
			if !strings.Contains(vm.RDPCommand, flag) {
				t.Fatalf("missing flag %s: %s", flag, vm.RDPCommand)
			}
		}
	}
	for _, call := range calls() {
		if !slices.Contains([]string{"list", "metadata", "domstate", "domiflist", "domifaddr", "net-dhcp-leases"}, call[0]) {
			t.Fatalf("non-read-only call: %q", call)
		}
		if len(call) > 1 && (call[1] == "GOAD-unrelated" || call[1] == "unassigned") && call[0] != "metadata" {
			t.Fatalf("unassigned guest queried beyond metadata: %q", call)
		}
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("metadata endpoint launched the RDP client")
	}
}

func TestVMConnectionsUnavailableStatesAndUnknownProfilesDoNotInventCommands(t *testing.T) {
	calls, _ := vmConnectionsFixture(t, map[string]vmConnectionFixture{
		"stopped":     {Profile: "kali", State: "shut off", IP: "192.0.2.10"},
		"no-ip":       {Profile: "windows-11-enterprise", State: "running"},
		"no-rdp":      {Profile: "ubuntu-lts", State: "running", IP: "192.0.2.11"},
		"future":      {Profile: "future-profile", State: "running", IP: "192.0.2.12"},
		"state-error": {Profile: "kali", StateError: true},
		"invalid-ip":  {Profile: "windows-11-enterprise", State: "running", IP: "999.0.0.1"},
		"foreign":     {Profile: "kali", State: "running", IP: "192.0.2.13", Namespace: "https://unrelated.example/metadata"},
	}, "xfreerdp3")
	result := discoverVMConnections(context.Background())
	if len(result.VMs) != 6 {
		t.Fatalf("wrong assigned rows: %+v", result)
	}
	for _, vm := range result.VMs {
		if vm.RDPCommand != "" || vm.RDPReason == "" {
			t.Fatalf("fabricated direct command/state: %+v", vm)
		}
		if (vm.Name == "future" || vm.Name == "no-rdp") && (vm.Connect != "" || vm.RDPUser != "") {
			t.Fatalf("unsupported profile given connection: %+v", vm)
		}
		if vm.Name == "state-error" && vm.State != "unknown" {
			t.Fatalf("unknown state fabricated: %+v", vm)
		}
	}
	for _, call := range calls() {
		if len(call) > 1 && slices.Contains([]string{"stopped", "no-rdp", "future", "foreign"}, call[1]) && slices.Contains([]string{"domiflist", "domifaddr"}, call[0]) {
			t.Fatalf("unneeded IP query: %q", call)
		}
	}
}

func TestVMConnectionsUseManagementMACAndRejectAmbiguousAddresses(t *testing.T) {
	mac, internal := "52:54:00:00:00:01", "52:54:00:00:00:02"
	interfaces := "Interface Type Source Model MAC\n-------------------\nvnet0 network default virtio " + mac + "\nvnet1 network Whonix-Internal virtio " + internal
	vmConnectionsFixture(t, map[string]vmConnectionFixture{
		"two-nics":  {Profile: "kali", State: "running", Interfaces: interfaces, Addresses: "eth1 " + internal + " ipv4 10.152.152.100/18\neth0 " + mac + " ipv4 192.0.2.20/24"},
		"ambiguous": {Profile: "kali", State: "running", Interfaces: interfaces, Addresses: "eth0 " + mac + " ipv4 192.0.2.30/24\neth0 " + mac + " ipv4 192.0.2.31/24"},
	}, "xfreerdp3")
	result := discoverVMConnections(context.Background())
	for _, vm := range result.VMs {
		if vm.Name == "two-nics" && (vm.IP != "192.0.2.20" || vm.RDPCommand == "") {
			t.Fatalf("wrong NIC selected: %+v", vm)
		}
		if vm.Name == "ambiguous" && (vm.IP != "" || vm.RDPCommand != "" || !strings.Contains(vm.RDPReason, "Multiple")) {
			t.Fatalf("ambiguous IP guessed: %+v", vm)
		}
	}
}

func TestVMConnectionsDHCPFallbackAndClientSelection(t *testing.T) {
	for _, test := range []struct {
		clients          []string
		preference, want string
	}{{[]string{"xfreerdp"}, "", "xfreerdp"}, {[]string{"remmina"}, "", "remmina"}, {[]string{"xfreerdp3", "remmina"}, "remmina", "remmina"}, {nil, "", ""}} {
		t.Run(fmt.Sprint(test.clients, test.preference), func(t *testing.T) {
			vmConnectionsFixture(t, map[string]vmConnectionFixture{"lab's guest": {Profile: "kali", State: "running", Leases: "2026-11-08 12:00:00 52:54:00:00:00:01 ipv4 192.0.2.40/24 fixture -"}}, test.clients...)
			t.Setenv("POMDOCK_RDP_CLIENT", test.preference)
			result := discoverVMConnections(context.Background())
			if len(result.VMs) != 1 {
				t.Fatal(result)
			}
			vm := result.VMs[0]
			if vm.IP != "192.0.2.40" || vm.RDPClient != test.want || vm.Connect != `pomdock vm rdp 'lab'"'"'s guest'` {
				t.Fatalf("wrong fallback/quoting: %+v", vm)
			}
			if test.want == "" && (vm.RDPCommand != "" || vm.RDPReason == "") {
				t.Fatalf("missing client fabricated: %+v", vm)
			}
			if test.want == "remmina" && (!strings.Contains(vm.RDPCommand, "rdp://kali@192.0.2.40") || strings.Contains(vm.RDPCommand, "/v:")) {
				t.Fatalf("Remmina mislabeled as FreeRDP: %+v", vm)
			}
		})
	}
}

func TestVMConnectionsEndpointAndMissingVirshAreIndependent(t *testing.T) {
	vmConnectionsFixture(t, map[string]vmConnectionFixture{"assigned": {Profile: "kali", State: "shut off"}}, "xfreerdp3")
	server := testWebServer()
	request := httptest.NewRequest(http.MethodGet, "/api/vms/connections", nil)
	request.Host = "127.0.0.1:7332"
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: server.token})
	recorder := httptest.NewRecorder()
	server.handler().ServeHTTP(recorder, request)
	var response vmConnectionsResponse
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &response) != nil || len(response.VMs) != 1 {
		t.Fatalf("endpoint not wired: %d %s", recorder.Code, recorder.Body.String())
	}
	t.Setenv("PATH", t.TempDir())
	response = discoverVMConnections(context.Background())
	if len(response.VMs) != 0 || len(response.Warnings) != 1 || !strings.Contains(response.Warnings[0], "virsh") {
		t.Fatalf("missing libvirt falsely looked empty: %+v", response)
	}
}

func TestVMConnectionDiscoveryRespectsRequestDeadline(t *testing.T) {
	vmConnectionsFixture(t, map[string]vmConnectionFixture{"slow": {Profile: "kali", State: "running"}}, "xfreerdp3")
	t.Setenv("POMDOCK_VM_CONNECTION_STALL", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	response := discoverVMConnections(ctx)
	if time.Since(start) > time.Second || len(response.Warnings) == 0 {
		t.Fatalf("discovery was not bounded: %+v", response)
	}
}
