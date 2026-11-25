package guildconn

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"nexus/internal/portmap"
	"nexus/internal/user"
)

// TestOtherAddresses checks that a guild tells members where else it answers, and the
// app moves to one of those when the usual address goes quiet.
func TestOtherAddresses(t *testing.T) {
	g, addr := startGuild(t)
	portmap.Mode = "demo" // These are made-up router addresses; nothing real is asked.
	defer func() { portmap.Mode = "" }()
	k := portmap.Open(50000, true, nil)
	defer k.Close()
	waitFor(t, "the made-up router", func() bool { return k.Status().Open })
	g.SetPublic(k, 50000)

	alice, _ := user.Create(filepath.Join(t.TempDir(), "Alice"), "Alice", "alice-password")
	a := New(alice, "pc-a", func() {})
	t.Cleanup(a.CloseAll)
	id := pairWith(t, a, g, addr)
	gc, _ := a.Get(id)
	waitFor(t, "online", func() bool { return gc.session() != nil })
	saved := func() user.Guild {
		list, _ := alice.Guilds()
		for _, x := range list {
			if x.GuildID == id {
				return x
			}
		}
		return user.Guild{}
	}
	waitFor(t, "the guild's addresses are kept", func() bool {
		return slices.Contains(saved().OtherAddresses, "203.0.113.7:50000")
	})

	// The usual address stops answering; the guild is found at another.
	gc.mu.Lock()
	gc.addr, gc.others = "127.0.0.1:9", []string{"127.0.0.1:8", addr}
	gc.mu.Unlock()
	c := gc.dialOthers()
	if c == nil {
		t.Fatal("the other address should answer")
	}
	c.Close()
	if gc.addr != addr || saved().GuildAddress != addr {
		t.Fatalf("the working address becomes the usual one: %q", gc.addr)
	}
	if !slices.Contains(saved().OtherAddresses, "127.0.0.1:9") {
		t.Fatal("the old address is kept as another")
	}
	_ = time.Second
}
