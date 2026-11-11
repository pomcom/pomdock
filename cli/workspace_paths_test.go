package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceDiscoveryFixture(t *testing.T) string {
	t.Helper()
	discoveryFixture(t)
	t.Setenv("PENTEST_LOOT_DIR", "")
	t.Setenv("POMDOCK_WORKSPACES_DIR", "")
	return os.Getenv("HOME")
}

func TestWorkspaceDefaultsAndExplicitRoot(t *testing.T) {
	home := workspaceDiscoveryFixture(t)
	expected := filepath.Join(home, "pomdock", "workspaces")
	if got := workspaceRoot(); got != expected {
		t.Fatalf("root=%q, want %q", got, expected)
	}
	for _, name := range []string{"", "(default)", "pcm-pentest"} {
		if got := defaultWorkspaceDir(name); got != filepath.Join(expected, "pcm-pentest") {
			t.Fatalf("default %q points at shared root: %s", name, got)
		}
	}
	if got := defaultWorkspaceDir("lab"); got != filepath.Join(expected, "lab") {
		t.Fatalf("named workspace=%q", got)
	}
	t.Setenv("POMDOCK_WORKSPACES_DIR", "~/alternate-workspaces")
	t.Setenv("PENTEST_LOOT_DIR", filepath.Join(home, "custom-exact-binding"))
	if got := workspaceRoot(); got != filepath.Join(home, "alternate-workspaces") {
		t.Fatalf("legacy exact binding overrode canonical root: %s", got)
	}
}

func TestCanonicalDefaultOwnsNamedDirectoryNotWorkspaceRoot(t *testing.T) {
	workspaceDiscoveryFixture(t)
	root := workspaceRoot()
	// Root-level unrelated history must never become a new default workspace.
	putDiscoveryFile(t, filepath.Join(root, ".atuin", "history.db"), "root sentinel")
	want := defaultWorkspaceDir("pcm-pentest")
	putDiscoveryFile(t, filepath.Join(want, ".pomdock", "engagement.json"), `{"version":1,"name":"(default)","container":"pcm-pentest"}`)
	got, err := listEngagements()
	if err != nil || len(got) != 1 || got[0].Name != "(default)" || got[0].DB != filepath.Join(want, ".atuin", "history.db") {
		t.Fatalf("default mapping: %+v %v", got, err)
	}
	resolved, err := lootDirForEngagement("(default)")
	if err != nil || resolved != want {
		t.Fatalf("default lookup=%q %v", resolved, err)
	}
}

func TestWorkspaceDiscoveryIncludesLegacyDataWithoutRewritingIt(t *testing.T) {
	home := workspaceDiscoveryFixture(t)
	canonical := defaultWorkspaceDir("new-instance")
	legacy := filepath.Join(home, "pentest")
	putDiscoveryFile(t, filepath.Join(canonical, ".pomdock", "engagement.json"), `{"version":1,"name":"new-instance"}`)
	putDiscoveryFile(t, filepath.Join(legacy, ".atuin", "history.db"), "")
	legacyCapture := `{"id":"recording","source":"imported-paste","entries":[{"command":"cd /home/kali/pentest && printf original","cwd":"~/pentest","output":"ORIGINAL"}]}`
	path := filepath.Join(legacy, "old-instance", "sessions", "recording.json")
	putDiscoveryFile(t, path, legacyCapture)
	list, err := listEngagements()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"new-instance": canonical, "(default)": legacy, "old-instance": filepath.Join(legacy, "old-instance")}
	if len(list) != len(expected) {
		t.Fatalf("legacy/canonical discovery: %+v", list)
	}
	for _, entry := range list {
		if entry.DB != filepath.Join(expected[entry.Name], ".atuin", "history.db") {
			t.Fatalf("wrong mapping: %+v", entry)
		}
	}
	after, _ := os.ReadFile(path)
	if string(after) != legacyCapture {
		t.Fatal("discovery rewrote historical command/cwd")
	}
	if _, err := os.Stat(filepath.Join(canonical, ".atuin", "history.db")); !os.IsNotExist(err) {
		t.Fatal("discovery created history")
	}
}

