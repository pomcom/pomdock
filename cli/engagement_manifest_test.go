package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func discoveryFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PENTEST_LOOT_DIR", root)
	t.Setenv("POMDOCK_WORKSPACES_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	invalidateEngagementCaches()
	t.Cleanup(invalidateEngagementCaches)
	return root
}

func putDiscoveryFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestEngagementDiscoveryWithoutAtuin(t *testing.T) {
	for _, tc := range []struct{ name, file, data string }{
		{"manifest", ".pomdock/engagement.json", `{"version":1,"name":"acme","backend":"docker"}`},
		{"transcript", "sessions/capture.log", "Script started on 2026-11-01 10:00:00\nfixture transcript\n"},
		{"capture", "sessions/capture.json", `{"id":"capture","source":"imported-paste","raw":"fixture"}`},
		{"index", "sessions/commands.jsonl", "{\"v\":2,\"subid\":\"recording\",\"cmd\":\"printf fixture\"}\n{\"partial\":"},
		{"archived-transcript", "sessions/.archive/capture.log", "Script started on 2026-11-01 10:00:00\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := discoveryFixture(t)
			loot := filepath.Join(root, "acme")
			putDiscoveryFile(t, filepath.Join(loot, tc.file), tc.data)
			got, err := listEngagements()
			if err != nil || len(got) != 1 {
				t.Fatalf("discovery: %+v, %v", got, err)
			}
			wantDB := filepath.Join(loot, ".atuin", "history.db")
			if got[0].Name != "acme" || got[0].DB != wantDB || got[0].HistoryAvailable || got[0].Archived {
				t.Fatalf("unexpected engagement: %+v", got[0])
			}
			if dir, err := sessionsDir("acme"); err != nil || dir != filepath.Join(loot, "sessions") {
				t.Fatalf("recording access without Atuin: %q, %v", dir, err)
			}
			if _, err := os.Stat(wantDB); !os.IsNotExist(err) {
				t.Fatalf("discovery created history: %v", err)
			}
		})
	}
}

func TestEngagementDiscoveryRejectsUnrelatedFolders(t *testing.T) {
	root := discoveryFixture(t)
	for file, data := range map[string]string{
		"corrupt/.pomdock/engagement.json":  `{"version":1,`,
		"future/.pomdock/engagement.json":   `{"version":2,"name":"future"}`,
		"unsafe/.pomdock/engagement.json":   `{"version":1,"name":"../unsafe"}`,
		"unrelated/sessions/readme.json":    `{"id":"notes","source":"unknown"}`,
		"unrelated/sessions/output.log":     "ordinary text\n",
		"unrelated/sessions/commands.jsonl": `{"v":2,"subid":"fixture"}`,
	} {
		putDiscoveryFile(t, filepath.Join(root, file), data)
	}
	got, err := listEngagements()
	if err != nil || len(got) != 0 {
		t.Fatalf("unrelated folders discovered: %+v, %v", got, err)
	}
}

func TestRememberedEngagementDiscoveryWithoutDocker(t *testing.T) {
	discoveryFixture(t)
	loot := filepath.Join(t.TempDir(), "custom-loot")
	putDiscoveryFile(t, filepath.Join(loot, ".pomdock", "engagement.json"), `{"version":1,"name":"client-fixture"}`)
	roots, _ := json.Marshal(map[string]string{"old-container-name": loot})
	rootsPath, err := archiveRootsPath()
	if err != nil {
		t.Fatal(err)
	}
	putDiscoveryFile(t, rootsPath, string(roots))
	got, err := listEngagements()
	if err != nil || len(got) != 1 || got[0].Name != "client-fixture" || got[0].DB != filepath.Join(loot, ".atuin", "history.db") {
		t.Fatalf("offline custom engagement: %+v, %v", got, err)
	}
}

func TestLegacyEngagementDiscoveryAndDefault(t *testing.T) {
	root := discoveryFixture(t)
	putDiscoveryFile(t, filepath.Join(root, ".atuin", "history.db"), "")
	putDiscoveryFile(t, filepath.Join(root, "legacy", ".atuin", "history.db"), "")
	got, err := listEngagements()
	if err != nil || len(got) != 2 {
		t.Fatalf("legacy discovery: %+v, %v", got, err)
	}
	if got[0].Name != "(default)" || got[1].Name != "legacy" || !got[0].HistoryAvailable || !got[1].HistoryAvailable {
		t.Fatalf("legacy identities changed: %+v", got)
	}
}

func TestContainerLootLabelFallback(t *testing.T) {
	discoveryFixture(t)
	bin := t.TempDir()
	putDiscoveryFile(t, filepath.Join(bin, "docker"), "#!/bin/sh\nprintf '/fixture\\t\\t/tmp/fixture-loot\\n'\n")
	if err := os.Chmod(filepath.Join(bin, "docker"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	got := atuinMountSources([]string{"fixture"})
	if got["fixture"] != "/tmp/fixture-loot/.atuin" {
		t.Fatalf("loot label was ignored: %+v", got)
	}
}
