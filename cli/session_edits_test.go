package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sessionFixture(t *testing.T) (string, *Session) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PENTEST_LOOT_DIR", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	loot := filepath.Join(root, "acme")
	if err := os.MkdirAll(filepath.Join(loot, ".atuin"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(loot, ".atuin", "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE history (id TEXT, timestamp INTEGER, duration INTEGER, exit INTEGER, command TEXT, intent TEXT, deleted_at INTEGER); INSERT INTO history VALUES ('row-1', 1700000000000000000, 1000, 0, 'whoami', NULL, NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	session := &Session{ID: "capture", Engagement: "acme", Source: "imported-paste", Title: "Fixture", Entries: []SessionEntry{
		{Command: "whoami", Output: "kali", Cwd: "~/pentest"},
		{Command: "whoami", Output: "kali again", Cwd: "~/pentest"},
		{Command: "nmap example.test", Output: "original ports", Cwd: "~/pentest"},
	}}
	if err := os.MkdirAll(filepath.Join(loot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(session)
	if err := os.WriteFile(filepath.Join(loot, "sessions", "capture.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	engagementCache.Lock()
	engagementCache.list = []Engagement{{Name: "acme", DB: filepath.Join(loot, ".atuin", "history.db")}}
	engagementCache.at = time.Now()
	engagementCache.Unlock()
	t.Cleanup(invalidateEngagementCaches)
	loaded, err := getSession("acme", "capture")
	if err != nil {
		t.Fatal(err)
	}
	return loot, loaded
}

func TestSessionEditsKeepOriginalsAndSearchCuratedOutput(t *testing.T) {
	loot, original := sessionFixture(t)
	capture, _ := os.ReadFile(filepath.Join(loot, "sessions", "capture.json"))
	db, _ := fileDigest(filepath.Join(loot, ".atuin", "history.db"))
	if original.Entries[0].ID == original.Entries[1].ID {
		t.Fatal("repeated commands need distinct IDs")
	}
	command, output, deleted, name := "edited nmap", strings.Repeat("x", 100000)+"curated marker", true, "Recon"
	changes := sessionChanges{DisplayName: &name, Entries: []entryChange{
		{ID: original.Entries[0].ID, Deleted: &deleted},
		{ID: original.Entries[2].ID, Command: &command, Output: &output},
	}}
	got, err := editSession("acme", original.ID, changes)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Entries[0].Deleted || !got.Entries[2].Edited || got.Entries[2].Output != output || *got.DisplayName != name {
		t.Fatalf("edits missing: %+v", got)
	}
	if original.Entries[0].Deleted || original.Entries[2].Command != "nmap example.test" {
		t.Fatal("cached original was mutated")
	}
	if raw, _ := os.ReadFile(filepath.Join(loot, "sessions", "capture.json")); !bytes.Equal(raw, capture) {
		t.Fatal("raw capture changed")
	}
	if after, _ := fileDigest(filepath.Join(loot, ".atuin", "history.db")); after != db {
		t.Fatal("Atuin DB changed")
	}
	hits, err := searchSessions("acme", "curated marker")
	if err != nil || len(hits) != 1 || hits[0].Index != 2 {
		t.Fatalf("curated search: %+v, %v", hits, err)
	}
	hits, err = searchSessions("acme", "whoami")
	if err != nil || len(hits) != 1 || hits[0].Index != 1 {
		t.Fatalf("deleted entries must stay out of search: %+v, %v", hits, err)
	}
	reset, err := editSession("acme", original.ID, sessionChanges{Entries: []entryChange{{ID: original.Entries[2].ID, Reset: true}}})
	if err != nil || reset.Entries[2].Edited || reset.Entries[2].Output != "original ports" {
		t.Fatalf("reset failed: %+v, %v", reset, err)
	}
}

func TestEntryIDsSurviveGrowingOutputAndRejectStaleEdits(t *testing.T) {
	_, original := sessionFixture(t)
	copy := *original
	copy.Entries = append([]SessionEntry(nil), original.Entries...)
	copy.Entries[0].Output += "\nnew output"
	copy.Entries = append(copy.Entries, SessionEntry{Command: "whoami", Cwd: "~/pentest"})
	identifyEntries(&copy)
	for i := range original.Entries {
		if original.Entries[i].ID != copy.Entries[i].ID {
			t.Fatal("appending output changed entry identity")
		}
	}
	deleted := true
	if _, err := editSession("acme", original.ID, sessionChanges{Entries: []entryChange{{ID: original.Entries[0].ID, Deleted: &deleted}, {ID: "unknown", Deleted: &deleted}}}); err == nil {
		t.Fatal("unknown ID accepted")
	}
	state, _ := readSessionEdits("acme")
	if len(state.Sessions) != 0 {
		t.Fatal("invalid bulk edit was partially applied")
	}
}

func TestBulkRecordingDeleteAndRestore(t *testing.T) {
	_, original := sessionFixture(t)
	srv := testWebServer()
	for _, deleted := range []bool{true, false} {
		body, _ := json.Marshal(map[string]any{"ids": []string{original.ID}, "deleted": deleted})
		req := httptest.NewRequest("PATCH", "/api/sessions?name=acme", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		srv.handleSessionsDelete(rec, req)
		if rec.Code != 200 {
			t.Fatalf("delete/restore: %d %s", rec.Code, rec.Body.String())
		}
		state, _ := readSessionEdits("acme")
		if state.Sessions[original.ID].Deleted != deleted {
			t.Fatal("bulk recording state did not persist")
		}
	}
}
