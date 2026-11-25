// Package assets carries the files built into the programs: the emoji font
// and the noise model, with their licences. The build script puts
// them in files/ and builds with the tags assetfont and assetmodel; without
// those tags nothing is built in, and the programs look for the files on
// disk as before.
//
// At start-up WriteOut writes each built-in file into this PC's nexus-data
// when it is missing or different, so a program copied to another PC needs
// nothing else.
package assets

import (
	"bytes"
	"os"
	"path/filepath"

	"nexus/internal/fsutil"
	"nexus/internal/nexusdata"
)

// file is one built-in file and where it goes in nexus-data.
type file struct {
	part, name string
	data       []byte
}

// files are the built-in files; the tagged files of this package add them.
var files []file

// WriteOut writes the built-in files into nexus-data where they are missing
// or different. It does nothing when no file is built in.
func WriteOut() error {
	for _, f := range files {
		dir, err := nexusdata.Part(f.part)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, f.name)
		if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, f.data) {
			continue // The file is already there and the same.
		}
		if err := fsutil.WriteFileAtomic(path, f.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
