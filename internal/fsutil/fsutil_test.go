package fsutil

import (
	"path/filepath"
	"testing"
)

func TestCleanPathInput(t *testing.T) {
	cases := map[string]string{
		`  "My Folder"  `: filepath.Clean("My Folder"),
		`'a b'`:           filepath.Clean("a b"),
		`plain`:           "plain",
		`""`:              "",
		`   `:             "",
	}
	for in, want := range cases {
		if got := CleanPathInput(in); got != want {
			t.Errorf("CleanPathInput(%q) = %q, want %q", in, got, want)
		}
	}
}
