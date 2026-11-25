package user

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// This file keeps contacts and private chat logs.
//
// Chat logs are kept in media/chats/<contact ID>.bin, encrypted with a key
// made from the user's private signing key, so a copied user folder doesn't
// give away the chats without the password.

// ChatEntry is one private message as kept on this computer.
type ChatEntry struct {
	ID        string              `json:"id"`   // It is the sender's message ID.
	From      string              `json:"from"` // It is the sender's user ID.
	Time      string              `json:"time"` // It is in RFC 3339.
	Text      string              `json:"text,omitempty"`
	ReplyTo   string              `json:"reply_to,omitempty"`
	Edited    bool                `json:"edited,omitempty"`
	Deleted   bool                `json:"deleted,omitempty"`
	Reactions map[string][]string `json:"reactions,omitempty"`
	ReadAt    string              `json:"read_at,omitempty"` // It is when they read it (for your messages).
	// It is when their app received it (for your messages).
	GotAt    string `json:"got_at,omitempty"`
	File     string `json:"file,omitempty"` // It is a file's name.
	FilePath string `json:"file_path,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

// EditContacts changes contacts.json in one step.
func (u *User) EditContacts(change func([]Contact) []Contact) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	var list []Contact
	if err := readJSON(filepath.Join(u.Dir, ContactsFile), &list); err != nil {
		return err
	}
	return writeJSON(filepath.Join(u.Dir, ContactsFile), change(list))
}

func (u *User) chatKey() ([]byte, error) {
	return hkdf.Key(sha256.New, u.SigningKey.Seed(), nil, "nexus-local-chats-v1", 32)
}

var safeName = regexp.MustCompile(`^[A-Z2-7]{52}$`)

func (u *User) chatPath(contact string) (string, error) {
	if !safeName.MatchString(contact) {
		return "", errors.New("bad contact ID")
	}
	return filepath.Join(u.Dir, "media", "chats", contact+".bin"), nil
}

// LoadChat reads the chat with a contact (empty if there is none).
func (u *User) LoadChat(contact string) ([]ChatEntry, error) {
	path, err := u.chatPath(contact)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	key, err := u.chatKey()
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(block)
	if len(data) < g.NonceSize() {
		return nil, errors.New("the chat file is damaged")
	}
	plain, err := g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], []byte(contact))
	if err != nil {
		return nil, errors.New("the chat file is damaged")
	}
	var list []ChatEntry
	return list, json.Unmarshal(plain, &list)
}

// SaveChat writes the chat with a contact, encrypted.
func (u *User) SaveChat(contact string, list []ChatEntry) error {
	path, err := u.chatPath(contact)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(list)
	if err != nil {
		return err
	}
	key, err := u.chatKey()
	if err != nil {
		return err
	}
	block, _ := aes.NewCipher(key)
	g, _ := cipher.NewGCM(block)
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	data := g.Seal(nonce, nonce, plain, []byte(contact))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveChatFile keeps a file received or sent in a chat, in media/files, and
// returns its path inside the user folder.
func (u *User) SaveChatFile(contact, id, name string, data []byte) (string, error) {
	if !safeName.MatchString(contact) {
		return "", errors.New("bad contact ID")
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	rel := filepath.Join("media", "files", contact, id+"-"+name)
	full := filepath.Join(u.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return "", err
	}
	return rel, os.WriteFile(full, data, 0o600)
}

// SetHideReadReceipts turns read receipts off (true) or on.
func (u *User) SetHideReadReceipts(hide bool) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.Profile.HideReadReceipts = hide
	return writeJSON(filepath.Join(u.Dir, ProfileFile), u.Profile)
}

// Now is used for chat times (replaceable in tests).
var Now = func() time.Time { return time.Now() }

// ClearChat deletes this computer's copy of the chat with a contact.
// With eraseMedia, the pictures and files sent in that chat go too. The
// contact stays, and nothing changes on their side.
func (u *User) ClearChat(contact string, eraseMedia bool) error {
	path, err := u.chatPath(contact)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if eraseMedia {
		return os.RemoveAll(filepath.Join(u.Dir, "media", "files", contact))
	}
	return nil
}
