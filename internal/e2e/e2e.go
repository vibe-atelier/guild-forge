// Package e2e is Nexus's end-to-end encryption for guild channels: random channel keys, keys sealed for one
// member, and encrypted message text. Only members' apps use it; the guild
// never sees a key or any message text.
package e2e

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// KeySize is the size of a channel key (256 bits).
const KeySize = 32

// ErrDamaged means a box or message could not be opened.
var ErrDamaged = errors.New("could not decrypt (damaged, or not meant for you)")

// NewChannelKey makes a fresh random channel key.
func NewChannelKey() ([]byte, error) {
	k := make([]byte, KeySize)
	_, err := rand.Read(k)
	return k, err
}

func gcm(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// SealKey encrypts a channel key so only the owner of recipientPub (an
// X25519 public key) can open it. The box is: a fresh X25519 public key |
// 12-byte nonce | the sealed key.
func SealKey(recipientPub, key []byte) ([]byte, error) {
	to, err := ecdh.X25519().NewPublicKey(recipientPub)
	if err != nil {
		return nil, errors.New("bad public key")
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := eph.ECDH(to)
	if err != nil {
		return nil, err
	}
	salt := append(eph.PublicKey().Bytes(), recipientPub...)
	k, err := hkdf.Key(sha256.New, shared, salt, "nexus-keybox-v1", 32)
	if err != nil {
		return nil, err
	}
	a, err := gcm(k)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(eph.PublicKey().Bytes(), nonce...)
	return a.Seal(out, nonce, key, nil), nil
}

// OpenKey opens a box made by SealKey with the recipient's private key.
func OpenKey(priv *ecdh.PrivateKey, box []byte) ([]byte, error) {
	if len(box) < 32+12+16 {
		return nil, ErrDamaged
	}
	ephPub, err := ecdh.X25519().NewPublicKey(box[:32])
	if err != nil {
		return nil, ErrDamaged
	}
	shared, err := priv.ECDH(ephPub)
	if err != nil {
		return nil, ErrDamaged
	}
	salt := append(append([]byte{}, box[:32]...), priv.PublicKey().Bytes()...)
	k, err := hkdf.Key(sha256.New, shared, salt, "nexus-keybox-v1", 32)
	if err != nil {
		return nil, err
	}
	a, err := gcm(k)
	if err != nil {
		return nil, err
	}
	key, err := a.Open(nil, box[32:44], box[44:], nil)
	if err != nil || len(key) != KeySize {
		return nil, ErrDamaged
	}
	return key, nil
}

func aad(channel string, version int) []byte {
	return append([]byte("nexus-chat-v1|"+channel+"|"), byte(version>>24), byte(version>>16), byte(version>>8), byte(version))
}

// Encrypt encrypts message text with a channel key. The channel and key
// version are bound in, so a message can't be moved to another channel.
func Encrypt(key []byte, channel string, version int, plain []byte) (nonce, sealed []byte, err error) {
	a, err := gcm(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, a.Seal(nil, nonce, plain, aad(channel, version)), nil
}

// Decrypt opens message text encrypted by Encrypt.
func Decrypt(key []byte, channel string, version int, nonce, sealed []byte) ([]byte, error) {
	a, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != a.NonceSize() {
		return nil, ErrDamaged
	}
	p, err := a.Open(nil, nonce, sealed, aad(channel, version))
	if err != nil {
		return nil, ErrDamaged
	}
	return p, nil
}
