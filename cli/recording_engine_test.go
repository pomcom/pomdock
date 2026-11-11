package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func indexedFixture(t *testing.T) (string, string, string) {
	t.Helper()
	loot, _ := sessionFixture(t)
	dir := filepath.Join(loot, "sessions")
	sub := "20261106T100000Z-main-pid45"
	header := "Script started on 2026-11-06 10:00:00+00:00 [COLUMNS=\"100\" LINES=\"30\"]\n"
	chunks := []string{"$ printf synthetic\x1b[?2004l\r\n", "SYNTHETIC_RESULT\r\n", "\x1b[?1049h\x1b[2J\x1b[HSCREEN_RESULT\x1b[?1049l"}
	body := strings.Join(chunks, "") + "\nScript done on 2026-11-06 10:00:05+00:00 [COMMAND_EXIT_CODE=\"0\"]\n"
	timing := "H 0.000000 START_TIME 2026-11-06 10:00:00+00:00\n"
	for _, chunk := range chunks {
		timing += fmt.Sprintf("O 1.000000 %d\n", len(chunk))
	}
	for ext, data := range map[string]string{".log": header + body, ".timing": timing, ".start": "2026-11-06T10:00:00.000Z\n"} {
		if err := os.WriteFile(filepath.Join(dir, sub+ext), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	records := []map[string]any{
		{"v": 2, "id": "cmd-one", "start": "2026-11-06T10:00:01.000Z", "cwd": "/pentest", "session": "acme", "subid": sub, "pid": 45, "pos": len(header) + len(chunks[0]), "cmd": "printf synthetic"},
		{"v": 2, "id": "cmd-one", "end": "2026-11-06T10:00:02.000Z", "exit": 0, "end_pos": len(header) + len(chunks[0]) + len(chunks[1])},
		{"v": 2, "id": "cmd-two", "start": "2026-11-06T10:00:02.100Z", "cwd": "/pentest", "session": "acme", "subid": sub, "pid": 45, "cmd": "less synthetic.txt"},
	}
	f, err := os.Create(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if err := json.NewEncoder(f).Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	return loot, dir, sub
}

func TestEmbeddedRecordingEngine(t *testing.T) {
	_, dir, sub := indexedFixture(t)
	var timeline struct {
		Commands []recordingCommand `json:"commands"`
		Shells   []recordingShell   `json:"shells"`
	}
	if err := runRecordingEngine(context.Background(), "acme", dir, "timeline", nil, &timeline); err != nil {
		t.Fatal(err)
	}
	if len(timeline.Commands) != 2 || len(timeline.Shells) != 1 {
		t.Fatalf("wrong timeline: %+v", timeline)
	}
	if timeline.Commands[0].Exit == nil || *timeline.Commands[0].Exit != 0 || timeline.Commands[1].Exit != nil {
		t.Fatal("completion state was fabricated")
	}
	var found struct {
		Groups []json.RawMessage `json:"groups"`
		Error  string            `json:"error"`
	}
	for _, query := range []string{"SYNTHETIC_RESULT", "SCREEN_RESULT", "printf synthetic"} {
		if err := runRecordingEngine(context.Background(), "acme", dir, "search", map[string]any{"query": query}, &found); err != nil {
			t.Fatal(err)
		}
		if len(found.Groups) == 0 {
			t.Fatalf("missing search result %s", query)
		}
	}
	if err := runRecordingEngine(context.Background(), "acme", dir, "search", map[string]any{"query": "[", "regex": true}, &found); err != nil || found.Error == "" {
		t.Fatalf("invalid regex not explained: %v %+v", err, found)
	}
	var info struct {
		Markers [][]any `json:"markers"`
		Table   [][]any `json:"table"`
	}
	if err := runRecordingEngine(context.Background(), "acme", dir, "info", map[string]any{"sub": sub}, &info); err != nil {
		t.Fatal(err)
	}
	if len(info.Markers) != 2 || len(info.Table) == 0 {
		t.Fatal("replay has no command markers/clock")
	}
	var cast struct {
		Path string `json:"path"`
	}
	if err := runRecordingEngine(context.Background(), "acme", dir, "cast", map[string]any{"sub": sub}, &cast); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cast.Path)
	if err != nil || !strings.Contains(string(raw), "SYNTHETIC_RESULT") {
		t.Fatal("cast output missing")
	}
	sessions, err := loadSessions(filepath.Join(dir, sub+".log"), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || len(sessions[0].Entries) != 2 || sessions[0].Entries[0].ID != timeline.Commands[0].ID {
		t.Fatal("curation IDs do not match index")
	}
}

func TestIndexedCaptureArchiveRoundTrip(t *testing.T) {
	_, dir, sub := indexedFixture(t)
	before, err := getSession("acme", sub)
	if err != nil {
		t.Fatal(err)
	}
	if err = archiveSession("acme", sub); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".log", ".timing", ".start", ".commands.jsonl"} {
		if !fileExists(filepath.Join(dir, archiveSubdir, sub+ext)) {
			t.Fatalf("archive lost %s", ext)
		}
	}
	archived, err := getArchivedSession("acme", sub)
	if err != nil || len(archived.Entries) != len(before.Entries) {
		t.Fatalf("archived index missing: %v", err)
	}
	if err = unarchiveSession("acme", sub); err != nil {
		t.Fatal(err)
	}
	after, err := getSession("acme", sub)
	if err != nil || after.Entries[0].ID != before.Entries[0].ID {
		t.Fatalf("restored identity changed: %v", err)
	}
}

func TestRecordingAPIHonorsCurationAndPaths(t *testing.T) {
	_, _, sub := indexedFixture(t)
	session, err := getSession("acme", sub)
	if err != nil {
		t.Fatal(err)
	}
	hidden := true
	if _, err = editSession("acme", sub, sessionChanges{Entries: []entryChange{{ID: session.Entries[0].ID, Deleted: &hidden}}}); err != nil {
		t.Fatal(err)
	}
	handler := testWebServer().handler()
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Host = "127.0.0.1:7332"
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "secret-token"})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	response := get("/api/recordings/timeline?name=acme")
	if response.Code != 200 || strings.Contains(response.Body.String(), `"cmd":"printf synthetic"`) {
		t.Fatalf("deleted command visible: %d %s", response.Code, response.Body.String())
	}
	response = get("/api/recordings/search?name=acme&q=SYNTHETIC_RESULT")
	if response.Code != 200 || strings.Contains(response.Body.String(), "SYNTHETIC_RESULT") {
		t.Fatal("search exposes deleted command")
	}
	if response = get("/api/recordings/cast?name=acme&shell=..%2Foutside"); response.Code != 404 {
		t.Fatal("path traversal accepted")
	}
	if response = get("/api/recordings/cast?name=acme&shell=" + sub); response.Code != 200 || !strings.Contains(response.Body.String(), "SYNTHETIC_RESULT") {
		t.Fatal("raw replay did not preserve original")
	}
}

