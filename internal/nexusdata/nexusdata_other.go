//go:build !windows

package nexusdata

import (
	"os"
	"path/filepath"
)

// baseDir is the user's data folder: $XDG_DATA_HOME, or ~/.local/share.
func baseDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

// Here nexus-data belongs to one account, so there is nothing to share or
// lock away: the folder's own permissions already do that.
func share(string)       {}
func reshare(string)     {}
func keepPrivate(string) {}
