// Package user is a Nexus user: a folder of files on the user's computer. It holds
// the user's identity keys, profile, contacts, guilds and media.
//
//	<user folder>/
//	  profile.json   the user's profile; private keys are encrypted
//	  contacts.json  contact list
//	  guilds.json    joined guilds
//	  media/profile/ profile picture and banner
//	  media/chats/   private chat logs
//	  media/files/   pictures and files from chats
package user

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	ProfileFile  = "profile.json"
	ContactsFile = "contacts.json"
	GuildsFile   = "guilds.json"

	MinPasswordLength = 8
	MaxNameLength     = 32
	kdfRounds         = 600_000
)

// Online statuses.
// Statuses are the online statuses you can pick (Invisible was removed).
var Statuses = []string{"Online", "Idle", "DND"}

// Profile is profile.json.
type Profile struct {
	FormatVersion int `json:"format_version"`

	UserID       string `json:"user_ID"` // It is the public signing key, in base32.
	DefaultName  string `json:"default_name"`
	PasswordHash string `json:"password_hash"`
	// It is the password's salt: random (older profiles used the creation date).
	Salt            string    `json:"salt"`
	CreationDate    time.Time `json:"creation_date"`
	ProfilePicture  string    `json:"profile_picture"` // It is a file in media/profile, or "".
	Banner          string    `json:"banner"`
	Bio             string    `json:"bio"`
	OnlineStatus    string    `json:"online_status"`
	TypingIndicator string    `json:"typing_indicator"`
	// When it is off, others see when you read their messages.
	HideReadReceipts bool `json:"hide_read_receipts,omitempty"`
	// It turns notification sounds off.
	MuteSounds bool `json:"mute_sounds,omitempty"`
	// These are the voice settings.
	Voice Voice `json:"voice"`

	// ServerCount is how many guilds the user is in right now; sent to a
	// guild when joining. Kept equal to the length of guilds.json.
	ServerCount int `json:"server_count"`

	PublicEncryptionKey string `json:"public_encryption_key"` // It is the X25519 public key, in base32.

	// The private signing key and private decryption key, encrypted with a
	// key made from the password.
	EncryptedPrivateKeys sealedBox `json:"encrypted_private_keys"`

	// A second copy of the private keys, sealed with the recovery code; nil until one is made.
	RecoveryKeys *sealedBox `json:"recovery_keys,omitempty"`
}

type sealedBox struct {
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

type privateKeys struct {
	SigningSeed   []byte `json:"signing_seed"`
	DecryptionKey []byte `json:"private_decryption_key"`
}

// Contact is one entry of contacts.json.
type Contact struct {
	UserID                   string    `json:"user_ID"`
	Name                     string    `json:"name"`
	PublicEncryptionKey      string    `json:"public_encryption_key"`
	SignatureVerificationKey string    `json:"signature_verification_key"`
	VerificationStatus       string    `json:"verification_status"` // It is unverified, verified or changed.
	BlockStatus              bool      `json:"block_status"`
	PersonalNote             string    `json:"personal_note"`
	LastChat                 time.Time `json:"last_chat,omitzero"`

	// RequestState is "" once accepted, "sent" (you asked them), or "received" (they
	// asked you). VerifiedKey is their encryption key when verified,
	// to notice if it changes.
	RequestState string `json:"request_state,omitempty"`
	VerifiedKey  string `json:"verified_key,omitempty"`
}

// Guild is one entry of guilds.json.
type Guild struct {
	GuildID                  string    `json:"guild_ID"`
	GuildName                string    `json:"guild_name"`
	SignatureVerificationKey string    `json:"signature_verification_key"`
	JoinDate                 time.Time `json:"join_date"`
	GuildAddress             string    `json:"guild_address"`
	// It is the last time the guild was reachable.
	LastSeen time.Time `json:"last_seen,omitzero"`
	// Other addresses the guild said it answers on, tried
	// when GuildAddress doesn't answer.
	OtherAddresses []string `json:"other_addresses,omitempty"`

	// Membership is "pending" while waiting for the owner's approval;
	// empty once a member. JoinReceipt is the guild's signed proof.
	Membership  string        `json:"membership,omitempty"`
	JoinReceipt *GuildReceipt `json:"join_receipt,omitempty"`
}

// User is an unlocked user: profile plus private keys in memory.
type User struct {
	Dir     string
	Profile Profile

	SigningKey    ed25519.PrivateKey
	DecryptionKey *ecdh.PrivateKey

	keyEncKey []byte // It re-seals the private keys (for example, after a password change).

	mu sync.Mutex // It guards writes to profile.json and guilds.json.
}

var keyText = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrWrongPassword is returned by Unlock for a wrong password.
var ErrWrongPassword = errors.New("Wrong password. Please try again.")

// newSalt makes a random salt: 16 bytes, stored as base64. Older
// accounts used the creation date as the salt ("DDMMYYYY"), which
// anyone could guess and which every account made on the same day shared.
func newSalt() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawStdEncoding.EncodeToString(b)
}

