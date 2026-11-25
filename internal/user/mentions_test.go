package user

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestMentions checks that counts survive, are sealed, and 0 removes them.
func TestMentions(t *testing.T) {
	u, err := Create(t.TempDir(), "Alice", "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	if len(u.Mentions()) != 0 {
		t.Fatal("none at first")
	}
	u.SetMentions("G1", "C1", 2)
	u.SetMentions("G1", "C2", 1)
	if m := u.Mentions(); m["G1"]["C1"] != 2 || m["G1"]["C2"] != 1 {
		t.Fatalf("got %v", m)
	}
	data, _ := os.ReadFile(filepath.Join(u.Dir, MentionsFile))
	if bytes.Contains(data, []byte("C1")) {
		t.Fatal("the file is sealed")
	}
	u.SetMentions("G1", "C1", 0)
	u.SetMentions("G1", "C2", 0)
	if len(u.Mentions()) != 0 {
		t.Fatalf("0 removes: %v", u.Mentions())
	}
}
