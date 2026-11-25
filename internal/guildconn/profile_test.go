package guildconn

import (
	"path/filepath"
	"testing"

	"nexus/internal/proto"
	"nexus/internal/user"
)

// TestLookAndSettings checks that your picture, bio and new name reach the guild;
// notification choices and read positions are kept by it.
func TestLookAndSettings(t *testing.T) {
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
	online := func(m *Manager) bool {
		for _, v := range m.Views() {
			if v.ID == id && v.Status == "online" {
				return true
			}
		}
		return false
	}
	waitFor(t, "both online", func() bool { return online(a) && online(b) })
	ga, _ := a.Get(id)
	gb, _ := b.Get(id)

	if err := a.SetLook("Pixel **artist**", testPNG, nil, false, false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetName("Alice B"); err != nil {
		t.Fatal(err)
	}
	if err := ga.SetNick("", "Ali"); err != nil {
		t.Fatal(err)
	}
	aliceInBen := func() MemberView {
		for _, v := range b.Views() {
			for _, m := range v.Members {
				if m.UserKey == ga.me() {
					return m
				}
			}
		}
		return MemberView{}
	}
	waitFor(t, "Ben sees Alice's look", func() bool {
		if s := gb.session(); s != nil {
			gb.refreshMembers(s)
		}
		m := aliceInBen()
		return m.Bio == "Pixel **artist**" && m.PictureHash == hashOf(testPNG) && m.Name == "Alice B" && m.Nick == "Ali"
	})
	if d, err := gb.Call(proto.Request{Type: "image_get", Hash: hashOf(testPNG)}); err != nil || string(d.Data) != string(testPNG) {
		t.Fatalf("picture: %v", err)
	}

	// Test the notification choices.
	if c, chosen := gb.Notify("any"); c != proto.NotifyMentions || chosen {
		t.Fatalf("default: %s %v", c, chosen)
	}
	if err := gb.SetNotify("", proto.NotifyNothing); err != nil {
		t.Fatal(err)
	}
	if c, _ := gb.Notify("any"); c != proto.NotifyNothing {
		t.Fatal("a muted guild mutes its channels")
	}
	r, err := ga.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "general"}})
	if err != nil {
		t.Fatal(err)
	}
	ch := r.Message
	if err := gb.SetNotify(ch, proto.NotifyAll); err != nil {
		t.Fatal(err)
	}
	if c, chosen := gb.Notify(ch); c != proto.NotifyAll || !chosen {
		t.Fatal("a channel's own choice wins")
	}
	if err := gb.SetCollapsed([]string{"cat"}); err != nil {
		t.Fatal(err)
	}

	// Read positions go to the guild.
	waitFor(t, "Alice has the key", func() bool {
		ga.mu.Lock()
		defer ga.mu.Unlock()
		return len(ga.chat.keys[ch]) > 0
	})
	if err := ga.Send(ch, "hello", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the read position is kept by the guild", func() bool {
		ga.History(ch, "")
		got, err := ga.Call(proto.Request{Type: "settings_get"})
		return err == nil && got.Settings != nil && got.Settings.Reads[ch] != ""
	})
	got, _ := gb.Call(proto.Request{Type: "settings_get"})
	if got.Settings.Notify[ch] != proto.NotifyAll || len(got.Settings.Collapsed) != 1 {
		t.Fatalf("Ben's settings: %+v", got.Settings)
	}
}
