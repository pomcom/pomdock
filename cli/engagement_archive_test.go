package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngagementArchiveRoundTripAndFailurePreservesData(t *testing.T) {
	loot, session := sessionFixture(t)
	name, command := "Named recon", "curated whoami"
	if _, err := editSession("acme", session.ID, sessionChanges{DisplayName: &name, Entries: []entryChange{{ID: session.Entries[0].ID, Command: &command}}}); err != nil {
		t.Fatal(err)
	}
	tracker := []byte(`{"version":1,"rows":{"row-1":{"incl":true,"measure":"Identify user"}},"merged":[]}`)
	if err := os.WriteFile(filepath.Join(loot, ".pomdock", "tracker.json"), tracker, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loot, "notes.txt"), []byte("unrelated loot"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, rel := range []string{".atuin/history.db", "sessions/capture.json", ".pomdock/sessions.json", ".pomdock/tracker.json"} {
		before[rel], _ = fileDigest(filepath.Join(loot, rel))
	}
	file, err := archiveEngagementAt(loot, "acme", "test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range engagementDirs {
		if fileExists(filepath.Join(loot, dir)) {
			t.Fatalf("live %s remains after archive", dir)
		}
	}
	if !fileExists(filepath.Join(loot, "notes.txt")) {
		t.Fatal("unrelated loot was removed")
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
		t.Fatal("archive must be private")
	}
	archives, err := archivesAt(loot)
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives: %+v, %v", archives, err)
	}
	if err := restoreEngagementAt(loot, file, "wrong passphrase"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if fileExists(filepath.Join(loot, ".atuin")) {
		t.Fatal("failed restore created live history")
	}
	if err := restoreEngagementAt(loot, file, "test passphrase", "other-client"); err == nil {
		t.Fatal("restored into the wrong engagement")
	}
	if err := restoreEngagementAt(loot, file, "test passphrase"); err != nil {
		t.Fatal(err)
	}
	for rel, digest := range before {
		if got, _ := fileDigest(filepath.Join(loot, rel)); got != digest {
			t.Fatalf("restore changed %s", rel)
		}
	}
	if !fileExists(file) {
		t.Fatal("restoring must retain the encrypted archive")
	}
	if err := restoreEngagementAt(loot, file, "test passphrase"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("live overwrite was not refused: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(loot, ".pomdock", "tracker.json")); !bytes.Equal(got, tracker) {
		t.Fatal("Tracker was overwritten")
	}
}

func TestEngagementArchiveRollback(t *testing.T) {
	loot, _ := sessionFixture(t)
	digest, _ := fileDigest(filepath.Join(loot, ".atuin", "history.db"))
	if err := os.Symlink(filepath.Join(loot, ".atuin", "history.db"), filepath.Join(loot, "sessions", "bad-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveEngagementAt(loot, "acme", "test passphrase"); err == nil {
		t.Fatal("symlink archive accepted")
	}
	if got, _ := fileDigest(filepath.Join(loot, ".atuin", "history.db")); got != digest {
		t.Fatal("archive failure lost original database")
	}
	if !fileExists(filepath.Join(loot, "sessions", "capture.json")) {
		t.Fatal("archive failure lost recording")
	}
	if leftovers, _ := filepath.Glob(filepath.Join(loot, ".archive-stage-*")); len(leftovers) != 0 {
		t.Fatal("rollback left staging files")
	}
}

func TestArchivedEngagementDiscoveryAndTruncatedRestore(t *testing.T) {
	loot, _ := sessionFixture(t)
	file, err := archiveEngagementAt(loot, "acme", "test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	// Avoid involving a real Docker daemon in discovery tests.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	invalidateEngagementCaches()
	list, err := listEngagements()
	if err != nil || len(list) != 1 || list[0].Name != "acme" || !list[0].Archived {
		t.Fatalf("archive disappeared from picker: %+v, %v", list, err)
	}
	data, _ := os.ReadFile(file)
	if err := os.WriteFile(file, data[:len(data)-20], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreEngagementAt(loot, file, "test passphrase"); err == nil {
		t.Fatal("truncated archive accepted")
	}
	if fileExists(filepath.Join(loot, "sessions")) {
		t.Fatal("corrupt restore committed files")
	}
}

func TestEngagementArchiveCLIRestoreAndPreferences(t *testing.T) {
	loot, _ := sessionFixture(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	prefs, _ := webPrefsPath()
	if err := writeJSONDoc(prefs, []byte(`{"tester":"AB","source":"192.0.2.1","theme":"light","sessionNames":{"[\"acme\",\"capture\"]":"Recon","[\"other\",\"private\"]":"Other client"}}`)); err != nil {
		t.Fatal(err)
	}
	state, err := readSessionEdits("acme")
	if err != nil {
		t.Fatal(err)
	}
	if state.Sessions["capture"].DisplayName == nil || *state.Sessions["capture"].DisplayName != "Recon" {
		t.Fatal("legacy session names were not migrated")
	}
	if err := writeSessionEdits("acme", state); err != nil {
		t.Fatal(err)
	}
	if err := snapshotReportPreferences(loot, "LS"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(loot, ".pomdock", "preferences.json"))
	if strings.Contains(string(data), "Other client") || strings.Contains(string(data), "sessionNames") {
		t.Fatal("archive preferences contain unrelated session names")
	}
	file, err := archiveEngagementAt(loot, "acme", "test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if defaultRestoreDir(file) != loot {
		t.Fatal("CLI default restore destination must be the loot directory")
	}
	if _, err := decryptArchive(file, "test passphrase", loot); err != nil {
		t.Fatal(err)
	}
	restored, err := readSessionEdits("acme")
	if err != nil || *restored.Sessions["capture"].DisplayName != "Recon" {
		t.Fatal("CLI restore lost session edits")
	}
}

func TestMigratedWorkspaceArchivePreservesHistoryAndRecordedPaths(t *testing.T) {
	source, session := sessionFixture(t)
	display := "Keep this recording"
	if _, err := editSession("acme", session.ID, sessionChanges{DisplayName: &display}); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, rel := range []string{".atuin/history.db", "sessions/capture.json", ".pomdock/sessions.json"} {
		digest, err := fileDigest(filepath.Join(source, rel))
		if err != nil {
			t.Fatal(err)
		}
		before[rel] = digest
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("POMDOCK_WORKSPACES_DIR", filepath.Join(home, "pomdock", "workspaces"))
	t.Setenv("PENTEST_LOOT_DIR", "")
	target := defaultWorkspaceDir("acme")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, target); err != nil {
		t.Fatal(err)
	} // synthetic migration only
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	invalidateEngagementCaches()
	resolved, err := lootDirForEngagement("acme")
	if err != nil || resolved != target {
		t.Fatalf("migrated workspace=%q %v", resolved, err)
	}
	archive, err := archiveEngagementAt(target, "acme", "fixture passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if defaultRestoreDir(archive) != target {
		t.Fatal("restore destination changed with workspace root")
	}
	invalidateEngagementCaches()
	list, err := listEngagements()
	if err != nil || len(list) != 1 || list[0].Name != "acme" || !list[0].Archived {
		t.Fatalf("migrated archive disappeared: %+v %v", list, err)
	}
	if err := restoreEngagementAt(target, archive, "fixture passphrase", "acme"); err != nil {
		t.Fatal(err)
	}
	for rel, want := range before {
		got, err := fileDigest(filepath.Join(target, rel))
		if err != nil || got != want {
			t.Fatalf("migration/archive changed %s: %s %v", rel, got, err)
		}
	}
	invalidateEngagementCaches()
	restored, err := getSession("acme", session.ID)
	if err != nil || restored.Entries[0].Cwd != "~/pentest" || restored.Entries[0].ID != session.Entries[0].ID {
		t.Fatalf("historical recording identity/path changed: %+v %v", restored, err)
	}
}
