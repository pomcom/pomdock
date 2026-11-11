package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// All processing code and pure Python dependencies ship in the binary.
//
//go:embed recording_engine/NOTICE.md recording_engine/recording/*.py recording_engine/pyte/*.py recording_engine/wcwidth/*.py recording_engine/wcwidth/table_grapheme_overrides/*.py
//go:embed recording_engine/pyte-0.8.2.dist-info recording_engine/wcwidth-0.9.4.dist-info
var recordingEngineFiles embed.FS
var recordingEngineOnce sync.Once
var recordingEngineZip string
var recordingEngineErr error

func bundledRecordingEngine() (string, error) {
	recordingEngineOnce.Do(func() {
		var data bytes.Buffer
		zw := zip.NewWriter(&data)
		recordingEngineErr = fs.WalkDir(recordingEngineFiles, "recording_engine", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "__pycache__" {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(d.Name(), ".pyc") || strings.HasSuffix(d.Name(), ".pyo") {
				return nil
			}
			b, err := recordingEngineFiles.ReadFile(path)
			if err != nil {
				return err
			}
			w, err := zw.Create(strings.TrimPrefix(path, "recording_engine/"))
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		})
		if recordingEngineErr != nil {
			return
		}
		if recordingEngineErr = zw.Close(); recordingEngineErr != nil {
			return
		}
		sum := sha256.Sum256(data.Bytes())
		// Code only: engagement data stays inside its loot directory.
		dir, err := os.UserCacheDir()
		if err != nil {
			recordingEngineErr = err
			return
		}
		dir = filepath.Join(dir, "pomdock", "recording-engine")
		if err = os.MkdirAll(dir, 0700); err != nil {
			recordingEngineErr = err
			return
		}
		recordingEngineZip = filepath.Join(dir, fmt.Sprintf("%x.zip", sum[:12]))
		if _, err = os.Stat(recordingEngineZip); os.IsNotExist(err) {
			tmp, err := os.CreateTemp(dir, "engine-*.zip")
			if err != nil {
				recordingEngineErr = err
				return
			}
			defer os.Remove(tmp.Name())
			_, err = tmp.Write(data.Bytes())
			closeErr := tmp.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(tmp.Name(), recordingEngineZip)
			}
			recordingEngineErr = err
		}
	})
	return recordingEngineZip, recordingEngineErr
}

func runRecordingEngine(ctx context.Context, name, dir, action string, args map[string]any, result any) error {
	engine, err := bundledRecordingEngine()
	if err != nil {
		return err
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("recording search and replay require Python 3.11+ on the host")
	}
	loot, err := lootDirForEngagement(name)
	if err != nil {
		return err
	}
	cache := filepath.Join(loot, ".pomdock", "cache", "recordings")
	if filepath.Base(dir) == archiveSubdir {
		cache = filepath.Join(cache, "archived")
	}
	if err = os.MkdirAll(cache, 0700); err != nil {
		return err
	}
	req := map[string]any{"name": name, "logdir": dir, "cache": cache, "action": action}
	for k, v := range args {
		req[k] = v
	}
	input, err := json.Marshal(req)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-B", "-c", "import sys; assert sys.version_info >= (3,11), 'Python 3.11+ required'; sys.path.insert(0,sys.argv[1]); from recording.bridge import main; main()", engine)
	cmd.Stdin = bytes.NewReader(input)
	var output, stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var fail struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(output.Bytes(), &fail)
		if fail.Error != "" {
			return fmt.Errorf("recording processor: %s", fail.Error)
		}
		return fmt.Errorf("recording processor unavailable (Python 3.11+ required): %w", err)
	}
	return json.Unmarshal(output.Bytes(), result)
}

