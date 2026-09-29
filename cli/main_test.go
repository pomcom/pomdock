package main

import (
	"bytes"
	"os"
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
