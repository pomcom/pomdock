package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