func TestIndexedTrackerKeepsIDsWhenAtuinCatchesUp(t *testing.T) {
	_, dir, _ := indexedFixture(t)
	var indexed struct {
		Commands []recordingCommand `json:"commands"`
	}
	if err := runRecordingEngine(context.Background(), "acme", dir, "timeline", nil, &indexed); err != nil {
		t.Fatal(err)
	}
	original := ReportRow{ID: "atuin-existing", Command: indexed.Commands[0].Cmd, TS: indexed.Commands[0].T * int64(time.Millisecond)}
	rows, err := appendIndexedReportRows(context.Background(), "acme", "DEMO", []ReportRow{original})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != original.ID {
		t.Fatal("duplicated existing Atuin record")
	}
	// Start with an index-only completed command, then let the Atuin row appear.
	links, _ := sessionEditsPath("acme")
	if err = os.Remove(filepath.Join(filepath.Dir(links), "recording-links.json")); err != nil {
		t.Fatal(err)
	}
	first, err := appendIndexedReportRows(context.Background(), "acme", "DEMO", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || !strings.HasPrefix(first[0].ID, "capture-") {
		t.Fatal("index-only command missing")
	}
	later, err := appendIndexedReportRows(context.Background(), "acme", "DEMO", []ReportRow{original})
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 1 || later[0].ID != first[0].ID {
		t.Fatal("late Atuin record changed the curation/export identity")
	}
}

func TestIndexedCaptureEncryptionKeepsSidecars(t *testing.T) {
	_, dir, sub := indexedFixture(t)
	if err := archiveSession("acme", sub); err != nil {
		t.Fatal(err)
	}
	file, err := encryptSession("acme", sub, "synthetic test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	if _, err = decryptArchive(file, "synthetic test passphrase", restored); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".log", ".timing", ".start", ".commands.jsonl"} {
		if !fileExists(filepath.Join(restored, sub+ext)) {
			t.Fatalf("encrypted recording lost %s", ext)
		}
		if fileExists(filepath.Join(dir, archiveSubdir, sub+ext)) {
			t.Fatalf("plaintext %s remains", ext)
		}
	}
}

func TestBundledRecordingEngineContainsSourceOnly(t *testing.T) {
	if err := fs.WalkDir(recordingEngineFiles, "recording_engine", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, "__pycache__") || strings.HasSuffix(path, ".pyc") || strings.HasSuffix(path, ".pyo") || strings.HasSuffix(path, ".so") {
			t.Fatalf("generated artifact embedded in binary: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path, err := bundledRecordingEngine()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	source := false
	for _, file := range archive.File {
		if file.Name == "recording/bridge.py" {
			source = true
		}
		if strings.Contains(file.Name, "__pycache__/") || strings.HasSuffix(file.Name, ".pyc") || strings.HasSuffix(file.Name, ".pyo") {
			t.Fatalf("generated Python bytecode bundled: %s", file.Name)
		}
	}
	if !source {
		t.Fatal("recording processor source missing")
	}
}
