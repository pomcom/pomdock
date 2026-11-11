package main

import (
	"net/http"
	"os"
	"strings"
)

// Read-only access to the stored capture, independently of display parsing and
// curation. Legacy pasted imports retain parsed JSON, not their raw transcript.
func (s *webServer) handleSessionSource(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	list := sessionFiles
	if r.URL.Query().Get("archived") == "1" {
		list = archivedFiles
	}
	files, err := list(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	path, err := capturePathForID(name, r.URL.Query().Get("id"), files)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		httpError(w, http.StatusNotFound, err)
		return
	}
	format := "transcript"
	if strings.HasSuffix(path, ".json") {
		format = "session-json"
	}
	writeJSON(w, map[string]string{"text": string(data), "format": format})
}
