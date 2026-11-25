// Package guildstore reads and writes the files inside a guild's folder:
// its settings and keys, owner, public profile, member list and picture,
// and packs a guild folder into an archive (backups) or an encrypted
// export file.
package guildstore

import (
	"nexus/internal/fsutil"
	"nexus/internal/platform"

	"crypto/ed25519"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// A guild is a folder. Inside it:
//
//	nexus-guild.json   settings and keys (written by GuildForge commands)
//	owner.json         the paired owner (written by the running guild)
//	runtime/           files that exist only while the guild runs
//	logs/              the guild's log
//
// The other files (channels, messages, members, pages, media and more) are
// described in the files of this package that keep them.
const (
	GuildFileName = "nexus-guild.json"
	ownerFileName = "owner.json"
	RuntimeDir    = "runtime"
	LogsDir       = "logs"
)

// GuildData is the guild's settings file. It travels with the guild on
// export/import, unlike the registry entry.
type GuildData struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`

	// The guild password's stored form: salted PBKDF2. Guilds made
	// before then keep plain SHA-256 until the right password is next
	// typed.
	PasswordHash string `json:"password_hash"`

	// The guild's own signing key. The seed is the secret part;
	// PublicKey is the matching public key in base32 and is also the
	// guild's ID.
	SigningKeySeed []byte `json:"signing_key_seed"`
	PublicKey      string `json:"public_key"`
}

// Owner is the user who paired with the guild first.
type Owner struct {
	Key      string    `json:"owner_key"` // It is the owner's public key in base32.
	PairedAt time.Time `json:"paired_at"`
}

// keyText is how keys are shown to people: base32, no padding.
var keyText = base32.StdEncoding.WithPadding(base32.NoPadding)

// KeyText turns a public key into its text form.
func KeyText(key []byte) string { return keyText.EncodeToString(key) }

// NewGuildData makes the data for a brand-new guild with a fresh key.
func NewGuildData(password string) (*GuildData, error) {
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("cannot make a guild key: %w", err)
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	return &GuildData{
		FormatVersion:  1,
		CreatedAt:      time.Now().UTC().Truncate(time.Second),
		PasswordHash:   HashPassword(password),
		SigningKeySeed: seed,
		PublicKey:      KeyText(pub),
	}, nil
}

// SigningKey returns the guild's private signing key.
func (s *GuildData) SigningKey() (ed25519.PrivateKey, error) {
	if len(s.SigningKeySeed) != ed25519.SeedSize {
		return nil, errors.New("the guild's key is damaged")
	}
	return ed25519.NewKeyFromSeed(s.SigningKeySeed), nil
}

// LoadGuildData reads the settings file from a guild folder.
func LoadGuildData(dataDir string) (*GuildData, error) {
	path := filepath.Join(dataDir, GuildFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s GuildData
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s is damaged (%v)", path, err)
	}
	return &s, nil
}

// Save writes the settings file into the guild folder.
func (s *GuildData) Save(dataDir string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, GuildFileName)
	if err := fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return platform.GiveToGuildUser(path)
}

// HasGuildData reports whether dataDir contains a Nexus guild.
func HasGuildData(dataDir string) bool {
	info, err := os.Stat(filepath.Join(dataDir, GuildFileName))
	return err == nil && info.Mode().IsRegular()
}

// LoadOwner returns the guild's owner, or nil if it has none.
func LoadOwner(dataDir string) (*Owner, error) {
	data, err := os.ReadFile(filepath.Join(dataDir, ownerFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var o Owner
	if err := json.Unmarshal(data, &o); err != nil || o.Key == "" {
		return nil, fmt.Errorf("%s is damaged", filepath.Join(dataDir, ownerFileName))
	}
	return &o, nil
}

// OwnerKey returns the owner's key text, or "" if there is no owner.
func OwnerKey(dataDir string) string {
	if o, err := LoadOwner(dataDir); err == nil && o != nil {
		return o.Key
	}
	return ""
}

// SaveOwner records the guild's owner.
func SaveOwner(dataDir string, o Owner) error {
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, ownerFileName)
	if err := fsutil.WriteFileAtomic(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return platform.GiveToGuildUser(path)
}

// KeyFromText turns a key's text form back into bytes.
func KeyFromText(s string) ([]byte, error) { return keyText.DecodeString(s) }
