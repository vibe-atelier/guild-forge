package voice

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"nexus/internal/capture"
	"nexus/internal/guild"
	"nexus/internal/guildconn"
	"nexus/internal/guildstore"
	"nexus/internal/proto"
	"nexus/internal/user"
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestShareAndWatch checks that, when Alice shares a (made-up) screen, Ben, watching, gets
// pictures through the guild, sealed with the voice channel's key.
func TestShareAndWatch(t *testing.T) {
	capture.Demo = true
	defer func() { capture.Demo = false }()

	dir := t.TempDir()
	gd, _ := guildstore.NewGuildData("goodpassword")
	gd.Save(dir)
	g, err := guild.NewServer(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	go g.Serve(l)
	defer func() { g.Stop(); l.Close() }()
	addr := l.Addr().String()

	alice, _ := user.Create(filepath.Join(t.TempDir(), "Alice"), "Alice", "alice-password")
	ben, _ := user.Create(filepath.Join(t.TempDir(), "Ben"), "Ben", "ben-password")
	a := guildconn.New(alice, "pc-a", func() {})
	defer a.CloseAll()
	g.StartBroadcast()
	otp, _, _ := g.NewOTP()
	c, err := a.Check(addr)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := a.Pair(c.Address, c.GuildID, otp); err != nil || !r.OK {
		t.Fatalf("pair: %v %+v", err, r)
	}
	b := guildconn.New(ben, "pc-b", func() {})
	defer b.CloseAll()
	if r, err := b.JoinStart(addr, c.GuildID, "", false); err != nil || r.Code != "joined" {
		t.Fatalf("join: %v %+v", err, r)
	}
	ca, _ := a.Get(c.GuildID)
	cb, _ := b.Get(c.GuildID)
	waitFor(t, "both online", func() bool { return ca.VoiceLeave() == nil && cb.VoiceLeave() == nil })
	r, err := ca.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge"}})
	if err != nil || !r.OK {
		t.Fatalf("voice channel: %v %+v", err, r)
	}
	room := r.Message
	waitFor(t, "voice keys", func() bool { return ca.HasVoiceKey(room) && cb.HasVoiceKey(room) })

	// Engines without sound devices: only sharing and watching are tested.
	ea := &Engine{m: a, speakers: map[string]*speaker{}}
	eb := &Engine{m: b, speakers: map[string]*speaker{}}
	ea.streamHooks(a)
	eb.streamHooks(b)
	for _, x := range []struct {
		e  *Engine
		gc *guildconn.Conn
	}{{ea, ca}, {eb, cb}} {
		if err := x.gc.VoiceJoin(room); err != nil {
			t.Fatal(err)
		}
		x.e.conn, x.e.guild, x.e.channel = x.gc, c.GuildID, room
	}
	targets, _ := capture.Targets()
	if err := ea.StartSharing(targets[0], Qualities[0], false); err != nil {
		t.Fatal(err)
	}
	defer ea.StopSharing()
	if err := eb.Watch(alice.Profile.UserID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Ben sees Alice's screen", func() bool {
		_, f := eb.Watching()
		return f != nil && f.Bounds().Dx() == 1280 && f.Bounds().Dy() == 720
	})
}
