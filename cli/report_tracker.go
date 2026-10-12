package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The tracker's curation (which rows are included, edited measures, merges,
// deleted rows) and the web UI's preferences are owned by the browser but
// persisted here, so a reload, a new port, or a different browser never loses
// work. The server treats both documents as opaque JSON; only the size and
// syntax are checked.

const maxStateBytes = 16 << 20

// trackerStatePath is <loot>/.pomdock/tracker.json, next to the engagement's
// atuin history so the curation lives and dies with the engagement.
func trackerStatePath(engagement string) (string, error) {
	loot, err := lootDirForEngagement(engagement)
	if err != nil {
		return "", err
	}
	return filepath.Join(loot, ".pomdock", "tracker.json"), nil
}

// webPrefsPath is $XDG_CONFIG_HOME/pomdock/web-prefs.json.
func webPrefsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pomdock", "web-prefs.json"), nil
}

// readJSONDoc returns the stored document, or JSON null when none exists yet.
func readJSONDoc(path string) (json.RawMessage, error) {
	buf, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage("null"), nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(buf) {
		return nil, fmt.Errorf("%s is not valid JSON", path)
	}
	return buf, nil
}

// writeJSONDoc validates data and replaces path atomically (temp file + rename)
// so a crash mid-write never leaves a truncated document behind.
func writeJSONDoc(path string, data []byte) error {
	if len(data) > maxStateBytes {
		return fmt.Errorf("document too large (%d bytes, limit %d)", len(data), maxStateBytes)
	}
	if !json.Valid(data) {
		return fmt.Errorf("document is not valid JSON")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
