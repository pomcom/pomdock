package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/table"
)

func TestWhonixAllocationIsStableAndAvoidsActualClaims(t *testing.T) {
	used := map[string]string{}
	a, err := chooseWhonixAddress("lab-a", WhonixGatewayIP(), "", used)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := chooseWhonixAddress("lab-a", WhonixGatewayIP(), "", used)
	if a != again {
		t.Fatal("allocation is not deterministic")
	}
	used[a] = "other"
	next, err := chooseWhonixAddress("lab-a", WhonixGatewayIP(), "", used)
	if err != nil || next == a {
		t.Fatalf("collision not avoided: %q %v", next, err)
	}
	if _, err = chooseWhonixAddress("lab-a", WhonixGatewayIP(), a, used); err == nil {
		t.Fatal("saved conflicting address accepted")
	}
	used[a] = "lab-a"
	if saved, err := chooseWhonixAddress("lab-a", WhonixGatewayIP(), a, used); err != nil || saved != a {
		t.Fatal("valid saved allocation changed")
	}
	for n := 100; n <= 254; n++ {
		used[fmt.Sprintf("10.152.152.%d", n)] = "occupied"
	}
	if _, err = chooseWhonixAddress("new-lab", WhonixGatewayIP(), "", used); err == nil {
		t.Fatal("full range silently reused")
	}
	for _, bad := range []string{"10.152.152.10", "10.152.153.100", "invalid"} {
		if _, err := chooseWhonixAddress("x", WhonixGatewayIP(), bad, nil); err == nil {
			t.Fatalf("invalid saved allocation accepted: %s", bad)
		}
	}
}

func TestWhonixIdentityUsesNetworkMACAndPreservesProfileNamespace(t *testing.T) {
	document := `<domain><name>fixture</name><metadata><guest xmlns="https://pomdock.dev/xmlns/domain/1.0" profile="kali"/><route xmlns="https://pomdock.dev/xmlns/whonix/1.0" address="10.152.152.141" network="Whonix-Internal"/></metadata><devices><interface><mac address="52:54:00:00:00:01"/><source network="default"/></interface><interface><mac address="52:54:00:00:00:02"/><source network="Whonix-Internal"/></interface></devices></domain>`
	var domain whonixDomain
	if err := xml.Unmarshal([]byte(document), &domain); err != nil {
		t.Fatal(err)
	}
	management, tor, err := whonixNICs(domain)
	if err != nil || management != "52:54:00:00:00:01" || tor != "52:54:00:00:00:02" {
		t.Fatalf("wrong identity %q %q %v", management, tor, err)
	}
	if domain.Metadata.Route == nil || domain.Metadata.Route.Address != "10.152.152.141" {
		t.Fatal("allocation metadata not resolved")
	}
	addresses := whonixObservedAddresses("ens3 52:54:00:00:00:01 ipv4 192.0.2.15/24\nens9 52:54:00:00:00:02 ipv4 10.152.152.141/18\n", tor)
	if len(addresses) != 1 || addresses[0] != "10.152.152.141" {
		t.Fatalf("wrong observed addresses: %v", addresses)
	}
}

