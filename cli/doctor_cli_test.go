package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPythonSupported(t *testing.T) {
	for _, test := range []struct {
		output    string
		supported bool
	}{
		{"Python 3.11.0", true}, {"Python 3.14.1\n", true}, {"Python 4.0.0", true},
		{"Python 3.10.9", false}, {"Python 2.7.18", false}, {"other 3.99", false}, {"Python invalid", false},
	} {
		if got := pythonSupported(test.output); got != test.supported {
			t.Errorf("%q = %v", test.output, got)
		}
	}
}

func TestDoctorIsReadOnlyAndReportsImageMismatch(t *testing.T) {
	old := repoRoot
	repoRoot = makeRuntimeRoot(t)
	t.Cleanup(func() { repoRoot = old })
	if err := os.MkdirAll(filepath.Join(repoRoot, "scripts", "recording"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"session-log.sh", "cmdlog.zsh", "cmdlog.bash"} {
		if err := os.WriteFile(filepath.Join(repoRoot, "scripts", "recording", file), []byte("# synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "scripts", "pomdock-tmux"), []byte("# synthetic helper"), 0700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	t.Setenv("POMDOCK_DOCTOR_CALLS", calls)
	for name, body := range map[string]string{
		"python3": "#!/bin/sh\necho 'Python 3.11.9'\n",
		"tmux":    "#!/bin/sh\necho 'tmux 3.4'\n",
		"docker": `#!/bin/sh
printf '%s\n' "$*" >> "$POMDOCK_DOCTOR_CALLS"
case "$1" in
  --version) echo 'Docker version synthetic';;
  version) echo '28.synthetic';;
  image) echo '{"io.pomdock.recording-format":"2"}';;
  *) exit 99;;
esac
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	checks := collectDoctorChecks(context.Background())
	foundMismatch := false
	for _, check := range checks {
		if check.Status == "error" {
			t.Fatalf("unexpected error: %+v", check)
		}
		if check.Name == "image" && check.Status == "warning" && strings.Contains(check.Detail, "rebuild") {
			foundMismatch = true
		}
	}
	if !foundMismatch {
		t.Fatal("stale image not reported")
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "--version" && !strings.HasPrefix(line, "version --format ") && !strings.HasPrefix(line, "image inspect ") {
			t.Fatalf("unexpected docker action: %s", line)
		}
	}
}

func TestDoctorHostTmuxOptionalAndContainerRuntimeExplicit(t *testing.T) {
	old := repoRoot
	repoRoot = makeRuntimeRoot(t)
	t.Cleanup(func() { repoRoot = old })
	for _, relative := range []string{"scripts/recording/session-log.sh", "scripts/recording/cmdlog.zsh", "scripts/recording/cmdlog.bash", "scripts/pomdock-tmux"} {
		path := filepath.Join(repoRoot, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# synthetic runtime"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	for name, body := range map[string]string{
		"python3": "#!/bin/sh\necho 'Python 3.11.9'\n",
		"docker": `#!/bin/sh
case "$1" in
 --version) echo 'Docker synthetic';;
 version) echo '28.synthetic';;
 image) printf '%s\n' "$POMDOCK_DOCTOR_TEST_LABELS";;
 *) exit 99;;
esac
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("POMDOCK_DOCTOR_TEST_LABELS", `{"io.pomdock.recording-format":"3","io.pomdock.container-tmux":"1"}`)
	checks := collectDoctorChecks(context.Background())
	optional := false
	for _, check := range checks {
		if check.Status == "error" {
			t.Fatalf("Docker-only setup rejected: %+v", check)
		}
		if check.Name == "tmux" && check.Status == "warning" && strings.Contains(check.Detail, "host/VM terminals") && strings.Contains(check.Detail, "inside the container") {
			optional = true
		}
	}
	if !optional {
		t.Fatal("missing host tmux not explained as optional")
	}
	t.Setenv("POMDOCK_DOCTOR_TEST_LABELS", `{"io.pomdock.recording-format":"3"}`)
	missingMarker := false
	for _, check := range collectDoctorChecks(context.Background()) {
		if check.Name == "image" && check.Status == "warning" && strings.Contains(check.Detail, "container tmux") {
			missingMarker = true
		}
	}
	if !missingMarker {
		t.Fatal("older image capability not reported")
	}
	if err := os.Remove(filepath.Join(repoRoot, "scripts", "pomdock-tmux")); err != nil {
		t.Fatal(err)
	}
	missingHelper := false
	for _, check := range collectDoctorChecks(context.Background()) {
		if check.Status == "error" && strings.Contains(check.Detail, "scripts/pomdock-tmux") {
			missingHelper = true
		}
	}
	if !missingHelper {
		t.Fatal("missing bundled container helper not reported")
	}
}
