package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// An engagement owns a loot folder. Atuin is one optional source inside it,
// rather than the identifier that makes its recordings accessible.
type engagementManifest struct {
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Backend   string `json:"backend,omitempty"`
	Container string `json:"container,omitempty"`
	Created   string `json:"created,omitempty"`
	Updated   string `json:"updated,omitempty"`
	Image     struct {
		Reference       string `json:"reference,omitempty"`
		ID              string `json:"id,omitempty"`
		Revision        string `json:"revision,omitempty"`
		Built           string `json:"built,omitempty"`
		Toolset         string `json:"toolset,omitempty"`
		RecordingFormat string `json:"recordingFormat,omitempty"`
	} `json:"image,omitempty"`
}

func readEngagementManifest(loot string) (engagementManifest, bool) {
	var manifest engagementManifest
	data, err := os.ReadFile(filepath.Join(loot, ".pomdock", "engagement.json"))
	if err != nil || json.Unmarshal(data, &manifest) != nil {
		return manifest, false
	}
	if manifest.Version != 1 || strings.TrimSpace(manifest.Name) == "" || strings.ContainsAny(manifest.Name, "/\\\x00\r\n") {
		return manifest, false
	}
	return manifest, true
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Recognize only supported recording artifacts, not arbitrary folders under the
// loot root. This also supports old recordings which predate the manifest.
func hasRecordingArtifacts(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(dir, name)
		switch {
		case strings.HasSuffix(name, ".log"):
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			line, _ := bufio.NewReader(io.LimitReader(f, 4096)).ReadString('\n')
			f.Close()
			if strings.HasPrefix(line, "Script started on ") {
				return true
			}
		case strings.HasSuffix(name, ".json"):
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			var capture struct {
				ID     string `json:"id"`
				Source string `json:"source"`
			}
			err = json.NewDecoder(io.LimitReader(f, 16<<20)).Decode(&capture)
			f.Close()
			if err == nil && capture.ID != "" && (capture.Source == "script" || capture.Source == "imported-paste") {
				return true
			}
		case name == "commands.jsonl" || strings.HasSuffix(name, ".commands.jsonl"):
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			scan := bufio.NewScanner(io.LimitReader(f, 1<<20))
			scan.Buffer(make([]byte, 4096), 1<<20)
			valid := false
			for scan.Scan() {
				var record struct {
					Version int    `json:"v"`
					Sub     string `json:"subid"`
					Command string `json:"cmd"`
				}
				if json.Unmarshal(scan.Bytes(), &record) == nil && record.Version == 2 && record.Sub != "" && record.Command != "" {
					valid = true
					break
				}
			}
			f.Close()
			if valid {
				return true
			}
		}
	}
	return false
}

func liveEngagementAt(loot string) bool {
	if _, ok := readEngagementManifest(loot); ok {
		return true
	}
	if regularFile(filepath.Join(loot, ".atuin", "history.db")) {
		return true
	}
	return hasRecordingArtifacts(filepath.Join(loot, "sessions")) || hasRecordingArtifacts(filepath.Join(loot, "sessions", archiveSubdir))
}
