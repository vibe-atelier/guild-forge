package e2e

import (
	"crypto/ecdh"
	"crypto/rand"
	"testing"
)

func TestPairKeyAndCode(t *testing.T) {
	a, _ := ecdh.X25519().GenerateKey(rand.Reader)
	b, _ := ecdh.X25519().GenerateKey(rand.Reader)
	k1, err := PairKey(a, b.PublicKey().Bytes(), "ALICE", "BEN")
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := PairKey(b, a.PublicKey().Bytes(), "BEN", "ALICE")
	if string(k1) != string(k2) {
		t.Fatal("both sides must get the same key")
	}
	ch := DMChannel("ALICE", "BEN")
	nonce, sealed, _ := Encrypt(k1, ch, 0, []byte("hello"))
	if p, err := Decrypt(k2, DMChannel("BEN", "ALICE"), 0, nonce, sealed); err != nil || string(p) != "hello" {
		t.Fatalf("decrypt: %q %v", p, err)
	}
	c := VerifyCode("BEN", "ALICE")
	if len(c) != 11 || c[3] != '-' || !SameCode("  "+c[:3]+c[4:7]+c[8:], c) || SameCode(c, VerifyCode("ALICE", "BEN")) {
		t.Fatalf("code %q", c)
	}
}