const mockWhonixVirsh = `#!/usr/bin/env python3
import os,sys,pathlib,xml.etree.ElementTree as ET
root=pathlib.Path(os.environ['POMDOCK_VM_TEST_DIR'])
args=sys.argv[1:]
if args[:2]!=['--connect','qemu:///system']:sys.exit(91)
args=args[2:]
with (root/'calls').open('a') as f:f.write(' '.join(args)+'\n')
command=args[0]
if command=='list':
 print('\n'.join(sorted(p.stem for p in root.glob('*.domain'))))
elif command=='dumpxml':
 name=args[1];domain=ET.fromstring((root/(name+'.domain')).read_text())
 route=root/(name+'.route')
 if route.exists():domain.find('metadata').append(ET.fromstring(route.read_text()))
 if (root/(name+'.attached')).exists():
  nic=ET.SubElement(domain.find('devices'),'interface');ET.SubElement(nic,'mac',address='52:54:00:00:00:02');ET.SubElement(nic,'source',network='Whonix-Internal')
 print(ET.tostring(domain,encoding='unicode'))
elif command=='metadata':
 if '--set' in args:(root/(args[1]+'.route')).write_text(args[args.index('--set')+1])
 else:print('<guest xmlns="https://pomdock.dev/xmlns/domain/1.0" profile="kali"/>')
elif command=='domifaddr':
 file=root/(args[1]+'.addresses')
 if file.exists():print(file.read_text())
 else:sys.exit(1)
elif command=='domiflist':
 if (root/(args[1]+'.attached')).exists():print('- network Whonix-Internal virtio 52:54:00:00:00:02')
elif command=='domstate':print('running')
elif command=='net-info':print('active')
elif command=='attach-interface':(root/(args[1]+'.attached')).touch()
elif command=='detach-interface':(root/(args[1]+'.detached')).touch()
else:sys.exit(92)
`

