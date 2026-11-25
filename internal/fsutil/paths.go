// Package fsutil has small file and path helpers shared by Nexus programs.
package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CleanPathInput tidies a folder path typed by the user: removes spaces
// around it, removes surrounding quotes, and expands "~" to the home
// folder. It returns "" if nothing usable was typed.
func CleanPathInput(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[1:])
		}
	}
	if s == "" {
		return ""
	}
	return filepath.Clean(s)
}

// SamePath reports whether two folder paths point to the same place.
// Windows ignores upper/lower case in paths; Linux does not.
func SamePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
