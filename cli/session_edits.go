package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

// All GUI changes live alongside the engagement, never in its recordings or
// Atuin database. The same lock serializes edits with archive/restore operations.
var engagementDataMu sync.Mutex

type entryEdit struct {
	Command *string `json:"command,omitempty"`
	Output  *string `json:"output,omitempty"`
	Deleted bool    `json:"deleted,omitempty"`
}

type sessionEdit struct {
	DisplayName *string              `json:"displayName,omitempty"`
	Deleted     bool                 `json:"deleted,omitempty"`
	Entries     map[string]entryEdit `json:"entries,omitempty"`
}

type sessionEdits struct {
	Version  int                    `json:"version"`
	Sessions map[string]sessionEdit `json:"sessions"`
}

func sessionEditsPath(name string) (string, error) {
	loot, err := lootDirForEngagement(name)
	return filepath.Join(loot, ".pomdock", "sessions.json"), err
}

func readSessionEdits(name string) (sessionEdits, error) {
	state := sessionEdits{Version: 1, Sessions: map[string]sessionEdit{}}
	p, err := sessionEditsPath(name)
	if err != nil {
		return state, err
	}
	data, err := os.ReadFile(p)
	if err == nil {
		if err = json.Unmarshal(data, &state); err != nil {
			return state, err
		}
		if state.Version != 1 {
			return state, fmt.Errorf("unsupported session edits version %d", state.Version)
		}
		if state.Sessions == nil {
			state.Sessions = map[string]sessionEdit{}
		}
		return state, nil
	}
	if !os.IsNotExist(err) {
		return state, err
	}
	// Carry old display names into engagement-local state, including archives.
	if prefs, e := webPrefsPath(); e == nil {
		if data, e := os.ReadFile(prefs); e == nil {
			var doc struct {
				Names map[string]string `json:"sessionNames"`
			}
			if json.Unmarshal(data, &doc) == nil {
				for key, value := range doc.Names {
					var pair []string
					if json.Unmarshal([]byte(key), &pair) == nil && len(pair) == 2 && pair[0] == name {
						v := value
						state.Sessions[pair[1]] = sessionEdit{DisplayName: &v}
					}
				}
			}
		}
	}
	return state, nil
}

func writeSessionEdits(name string, state sessionEdits) error {
	p, err := sessionEditsPath(name)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return writeJSONDoc(p, data)
}

func identifyEntries(s *Session) {
	seen := map[string]int{}
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.ID != "" {
			continue
		}
		key := e.Command + "\x00" + e.Cwd
		seen[key]++
		h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", s.ID, key, seen[key])))
		e.ID = hex.EncodeToString(h[:16])
	}
}

// recordingEntryEdit merges older whole-capture curation with section-specific
// edits. A hidden section never affects sibling sections of the same capture.
func recordingEntryEdit(state sessionEdits, sub, sessionID, entryID string) (bool, entryEdit) {
	parent := state.Sessions[sub]
	result := parent.Entries[entryID]
	hidden := parent.Deleted || result.Deleted
	if sessionID != "" && sessionID != sub {
		section := state.Sessions[sessionID]
		change := section.Entries[entryID]
		hidden = hidden || section.Deleted || change.Deleted
		if change.Command != nil {
			result.Command = change.Command
		}
		if change.Output != nil {
			result.Output = change.Output
		}
	}
	result.Deleted = hidden
	return hidden, result
}

func curateSession(s *Session, state sessionEdits) *Session {
	result := *s
	sub := s.CaptureID
	if sub == "" {
		sub = s.ID
	}
	edit := state.Sessions[s.ID]
	result.DisplayName = edit.DisplayName
	result.Deleted = edit.Deleted || state.Sessions[sub].Deleted
	result.Entries = append([]SessionEntry(nil), s.Entries...)
	for i := range result.Entries {
		e := &result.Entries[i]
		sectionID := e.SessionID
		if sectionID == "" {
			sectionID = s.ID
		}
		hidden, change := recordingEntryEdit(state, sub, sectionID, e.ID)
		if change.Command != nil {
			e.Command = *change.Command
		}
		if change.Output != nil {
			e.Output = *change.Output
		}
		e.Deleted = hidden
		e.Edited = change.Command != nil || change.Output != nil
	}
	return &result
}

type entryChange struct {
	ID      string  `json:"id"`
	Command *string `json:"command,omitempty"`
	Output  *string `json:"output,omitempty"`
	Deleted *bool   `json:"deleted,omitempty"`
	Reset   bool    `json:"reset,omitempty"`
}

type sessionChanges struct {
	DisplayName *string       `json:"displayName,omitempty"`
	Entries     []entryChange `json:"entries,omitempty"`
}

func editSession(name, id string, changes sessionChanges) (*Session, error) {
	s, err := getSession(name, id)
	if err != nil {
		s, err = getArchivedSession(name, id)
	}
	if err != nil {
		return nil, err
	}
	valid := map[string]bool{}
	for _, e := range s.Entries {
		valid[e.ID] = true
	}
	for _, c := range changes.Entries {
		if !valid[c.ID] {
			return nil, fmt.Errorf("entry %q no longer exists; refresh the recording", c.ID)
		}
	}
	state, err := readSessionEdits(name)
	if err != nil {
		return nil, err
	}
	edit := state.Sessions[id]
	if edit.Entries == nil {
		edit.Entries = map[string]entryEdit{}
	}
	if changes.DisplayName != nil {
		edit.DisplayName = changes.DisplayName
	}
	for _, c := range changes.Entries {
		if c.Reset {
			delete(edit.Entries, c.ID)
			continue
		}
		e := edit.Entries[c.ID]
		if c.Command != nil {
			e.Command = c.Command
		}
		if c.Output != nil {
			e.Output = c.Output
		}
		if c.Deleted != nil {
			e.Deleted = *c.Deleted
		}
		edit.Entries[c.ID] = e
	}
	state.Sessions[id] = edit
	if err = writeSessionEdits(name, state); err != nil {
		return nil, err
	}
	return curateSession(s, state), nil
}

func decodeEngagementBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStateBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpError(w, 400, err)
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		httpError(w, 400, fmt.Errorf("expected one JSON document"))
		return false
	}
	return true
}

func (s *webServer) handleSessionEdits(w http.ResponseWriter, r *http.Request) {
	engagementDataMu.Lock()
	defer engagementDataMu.Unlock()
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	var changes sessionChanges
	if !decodeEngagementBody(w, r, &changes) {
		return
	}
	result, err := editSession(name, r.URL.Query().Get("id"), changes)
	if err != nil {
		httpError(w, 400, err)
		return
	}
	writeJSON(w, result)
}

func (s *webServer) handleSessionsDelete(w http.ResponseWriter, r *http.Request) {
	engagementDataMu.Lock()
	defer engagementDataMu.Unlock()
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	var body struct {
		IDs     []string `json:"ids"`
		Deleted bool     `json:"deleted"`
	}
	if !decodeEngagementBody(w, r, &body) {
		return
	}
	state, err := readSessionEdits(name)
	if err != nil {
		httpError(w, 500, err)
		return
	}
	for _, id := range body.IDs {
		if _, err := getSession(name, id); err != nil {
			if _, err = getArchivedSession(name, id); err != nil {
				httpError(w, 404, err)
				return
			}
		}
		edit := state.Sessions[id]
		edit.Deleted = body.Deleted
		state.Sessions[id] = edit
	}
	if err := writeSessionEdits(name, state); err != nil {
		httpError(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