// saltFormat is the profile format with a random salt; older ones are
// upgraded the next time the user unlocks.
const saltFormat = 3

// saltBytes turns the stored salt back into bytes. Format 1 (the first test
// version) stored a hex salt, format 2 the creation date, format 3 a random
// salt in base64.
func saltBytes(p *Profile) ([]byte, error) {
	if p.FormatVersion == 1 {
		return hex.DecodeString(p.Salt)
	}
	if p.FormatVersion >= saltFormat {
		b, err := base64.RawStdEncoding.DecodeString(p.Salt)
		if err != nil || len(b) < 16 {
			return nil, errors.New("bad salt")
		}
		return b, nil
	}
	if len(p.Salt) != 8 {
		return nil, errors.New("bad salt")
	}
	return []byte(p.Salt), nil
}

// deriveKeys turns the password into (key for the private keys, password
// hash). Both come from one slow PBKDF2 run, so guessing passwords is slow;
// each user has its own random salt.
func deriveKeys(password string, salt []byte) (encKey []byte, hash string, err error) {
	k, err := pbkdf2.Key(sha256.New, password, salt, kdfRounds, 64)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(k[32:])
	return k[:32], hex.EncodeToString(h[:]), nil
}

// CheckName validates a display name.
func CheckName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch n := utf8.RuneCountInString(name); {
	case n == 0:
		return "", errors.New("please choose a name")
	case n > MaxNameLength:
		return "", fmt.Errorf("names can be at most %d characters", MaxNameLength)
	}
	if strings.ContainsAny(name, "\x00\r\n\t") {
		return "", errors.New("names cannot contain line breaks or tabs")
	}
	return name, nil
}

// FolderName turns a display name into a safe folder name.
func FolderName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			r = '_'
		}
		b.WriteRune(r)
	}
	s := strings.Trim(b.String(), " .")
	if s == "" {
		s = "user"
	}
	return s
}

