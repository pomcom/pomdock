package main

import (
	"strings"
	"testing"
	"time"
)

func TestIsPentestContainer(t *testing.T) {
	tests := []struct {
		name, image, labels string
		want                bool
	}{
		{name: "client-a", image: "pcm-kali-pentest", want: true},
		{name: "custom", image: "other", labels: "io.pomdock.role=pentest", want: true},
		{name: "legacy-pentest", image: "other", want: true},
		{name: "database", image: "postgres", want: false},
	}
	for _, tt := range tests {
		if got := isPentestContainer(tt.name, tt.image, tt.labels); got != tt.want {
			t.Errorf("isPentestContainer(%q, %q, %q) = %v, want %v", tt.name, tt.image, tt.labels, got, tt.want)
		}
	}
}

func TestContainerSidecarNames(t *testing.T) {
	vpn, tor := containerSidecarNames("client-pentest")
	if vpn != "client-pentest-gluetun" || tor != "client-pentest-whonix" {
		t.Fatalf("sidecars = %q, %q", vpn, tor)
	}
	vpn, tor = containerSidecarNames("pcm-pentest")
	if vpn != "pcm-gluetun" || tor != "pcm-whonix" {
		t.Fatalf("default sidecars = %q, %q", vpn, tor)
	}
}

func TestLegacyContainerDetection(t *testing.T) {
	if !isLegacyContainer("io.pomdock.role=pentest,io.pomdock.route=tor") {
		t.Fatal("container without profile label should require an upgrade")
	}
	if isLegacyContainer("io.pomdock.role=pentest,io.pomdock.profile=kali,io.pomdock.route=tor") {
		t.Fatal("current profile label should not require an upgrade")
	}
}

func TestParseCommandOutput(t *testing.T) {
	output, cwd := parseCommandOutput("hello\n"+cwdMarker+"/tmp\n", "/home/kali")
	if output != "hello" {
		t.Fatalf("output = %q", output)
	}
	if cwd != "/tmp" {
		t.Fatalf("cwd = %q, want /tmp", cwd)
	}
}

func TestCreateOptionsFromLabels(t *testing.T) {
	opts, ok := createOptionsFromLabelMap("client-a", map[string]string{
		"io.pomdock.role":     "pentest",
		"io.pomdock.route":    "tor-vpn",
		"io.pomdock.vpn-file": "/home/user/client.ovpn",
	})
	if !ok {
		t.Fatal("expected managed container labels")
	}
	if opts.Name != "client-a" || !opts.Whonix || opts.VPNFile != "/home/user/client.ovpn" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func TestCreateOptionsMigratesOriginalDefaultContainer(t *testing.T) {
	opts, ok := createOptionsFromLabelMap("pcm-pentest", map[string]string{})
	if !ok || opts.Name != "pcm-pentest" || opts.VPNFile != "" || opts.Whonix {
		t.Fatalf("unexpected legacy default options: ok=%v opts=%#v", ok, opts)
	}
}

func TestCommandOutputTimeout(t *testing.T) {
	_, err := commandOutputWithTimeout(20*time.Millisecond, "sh", "-c", "exec sleep 1")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout, got %v", err)
	}
}
