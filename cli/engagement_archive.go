package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filippo.io/age"
)

const engagementArchiveFormat = "pomdock-engagement-v1"

var engagementDirs = []string{".atuin", "sessions", ".pomdock"}

type engagementArchiveManifest struct {
	Format     string            `json:"format"`
	Engagement string            `json:"engagement"`
	Created    int64             `json:"created"`
	Files      map[string]string `json:"files"` // relative path -> SHA-256
}

type engagementArchiveInfo struct {
	File    string `json:"file"`
	Created int64  `json:"created"`
	Size    int64  `json:"size"`
}

func snapshotReportPreferences(loot, tester string) error {
	preferences := map[string]any{}
	if p, err := webPrefsPath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			if err := json.Unmarshal(data, &preferences); err != nil {
				return err
			}
		}
	}
	if preferences == nil {
		preferences = map[string]any{}
	}
	delete(preferences, "sessionNames") // names have already moved into sessions.json
	delete(preferences, "engagement")
	data, err := json.Marshal(preferences)
	if err != nil {
		return err
	}
	if err := writeJSONDoc(filepath.Join(loot, ".pomdock", "preferences.json"), data); err != nil {
		return err
	}
	p := filepath.Join(loot, ".pomdock", "tracker.json")
	data, err = os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var tracker map[string]any
	if err := json.Unmarshal(data, &tracker); err != nil {
		return err
	}
	if tracker == nil || tracker["defaults"] != nil {
		return nil
	}
	if v, ok := preferences["tester"].(string); ok && v != "" {
		tester = v
	}
	source, _ := preferences["source"].(string)
	tracker["defaults"] = map[string]string{"tester": tester, "source": source}
	data, err = json.Marshal(tracker)
	if err != nil {
		return err
	}
	return writeJSONDoc(p, data)
}

func archiveRootsPath() (string, error) {
	p, err := webPrefsPath()
	return filepath.Join(filepath.Dir(p), "engagement-roots.json"), err
}

func archiveRoots() map[string]string {
	roots := map[string]string{}
	if p, err := archiveRootsPath(); err == nil {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &roots)
		}
	}
	if roots == nil {
		roots = map[string]string{}
	}
	return roots
}

func rememberArchiveRoot(name, loot string) error {
	roots := archiveRoots()
	roots[name] = loot
	p, err := archiveRootsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(roots)
	if err != nil {
		return err
	}
	return writeJSONDoc(p, data)
}

func archivesAt(loot string) ([]engagementArchiveInfo, error) {
	items, err := os.ReadDir(filepath.Join(loot, "archives"))
	if os.IsNotExist(err) {
		return []engagementArchiveInfo{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []engagementArchiveInfo{}
	for _, item := range items {
		if item.IsDir() || item.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(item.Name(), archiveSuffix) {
			continue
		}
		info, err := item.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, engagementArchiveInfo{File: item.Name(), Created: info.ModTime().Unix(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

func ensureEngagementIdle(loot string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "docker", "ps", "-q").CombinedOutput()
	if err != nil {
		if strings.Contains(string(data), "Cannot connect to the Docker daemon") {
			return nil
		}
		return fmt.Errorf("cannot check for running containers: %s", strings.TrimSpace(string(data)))
	}
	ids := strings.Fields(string(data))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"inspect", "--format", `{{range .Mounts}}{{println .Source}}{{end}}`}, ids...)
	data, err = exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return fmt.Errorf("check engagement mounts: %w", err)
	}
	for _, src := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(src) == "" {
			continue
		}
		src, target := resolvePath(src), resolvePath(loot)
		if src == target || strings.HasPrefix(src, target+string(os.PathSeparator)) || strings.HasPrefix(target, src+string(os.PathSeparator)) {
			return fmt.Errorf("stop containers using this engagement before archiving or restoring it")
		}
	}
	return nil
}

// Freeze managed history first. Failed encryption or verification rolls it back.
// Arbitrary loot files and existing encrypted archives are never moved.
func archiveEngagementAt(loot, name, passphrase string) (string, error) {
	if strings.TrimSpace(passphrase) == "" {
		return "", fmt.Errorf("empty passphrase")
	}
	stage, err := os.MkdirTemp(loot, ".archive-stage-")
	if err != nil {
		return "", err
	}
	moved := []string{}
	committed := false
	defer func() {
		if !committed {
			for i := len(moved) - 1; i >= 0; i-- {
				_ = os.Rename(filepath.Join(stage, moved[i]), filepath.Join(loot, moved[i]))
			}
		}
		// Only delete an empty rollback directory. A failed rollback must retain data.
		_ = os.Remove(stage)
	}()
	for _, dir := range engagementDirs {
		if _, err := os.Lstat(filepath.Join(loot, dir)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", err
		}
		if err := os.Rename(filepath.Join(loot, dir), filepath.Join(stage, dir)); err != nil {
			return "", err
		}
		moved = append(moved, dir)
	}
	if len(moved) == 0 {
		return "", fmt.Errorf("no live engagement history to archive")
	}
	manifest := engagementArchiveManifest{Format: engagementArchiveFormat, Engagement: name, Created: time.Now().Unix(), Files: map[string]string{}}
	err = filepath.WalkDir(stage, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("history contains a symlink: %s", entry.Name())
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("history contains a non-regular file")
		}
		rel, _ := filepath.Rel(stage, p)
		digest, err := fileDigest(p)
		if err != nil {
			return err
		}
		manifest.Files[filepath.ToSlash(rel)] = digest
		return nil
	})
	if err != nil {
		return "", err
	}
	dir := filepath.Join(loot, "archives")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	out := filepath.Join(dir, sanitizeID(name)+"-"+time.Now().In(berlinLoc).Format("20060102-150405.000000000")+archiveSuffix)
	tmp := out + ".pending"
	defer os.Remove(tmp)
	if err := writeEngagementArchive(tmp, stage, manifest, passphrase); err != nil {
		return "", err
	}
	// Authenticate the complete stream and compare every archived byte with its digest.
	if _, err := readEngagementArchive(tmp, passphrase, ""); err != nil {
		return "", fmt.Errorf("archive verification failed: %w", err)
	}
	for rel, digest := range manifest.Files {
		if current, err := fileDigest(filepath.Join(stage, filepath.FromSlash(rel))); err != nil || current != digest {
			return "", fmt.Errorf("history changed while archiving; live history has been retained")
		}
	}
	if err := os.Rename(tmp, out); err != nil {
		return "", err
	}
	committed = true
	if err := os.RemoveAll(stage); err != nil {
		return out, fmt.Errorf("archive saved at %s; could not remove staging files: %w", out, err)
	}
	return out, nil
}