// Create makes a new user in dir (which must be new or empty) and returns it
// unlocked.
func Create(dir, name, password string) (*User, error) {
	name, err := CheckName(name)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return nil, fmt.Errorf("the password must be at least %d characters", MinPasswordLength)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		if IsUserFolder(dir) {
			return nil, errors.New("that folder already contains a Nexus user; use Load User instead")
		}
		return nil, errors.New("that folder is not empty; choose a new or empty folder")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	pub, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	dec, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	created := time.Now().UTC().Truncate(time.Second)
	salt := newSalt()
	saltRaw, _ := base64.RawStdEncoding.DecodeString(salt)
	encKey, hash, err := deriveKeys(password, saltRaw)
	if err != nil {
		return nil, err
	}
	u := &User{
		Dir: dir,
		Profile: Profile{
			FormatVersion:       saltFormat,
			UserID:              keyText.EncodeToString(pub),
			DefaultName:         name,
			PasswordHash:        hash,
			Salt:                salt,
			CreationDate:        created,
			OnlineStatus:        "Online",
			TypingIndicator:     "{name} is typing…",
			PublicEncryptionKey: keyText.EncodeToString(dec.PublicKey().Bytes()),
		},
		SigningKey:    signing,
		DecryptionKey: dec,
		keyEncKey:     encKey,
	}
	if err := u.sealKeys(); err != nil {
		return nil, err
	}
	for _, d := range []string{"media/profile", "media/chats", "media/files"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o700); err != nil {
			return nil, err
		}
	}
	if err := writeJSON(filepath.Join(dir, ContactsFile), []Contact{}); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(dir, GuildsFile), []Guild{}); err != nil {
		return nil, err
	}
	if err := u.Save(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *User) sealKeys() error {
	plain, err := json.Marshal(privateKeys{SigningSeed: u.SigningKey.Seed(), DecryptionKey: u.DecryptionKey.Bytes()})
	if err != nil {
		return err
	}
	aead, err := newAEAD(u.keyEncKey)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	u.Profile.EncryptedPrivateKeys = sealedBox{Nonce: nonce, Data: aead.Seal(nil, nonce, plain, []byte(u.Profile.UserID))}
	return nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// IsUserFolder reports whether dir contains a Nexus user.
func IsUserFolder(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ProfileFile))
	return err == nil && info.Mode().IsRegular()
}

// ReadProfile reads profile.json without unlocking (to show who it is).
func ReadProfile(dir string) (*Profile, error) {
	data, err := os.ReadFile(filepath.Join(dir, ProfileFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("that folder does not contain a Nexus user")
	}
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil || p.UserID == "" {
		return nil, errors.New("this user's profile.json is damaged")
	}
	return &p, nil
}

// Unlock loads the user in dir and decrypts its private keys.
func Unlock(dir, password string) (*User, error) {
	p, err := ReadProfile(dir)
	if err != nil {
		return nil, err
	}
	salt, err := saltBytes(p)
	if err != nil {
		return nil, errors.New("this user's profile.json is damaged")
	}
	encKey, hash, err := deriveKeys(password, []byte(salt))
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(p.PasswordHash)) != 1 {
		return nil, ErrWrongPassword
	}
	aead, err := newAEAD(encKey)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, p.EncryptedPrivateKeys.Nonce, p.EncryptedPrivateKeys.Data, []byte(p.UserID))
	if err != nil {
		return nil, errors.New("this user's keys are damaged")
	}
	u, err := fromKeys(dir, p, plain)
	if err != nil {
		return nil, err
	}
	u.keyEncKey = encKey
	if p.FormatVersion < saltFormat {
		u.setPassword(password) // From now on there is a random salt; this is best effort.
	}
	return u, nil
}

// fromKeys makes an unlocked user from its decrypted private keys.
func fromKeys(dir string, p *Profile, plain []byte) (*User, error) {
	var keys privateKeys
	if err := json.Unmarshal(plain, &keys); err != nil || len(keys.SigningSeed) != ed25519.SeedSize {
		return nil, errors.New("this user's keys are damaged")
	}
	dec, err := ecdh.X25519().NewPrivateKey(keys.DecryptionKey)
	if err != nil {
		return nil, errors.New("this user's keys are damaged")
	}
	signing := ed25519.NewKeyFromSeed(keys.SigningSeed)
	if keyText.EncodeToString(signing.Public().(ed25519.PublicKey)) != p.UserID {
		return nil, errors.New("this user's keys do not match its ID")
	}
	return &User{Dir: dir, Profile: *p, SigningKey: signing, DecryptionKey: dec}, nil
}

// Save writes profile.json.
func (u *User) Save() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return writeJSON(filepath.Join(u.Dir, ProfileFile), u.Profile)
}

// SetStatus changes the online status.
func (u *User) SetStatus(s string) error {
	for _, ok := range Statuses {
		if s == ok {
			u.Profile.OnlineStatus = s
			return u.Save()
		}
	}
	return fmt.Errorf("unknown status %q", s)
}

// Contacts reads contacts.json.
func (u *User) Contacts() ([]Contact, error) {
	var list []Contact
	err := readJSON(filepath.Join(u.Dir, ContactsFile), &list)
	return list, err
}