func TestRememberedCustomWorkspaceNameWinsOverDirectoryGuess(t *testing.T) {
	workspaceDiscoveryFixture(t)
	custom := filepath.Join(workspaceRoot(), "different-basename")
	putDiscoveryFile(t, filepath.Join(custom, "sessions", "recording.json"), `{"id":"capture","source":"imported-paste"}`)
	if err := rememberArchiveRoot("client-engagement", custom); err != nil {
		t.Fatal(err)
	}
	list, err := listEngagements()
	if err != nil || len(list) != 1 || list[0].Name != "client-engagement" || list[0].DB != filepath.Join(custom, ".atuin", "history.db") {
		t.Fatalf("custom identity lost: %+v %v", list, err)
	}
}

func TestWorkspaceAndLegacyRootSymlinkDoNotDuplicateEngagements(t *testing.T) {
	home := workspaceDiscoveryFixture(t)
	canonical := defaultWorkspaceDir("lab")
	putDiscoveryFile(t, filepath.Join(canonical, ".pomdock", "engagement.json"), `{"version":1,"name":"lab"}`)
	if err := os.Symlink(workspaceRoot(), filepath.Join(home, "pentest")); err != nil {
		t.Fatal(err)
	}
	list, err := listEngagements()
	if err != nil || len(list) != 1 || list[0].Name != "lab" {
		t.Fatalf("migration symlink duplicated engagement: %+v %v", list, err)
	}
}

func TestWorkspaceMountDiscoveryAcceptsNewLegacyAndCustomBindings(t *testing.T) {
	workspaceDiscoveryFixture(t)
	for _, tc := range []struct {
		name   string
		fields []string
		want   string
	}{
		{"new mount beats stale labels", []string{"/lab", "", "/old-label", "/new-mount", "", "/new-label"}, "/new-mount/.atuin"},
		{"old mount", []string{"/lab", "", "/old-label", "", "/old-mount", ""}, "/old-mount/.atuin"},
		{"new label", []string{"/lab", "", "/old-label", "", "", "/new-label"}, "/new-label/.atuin"},
		{"old label", []string{"/lab", "", "/old-label"}, "/old-label/.atuin"},
		{"actual history binding", []string{"/lab", "/actual/.atuin", "/stale-label"}, "/actual/.atuin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %s\n", shellQuote(strings.Join(tc.fields, "\t")))
			putDiscoveryFile(t, filepath.Join(bin, "docker"), script)
			if err := os.Chmod(filepath.Join(bin, "docker"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			if got := atuinMountSources([]string{"lab"})["lab"]; got != tc.want {
				t.Fatalf("mount source=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestActualCustomWorkspaceBindingRemainsNamedAfterContainer(t *testing.T) {
	workspaceDiscoveryFixture(t)
	custom := filepath.Join(t.TempDir(), "some-workspace-directory")
	putDiscoveryFile(t, filepath.Join(custom, "sessions", "recording.json"), `{"id":"capture","source":"imported-paste"}`)
	ps, _ := json.Marshal(dockerPS{Names: "client-instance", ID: "fixture", Status: "Up 1 minute", Image: "pcm-kali-pentest", Labels: "io.pomdock.role=pentest,io.pomdock.profile=kali"})
	inspect := strings.Join([]string{"/client-instance", "", "", "", custom, ""}, "\t")
	bin := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in ps) printf '%%s\\n' %s;; inspect) printf '%%s\\n' %s;; *) exit 1;; esac\n", shellQuote(string(ps)), shellQuote(inspect))
	putDiscoveryFile(t, filepath.Join(bin, "docker"), script)
	if err := os.Chmod(filepath.Join(bin, "docker"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	list, err := listEngagements()
	if err != nil || len(list) != 1 || list[0].Name != "client-instance" || list[0].DB != filepath.Join(custom, ".atuin", "history.db") {
		t.Fatalf("actual binding not discoverable: %+v %v", list, err)
	}
}
