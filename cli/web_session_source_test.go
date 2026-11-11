package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionSourceIsOriginalAndScoped(t *testing.T) {
	loot, session := sessionFixture(t)
	edited := "curated output"
	if _, err := editSession("acme", session.ID, sessionChanges{Entries: []entryChange{{ID: session.Entries[0].ID, Output: &edited}}}); err != nil {
		t.Fatal(err)
	}
	handler := testWebServer()
	read := func(id string, archived bool) *httptest.ResponseRecorder {
		target := "/api/session/source?name=acme&id=" + url.QueryEscape(id)
		if archived {
			target += "&archived=1"
		}
		req := httptest.NewRequest("GET", target, nil)
		rec := httptest.NewRecorder()
		handler.handleSessionSource(rec, req)
		return rec
	}
	original, _ := os.ReadFile(filepath.Join(loot, "sessions", "capture.json"))
	var got struct {
		Text   string `json:"text"`
		Format string `json:"format"`
	}
	response := read("capture", false)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != string(original) || got.Format != "session-json" {
		t.Fatal("source must expose original stored JSON, not curated or reconstructed text")
	}
	transcript := "Script started on 2026-10-25 09:00:00+01:00\n❯ printf synthetic\n\x1b[31msynthetic\x1b[0m\nScript done on 2026-10-25 09:00:01+01:00\n"
	path := filepath.Join(loot, "sessions", "raw-source.log")
	if err := os.WriteFile(path, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := loadSessions(path, "acme")
	if err != nil || len(parsed) == 0 {
		t.Fatalf("synthetic capture did not parse: %v", err)
	}
	rawResponse := read(parsed[0].ID, false)
	if rawResponse.Code != 200 {
		t.Fatal(rawResponse.Body.String())
	}
	if err := json.Unmarshal(rawResponse.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Format != "transcript" || got.Text != transcript {
		t.Fatal("source must preserve transcript text and ANSI escapes")
	}

	if response := read("../../.atuin/history.db", false); response.Code != 404 {
		t.Fatalf("unlisted ID accepted: %d", response.Code)
	}
	if err := archiveSession("acme", "capture"); err != nil {
		t.Fatal(err)
	}
	if read("capture", false).Code != 404 || read("capture", true).Code != 200 {
		t.Fatal("source escaped the requested archive collection")
	}
}
