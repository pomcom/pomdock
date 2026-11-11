package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstanceHostnameMatchesInstanceLabelRules(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"Lab_VPN.v2", "lab-vpn-v2"}, {"___...!!!", "vm"}, {"--Lab_One--", "lab-one"}, {"lab--two", "lab--two"},
		{strings.Repeat("A", 70), strings.Repeat("a", 63)}, {strings.Repeat("a", 62) + "--suffix", strings.Repeat("a", 62)},
	} {
		if got := instanceHostname(tc.input); got != tc.want {
			t.Errorf("instanceHostname(%q)=%q, want %q", tc.input, got, tc.want)
		}
	}
}

func cloneHostnameMock(t *testing.T, customizer bool) string {
	t.Helper()
	root := t.TempDir()
	bin := t.TempDir()
	t.Setenv("POMDOCK_HOSTNAME_TEST_DIR", root)
	t.Setenv("POMDOCK_HOSTNAME_TEST_PROFILE", "kali")
	t.Setenv("POMDOCK_HOSTNAME_TEST_STATE", "shut off")
	t.Setenv("PATH", bin)
	scripts := map[string]string{
		"virsh": `#!/bin/sh
[ "$1" = --connect ] && [ "$2" = qemu:///system ] || exit 91
shift 2
printf '%s\n' "$*" >> "$POMDOCK_HOSTNAME_TEST_DIR/virsh-calls"
case "$1" in
 dominfo) [ -e "$POMDOCK_HOSTNAME_TEST_DIR/created" ] || [ "${POMDOCK_HOSTNAME_TEST_EXISTS:-}" = 1 ];;
 metadata)
  [ "$POMDOCK_HOSTNAME_TEST_PROFILE" != unassigned ] || exit 1
  printf '<guest xmlns="https://pomdock.dev/xmlns/domain/1.0" profile="%s"/>\n' "$POMDOCK_HOSTNAME_TEST_PROFILE";;
 domstate) printf '%s\n' "$POMDOCK_HOSTNAME_TEST_STATE";;
 *) exit 92;;
esac
`,
		"virt-clone": `#!/bin/sh
printf '%s\000' "$@" > "$POMDOCK_HOSTNAME_TEST_DIR/clone-args"
[ "${POMDOCK_HOSTNAME_TEST_CLONE_STATUS:-0}" = 0 ] || exit "$POMDOCK_HOSTNAME_TEST_CLONE_STATUS"
: > "$POMDOCK_HOSTNAME_TEST_DIR/created"
`,
	}
	if customizer {
		scripts["virt-customize"] = `#!/bin/sh
printf '%s\000' "$@" > "$POMDOCK_HOSTNAME_TEST_DIR/customize-args"
exit "${POMDOCK_HOSTNAME_TEST_CUSTOMIZE_STATUS:-0}"
`
	}
	for name, source := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(source), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hostnameMockArgs(t *testing.T, root, file string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}

func TestLinuxCloneHostnameCustomizesOnlyNewStoppedTarget(t *testing.T) {
	for _, profile := range []string{"kali", "ubuntu-lts", "debian-stable", "rocky-9"} {
		t.Run(profile, func(t *testing.T) {
			root := cloneHostnameMock(t, true)
			t.Setenv("POMDOCK_HOSTNAME_TEST_PROFILE", profile)
			if err := CloneVM("original", "Lab_VPN.v2"); err != nil {
				t.Fatal(err)
			}
			clone := hostnameMockArgs(t, root, "clone-args")
			if !slices.Equal(clone, []string{"--connect", libvirtURI, "--original", "original", "--name", "Lab_VPN.v2", "--file", filepath.Join(imageDir, "Lab_VPN.v2.qcow2")}) {
				t.Fatalf("clone behavior changed: %v", clone)
			}
			args := hostnameMockArgs(t, root, "customize-args")
			for _, pair := range [][2]string{{"--connect", libvirtURI}, {"--domain", "Lab_VPN.v2"}, {"--hostname", "lab-vpn-v2"}, {"--mkdir", "/etc/cloud/cloud.cfg.d"}} {
				index := slices.Index(args, pair[0])
				if index < 0 || index+1 >= len(args) || args[index+1] != pair[1] {
					t.Fatalf("missing customization argument %v in %v", pair, args)
				}
			}
			if !slices.Contains(args, "--no-network") || slices.Contains(args, "original") || slices.Contains(args, "--run-command") {
				t.Fatalf("customization touched wrong scope: %v", args)
			}
			index := slices.Index(args, "--write")
			if index < 0 || !strings.Contains(args[index+1], "preserve_hostname: true\nhostname: lab-vpn-v2\n") {
				t.Fatalf("clone hostname not preserved for cloud-init: %v", args)
			}
			calls, _ := os.ReadFile(filepath.Join(root, "virsh-calls"))
			if strings.Contains(string(calls), "domstate original") || !strings.Contains(string(calls), "domstate Lab_VPN.v2") {
				t.Fatalf("wrong stopped-state guard: %s", calls)
			}
		})
	}
}

func TestCloneHostnameFailsBeforeCloningWhenDependencyMissing(t *testing.T) {
	root := cloneHostnameMock(t, false)
	err := CloneVM("original", "new-clone")
	if err == nil || !strings.Contains(err.Error(), "requires virt-customize") {
		t.Fatalf("missing preflight: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "clone-args")); !os.IsNotExist(err) {
		t.Fatal("cloned before dependency preflight")
	}
}

func TestCloneHostnameNeverCustomizesRunningCloneOrClaimsFailureSuccess(t *testing.T) {
	for _, scenario := range []string{"running", "clone-failure", "customize-failure"} {
		t.Run(scenario, func(t *testing.T) {
			root := cloneHostnameMock(t, true)
			switch scenario {
			case "running":
				t.Setenv("POMDOCK_HOSTNAME_TEST_STATE", "running")
			case "clone-failure":
				t.Setenv("POMDOCK_HOSTNAME_TEST_CLONE_STATUS", "17")
			case "customize-failure":
				t.Setenv("POMDOCK_HOSTNAME_TEST_CUSTOMIZE_STATUS", "19")
			}
			err := CloneVM("original", "new-clone")
			if err == nil {
				t.Fatal("failed hostname operation returned success")
			}
			if scenario == "customize-failure" {
				if !strings.Contains(err.Error(), "was created") || !strings.Contains(err.Error(), "remains available for inspection") {
					t.Fatalf("partial clone not explained: %v", err)
				}
			} else if _, statErr := os.Stat(filepath.Join(root, "customize-args")); !os.IsNotExist(statErr) {
				t.Fatal("customized running/uncreated clone")
			}
		})
	}
}

func TestCloneHostnameSkipsWindowsAndUnknownProfiles(t *testing.T) {
	for _, profile := range []string{"windows-11-enterprise", "windows-server-2025", "unassigned"} {
		t.Run(profile, func(t *testing.T) {
			root := cloneHostnameMock(t, false)
			t.Setenv("POMDOCK_HOSTNAME_TEST_PROFILE", profile)
			if err := CloneVM("original", "new-clone"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "clone-args")); err != nil {
				t.Fatal("existing clone feature stopped working")
			}
			if _, err := os.Stat(filepath.Join(root, "customize-args")); !os.IsNotExist(err) {
				t.Fatal("unknown/Windows guest blindly Linux-customized")
			}
		})
	}
}

func TestCloneHostnameRejectsOriginalAndExistingTarget(t *testing.T) {
	for _, scenario := range []string{"same-name", "existing-target"} {
		t.Run(scenario, func(t *testing.T) {
			root := cloneHostnameMock(t, true)
			destination := "new-clone"
			if scenario == "same-name" {
				destination = "original"
			} else {
				t.Setenv("POMDOCK_HOSTNAME_TEST_EXISTS", "1")
			}
			if err := CloneVM("original", destination); err == nil {
				t.Fatal("not-a-new-clone target accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "clone-args")); !os.IsNotExist(err) {
				t.Fatal("unsafe clone command executed")
			}
		})
	}
}
