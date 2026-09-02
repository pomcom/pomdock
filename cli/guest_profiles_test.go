package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestGuestProfilesExposeExpectedConnections(t *testing.T) {
	tests := []struct {
		id      string
		sshUser string
		rdpUser string
	}{
		{id: "kali", sshUser: "kali", rdpUser: "kali"},
		{id: "ubuntu-lts", sshUser: "ubuntu"},
		{id: "debian-stable", sshUser: "debian"},
		{id: "rocky-9", sshUser: "rocky"},
		{id: "windows-server-2025", rdpUser: "Administrator"},
		{id: "windows-11-enterprise", rdpUser: "pomdock"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			profile := GuestProfileByID(tt.id)
			if profile.ID != tt.id || profile.SSHUser != tt.sshUser || profile.RDPUser != tt.rdpUser {
				t.Fatalf("unexpected profile: %#v", profile)
			}
			if profile.SupportsSSH != (tt.sshUser != "") {
				t.Fatalf("SupportsSSH = %v, want %v", profile.SupportsSSH, tt.sshUser != "")
			}
			if profile.SupportsRDP != (tt.rdpUser != "") {
				t.Fatalf("SupportsRDP = %v, want %v", profile.SupportsRDP, tt.rdpUser != "")
			}
		})
	}
}

func TestProvisionableGuestProfilesHaveVerifiedImages(t *testing.T) {
	profiles := ProvisionableGuestProfiles()
	if len(profiles) != 8 || profiles[0].ID != "kali" {
		t.Fatalf("unexpected provisioning order: %#v", profiles)
	}
	for _, profile := range profiles[1:4] {
		if profile.Provisioner != "cloud-image" || profile.ImageURL == "" || profile.ChecksumURL == "" {
			t.Fatalf("incomplete cloud profile: %#v", profile)
		}
		if profile.ImageURL[:8] != "https://" || profile.ChecksumURL[:8] != "https://" {
			t.Fatalf("profile does not use HTTPS: %#v", profile)
		}
	}
	for _, profile := range profiles[4:] {
		if profile.Provisioner != "windows-iso" || profile.Family != "windows" {
			t.Fatalf("incomplete Windows profile: %#v", profile)
		}
	}
}

func TestPrepareVMProvisionBuildsCloudCommand(t *testing.T) {
	root := t.TempDir()
	scriptDir := filepath.Join(root, "vm-profiles")
	if err := os.Mkdir(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "linux-cloud-setup.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldRoot := repoRoot
	repoRoot = root
	t.Cleanup(func() { repoRoot = oldRoot })

	cmd, profile, err := PrepareVMProvision(VMProvisionOptions{ProfileID: "debian-stable", Name: "debian-lab"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "debian-stable" || len(cmd.Args) != 10 {
		t.Fatalf("unexpected provision command: profile=%#v args=%v", profile, cmd.Args)
	}
	if cmd.Args[0] != "bash" || cmd.Args[1] != script || cmd.Args[2] != "debian-stable" || cmd.Args[3] != "debian-lab" {
		t.Fatalf("unexpected command prefix: %v", cmd.Args)
	}
}

func TestPrepareWindowsProvisionRequiresISO(t *testing.T) {
	_, _, err := PrepareVMProvision(VMProvisionOptions{
		ProfileID: "windows-11-enterprise",
		Name:      "win11-lab",
	})
	if err == nil {
		t.Fatal("expected missing Windows ISO to fail")
	}
}

func TestPrepareWindowsProvisionBuildsISOCommand(t *testing.T) {
	root := t.TempDir()
	scriptDir := filepath.Join(root, "vm-profiles")
	if err := os.Mkdir(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(scriptDir, "windows-iso-setup.sh")
	iso := filepath.Join(root, "windows.iso")
	virtio := filepath.Join(root, "virtio.iso")
	for _, file := range []string{script, iso, virtio} {
		if err := os.WriteFile(file, []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldRoot := repoRoot
	repoRoot = root
	t.Cleanup(func() { repoRoot = oldRoot })

	cmd, profile, err := PrepareVMProvision(VMProvisionOptions{
		ProfileID: "windows-server-2025",
		Name:      "server-lab",
		ISO:       iso,
		VirtioISO: virtio,
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.ID != "windows-server-2025" || len(cmd.Args) != 6 {
		t.Fatalf("unexpected provision command: profile=%#v args=%v", profile, cmd.Args)
	}
	if cmd.Args[1] != script || cmd.Args[2] != profile.ID || cmd.Args[3] != "server-lab" || cmd.Args[4] != iso || cmd.Args[5] != virtio {
		t.Fatalf("unexpected command: %v", cmd.Args)
	}
}

func TestUnknownGuestProfileIsUnassignedWithLegacyConnections(t *testing.T) {
	profile := GuestProfileByID("not-labelled")
	if profile.ID != fallbackGuestProfileID {
		t.Fatalf("fallback profile = %q, want %q", profile.ID, fallbackGuestProfileID)
	}
	if profile.SSHUser != "kali" || profile.RDPUser != "kali" {
		t.Fatalf("fallback connection behavior changed: %#v", profile)
	}
}

func TestGuestProfileIDsAreSorted(t *testing.T) {
	ids := GuestProfileIDs()
	if !slices.IsSorted(ids) {
		t.Fatalf("profile IDs are not sorted: %v", ids)
	}
	for _, expected := range []string{"kali", "ubuntu-lts", "windows-server-2025"} {
		if !slices.Contains(ids, expected) {
			t.Fatalf("profile IDs do not contain %q: %v", expected, ids)
		}
	}
	if slices.Contains(ids, fallbackGuestProfileID) {
		t.Fatalf("fallback profile must not be assignable: %v", ids)
	}
}

func TestProfileSSHKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := profileSSHKey(GuestProfileByID("ubuntu-lts"))
	want := filepath.Join(home, ".ssh", "pomdock")
	if got != want {
		t.Fatalf("key path = %q, want %q", got, want)
	}
}

func TestSetVMProfileRejectsNonProfilesBeforeVirsh(t *testing.T) {
	for _, profileID := range []string{"missing", fallbackGuestProfileID} {
		if err := SetVMProfileID("any-vm", profileID); err == nil {
			t.Fatalf("SetVMProfileID accepted %q", profileID)
		}
	}
}
