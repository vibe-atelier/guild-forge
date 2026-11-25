package nexusdata

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMoveIn checks that old settings move in once, and never over a part that already
// has its own.
func TestMoveIn(t *testing.T) {
	t.Setenv("NEXUS_DATA", filepath.Join(t.TempDir(), Name))
	old := filepath.Join(t.TempDir(), "nexus")
	os.MkdirAll(filepath.Join(old, "sub"), 0o755)
	os.WriteFile(filepath.Join(old, "device.json"), []byte("{}"), 0o600)

	moved, err := MoveIn(old, AppPart)
	if err != nil || !moved {
		t.Fatalf("the first move: %v, %v", moved, err)
	}
	app, _ := Part(AppPart)
	if _, err := os.Stat(filepath.Join(app, "device.json")); err != nil {
		t.Fatal("the settings are in nexus-data now")
	}
	if _, err := os.Stat(filepath.Join(app, "sub")); err != nil {
		t.Fatal("folders move too")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the emptied old folder is removed")
	}

	// A second old folder doesn't move over the part that now has settings.
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "other.json"), []byte("{}"), 0o600)
	if moved, _ := MoveIn(old, AppPart); moved {
		t.Fatal("nothing moves into a part that already has settings")
	}
	if moved, _ := MoveIn(filepath.Join(t.TempDir(), "missing"), ModelsPart); moved {
		t.Fatal("a missing old folder moves nothing")
	}
}
