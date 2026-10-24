package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

// Encrypted session archives: an archived capture (its .log + .timing, or its
// .json) is packed into a gzip tarball and age-encrypted with a passphrase into
// <sessionsDir>/.archive/encrypted/<id>.tar.age. The plaintext is removed, so an
// encrypted archive is cold storage — invisible to the web UI and recoverable
// only with the passphrase, via `pomdock web session-restore`.

const (
	encryptedSubdir  = "encrypted" // under <sessionsDir>/.archive/
	archiveFormatTag = "pomdock-session-v1"
	archiveSuffix    = ".tar.age"
)

type archiveManifest struct {
	Format     string   `json:"format"`
	Engagement string   `json:"engagement"`
	ID         string   `json:"id"`
	Files      []string `json:"files"`
	ArchivedAt int64    `json:"archivedAtMs"`
}

// encryptedDir returns <sessionsDir>/.archive/encrypted, creating it (0700).
func encryptedDir(engagement string) (string, error) {
	dir, err := sessionsDir(engagement)
	if err != nil {
		return "", err
	}
	d := filepath.Join(dir, archiveSubdir, encryptedSubdir)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// encryptSession packs the archived capture for id, age-encrypts it with the
// passphrase, and removes the plaintext. The session must already be archived.
// Returns the archive path.
func encryptSession(engagement, id, passphrase string) (string, error) {
	if strings.TrimSpace(passphrase) == "" {
		return "", fmt.Errorf("empty passphrase")
	}
	files, err := archivedFiles(engagement)
	if err != nil {
		return "", err
	}
	src, err := capturePathForID(engagement, id, files)
	if err != nil {
		return "", fmt.Errorf("%w — archive the session before encrypting it", err)
	}
	members := []string{src}
	if strings.HasSuffix(src, ".log") {
		if t := strings.TrimSuffix(src, ".log") + ".timing"; fileExists(t) {
			members = append(members, t)
		}
	}
	outDir, err := encryptedDir(engagement)
	if err != nil {
		return "", err
	}
	out := versionedArchivePath(filepath.Join(outDir, sanitizeID(id)+archiveSuffix))

	manifest := archiveManifest{
		Format: archiveFormatTag, Engagement: engagement, ID: id,
		ArchivedAt: time.Now().UnixMilli(),
	}
	for _, m := range members {
		manifest.Files = append(manifest.Files, filepath.Base(m))
	}
	if err := writeEncryptedTar(out, members, manifest, passphrase); err != nil {
		os.Remove(out)
		return "", err
	}
	// Destroy the plaintext only once the archive is safely written and non-empty.
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		os.Remove(out)
		return "", fmt.Errorf("encrypted archive was not written")
	}
	for _, m := range members {
		dropSessionCache(engagement, m)
		_ = os.Remove(m)
	}
	return out, nil
}

func writeEncryptedTar(out string, files []string, manifest archiveManifest, passphrase string) error {
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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

	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err := writeTarBytes(tw, "manifest.json", mb); err != nil {
		return err
	}
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := writeTarBytes(tw, filepath.Base(p), data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	return enc.Close()
}

func writeTarBytes(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// decryptArchive decrypts archivePath with passphrase and extracts the capture
// files (not the manifest) into intoDir. Returns the extracted file names.
func decryptArchive(archivePath, passphrase, intoDir string) ([]string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, err
	}
	dec, err := age.Decrypt(f, identity)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong passphrase?): %w", err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(intoDir, 0o700); err != nil {
		return nil, err
	}
	var extracted []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		base := filepath.Base(h.Name) // defend against path traversal
		if base == "manifest.json" || base == "." || base == string(filepath.Separator) {
			continue
		}
		dest := filepath.Join(intoDir, base)
		w, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		if _, err := io.Copy(w, tr); err != nil {
			w.Close()
			return nil, err
		}
		w.Close()
		extracted = append(extracted, base)
	}
	return extracted, nil
}

// encryptedArchives lists the .tar.age files in an engagement's encrypted dir.
func encryptedArchives(engagement string) ([]string, error) {
	dir, err := sessionsDir(engagement)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, archiveSubdir, encryptedSubdir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), archiveSuffix) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// versionedArchivePath avoids overwriting an existing archive by inserting a UTC
// timestamp before the suffix.
func versionedArchivePath(path string) string {
	if !fileExists(path) {
		return path
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	return strings.TrimSuffix(path, archiveSuffix) + "-" + stamp + archiveSuffix
}
