package proto

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
)

func TestHandshakeAndMessages(t *testing.T) {
	_, guildPriv, _ := ed25519.GenerateKey(rand.Reader)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	type result struct {
		c   *Conn
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := ServerHandshake(b, guildPriv)
		done <- result{c, err}
	}()
	client, err := ClientHandshake(a)
	if err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	server := r.c
	if client.SessionID != server.SessionID {
		t.Fatal("both sides should agree on the session ID")
	}
	if !client.GuildKey.Equal(guildPriv.Public()) {
		t.Fatal("client got the wrong guild key")
	}

	go client.Send(Request{Type: "info"})
	var req Request
	if err := server.Recv(&req); err != nil || req.Type != "info" {
		t.Fatalf("got %+v, %v", req, err)
	}
	go server.Send(Response{Type: "info", OK: true, HasOwner: true})
	var resp Response
	if err := client.Recv(&resp); err != nil || !resp.HasOwner {
		t.Fatalf("got %+v, %v", resp, err)
	}
	// A second message in the same direction uses the next nonce.
	go client.Send(Request{Type: "pair", OTP: "ABC"})
	if err := server.Recv(&req); err != nil || req.OTP != "ABC" {
		t.Fatalf("got %+v, %v", req, err)
	}
}

func TestHandshakeRejectsWrongSignature(t *testing.T) {
	_, guildPriv, _ := ed25519.GenerateKey(rand.Reader)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	// A fake guild that signs with a key different from the one it claims.
	go func() {
		var ch clientHello
		readJSONFrame(b, &ch)
		_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
		writeJSONFrame(b, serverHello{
			X25519:   make([]byte, 32),
			GuildKey: guildPriv.Public().(ed25519.PublicKey),
			Sig:      ed25519.Sign(otherPriv, []byte("whatever")),
		})
	}()
	if _, err := ClientHandshake(a); err == nil {
		t.Fatal("handshake should fail")
	}
}
