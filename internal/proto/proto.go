// Package proto is the Nexus network protocol: how a client and a guild set up
// an encrypted connection and exchange messages.
//
// Handshake (both sides make a fresh X25519 key pair for every connection):
//
//	client -> guild : {"nexus":1, "x25519": <client key>}          (plain)
//	guild  -> client: {"x25519": <guild key>, "guild_key": <guild's
//	                  Ed25519 public key>, "sig": <signature>}       (plain)
//
// The guild signs a "transcript" (a fingerprint of both X25519 keys and its
// own public key) with its long-term signing key, so the client knows it is
// talking to the real guild and not someone in the middle. Both sides then
// compute the same shared secret and derive two AES-256-GCM keys from it, one
// per direction. Everything after that is encrypted.
package proto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Version is the protocol version spoken by this build.
const Version = 1

// MaxFrame is the largest single message accepted, in bytes.
const MaxFrame = 4 * 1024 * 1024 // This leaves room for a 512×512 picture.

// HandshakeTimeout is how long either side waits for the handshake.
const HandshakeTimeout = 10 * time.Second

const (
	transcriptLabel = "nexus-handshake-v1"
	keyLabel        = "nexus-session-keys-v1"
	pairLabel       = "nexus-pair-v1"
)

// Conn is an encrypted connection.
type Conn struct {
	raw          net.Conn
	sendMu       sync.Mutex // Send may be called from several goroutines.
	send, recv   cipher.AEAD
	sendN, recvN uint64

	// SessionID identifies this connection's handshake. Signatures made for
	// this connection include it, so they cannot be replayed elsewhere.
	SessionID [32]byte
	// GuildKey is the guild's long-term public key.
	GuildKey ed25519.PublicKey
}

type clientHello struct {
	Nexus  int    `json:"nexus"`
	X25519 []byte `json:"x25519"`
}

type serverHello struct {
	X25519   []byte `json:"x25519"`
	GuildKey []byte `json:"guild_key"`
	Sig      []byte `json:"sig"`
	Error    string `json:"error,omitempty"`
}

// Dial connects to a guild at address ("host:port") and performs the
// handshake. The caller should check c.GuildKey against a remembered key.
func Dial(address string, timeout time.Duration) (*Conn, error) {
	raw, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, err
	}
	c, err := ClientHandshake(raw)
	if err != nil {
		raw.Close()
		return nil, err
	}
	return c, nil
}