type recordingCommand struct {
	SessionID string `json:"sessionId"`
	Tag       string `json:"tag,omitempty"`
	ID        string `json:"id"`
	Sub       string `json:"sub"`
	Label     string `json:"label"`
	T         int64  `json:"t"`
	End       *int64 `json:"end"`
	Exit      *int   `json:"exit"`
	Cwd       string `json:"cwd"`
	Cmd       string `json:"cmd"`
	Output    string `json:"output,omitempty"`
	Edited    bool   `json:"edited,omitempty"`
}

type recordingShell struct {
	Sub        string `json:"sub"`
	Label      string `json:"label"`
	Start      *int64 `json:"start"`
	Bytes      int64  `json:"bytes"`
	Big        bool   `json:"big"`
	Finished   bool   `json:"finished"`
	Background bool   `json:"background"`
}

// Recording routes use the existing authentication and engagement resolution.
// User-controlled paths are never sent to the processor.
func recordingDir(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	name, ok := engagementParam(w, r)
	if !ok {
		return "", "", false
	}
	dir, err := sessionsDir(name)
	if err != nil {
		httpError(w, 404, err)
		return "", "", false
	}
	if r.URL.Query().Get("archived") == "1" {
		dir = filepath.Join(dir, archiveSubdir)
	}
	return name, dir, true
}

func (s *webServer) handleRecordingTimeline(w http.ResponseWriter, r *http.Request) {
	name, dir, ok := recordingDir(w, r)
	if !ok {
		return
	}
	var result struct {
		Commands []recordingCommand `json:"commands"`
		Shells   []recordingShell   `json:"shells"`
	}
	if err := runRecordingEngine(r.Context(), name, dir, "timeline", nil, &result); err != nil {
		httpError(w, 503, err)
		return
	}
	state, err := readSessionEdits(name)
	if err != nil {
		httpError(w, 500, err)
		return
	}
	filtered := make([]recordingCommand, 0, len(result.Commands))
	for _, c := range result.Commands {
		hidden, change := recordingEntryEdit(state, c.Sub, c.SessionID, c.ID)
		if hidden {
			continue
		}
		if change.Command != nil {
			c.Cmd = *change.Command
			c.Edited = true
		}
		filtered = append(filtered, c)
	}
	result.Commands = filtered
	if result.Shells == nil {
		result.Shells = []recordingShell{}
	}
	writeJSON(w, result)
}

func (s *webServer) handleRecordingSearch(w http.ResponseWriter, r *http.Request) {
	name, dir, ok := recordingDir(w, r)
	if !ok {
		return
	}
	query := r.URL.Query().Get("q")
	if len(query) > 1024 {
		httpError(w, 400, fmt.Errorf("search is limited to 1024 characters"))
		return
	}
	var result struct {
		Groups []struct {
			Sub        string            `json:"sub"`
			SessionID  string            `json:"sessionId"`
			Tag        string            `json:"tag,omitempty"`
			Label      string            `json:"label"`
			Command    *recordingCommand `json:"command"`
			CommandHit bool              `json:"commandHit"`
			Hits       []struct {
				T      int64  `json:"t"`
				Text   string `json:"text"`
				Screen bool   `json:"screen"`
			} `json:"hits"`
		} `json:"groups"`
		Truncated bool   `json:"truncated"`
		Error     string `json:"error,omitempty"`
	}
	if query == "" {
		writeJSON(w, map[string]any{"groups": []any{}, "truncated": false})
		return
	}
	state, err := readSessionEdits(name)
	if err != nil {
		httpError(w, 500, err)
		return
	}
	imports, err := cliPasteImports(dir, state, false)
	if err != nil && !os.IsNotExist(err) {
		httpError(w, 500, err)
		return
	}
	args := map[string]any{"imports": imports, "query": query, "regex": r.URL.Query().Get("regex") == "1", "case": r.URL.Query().Get("case") == "1", "edits": state.Sessions}
	if err := runRecordingEngine(r.Context(), name, dir, "search", args, &result); err != nil {
		httpError(w, 503, err)
		return
	}
	groups := result.Groups[:0]
	for _, g := range result.Groups {
		entryID := ""
		if g.Command != nil {
			entryID = g.Command.ID
		}
		if hidden, _ := recordingEntryEdit(state, g.Sub, g.SessionID, entryID); hidden {
			continue
		}
		groups = append(groups, g)
	}
	result.Groups = groups
	writeJSON(w, result)
}

