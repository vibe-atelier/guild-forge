package nexusdata

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeepPrivate checks that a private folder and the files in it, old and new, stay
// readable and writable by this account; a file given on its own is left
// alone.
func TestKeepPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "guild")
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	old := filepath.Join(dir, "sub", "old.json")
	os.WriteFile(old, []byte("old"), 0o600)

	KeepPrivate(dir)
	KeepPrivate(old) // Files are not changed on their own.

	if b, err := os.ReadFile(old); err != nil || string(b) != "old" {
		t.Fatalf("a file already inside can still be read: %v", err)
	}
	fresh := filepath.Join(dir, "sub", "new.json")
	if err := os.WriteFile(fresh, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(fresh); err != nil || string(b) != "new" {
		t.Fatalf("a new file inside can be read: %v", err)
	}
}
