package guildconn

import (
	"path/filepath"
	"sync"
	"testing"

	"nexus/internal/proto"
	"nexus/internal/user"
)

// TestVoiceSealed: voice is sealed with the voice channel's key by one app
// and opened by another; the guild only passes it on.
func TestVoiceSealed(t *testing.T) {
	g, addr := startGuild(t)
	alice, _ := user.Create(filepath.Join(t.TempDir(), "Alice"), "Alice", "alice-password")
	ben, _ := user.Create(filepath.Join(t.TempDir(), "Ben"), "Ben", "ben-password")
	a := New(alice, "pc-a", func() {})
	t.Cleanup(a.CloseAll)
	id := pairWith(t, a, g, addr)
	b := New(ben, "pc-b", func() {})
	t.Cleanup(b.CloseAll)
	if r, err := b.JoinStart(addr, id, "", false); err != nil || r.Code != "joined" {
		t.Fatalf("join: %v %+v", err, r)
	}
	ca, _ := a.Get(id)
	cb, _ := b.Get(id)
	waitFor(t, "both online", func() bool { return ca.session() != nil && cb.session() != nil })

	r, err := ca.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge"}})
	if err != nil || !r.OK {
		t.Fatalf("voice channel: %v %+v", err, r)
	}
	room := r.Message
	waitFor(t, "both have the voice key", func() bool { return ca.HasVoiceKey(room) && cb.HasVoiceKey(room) })

	var mu sync.Mutex
	var heard [][]byte
	b.SetVoiceSink(func(guildID, from string, seq uint32, opus []byte, _ bool) {
		mu.Lock()
		heard = append(heard, opus)
		mu.Unlock()
	})
	if err := ca.VoiceJoin(room); err != nil {
		t.Fatal(err)
	}
	if err := cb.VoiceJoin(room); err != nil {
		t.Fatal(err)
	}
	for i := uint32(1); i <= 3; i++ {
		if err := ca.SendVoice(room, i, []byte("pretend opus"), false); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "Ben hears Alice", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(heard) == 3 && string(heard[0]) == "pretend opus"
	})
}
