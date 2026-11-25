package e2e

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
)

// This file handles private messages. Two people share one key, worked out by each
// of them from their own private key and the other's public key, so the
// guild relaying the message can't read it.

// PairKey is the key two people use for their private messages. It is the
// same whichever of the two works it out. myID and theirID are the user IDs.
func PairKey(mine *ecdh.PrivateKey, theirPub []byte, myID, theirID string) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(theirPub)
	if err != nil {
		return nil, errors.New("their encryption key is damaged")
	}
	shared, err := mine.ECDH(pub)
	if err != nil {
		return nil, err
	}
	a, b := myID, theirID
	if b < a {
		a, b = b, a
	}
	return hkdf.Key(sha256.New, shared, nil, "nexus-dm-v1|"+a+"|"+b, KeySize)
}

// DMChannel is the label private messages between two people are encrypted
// under (in place of a channel ID).
func DMChannel(aID, bID string) string {
	if bID < aID {
		aID, bID = bID, aID
	}
	return "dm:" + aID + ":" + bID
}

// VerifyCode is the 9-character code (shown as ABC-DEF-GHJ) that proves who
// someone is. The person being verified shows VerifyCode(their ID,
// verifier's ID); the verifier's app works out the same and compares.
func VerifyCode(verifiedID, verifierID string) string {
	sum := sha256.Sum256([]byte("nexus-verify-v1|" + verifiedID + "|" + verifierID))
	s := base32.StdEncoding.EncodeToString(sum[:])[:9]
	return s[:3] + "-" + s[3:6] + "-" + s[6:]
}

// SameCode compares a typed code with the expected one, ignoring case,
// spaces and dashes.
func SameCode(typed, want string) bool {
	clean := func(s string) string {
		s = strings.ToUpper(s)
		return strings.NewReplacer("-", "", " ", "").Replace(s)
	}
	return clean(typed) == clean(want)
}