func fileDigest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeEngagementArchive(out, root string, manifest engagementArchiveManifest, passphrase string) (err error) {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return err
	}
	enc, err := age.Encrypt(f, recipient)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := writeTarBytes(tw, "manifest.json", data); err != nil {
		return err
	}
	paths := make([]string, 0, len(manifest.Files))
	for rel := range manifest.Files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		if err := func() error {
			src, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			defer src.Close()
			info, err := src.Stat()
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o600, Size: info.Size()}); err != nil {
				return err
			}
			_, err = io.Copy(tw, src)
			return err
		}(); err != nil {
			return err
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	if err = enc.Close(); err != nil {
		return err
	}
	return f.Sync()
}

func readEngagementArchive(file, passphrase, into string) (engagementArchiveManifest, error) {
	var manifest engagementArchiveManifest
	f, err := os.Open(file)
	if err != nil {
		return manifest, err
	}
	defer f.Close()
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return manifest, err
	}
	dec, err := age.Decrypt(f, identity)
	if err != nil {
		return manifest, fmt.Errorf("cannot decrypt archive (wrong passphrase?): %w", err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return manifest, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	if err != nil {
		return manifest, err
	}
	if header.Name != "manifest.json" || header.Size > maxStateBytes {
		return manifest, fmt.Errorf("invalid engagement archive manifest")
	}
	if err := json.NewDecoder(tr).Decode(&manifest); err != nil {
		return manifest, err
	}
	if manifest.Format != engagementArchiveFormat || manifest.Files[".atuin/history.db"] == "" {
		return manifest, fmt.Errorf("unsupported or empty engagement archive")
	}
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return manifest, err
		}
		clean := path.Clean(h.Name)
		root := strings.SplitN(clean, "/", 2)[0]
		allowed := root == ".atuin" || root == "sessions" || root == ".pomdock"
		if !allowed || !strings.Contains(clean, "/") || clean != h.Name || strings.Contains(clean, "\\") || h.Typeflag != tar.TypeReg || seen[clean] || manifest.Files[clean] == "" {
			return manifest, fmt.Errorf("invalid archive member %q", h.Name)
		}
		seen[clean] = true
		hash := sha256.New()
		var writer io.Writer = hash
		var dest *os.File
		if into != "" {
			p := filepath.Join(into, filepath.FromSlash(clean))
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				return manifest, err
			}
			dest, err = os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return manifest, err
			}
			writer = io.MultiWriter(hash, dest)
		}
		_, copyErr := io.Copy(writer, tr)
		if dest != nil {
			if closeErr := dest.Close(); copyErr == nil {
				copyErr = closeErr
			}
		}
		if copyErr != nil {
			return manifest, copyErr
		}
		if hex.EncodeToString(hash.Sum(nil)) != manifest.Files[clean] {
			return manifest, fmt.Errorf("archive checksum mismatch: %s", clean)
		}
	}
	if len(seen) != len(manifest.Files) {
		return manifest, fmt.Errorf("archive has missing files")
	}
	if _, err := io.Copy(io.Discard, gz); err != nil {
		return manifest, err
	}
	if err := gz.Close(); err != nil {
		return manifest, err
	}
	if _, err := io.Copy(io.Discard, dec); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func restoreEngagementAt(loot, file, passphrase string, expectedName ...string) error {
	stage, err := os.MkdirTemp(loot, ".restore-stage-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	manifest, err := readEngagementArchive(file, passphrase, stage)
	if err != nil {
		return err
	}
	if len(expectedName) > 0 && manifest.Engagement != expectedName[0] {
		return fmt.Errorf("archive belongs to %q, not %q", manifest.Engagement, expectedName[0])
	}
	for _, dir := range engagementDirs {
		if _, err := os.Lstat(filepath.Join(loot, dir)); err == nil {
			return fmt.Errorf("live %s already exists; archive live history before restoring", dir)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	moved := []string{}
	for _, dir := range engagementDirs {
		src, dst := filepath.Join(stage, dir), filepath.Join(loot, dir)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			for i := len(moved) - 1; i >= 0; i-- {
				if rollbackErr := os.Rename(filepath.Join(loot, moved[i]), filepath.Join(stage, moved[i])); rollbackErr != nil {
					cleanup = false
					return fmt.Errorf("restore failed: %w; rollback files retained in %s: %v", err, stage, rollbackErr)
				}
			}
			return err
		}
		moved = append(moved, dir)
	}
	return nil
}

func invalidateEngagementCaches() {
	engagementCache.Lock()
	engagementCache.list = nil
	engagementCache.Unlock()
	sessionCache.Lock()
	sessionCache.m = map[string]sessionCacheEntry{}
	sessionCache.Unlock()
}

func (s *webServer) handleEngagementArchives(w http.ResponseWriter, r *http.Request) {
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	loot, err := lootDirForEngagement(name)
	if err != nil {
		httpError(w, 404, err)
		return
	}
	archives, err := archivesAt(loot)
	if err != nil {
		httpError(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"archives": archives})
}

func (s *webServer) handleEngagementArchive(w http.ResponseWriter, r *http.Request) {
	engagementDataMu.Lock()
	defer engagementDataMu.Unlock()
	name, ok := engagementParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Passphrase string `json:"passphrase"`
		File       string `json:"file,omitempty"`
	}
	if !decodeEngagementBody(w, r, &body) {
		return
	}
	loot, err := lootDirForEngagement(name)
	if err != nil {
		httpError(w, 404, err)
		return
	}
	if strings.TrimSpace(body.Passphrase) == "" {
		httpError(w, 400, errors.New("empty passphrase"))
		return
	}
	if err := ensureEngagementIdle(loot); err != nil {
		httpError(w, 409, err)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/restore") {
		if body.File != filepath.Base(body.File) || !strings.HasSuffix(body.File, archiveSuffix) {
			httpError(w, 400, errors.New("invalid archive filename"))
			return
		}
		file := filepath.Join(loot, "archives", body.File)
		if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() {
			httpError(w, 404, errors.New("archive not found"))
			return
		}
		err = restoreEngagementAt(loot, file, body.Passphrase, name)
	} else {
		if _, err := os.Stat(filepath.Join(loot, ".atuin", "history.db")); err != nil {
			httpError(w, 409, errors.New("no live history to archive"))
			return
		}
		state, e := readSessionEdits(name)
		if e == nil {
			e = writeSessionEdits(name, state)
		}
		if e != nil {
			httpError(w, 500, e)
			return
		}
		if err := snapshotReportPreferences(loot, s.opts.tester); err != nil {
			httpError(w, 500, err)
			return
		}
		if err := rememberArchiveRoot(name, loot); err != nil {
			httpError(w, 500, err)
			return
		}
		var file string
		file, err = archiveEngagementAt(loot, name, body.Passphrase)
		if err == nil {
			body.File = filepath.Base(file)
		}
	}
	invalidateEngagementCaches()
	if err != nil {
		httpError(w, 409, err)
		return
	}
	var preferences json.RawMessage
	if strings.HasSuffix(r.URL.Path, "/restore") {
		preferences, _ = readJSONDoc(filepath.Join(loot, ".pomdock", "preferences.json"))
	}
	writeJSON(w, map[string]any{"ok": true, "file": body.File, "preferences": preferences})
}
