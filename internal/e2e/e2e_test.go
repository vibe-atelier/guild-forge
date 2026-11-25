package e2e

import (
	"crypto/ecdh"
	"crypto/rand"
	"testing"
)

func TestSealAndOpen(t *testing.T) {
	alice, _ := ecdh.X25519().GenerateKey(rand.Reader)
	eve, _ := ecdh.X25519().GenerateKey(rand.Reader)
	key, _ := NewChannelKey()
	box, err := SealKey(alice.PublicKey().Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenKey(alice, box); err != nil || string(got) != string(key) {
		t.Fatalf("open: %v", err)
	}
	if _, err := OpenKey(eve, box); err == nil {
		t.Fatal("someone else must not open the box")
	}
	box[len(box)-1] ^= 1
	if _, err := OpenKey(alice, box); err == nil {
		t.Fatal("a changed box must be refused")
	}
}

func TestEncryptBindsChannel(t *testing.T) {
	key, _ := NewChannelKey()
	nonce, sealed, _ := Encrypt(key, "chan-a", 1, []byte("secret"))
	if p, err := Decrypt(key, "chan-a", 1, nonce, sealed); err != nil || string(p) != "secret" {
		t.Fatalf("decrypt: %v", err)
	}
	if _, err := Decrypt(key, "chan-b", 1, nonce, sealed); err == nil {
		t.Fatal("a message moved to another channel must not open")
	}
	if _, err := Decrypt(key, "chan-a", 2, nonce, sealed); err == nil {
		t.Fatal("a wrong key version must not open")
	}
}