// Guilds reads guilds.json.
func (u *User) Guilds() ([]Guild, error) {
	var list []Guild
	err := readJSON(filepath.Join(u.Dir, GuildsFile), &list)
	return list, err
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON writes v to path safely (temporary file, then rename).
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	os.Chmod(tmp.Name(), 0o600)
	return os.Rename(tmp.Name(), path)
}

// SaveGuild adds g to guilds.json, or replaces the entry with the same
// guild ID, and keeps server_count in step.
func (u *User) SaveGuild(g Guild) error {
	return u.editGuilds(func(list []Guild) []Guild {
		for i := range list {
			if list[i].GuildID == g.GuildID {
				list[i] = g
				return list
			}
		}
		return append(list, g)
	})
}

// UpdateGuild changes the entry for guild id, if it is in the list.
func (u *User) UpdateGuild(id string, change func(*Guild)) error {
	return u.editGuilds(func(list []Guild) []Guild {
		for i := range list {
			if list[i].GuildID == id {
				change(&list[i])
			}
		}
		return list
	})
}

// RemoveGuild takes guild id off the list (after leaving it).
func (u *User) RemoveGuild(id string) error {
	err := u.editGuilds(func(list []Guild) []Guild {
		kept := list[:0]
		for _, g := range list {
			if g.GuildID != id {
				kept = append(kept, g)
			}
		}
		return kept
	})
	os.Remove(u.GuildPicturePath(id))
	return err
}

func (u *User) editGuilds(change func([]Guild) []Guild) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	list, err := u.Guilds()
	if err != nil {
		return err
	}
	list = change(list)
	if list == nil {
		list = []Guild{}
	}
	if err := writeJSON(filepath.Join(u.Dir, GuildsFile), list); err != nil {
		return err
	}
	if u.Profile.ServerCount != len(list) {
		u.Profile.ServerCount = len(list)
		return writeJSON(filepath.Join(u.Dir, ProfileFile), u.Profile)
	}
	return nil
}

// GuildPicturePath is where a guild's picture is kept, so it shows even
// when the guild is offline.
func (u *User) GuildPicturePath(id string) string {
	return filepath.Join(u.Dir, "media", "guilds", id)
}

// KeyText shows a key the way Nexus does everywhere (base32).
func KeyText(k []byte) string { return keyText.EncodeToString(k) }

// PictureHash identifies a picture's content (same as the guild's).
func PictureHash(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// GuildReceipt is a guild's signed statement of when the user joined.
type GuildReceipt struct {
	Joined string `json:"joined"` // It is in RFC 3339.
	Sig    []byte `json:"sig"`
}

// MemberGuildCount is how many guilds the user is a full member of (sent as
// server_count when joining).
func (u *User) MemberGuildCount() int {
	list, _ := u.Guilds()
	n := 0
	for _, g := range list {
		if g.Membership == "" {
			n++
		}
	}
	return n
}

// ReadsFile keeps, per guild and channel, the newest message the user has
// seen (for unread markers).
const ReadsFile = "reads.json"

// Reads returns guild ID -> channel ID -> last read message ID.
func (u *User) Reads() map[string]map[string]string {
	u.mu.Lock()
	defer u.mu.Unlock()
	r := map[string]map[string]string{}
	readJSON(filepath.Join(u.Dir, ReadsFile), &r)
	return r
}

// SetRead records that the user has seen messages up to id.
func (u *User) SetRead(guild, channel, id string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	r := map[string]map[string]string{}
	readJSON(filepath.Join(u.Dir, ReadsFile), &r)
	if r[guild] == nil {
		r[guild] = map[string]string{}
	}
	if id <= r[guild][channel] {
		return nil
	}
	r[guild][channel] = id
	return writeJSON(filepath.Join(u.Dir, ReadsFile), r)
}

// KeyFromText turns a key's text form back into bytes.
func KeyFromText(s string) ([]byte, error) { return keyText.DecodeString(s) }
