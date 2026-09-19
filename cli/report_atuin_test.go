package main

import "testing"

func TestExtractTargets(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"nmap -sV 10.0.0.5", "10.0.0.5"},
		{"curl https://dev.azure.com/example-org/", "https://dev.azure.com/example-org/"},
		{"ping host01.example.com", "host01.example.com"},
		{"nmap 10.0.0.1 10.0.0.2 10.0.0.1", "10.0.0.1, 10.0.0.2"},              // dedup
		{"git clone https://github.com/x/y.git", "https://github.com/x/y.git"}, // url kept as URL
		{"sudo apt install nmap", ""},                                          // no target
		{"ssh user@10.0.0.5 -p 22", "10.0.0.5"},
		{"nmap 999.1.1.1", ""}, // invalid octet dropped
	}
	for _, c := range cases {
		if got := extractTargets(c.cmd); got != c.want {
			t.Errorf("extractTargets(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestIsNoise(t *testing.T) {
	noise := []string{"cd /home", "ls -la", "clear", "curl ifconfig.me", "ping www.google.de", "vim notes.txt"}
	// Enumeration / post-exploitation commands must NOT be auto-hidden.
	signal := []string{"nmap -sV 10.0.0.5", "curl https://target.internal/api", "sudo apt install recon-ng",
		"ping 10.0.0.5", "whoami", "id", "cat /etc/passwd", "env", "history"}
	for _, c := range noise {
		if !isNoise(c) {
			t.Errorf("isNoise(%q) = false, want true", c)
		}
	}
	for _, c := range signal {
		if isNoise(c) {
			t.Errorf("isNoise(%q) = true, want false", c)
		}
	}
}

func TestFmtTS(t *testing.T) {
	// 1785243774213746445 ns = 2026-05-26 in Berlin; just assert format shape DD/MM/YYYY HH:MM
	got := fmtTS(1785243774213746445)
	if len(got) != len("02/01/2006 15:04") {
		t.Errorf("fmtTS format unexpected: %q", got)
	}
}
