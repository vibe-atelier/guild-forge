package user

// This file keeps mention counts: the red number on a channel, kept on this computer
// so it survives a restart. Sealed with the user's key, like private chats.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
)

// MentionsFile holds guild ID -> channel ID -> unread mentions, sealed.
const MentionsFile = "mentions.bin"

const mentionsLabel = "mentions"

func (u *User) mentionsAEAD() (cipher.AEAD, error) {
	key, err := u.chatKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Mentions returns the saved counts (empty when there are none, or the file
// can't be read: counts are only a convenience).
func (u *User) Mentions() map[string]map[string]int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.mentionsLocked()
}

func (u *User) mentionsLocked() map[string]map[string]int {
	out := map[string]map[string]int{}
	data, err := os.ReadFile(filepath.Join(u.Dir, MentionsFile))
	if err != nil {
		return out
	}
	g, err := u.mentionsAEAD()
	if err != nil || len(data) < g.NonceSize() {
		return out
	}
	plain, err := g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], []byte(mentionsLabel))
	if err != nil {
		return out
	}
	json.Unmarshal(plain, &out)
	return out
}

// SetMentions saves one channel's count (0 removes it).
func (u *User) SetMentions(guild, channel string, n int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	all := u.mentionsLocked()
	if all[guild][channel] == n {
		return nil
	}
	if n <= 0 {
		delete(all[guild], channel)
		if len(all[guild]) == 0 {
			delete(all, guild)
		}
	} else {
		if all[guild] == nil {
			all[guild] = map[string]int{}
		}
		all[guild][channel] = n
	}
	plain, err := json.Marshal(all)
	if err != nil {
		return err
	}
	g, err := u.mentionsAEAD()
	if err != nil {
		return err
	}
	nonce := make([]byte, g.NonceSize())
	rand.Read(nonce)
	path := filepath.Join(u.Dir, MentionsFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, g.Seal(nonce, nonce, plain, []byte(mentionsLabel)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
