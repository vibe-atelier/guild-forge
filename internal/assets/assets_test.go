package assets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexus/internal/nexusdata"
)

// TestWriteOut checks that built-in files are written into nexus-data, and are left
// alone when they are already there. Built without the asset tags,
// there is nothing to write.
func TestWriteOut(t *testing.T) {
	t.Setenv("NEXUS_DATA", filepath.Join(t.TempDir(), nexusdata.Name))
	if err := WriteOut(); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		path := filepath.Join(nexusdata.Path(f.part), f.name)
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, f.data) {
			t.Fatalf("%s was not written out whole", f.name)
		}
		old := time.Now().Add(-time.Hour)
		os.Chtimes(path, old, old)
	}
	if err := WriteOut(); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		st, _ := os.Stat(filepath.Join(nexusdata.Path(f.part), f.name))
		if time.Since(st.ModTime()) < 30*time.Minute {
			t.Fatalf("%s was written again although it was the same", f.name)
		}
	}
	t.Logf("%d built-in files", len(files))
}