func (s *webServer) handleRecordingReplay(w http.ResponseWriter, r *http.Request) {
	name, dir, ok := recordingDir(w, r)
	if !ok {
		return
	}
	sub := r.URL.Query().Get("shell")
	if sub == "" || filepath.Base(sub) != sub || strings.ContainsAny(sub, "/\\") || !fileExists(filepath.Join(dir, sub+".timing")) {
		httpError(w, 404, fmt.Errorf("recording not found"))
		return
	}
	action := "info"
	if strings.HasSuffix(r.URL.Path, "/cast") {
		action = "cast"
	}
	var result json.RawMessage
	if err := runRecordingEngine(r.Context(), name, dir, action, map[string]any{"sub": sub}, &result); err != nil {
		httpError(w, 503, err)
		return
	}
	if action == "cast" {
		var cast struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(result, &cast); err != nil {
			httpError(w, 500, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, cast.Path)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(result)
}

type recordingSection struct {
	ID        string `json:"id"`
	CaptureID string `json:"captureId"`
	Tag       string `json:"tag"`
	Start     *int64 `json:"start"`
	End       *int64 `json:"end"`
}

func sectionSessions(meta *Session, sections []recordingSection) []*Session {
	out := make([]*Session, 0, len(sections))
	for _, part := range sections {
		section := *meta
		section.ID, section.CaptureID, section.Tag = part.ID, meta.ID, part.Tag
		if part.Tag != "" {
			section.Title = part.Tag
		}
		if part.Start != nil {
			section.Created = *part.Start / 1000
			section.Date = time.UnixMilli(*part.Start).In(berlinLoc).Format("2006-01-02")
		}
		if part.End != nil {
			section.State, section.Ended = "ended", *part.End/1000
		}
		section.Entries = []SessionEntry{}
		out = append(out, &section)
	}
	if len(out) == 0 {
		out = append(out, meta)
	}
	return out
}

func readIndexedSections(dir, engagement, sub string) ([]recordingSection, error) {
	var result struct {
		Sections []recordingSection `json:"sections"`
	}
	args := map[string]any{}
	if sub != "" {
		args["sub"] = sub
	}
	if err := runRecordingEngine(context.Background(), engagement, dir, "sections", args, &result); err != nil {
		return nil, err
	}
	return result.Sections, nil
}

func indexedCaptureSections(path, engagement string) ([]*Session, error) {
	meta, err := indexedCaptureMeta(path, engagement)
	if err != nil {
		return nil, err
	}
	sections, err := readIndexedSections(filepath.Dir(path), engagement, meta.ID)
	if err != nil {
		return nil, err
	}
	return sectionSessions(meta, sections), nil
}

func loadIndexedCapture(path, engagement string) ([]*Session, error) {
	sub := strings.TrimSuffix(filepath.Base(path), ".log")
	var result struct {
		Entries  []recordingCommand `json:"entries"`
		Sections []recordingSection `json:"sections"`
	}
	if err := runRecordingEngine(context.Background(), engagement, filepath.Dir(path), "entries", map[string]any{"sub": sub}, &result); err != nil {
		return nil, err
	}
	meta, err := indexedCaptureMeta(path, engagement)
	if err != nil {
		return nil, err
	}
	sections := sectionSessions(meta, result.Sections)
	byID := make(map[string]*Session, len(sections))
	for _, section := range sections {
		byID[section.ID] = section
	}
	for _, c := range result.Entries {
		section := byID[c.SessionID]
		if section == nil {
			section = sections[0]
		}
		t := time.UnixMilli(c.T).In(berlinLoc)
		section.Entries = append(section.Entries, SessionEntry{ID: c.ID, SessionID: section.ID, TS: c.T * int64(time.Millisecond), Date: t.Format("2006-01-02"), Time: t.Format("15:04:05"), Cwd: c.Cwd, Command: c.Cmd, Output: c.Output})
	}
	return sections, nil
}

func appendIndexedReportRows(ctx context.Context, name, tester string, rows []ReportRow) ([]ReportRow, error) {
	engagementDataMu.Lock()
	defer engagementDataMu.Unlock()
	dir, err := sessionsDir(name)
	if err != nil {
		return nil, err
	}
	if !fileExists(filepath.Join(dir, "commands.jsonl")) {
		return rows, nil
	}
	var indexed struct {
		Commands []recordingCommand `json:"commands"`
	}
	if err = runRecordingEngine(ctx, name, dir, "timeline", nil, &indexed); err != nil {
		return nil, err
	}
	loot, err := lootDirForEngagement(name)
	if err != nil {
		return nil, err
	}
	linksPath := filepath.Join(loot, ".pomdock", "recording-links.json")
	links := map[string]string{}
	if data, e := os.ReadFile(linksPath); e == nil {
		if e = json.Unmarshal(data, &links); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	if links == nil {
		links = map[string]string{}
	}
	changed := false
	// Match occurrences, not just command text. Existing Atuin IDs keep all saved
	// annotations and export inclusion stable while additional records get own IDs.
	used := make(map[string]bool)
	for _, c := range indexed.Commands {
		if c.End == nil || c.Exit == nil {
			continue
		}
		ts := c.T * int64(time.Millisecond)
		best := -1
		distance := int64(2 * time.Second)
		for i, row := range rows {
			if used[row.ID] || strings.TrimSpace(row.Command) != strings.TrimSpace(c.Cmd) {
				continue
			}
			delta := row.TS - ts
			if delta < 0 {
				delta = -delta
			}
			if delta <= distance {
				best = i
				distance = delta
			}
		}
		if best >= 0 {
			if links[c.ID] == "" {
				links[c.ID] = rows[best].ID
				changed = true
			}
			rows[best].ID = links[c.ID]
			used[rows[best].ID] = true
			continue
		}
		end := *c.End * int64(time.Millisecond)
		cmd := strings.TrimSpace(c.Cmd)
		if cmd == "" {
			continue
		}
		if links[c.ID] == "" {
			links[c.ID] = "capture-" + c.ID
			changed = true
		}
		row := ReportRow{ID: links[c.ID], TS: ts, EndTS: end, Started: fmtTS(ts), Ended: fmtTS(end), Measure: cmd, Command: cmd, Targets: extractTargets(cmd), Tester: tester, Exit: *c.Exit, Noise: isNoise(cmd)}
		rows = append(rows, row)
		used[row.ID] = true
	}
	if changed {
		data, e := json.Marshal(links)
		if e != nil {
			return nil, e
		}
		if e = writeJSONDoc(linksPath, data); e != nil {
			return nil, e
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].TS < rows[j].TS })
	return rows, nil
}

// Listing and resolving indexed shells must not replay every engagement's raw
// output. Their precise start and script trailer provide lightweight metadata.
func indexedCaptureMeta(path, name string) (*Session, error) {
	sub := strings.TrimSuffix(filepath.Base(path), ".log")
	start, err := os.ReadFile(strings.TrimSuffix(path, ".log") + ".start")
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(start)))
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	offset := stat.Size() - 1024
	if offset < 0 {
		offset = 0
	}
	tail := make([]byte, stat.Size()-offset)
	if _, err = f.ReadAt(tail, offset); err != nil {
		return nil, err
	}
	state, end := captureEnd(string(tail))
	return &Session{ID: sub, CaptureID: sub, Engagement: name, Source: "script", Title: sub, Created: t.Unix(), Date: t.In(berlinLoc).Format("2006-01-02"), State: state, Ended: end}, nil
}
