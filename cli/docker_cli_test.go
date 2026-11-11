package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mockCLIBackend(t *testing.T) string {
	t.Helper()
	old := repoRoot
	repoRoot = t.TempDir()
	t.Cleanup(func() { repoRoot = old })
	log := filepath.Join(repoRoot, "called")
	t.Setenv("POMDOCK_TEST_ARGS", log)
	if err := os.WriteFile(filepath.Join(repoRoot, "pentest.sh"), []byte("#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$POMDOCK_TEST_ARGS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestDockerCLIPassesExactTargets(t *testing.T) {
	for _, test := range []struct {
		args, want []string
	}{
		{[]string{"exec", "acme"}, []string{"--name", "acme", "exec"}},
		{[]string{"exec", "--name", "acme"}, []string{"--name", "acme", "exec"}},
		{[]string{"start", "acme", "--vpn", "/tmp/synthetic profile.conf"}, []string{"--name", "acme", "--vpn", "/tmp/synthetic profile.conf", "start"}},
		{[]string{"start", "acme", "--route", "tor"}, []string{"--name", "acme", "--route", "tor", "start"}},
		{[]string{"stop", "acme"}, []string{"--name", "acme", "stop"}},
		{[]string{"logs", "acme"}, []string{"--name", "acme", "logs"}},
		{[]string{"burp", "--name", "acme"}, []string{"--name", "acme", "burp"}},
		{[]string{"rm", "acme"}, []string{"--name", "acme", "rm"}},
		{[]string{"rm", "acme", "--purge", "--yes"}, []string{"--name", "acme", "--purge", "--yes", "rm"}},
		{[]string{"build", "--toolset", "minimal", "--fresh"}, []string{"--toolset", "minimal", "--fresh", "build"}},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			log := mockCLIBackend(t)
			cmd := newRootCommand()
			cmd.SetArgs(append([]string{"docker"}, test.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("backend got %q, want %q", got, test.want)
			}
		})
	}
}

func TestDockerCLIRejectsAmbiguousOrDestructiveDefaults(t *testing.T) {
	for _, args := range [][]string{
		{"rm"}, {"rm", "acme", "--yes"}, {"exec", "acme", "other"}, {"logs", "acme", "--name", "other"},
		{"burp", "bad/name"}, {"status", "acme", "other"}, {"build", "extra"}, {"build", "--toolset", "invalid"},
		{"start", "acme", "--route", "bad"}, {"exec", "acme", "--route", "direct", "--vpn", "synthetic.conf"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			log := mockCLIBackend(t)
			cmd := newRootCommand()
			cmd.SetArgs(append([]string{"docker"}, args...))
			if err := cmd.Execute(); err == nil {
				t.Fatal("invalid invocation accepted")
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatal("backend invoked for invalid request")
			}
		})
	}
}

func TestDockerStatusFiltersRequestedContainer(t *testing.T) {
	bin := t.TempDir()
	fixture := `#!/bin/sh
cat <<'JSON'
{"Names":"acme","ID":"one","Status":"Up 2 hours","Image":"pcm-kali-pentest","Labels":"io.pomdock.route=direct"}
{"Names":"other","ID":"two","Status":"Exited (0)","Image":"pcm-kali-pentest","Labels":"io.pomdock.route=vpn"}
JSON
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	for _, args := range [][]string{{"acme"}, {"--name", "acme"}} {
		cmd := dockerStatus()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "acme") || strings.Contains(output.String(), "other") {
			t.Fatalf("wrong scope: %s", output.String())
		}
	}
	cmd := dockerStatus()
	cmd.SetArgs([]string{"missing"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("missing target presented as successful status")
	}
}
