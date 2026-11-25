package host

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOnlyEmptyFolders checks that the empty folders GuildForge makes at start-up may
// be cleared for a move, but never a folder with a file in it.
func TestOnlyEmptyFolders(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "guilds"), 0o755)
	os.MkdirAll(filepath.Join(dir, "backups", "x"), 0o755)
	if !onlyEmptyFolders(dir) {
		t.Fatal("empty folders only")
	}
	os.WriteFile(filepath.Join(dir, "backups", "x", "keep.tar.gz"), []byte("data"), 0o600)
	if onlyEmptyFolders(dir) {
		t.Fatal("a folder with a file in it must stay")
	}
}
