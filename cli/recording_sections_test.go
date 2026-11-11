package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func taggedIndexedFixture(t *testing.T) (string, string, string) {
	t.Helper()
	loot, dir, sub := indexedFixture(t)
	start := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	header := "Script started on 2026-11-06 10:00:00+00:00 [COLUMNS=\"100\" LINES=\"30\"]\n"
	body := fmt.Sprintf("###POMDOCK-SESSION::recon::%d###\r\n\x1b]777;pomdock-start;one\aRECON_RESULT\r\n\x1b]777;pomdock-end;one\a\r\n###POMDOCK-SESSION::exploit::%d###\r\n\x1b]777;pomdock-start;two\aEXPLOIT_RESULT\r\n\x1b]777;pomdock-end;two\a", start.Unix(), start.Unix()+1)
	for ext, data := range map[string]string{".log": header + body + "\nScript done on 2026-11-06 10:00:05+00:00 [COMMAND_EXIT_CODE=\"0\"]\n", ".timing": fmt.Sprintf("O 0.010000 %d\n", len(body))} {
		if err := os.WriteFile(filepath.Join(dir, sub+ext), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	records := []map[string]any{}
	for i, tag := range []string{"recon", "exploit"} {
		records = append(records, map[string]any{"v": 2, "id": []string{"one", "two"}[i], "start": start.Add(time.Duration(i)*time.Second + 100*time.Millisecond).Format(time.RFC3339Nano), "end": start.Add(time.Duration(i)*time.Second + 900*time.Millisecond).Format(time.RFC3339Nano), "exit": 0, "cwd": "/pentest", "session": "acme", "subid": sub, "pid": 45, "pos": 0, "end_pos": 0, "cmd": "printf " + tag})
	}
	var index strings.Builder
	for _, record := range records {
		data, _ := json.Marshal(record)
		index.Write(data)
		index.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "commands.jsonl"), []byte(index.String()), 0600); err != nil {
		t.Fatal(err)
	}
	dropSessionCache("acme", filepath.Join(dir, sub+".log"))
	return loot, dir, sub
}

func TestIndexedTaggedSectionsPreserveIdentityCurationAndArchives(t *testing.T) {
	_, dir, sub := taggedIndexedFixture(t)
	rawBefore, _ := os.ReadFile(filepath.Join(dir, sub+".log"))
	sessions, err := listSessions("acme")
	if err != nil {
		t.Fatal(err)
	}
	tagged := []Session{}
	for _, session := range sessions {
		if session.CaptureID == sub {
			tagged = append(tagged, session)
		}
	}
	if len(tagged) != 2 {
		t.Fatalf("sections missing from list: %+v", sessions)
	}
	for _, section := range tagged {
		if section.Tag == "" || section.CaptureID != sub || len(section.Entries) != 0 {
			t.Fatalf("wrong metadata: %+v", section)
		}
	}
	recon, err := getSession("acme", sub+"#0")
	if err != nil || recon.Tag != "recon" || len(recon.Entries) != 1 || recon.Entries[0].Output != "RECON_RESULT" {
		t.Fatalf("recon: %+v, %v", recon, err)
	}
	exploit, err := getSession("acme", sub+"#1")
	if err != nil || exploit.Tag != "exploit" || len(exploit.Entries) != 1 {
		t.Fatalf("exploit: %+v, %v", exploit, err)
	}
	aggregate, err := getSession("acme", sub)
	if err != nil || len(aggregate.Entries) != 2 || aggregate.Entries[0].ID != recon.Entries[0].ID {
		t.Fatalf("raw shell aggregate: %+v, %v", aggregate, err)
	}
	parentCommand, childOutput := "parent edited recon", "CURATED_RECON"
	if _, err = editSession("acme", sub, sessionChanges{Entries: []entryChange{{ID: recon.Entries[0].ID, Command: &parentCommand}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = editSession("acme", recon.ID, sessionChanges{Entries: []entryChange{{ID: recon.Entries[0].ID, Output: &childOutput}}}); err != nil {
		t.Fatal(err)
	}
	state, err := readSessionEdits("acme")
	if err != nil {
		t.Fatal(err)
	}
	state.Sessions[exploit.ID] = sessionEdit{Deleted: true}
	if err = writeSessionEdits("acme", state); err != nil {
		t.Fatal(err)
	}
	curated := curateSession(aggregate, state)
	if curated.Entries[0].Command != parentCommand || curated.Entries[0].Output != childOutput || curated.Entries[0].Deleted || !curated.Entries[1].Deleted {
		t.Fatalf("curation crossed section boundaries: %+v", curated.Entries)
	}
	if _, err = editSession("acme", recon.ID, sessionChanges{Entries: []entryChange{{ID: exploit.Entries[0].ID, Output: &childOutput}}}); err == nil {
		t.Fatal("editing sibling entry through wrong section accepted")
	}
	if err = archiveSession("acme", recon.ID); err != nil {
		t.Fatal(err)
	}
	archived, err := getArchivedSession("acme", recon.ID)
	if err != nil || archived.Entries[0].ID != recon.Entries[0].ID {
		t.Fatalf("archive lost section identity: %+v %v", archived, err)
	}
	if err = unarchiveSession("acme", exploit.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := getSession("acme", sub)
	if err != nil || len(restored.Entries) != 2 {
		t.Fatalf("restore lost sibling: %+v %v", restored, err)
	}
	if curateSession(restored, state).Entries[0].Output != childOutput {
		t.Fatal("restore lost curation")
	}
	rawAfter, _ := os.ReadFile(filepath.Join(dir, sub+".log"))
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("tag operations modified original capture")
	}
}

func TestTaggedRecordingAPIUsesSectionEditsAndOriginalReplay(t *testing.T) {
	_, _, sub := taggedIndexedFixture(t)
	recon, err := getSession("acme", sub+"#0")
	if err != nil {
		t.Fatal(err)
	}
	replacement := "CURATED_RECON"
	if _, err = editSession("acme", recon.ID, sessionChanges{Entries: []entryChange{{ID: recon.Entries[0].ID, Output: &replacement}}}); err != nil {
		t.Fatal(err)
	}
	state, _ := readSessionEdits("acme")
	state.Sessions[sub+"#1"] = sessionEdit{Deleted: true}
	if err = writeSessionEdits("acme", state); err != nil {
		t.Fatal(err)
	}
	handler := testWebServer().handler()
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1:7332"
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret-token"})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	for _, tc := range []struct {
		query string
		want  bool
	}{{"RECON_RESULT", false}, {"CURATED_RECON", true}, {"EXPLOIT_RESULT", false}} {
		response := get("/api/recordings/search?name=acme&q=" + url.QueryEscape(tc.query))
		if response.Code != 200 {
			t.Fatalf("search: %d %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), tc.query) != tc.want {
			t.Fatalf("search ignored section edits: %s", response.Body.String())
		}
	}
	response := get("/api/recordings/timeline?name=acme")
	if response.Code != 200 || strings.Contains(response.Body.String(), `"cmd":"printf exploit"`) || !strings.Contains(response.Body.String(), `"sessionId":"`+sub+`#0"`) {
		t.Fatalf("timeline: %d %s", response.Code, response.Body.String())
	}
	response = get("/api/recordings/cast?name=acme&shell=" + sub)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "EXPLOIT_RESULT") {
		t.Fatalf("raw replay no longer original: %d %s", response.Code, response.Body.String())
	}
	var transcript struct {
		Lines []struct {
			T    int64  `json:"t"`
			Text string `json:"text"`
		} `json:"lines"`
	}
	dir, _ := sessionsDir("acme")
	if err = runRecordingEngine(context.Background(), "acme", dir, "transcript", map[string]any{"sub": sub}, &transcript); err != nil {
		t.Fatal(err)
	}
	if len(transcript.Lines) == 0 || transcript.Lines[0].T == 0 {
		t.Fatal("clean transcript has no actual clock")
	}
}
