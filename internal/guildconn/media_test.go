package guildconn

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/proto"
	"nexus/internal/user"
)

func startGuild(t *testing.T) (*guild.Server, string) {
	t.Helper()
	dir := t.TempDir()
	gd, err := guildstore.NewGuildData("goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	gd.Save(dir)
	g, err := guild.NewServer(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(l)
	t.Cleanup(func() { g.Stop(); l.Close() })
	return g, l.Addr().String()
}

func pairWith(t *testing.T, m *Manager, g *guild.Server, addr string) string {
	t.Helper()
	g.StartBroadcast()
	otp, _, _ := g.NewOTP()
	c, err := m.Check(addr)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := m.Pair(c.Address, c.GuildID, otp); err != nil || !r.OK {
		t.Fatalf("pair: %v %+v", err, r)
	}
	return c.GuildID
}

var testPNG = []byte("\x89PNG\r\n\x1a\nan emoji")

// TestFilesAndBorrowedEmoji checks that a file in a channel is encrypted and comes back
// whole; an emoji from guild A used in guild B is borrowed into B, so a
// member of only B can see it.
func TestFilesAndBorrowedEmoji(t *testing.T) {
	ga, addrA := startGuild(t)
	gb, addrB := startGuild(t)
	alice, _ := user.Create(filepath.Join(t.TempDir(), "Alice"), "Alice", "alice-password")
	ben, _ := user.Create(filepath.Join(t.TempDir(), "Ben"), "Ben", "ben-password")
	a := New(alice, "pc-a", func() {})
	t.Cleanup(a.CloseAll)
	idA := pairWith(t, a, ga, addrA)
	idB := pairWith(t, a, gb, addrB)
	b := New(ben, "pc-b", func() {})
	t.Cleanup(b.CloseAll)
	if r, err := b.JoinStart(addrB, idB, "", false); err != nil || r.Code != "joined" {
		t.Fatalf("join: %v %+v", err, r)
	}
	connA, _ := a.Get(idA)
	connB, _ := a.Get(idB)
	online := func(m *Manager, ids ...string) bool {
		n := 0
		for _, v := range m.Views() {
			for _, id := range ids {
				if v.ID == id && v.Status == "online" {
					n++
				}
			}
		}
		return n == len(ids)
	}
	waitFor(t, "Alice online in both", func() bool { return online(a, idA, idB) })
	waitFor(t, "Ben online", func() bool { return online(b, idB) })

	// Add Guild A's emoji.
	if err := connA.PutEmoji("pixel_heart", testPNG); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the emoji listed", func() bool { return len(a.UsableEmoji(idB)) == 1 })

	// Make a channel in B, with its key.
	r, err := connB.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "general"}})
	if err != nil {
		t.Fatal(err)
	}
	ch := r.Message
	waitFor(t, "Alice has the key", func() bool {
		connB.mu.Lock()
		defer connB.mu.Unlock()
		return len(connB.chat.keys[ch]) > 0
	})

	// Using :pixel_heart: in guild B borrows it there.
	if err := connB.Send(ch, "Hello :pixel_heart:", ""); err != nil {
		t.Fatal(err)
	}
	benB, _ := b.Get(idB)
	waitFor(t, "Ben sees the borrowed emoji", func() bool {
		if s := benB.session(); s != nil {
			benB.refreshEmoji(s)
		}
		benB.mu.Lock()
		defer benB.mu.Unlock()
		return len(benB.emoji) == 1 && benB.emoji[0].Borrowed
	})
	path, err := b.EmojiFile(benB.GuildEmojiAll()[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(testPNG) {
		t.Fatal("Ben should get the emoji's picture from guild B")
	}

	// A file is encrypted on the guild, and whole again for Ben.
	if err := connB.SendFile(ch, "sprite.png", []byte("pretend picture"), ""); err != nil {
		t.Fatal(err)
	}
	var ref FileRef
	waitFor(t, "Ben gets the file message", func() bool {
		if benB.session() == nil {
			return false
		}
		msgs, _, err := benB.History(ch, "")
		if err != nil {
			return false
		}
		for _, m := range msgs {
			if f, ok := ParseFileRef(m.Text); ok {
				ref = f
				return true
			}
		}
		return false
	})
	if ref.Name != "sprite.png" || ref.Size != 15 {
		t.Fatalf("ref: %+v", ref)
	}
	p, err := benB.OpenFile(ch, ref)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); string(data) != "pretend picture" {
		t.Fatal("the file should come back whole")
	}
	msgs, _, _ := benB.History(ch, "")
	if !strings.Contains(msgs[0].Text, ":pixel_heart:") {
		t.Fatalf("the emoji token should be in the text: %q", msgs[0].Text)
	}
}

// GuildEmojiAll lists all emoji this guild can show (test helper).
func (gc *Conn) GuildEmojiAll() []proto.Emoji {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return append([]proto.Emoji{}, gc.emoji...)
}
