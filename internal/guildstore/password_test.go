package guildstore

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestPasswords checks salted, slow hashes; old plain ones still work and
// are replaced once the right password is typed.
func TestPasswords(t *testing.T) {
	a, b := HashPassword("guildpass1"), HashPassword("guildpass1")
	if a == b {
		t.Fatal("each hash has its own salt")
	}
	if !strings.HasPrefix(a, "pbkdf2-sha256$600000$") {
		t.Fatalf("form: %s", a)
	}
	if !VerifyPassword("guildpass1", a) || VerifyPassword("guildpass2", a) || VerifyPassword("guildpass1", "") {
		t.Fatal("checking")
	}
	for _, bad := range []string{"pbkdf2-sha256$x$a$b", "pbkdf2-sha256$600000$!!$!!", "md5$1$a$b"} {
		if VerifyPassword("guildpass1", bad) {
			t.Fatalf("%q should never match", bad)
		}
	}

	// This is a guild with an older password.
	dir := t.TempDir()
	gd, _ := NewGuildData("guildpass1")
	sum := sha256.Sum256([]byte("guildpass1"))
	gd.PasswordHash = hex.EncodeToString(sum[:])
	gd.Save(dir)
	if !OldPasswordHash(gd.PasswordHash) || !gd.CheckPassword("guildpass1") || gd.CheckPassword("nope") {
		t.Fatal("old hashes still work")
	}
	gd.UpgradePassword("nope", dir) // A wrong password changes nothing.
	if !OldPasswordHash(gd.PasswordHash) {
		t.Fatal("a wrong password must not change anything")
	}
	if err := gd.UpgradePassword("guildpass1", dir); err != nil {
		t.Fatal(err)
	}
	again, _ := LoadGuildData(dir)
	if OldPasswordHash(again.PasswordHash) || !again.CheckPassword("guildpass1") {
		t.Fatalf("after upgrading: %s", again.PasswordHash)
	}
}
