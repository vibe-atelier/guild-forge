package guildstore

// Guild passwords (replacing an older single SHA-256): the guild's own
// password and the join password are kept as salted PBKDF2-SHA256 with
// 600,000 rounds, the same as users' passwords. Each check takes about a
// tenth of a second, so guessing is millions of times slower. Old plain
// SHA-256 hashes still work, and are replaced the next time the right
// password is typed.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
)

const (
	passwordScheme = "pbkdf2-sha256"
	passwordRounds = 600_000
)

var b64 = base64.RawStdEncoding

// HashPassword makes a new stored form of a password, with its own salt:
// "pbkdf2-sha256$ROUNDS$SALT$KEY".
func HashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, _ := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	return passwordScheme + "$" + strconv.Itoa(passwordRounds) + "$" + b64.EncodeToString(salt) + "$" + b64.EncodeToString(key)
}

// VerifyPassword says whether password matches a stored form, new or old.
func VerifyPassword(password, stored string) bool {
	if stored == "" {
		return false
	}
	if OldPasswordHash(stored) {
		sum := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(stored)) == 1
	}
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != passwordScheme {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 1 || rounds > 10_000_000 {
		return false
	}
	salt, err1 := b64.DecodeString(parts[2])
	want, err2 := b64.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, rounds, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// OldPasswordHash says whether a stored form is the old plain SHA-256, to
// be replaced once the right password is known.
func OldPasswordHash(stored string) bool {
	if len(stored) != 64 {
		return false
	}
	_, err := hex.DecodeString(stored)
	return err == nil
}

// CheckPassword reports whether password is this guild's password.
func (s *GuildData) CheckPassword(password string) bool {
	return VerifyPassword(password, s.PasswordHash)
}

// UpgradePassword replaces an old plain hash with the new form, once the
// right password has been typed. It saves only when something changed.
func (s *GuildData) UpgradePassword(password, dataDir string) error {
	if !OldPasswordHash(s.PasswordHash) || !s.CheckPassword(password) {
		return nil
	}
	s.PasswordHash = HashPassword(password)
	return s.Save(dataDir)
}
