package user

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateAndUnlock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Alice")
	u, err := Create(dir, "  Alice  ", "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	if u.Profile.DefaultName != "Alice" || len(u.Profile.UserID) != 52 || u.Profile.OnlineStatus != "Online" || u.Profile.ServerCount != 0 {
		t.Fatalf("profile: %+v", u.Profile)
	}
	for _, f := range []string{ProfileFile, ContactsFile, GuildsFile, "media/profile", "media/chats", "media/files"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, ProfileFile))
	if !strings.Contains(string(data), `"server_count": 0`) {
		t.Fatal("profile.json should have server_count")
	}
	if strings.Contains(string(data), "signing_seed") {
		t.Fatal("private keys must not be stored in plain form")
	}

	if _, err := Unlock(dir, "wrongpassword"); err != ErrWrongPassword {
		t.Fatalf("want ErrWrongPassword, got %v", err)
	}
	u2, err := Unlock(dir, "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	if !u2.SigningKey.Equal(u.SigningKey) || !u2.DecryptionKey.Equal(u.DecryptionKey) {
		t.Fatal("keys changed after unlock")
	}
	if err := u2.SetStatus("DND"); err != nil {
		t.Fatal(err)
	}
	if p, _ := ReadProfile(dir); p.OnlineStatus != "DND" {
		t.Fatal("status not saved")
	}
	if err := u2.SetStatus("Sleeping"); err == nil {
		t.Fatal("unknown status should be refused")
	}
}

func TestCreateRefusesBadInput(t *testing.T) {
	if _, err := Create(t.TempDir(), "", "goodpassword"); err == nil {
		t.Error("empty name should be refused")
	}
	if _, err := Create(t.TempDir(), "Bob", "short"); err == nil {
		t.Error("short password should be refused")
	}
	full := t.TempDir()
	os.WriteFile(filepath.Join(full, "x.txt"), []byte("x"), 0o600)
	if _, err := Create(full, "Bob", "goodpassword"); err == nil {
		t.Error("non-empty folder should be refused")
	}
	existing := filepath.Join(t.TempDir(), "u")
	Create(existing, "Bob", "goodpassword")
	if _, err := Create(existing, "Bob", "goodpassword"); err == nil || !strings.Contains(err.Error(), "Load User") {
		t.Errorf("existing user folder: %v", err)
	}
}

func TestFolderName(t *testing.T) {
	if got := FolderName(`a/b:c*`); got != "a_b_c_" {
		t.Fatalf("got %q", got)
	}
	if got := FolderName(" .. "); got != "user" {
		t.Fatalf("got %q", got)
	}
}

// TestRandomSalt checks that each user gets its own random salt, and a user
// made with the old date salt moves to a random one when it next unlocks,
// keeping its keys and ID.
func TestRandomSalt(t *testing.T) {
	a, err := Create(filepath.Join(t.TempDir(), "a"), "Sam", "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Create(filepath.Join(t.TempDir(), "b"), "Sue", "goodpassword")
	if a.Profile.Salt == b.Profile.Salt || len(a.Profile.Salt) < 20 || a.Profile.FormatVersion != saltFormat {
		t.Fatalf("salts %q and %q", a.Profile.Salt, b.Profile.Salt)
	}

	// This is an old user, with the date salt (format 2).
	dir := filepath.Join(t.TempDir(), "old")
	old, _ := Create(dir, "Old", "goodpassword")
	id := old.Profile.UserID
	date := old.Profile.CreationDate.UTC().Format("02012006")
	encKey, hash, _ := deriveKeys("goodpassword", []byte(date))
	old.keyEncKey, old.Profile.PasswordHash, old.Profile.Salt, old.Profile.FormatVersion = encKey, hash, date, 2
	if err := old.sealKeys(); err != nil {
		t.Fatal(err)
	}
	old.Save()

	if _, err := Unlock(dir, "wrongpassword"); err == nil {
		t.Fatal("a wrong password still fails")
	}
	if p, _ := ReadProfile(dir); p.FormatVersion != 2 {
		t.Fatal("a wrong password must not change anything")
	}
	u, err := Unlock(dir, "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := ReadProfile(dir)
	if p.FormatVersion != saltFormat || p.Salt == date || p.UserID != id {
		t.Fatalf("after unlocking: format %d, salt %q", p.FormatVersion, p.Salt)
	}
	again, err := Unlock(dir, "goodpassword")
	if err != nil || again.Profile.UserID != id || string(again.SigningKey) != string(u.SigningKey) {
		t.Fatalf("unlocking again: %v", err)
	}
}
