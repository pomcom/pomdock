package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestShowRootHelp(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{
		Use:  "pomdock",
		Args: cobra.NoArgs,
		RunE: showRootHelp,
	}
	cmd.AddCommand(&cobra.Command{Use: "tui", Run: func(_ *cobra.Command, _ []string) {
		t.Fatal("tui command must not run for bare pomdock")
	}})
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("bare root command: %v", err)
	}
	for _, want := range []string{"Usage:", "pomdock [command]", "tui"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("help output missing %q:\n%s", want, output.String())
		}
	}
}

func makeRuntimeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pentest.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "kali-vm"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFirstRepoRootUsesFirstValidCandidate(t *testing.T) {
	wanted := makeRuntimeRoot(t)
	got, ok := firstRepoRoot([]string{t.TempDir(), wanted, installedRoot})
	if !ok {
		t.Fatal("expected a runtime root")
	}
	if got != wanted {
		t.Fatalf("got %q, want %q", got, wanted)
	}
}

func TestIsRepoRootRequiresScriptsAndVMDirectory(t *testing.T) {
	root := t.TempDir()
	if isRepoRoot(root) {
		t.Fatal("empty directory identified as a runtime root")
	}
	if err := os.WriteFile(filepath.Join(root, "pentest.sh"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if isRepoRoot(root) {
		t.Fatal("root without kali-vm identified as valid")
	}
}

func TestDockerExecTargetForms(t *testing.T) {
	for _, args := range [][]string{{"lab-kali"}, {"--name", "lab-kali"}} {
		cmd := dockerExec()
		if err := cmd.Flags().Parse(args); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Args(cmd, cmd.Flags().Args()); err != nil {
			t.Fatalf("target rejected: %v", err)
		}
	}
	if got := containerDirectShellCommand("lab-kali"); got != "pomdock docker exec --name 'lab-kali'" {
		t.Fatalf("wrong direct command: %s", got)
	}
}

func TestReportCLIErrorPrintsWrappedExitContextAndPreservesStatus(t *testing.T) {
	err := exec.Command("/bin/sh", "-c", "exit 23").Run()
	if err == nil {
		t.Fatal("expected subprocess failure")
	}
	for _, test := range []struct {
		err     error
		message string
		status  int
	}{
		{err, "", 23},
		{fmt.Errorf("cannot inspect storage pool fixture: %w", err), "cannot inspect storage pool fixture", 23},
		{fmt.Errorf("invalid fixture input"), "invalid fixture input", 1},
	} {
		var output bytes.Buffer
		if status := reportCLIError(test.err, &output); status != test.status {
			t.Fatalf("exit status %d, want %d", status, test.status)
		}
		if test.message == "" && output.Len() != 0 {
			t.Fatalf("bare script error duplicated: %s", output.String())
		}
		if test.message != "" && !strings.Contains(output.String(), test.message) {
			t.Fatalf("wrapped context hidden: %s", output.String())
		}
	}
}
