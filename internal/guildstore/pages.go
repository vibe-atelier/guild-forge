package guildstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

// Calendar, Threads and Wiki pages are kept as small JSON files in the guild
// folder: pages.json (which pages are on), calendar.json, wiki.json,
// threads.json, comments/<post>.json, emoji.json. Pictures are kept in
// images/, emoji/ and borrowed/, each file named by its content's hash.

// LoadPage reads one of the page files (missing is not an error).
func LoadPage(dataDir, name string, v any) error { return loadJSONFile(dataDir, name, v) }

// SavePage writes one of the page files.
func SavePage(dataDir, name string, v any) error {
	if dir := filepath.Dir(name); dir != "." {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0o700); err != nil {
			return err
		}
	}
	return saveJSONFile(dataDir, name, v)
}

var hashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// HashOf is the name a picture is stored under.
func HashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SaveBlob stores a picture in dir (images, emoji or borrowed) and returns
// its hash.
func SaveBlob(dataDir, dir string, data []byte) (string, error) {
	h := HashOf(data)
	full := filepath.Join(dataDir, dir)
	if err := os.MkdirAll(full, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(full, h)
	if _, err := os.Stat(path); err == nil {
		return h, nil // The picture is already there.
	}
	return h, os.WriteFile(path, data, 0o600)
}

// LoadBlob reads a stored picture.
func LoadBlob(dataDir, dir, hash string) ([]byte, error) {
	if !hashRe.MatchString(hash) {
		return nil, errors.New("bad picture name")
	}
	return os.ReadFile(filepath.Join(dataDir, dir, hash))
}

// RemoveBlob deletes a stored picture.
func RemoveBlob(dataDir, dir, hash string) {
	if hashRe.MatchString(hash) {
		os.Remove(filepath.Join(dataDir, dir, hash))
	}
}

// HasBlob reports whether a picture is stored.
func HasBlob(dataDir, dir, hash string) bool {
	if !hashRe.MatchString(hash) {
		return false
	}
	_, err := os.Stat(filepath.Join(dataDir, dir, hash))
	return err == nil
}

// DeleteBlob removes a stored picture.
func DeleteBlob(dataDir, dir, hash string) error {
	if !hashRe.MatchString(hash) {
		return errors.New("bad picture name")
	}
	return os.Remove(filepath.Join(dataDir, dir, hash))
}