// ClientHandshake runs the client side of the handshake on raw.
func ClientHandshake(raw net.Conn) (*Conn, error) {
	raw.SetDeadline(time.Now().Add(HandshakeTimeout))
	defer raw.SetDeadline(time.Time{})

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := writeJSONFrame(raw, clientHello{Nexus: Version, X25519: priv.PublicKey().Bytes()}); err != nil {
		return nil, err
	}
	var sh serverHello
	if err := readJSONFrame(raw, &sh); err != nil {
		return nil, fmt.Errorf("guild did not answer the handshake: %w", err)
	}
	if sh.Error != "" {
		return nil, errors.New("guild refused: " + sh.Error)
	}
	if len(sh.GuildKey) != ed25519.PublicKeySize {
		return nil, errors.New("guild sent a bad key")
	}
	peer, err := ecdh.X25519().NewPublicKey(sh.X25519)
	if err != nil {
		return nil, errors.New("guild sent a bad key")
	}
	t := transcript(priv.PublicKey().Bytes(), sh.X25519, sh.GuildKey)
	if !ed25519.Verify(sh.GuildKey, t[:], sh.Sig) {
		return nil, errors.New("the guild's signature is wrong (possible impostor)")
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	c, err := newConn(raw, shared, t, true)
	if err != nil {
		return nil, err
	}
	c.GuildKey = ed25519.PublicKey(sh.GuildKey)
	return c, nil
}

// ServerHandshake runs the guild side of the handshake on raw, proving the
// guild's identity with signingKey.
func ServerHandshake(raw net.Conn, signingKey ed25519.PrivateKey) (*Conn, error) {
	raw.SetDeadline(time.Now().Add(HandshakeTimeout))
	defer raw.SetDeadline(time.Time{})

	var ch clientHello
	if err := readJSONFrame(raw, &ch); err != nil {
		return nil, err
	}
	if ch.Nexus != Version {
		writeJSONFrame(raw, serverHello{Error: fmt.Sprintf("unsupported protocol version %d (this guild speaks %d)", ch.Nexus, Version)})
		return nil, fmt.Errorf("client speaks protocol version %d", ch.Nexus)
	}
	peer, err := ecdh.X25519().NewPublicKey(ch.X25519)
	if err != nil {
		return nil, errors.New("client sent a bad key")
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	guildPub := signingKey.Public().(ed25519.PublicKey)
	t := transcript(ch.X25519, priv.PublicKey().Bytes(), guildPub)
	sh := serverHello{
		X25519:   priv.PublicKey().Bytes(),
		GuildKey: guildPub,
		Sig:      ed25519.Sign(signingKey, t[:]),
	}
	if err := writeJSONFrame(raw, sh); err != nil {
		return nil, err
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, err
	}
	c, err := newConn(raw, shared, t, false)
	if err != nil {
		return nil, err
	}
	c.GuildKey = guildPub
	return c, nil
}

func transcript(clientX, serverX, guildKey []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(transcriptLabel))
	h.Write(clientX)
	h.Write(serverX)
	h.Write(guildKey)
	var t [32]byte
	copy(t[:], h.Sum(nil))
	return t
}

func newConn(raw net.Conn, shared []byte, t [32]byte, isClient bool) (*Conn, error) {
	keys, err := hkdf.Key(sha256.New, shared, t[:], keyLabel, 64)
	if err != nil {
		return nil, err
	}
	c2s, err := newAEAD(keys[:32])
	if err != nil {
		return nil, err
	}
	s2c, err := newAEAD(keys[32:])
	if err != nil {
		return nil, err
	}
	c := &Conn{raw: raw, SessionID: t}
	if isClient {
		c.send, c.recv = c2s, s2c
	} else {
		c.send, c.recv = s2c, c2s
	}
	return c, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(n uint64) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint64(b[4:], n)
	return b
}

// Send encrypts v (as JSON) and sends it.
func (c *Conn) Send(v any) error {
	plain, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	sealed := c.send.Seal(nil, nonce(c.sendN), plain, nil)
	c.sendN++
	return writeFrame(c.raw, sealed)
}

// Recv receives one message and decodes it (as JSON) into v.
func (c *Conn) Recv(v any) error {
	sealed, err := readFrame(c.raw, MaxFrame+64)
	if err != nil {
		return err
	}
	plain, err := c.recv.Open(nil, nonce(c.recvN), sealed, nil)
	if err != nil {
		return errors.New("a message was damaged or tampered with")
	}
	c.recvN++
	return json.Unmarshal(plain, v)
}

// SetDeadline limits how long the next Send/Recv may take.
func (c *Conn) SetDeadline(t time.Time) error { return c.raw.SetDeadline(t) }

// RemoteAddr is the other side's network address.
func (c *Conn) RemoteAddr() net.Addr { return c.raw.RemoteAddr() }

// Close closes the connection.
func (c *Conn) Close() error { return c.raw.Close() }

// PairProof is what a user signs to prove they own their key when pairing.
// It is tied to this connection's SessionID so it cannot be reused.
func PairProof(sessionID [32]byte) []byte {
	return append([]byte(pairLabel), sessionID[:]...)
}

func writeFrame(w io.Writer, b []byte) error {
	if len(b) > MaxFrame {
		return errors.New("message too large")
	}
	buf := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err := w.Write(buf)
	return err
}

func readFrame(r io.Reader, max uint32) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > max {
		return nil, errors.New("message too large")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}

func writeJSONFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, b)
}

func readJSONFrame(r io.Reader, v any) error {
	b, err := readFrame(r, 4096) // Handshake messages are small.
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