func whonixMock(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	bin := t.TempDir()
	t.Setenv("POMDOCK_VM_TEST_DIR", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for name, script := range map[string]string{"virsh": mockWhonixVirsh, "ssh": "#!/bin/bash\nscript=$(cat)\nif [[ \"$script\" != true ]]; then printf '%s' \"$script\" > \"$POMDOCK_VM_TEST_DIR/guest-script\"; exit \"${POMDOCK_VM_TEST_SSH_STATUS:-0}\"; fi\n"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	key := filepath.Join(os.Getenv("HOME"), ".ssh", "kali")
	if err := os.MkdirAll(filepath.Dir(key), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"lab-a", "Whonix-Gateway"} {
		writeWhonixMockDomain(t, root, name, "")
	}
	if err := os.WriteFile(filepath.Join(root, "lab-a.addresses"), []byte("ens3 52:54:00:00:00:01 ipv4 192.0.2.15/24\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeWhonixMockDomain(t *testing.T, root, name, extra string) {
	t.Helper()
	document := fmt.Sprintf(`<domain><name>%s</name><metadata><guest xmlns="https://pomdock.dev/xmlns/domain/1.0" profile="kali"/></metadata><devices><interface><mac address="52:54:00:00:00:01"/><source network="default"/></interface>%s</devices></domain>`, name, extra)
	if err := os.WriteFile(filepath.Join(root, name+".domain"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWhonixReservationChecksMetadataObservedIPsAndLegacyUnknowns(t *testing.T) {
	root := whonixMock(t)
	first, err := reserveWhonixAddress("lab-a")
	if err != nil {
		t.Fatal(err)
	}
	again, err := reserveWhonixAddress("lab-a")
	if err != nil || again != first {
		t.Fatalf("reservation changed: %s %v", again, err)
	}
	writeWhonixMockDomain(t, root, "lab-b", `<interface><mac address="52:54:00:00:00:03"/><source network="Whonix-Internal"/></interface>`)
	if _, err := reserveWhonixAddress("lab-a"); err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("unknown legacy NIC was guessed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "lab-b.addresses"), []byte("ens9 52:54:00:00:00:03 ipv4 "+first+"/18\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveWhonixAddress("lab-a"); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("observed collision accepted: %v", err)
	}
}

func TestWhonixGuestFailureIsReportedAndDoesNotDetach(t *testing.T) {
	root := whonixMock(t)
	t.Setenv("POMDOCK_VM_TEST_SSH_STATUS", "23")
	var output bytes.Buffer
	err := configureVMWhonix(context.Background(), "lab-a", &output, &output)
	if err == nil || !strings.Contains(err.Error(), "SSH/configuration failed") || strings.Contains(output.String(), "route verified") {
		t.Fatalf("guest error hidden: %v %s", err, output.String())
	}
	script, _ := os.ReadFile(filepath.Join(root, "guest-script"))
	if !strings.Contains(string(script), "52:54:00:00:00:02") || !strings.Contains(string(script), "ipv6.never-default yes") {
		t.Fatal("wrong MAC/IPv6 routing script")
	}
	output.Reset()
	err = restoreVMWhonix(context.Background(), "lab-a", &output, &output)
	if err == nil {
		t.Fatal("restore guest failure hidden")
	}
	if _, err := os.Stat(filepath.Join(root, "lab-a.detached")); !os.IsNotExist(err) {
		t.Fatal("NIC detached before successful management route restore")
	}
}

func TestWhonixGuestSuccessUsesAllocatedAddressAndAccurateMessage(t *testing.T) {
	root := whonixMock(t)
	var output bytes.Buffer
	if err := configureVMWhonix(context.Background(), "lab-a", &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "IPv4 default route verified") || strings.Contains(output.String(), "All traffic") {
		t.Fatalf("misleading result: %s", output.String())
	}
	var allocation whonixAllocation
	data, _ := os.ReadFile(filepath.Join(root, "lab-a.route"))
	if err := xml.Unmarshal(data, &allocation); err != nil {
		t.Fatal(err)
	}
	if allocation.MAC != "52:54:00:00:00:02" || allocation.Address == "" {
		t.Fatalf("missing reservation identity: %+v", allocation)
	}
	if err := restoreVMWhonix(context.Background(), "lab-a", &output, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lab-a.detached")); err != nil {
		t.Fatal("successful restore did not detach")
	}
}

func TestWhonixGuestScriptValidationAndSyntax(t *testing.T) {
	script, err := buildWhonixRoutingScript("10.152.152.10", "10.152.152.141", "52:54:00:00:00:02", "52:54:00:00:00:01", "192.0.2.15")
	if err != nil {
		t.Fatal(err)
	}
	restore, err := buildWhonixRestoreScript("52:54:00:00:00:01")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{script, restore} {
		cmd := exec.Command("bash", "-n")
		cmd.Stdin = strings.NewReader(source)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("invalid script: %v %s", err, out)
		}
	}
	if _, err := buildWhonixRoutingScript("10.152.152.10;exit", "10.152.152.141", "52:54:00:00:00:02", "52:54:00:00:00:01", "192.0.2.15"); err == nil {
		t.Fatal("script injection accepted")
	}
}

func TestWhonixGuestScriptRunsAgainstActualMACsAndRejectsBadRoutes(t *testing.T) {
	for _, scenario := range []string{"success", "wrong-ipv4", "ipv6-default", "nmcli-failure"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			bin := t.TempDir()
			t.Setenv("POMDOCK_VM_TEST_DIR", root)
			t.Setenv("POMDOCK_VM_TEST_SCENARIO", scenario)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			commands := map[string]string{
				"sudo": "#!/bin/sh\n[ \"$1\" != -n ] || shift\nexec \"$@\"\n",
				"ip": `#!/bin/bash
case "$*" in
 '-o link show') printf '2: ens3: UP link/ether 52:54:00:00:00:01 brd ff:ff:ff:ff:ff:ff\n9: enp7s0: UP link/ether 52:54:00:00:00:02 brd ff:ff:ff:ff:ff:ff\n';;
 '-o -4 addr show dev ens3') printf '2: ens3 inet 192.0.2.15/24 scope global ens3\n';;
 '-4 route show default')
  if [[ "$POMDOCK_VM_TEST_SCENARIO" == wrong-ipv4 || "${POMDOCK_VM_TEST_RESTORE:-}" == 1 ]]; then printf 'default via 192.0.2.1 dev ens3\n'; else printf 'default via 10.152.152.10 dev enp7s0\n'; fi;;
 '-6 route show default') [[ "$POMDOCK_VM_TEST_SCENARIO" != ipv6-default ]] || printf 'default via fe80::1 dev ens3\n';;
 *) exit 75;;
esac
exit 0
`,
				"nmcli": `#!/bin/bash
printf '%s\n' "$*" >> "$POMDOCK_VM_TEST_DIR/nmcli-calls"
case "$*" in
 '-g GENERAL.CON-UUID device show ens3') printf '00000000-0000-0000-0000-000000000001\n';;
 '-g ipv4.never-default connection show uuid '*|'-g ipv6.never-default connection show uuid '*) printf 'no\n';;
 'connection show id whonix-internal') [[ -e "$POMDOCK_VM_TEST_DIR/connection" ]] || exit 10;;
 'connection add '*) touch "$POMDOCK_VM_TEST_DIR/connection";;
 '-g 802-3-ethernet.mac-address connection show id whonix-internal') printf '52:54:00:00:00:02\n';;
 'connection modify id whonix-internal '*) [[ "$POMDOCK_VM_TEST_SCENARIO" != nmcli-failure ]] || exit 17;;
 'connection delete id whonix-internal') rm -f "$POMDOCK_VM_TEST_DIR/connection";;
esac
`,
			}
			for name, source := range commands {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(source), 0700); err != nil {
					t.Fatal(err)
				}
			}
			source, err := buildWhonixRoutingScript("10.152.152.10", "10.152.152.141", "52:54:00:00:00:02", "52:54:00:00:00:01", "192.0.2.15")
			if err != nil {
				t.Fatal(err)
			}
			stateDir := filepath.Join(root, "state")
			source = strings.Replace(source, "state_dir=/var/lib/pomdock", "state_dir="+shellQuote(stateDir), 1)
			cmd := exec.Command("bash")
			cmd.Stdin = strings.NewReader(source)
			output, err := cmd.CombinedOutput()
			if scenario != "success" {
				if err == nil {
					t.Fatalf("%s was reported successful: %s", scenario, output)
				}
				if strings.Contains(string(output), "Verified IPv4") {
					t.Fatal("failed route reported verified")
				}
				return
			}
			if err != nil {
				t.Fatalf("valid config failed: %v %s", err, output)
			}
			if !strings.Contains(string(output), "dev enp7s0") {
				t.Fatalf("actual NIC not used: %s", output)
			}
			calls, _ := os.ReadFile(filepath.Join(root, "nmcli-calls"))
			if !strings.Contains(string(calls), "ipv4.never-default yes ipv6.never-default yes") || !strings.Contains(string(calls), "device reapply ens3") {
				t.Fatal("management default route flags not applied in place")
			}
			t.Setenv("POMDOCK_VM_TEST_RESTORE", "1")
			restore, err := buildWhonixRestoreScript("52:54:00:00:00:01")
			if err != nil {
				t.Fatal(err)
			}
			restore = strings.Replace(restore, "state_dir=/var/lib/pomdock", "state_dir="+shellQuote(stateDir), 1)
			cmd = exec.Command("bash")
			cmd.Stdin = strings.NewReader(restore)
			output, err = cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("restore failed: %v %s", err, output)
			}
			if _, err := os.Stat(filepath.Join(stateDir, "whonix-52:54:00:00:00:01.state")); !os.IsNotExist(err) {
				t.Fatal("successful restore left routing state")
			}
		})
	}
}

func TestWhonixTUIUsesVerifiedRoutingAndSurfacesGuestErrors(t *testing.T) {
	root := whonixMock(t)
	t.Setenv("POMDOCK_VM_TEST_SSH_STATUS", "23")
	for _, key := range []string{"w", "W"} {
		if key == "W" {
			if err := os.WriteFile(filepath.Join(root, "lab-a.attached"), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		model := newTUI()
		model.vms = []VM{{Name: "lab-a", ProfileID: "kali", State: "running"}}
		model.vmTable.SetRows([]table.Row{{"lab-a", "Kali", "running", ""}})
		commands := model.handleVMKey(key)
		found := false
		for _, command := range commands {
			if done, ok := command().(doneMsg); ok {
				found = true
				if done.err == nil || !strings.Contains(done.err.Error(), "SSH/configuration failed") {
					t.Fatalf("TUI %s hid guest failure: %+v", key, done)
				}
				if strings.Contains(done.text, "route verified") || strings.Contains(done.text, "routing restored") {
					t.Fatalf("TUI %s falsely reported success: %+v", key, done)
				}
			}
		}
		if !found {
			t.Fatalf("TUI %s did not run shared routing helper", key)
		}
	}
}
