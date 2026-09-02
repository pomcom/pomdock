package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaitForTCP(t *testing.T) {
	var address string
	err := waitForTCP("192.0.2.10", 3389, time.Second, func(got string, timeout time.Duration) error {
		address = got
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if address != "192.0.2.10:3389" {
		t.Fatalf("probe address = %q", address)
	}
}

func TestRDPClientPrefersFreeRDP(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"xfreerdp3", "remmina"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	cmd, err := rdpClientCommand(GuestProfile{RDPUser: "kali"}, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	if filepath.Base(cmd.Path) != "xfreerdp3" {
		t.Fatalf("unexpected RDP client: path=%q args=%v", cmd.Path, cmd.Args)
	}
	for _, want := range []string{"/v:192.0.2.10", "/u:kali", "/dynamic-resolution", "/gfx:avc444", "+clipboard", "/cert:tofu"} {
		if !strings.Contains(args, want) {
			t.Fatalf("FreeRDP args missing %q: %v", want, cmd.Args)
		}
	}
}

func TestRDPClientHonorsRemminaOverride(t *testing.T) {
	dir := t.TempDir()
	remmina := filepath.Join(dir, "remmina")
	if err := os.WriteFile(remmina, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("POMDOCK_RDP_CLIENT", "remmina")
	cmd, err := rdpClientCommand(GuestProfile{RDPUser: "kali"}, "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != remmina || !strings.Contains(strings.Join(cmd.Args, " "), "rdp://kali@192.0.2.10") {
		t.Fatalf("unexpected Remmina override: path=%q args=%v", cmd.Path, cmd.Args)
	}
}

func TestDesktopClientReportsImmediateFailure(t *testing.T) {
	dir := t.TempDir()
	client := filepath.Join(dir, "broken-rdp")
	if err := os.WriteFile(client, []byte("#!/bin/sh\necho display-unavailable >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := launchDesktopClient(exec.Command(client), time.Second)
	if err == nil || !strings.Contains(err.Error(), "display-unavailable") {
		t.Fatalf("immediate failure was not reported: %v", err)
	}
}
