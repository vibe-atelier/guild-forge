package guild

import (
	"testing"
	"time"

	"nexus/internal/proto"
)

// TestVoiceModeration checks guild mute and deafen, moving, disconnecting and
// locking, Priority Speaker and previews.
func TestVoiceModeration(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "vm-o")
	mod := member(t, addr, "vm-m")
	ben := member(t, addr, "vm-b")
	cara := member(t, addr, "vm-c")
	modRole := makeRole(t, o, "Mod", 10, proto.PermMuteMembers, proto.PermDeafenMembers, proto.PermMoveMembers,
		proto.PermDisconnectMembers, proto.PermLockChannel, proto.PermPrioritySpeaker)
	o.must(t, proto.Request{Type: "member_roles", Target: mod.key, Roles: []string{modRole}})

	room := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge"}}).Message
	other := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Quiet"}}).Message
	for _, c := range []*chatter{mod, ben, cara} {
		c.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	}
	state := func(user string) (proto.VoiceMember, string) {
		for _, n := range o.must(t, proto.Request{Type: "channels"}).Channels {
			for _, v := range n.Voice {
				if v.User == user {
					return v, n.ID
				}
			}
		}
		return proto.VoiceMember{}, ""
	}
	send := func(c *chatter, seq uint32, priority bool) {
		c.s.Post(proto.Request{Type: "voice", Voice: &proto.VoicePacket{Channel: room, Version: 1, Seq: seq, Priority: priority, Nonce: []byte{1}, Cipher: []byte{2}}})
	}
	hear := func(c *chatter) *proto.VoicePacket {
		t.Helper()
		for {
			select {
			case r := <-c.s.Voice:
				if r.Voice != nil {
					return r.Voice
				}
			case <-time.After(2 * time.Second):
				t.Fatal("nothing heard")
			}
		}
	}
	quiet := func(c *chatter) {
		t.Helper()
		select {
		case r := <-c.s.Voice:
			t.Fatalf("should hear nothing, heard %+v", r)
		case <-time.After(300 * time.Millisecond):
		}
	}

	// Guild mute: Ben's voice goes nowhere, and he can't lift it himself.
	ben.refused(t, proto.Request{Type: "voice_mute", Target: mod.key, On: true}, "no permission")
	mod.must(t, proto.Request{Type: "voice_mute", Target: ben.key, On: true})
	if v, _ := state(ben.key); !v.GuildMuted {
		t.Fatal("Ben should show as muted by a moderator")
	}
	send(ben, 1, false)
	quiet(cara)
	ben.must(t, proto.Request{Type: "voice_state"}) // His own unmute changes nothing.
	send(ben, 2, false)
	quiet(cara)
	ben.refused(t, proto.Request{Type: "sound_play", SoundID: "x"}, "no sounds while muted")
	// It stays after leaving and coming back.
	ben.must(t, proto.Request{Type: "voice_leave"})
	ben.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	if v, _ := state(ben.key); !v.GuildMuted {
		t.Fatal("a guild mute stays until a moderator lifts it")
	}
	mod.must(t, proto.Request{Type: "voice_mute", Target: ben.key, On: false})
	send(ben, 3, false)
	if p := hear(cara); p.Seq != 3 {
		t.Fatalf("heard %+v", p)
	}
	hear(mod)

	// Guild deafen: Cara hears nothing.
	mod.must(t, proto.Request{Type: "voice_deafen", Target: cara.key, On: true})
	send(ben, 4, false)
	hear(mod)
	quiet(cara)
	mod.must(t, proto.Request{Type: "voice_deafen", Target: cara.key, On: false})

	// Ranks count: the mod can't act on the owner.
	o.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	mod.refused(t, proto.Request{Type: "voice_mute", Target: o.key, On: true}, "the owner outranks everyone")

	// Priority Speaker is passed on for the mod, and cleared for Ben.
	send(mod, 5, true)
	if p := hear(cara); !p.Priority {
		t.Fatal("the mod's priority should be passed on")
	}
	hear(ben)
	hear(o)
	send(ben, 6, true)
	if p := hear(cara); p.Priority {
		t.Fatal("Ben has no Priority Speaker permission")
	}
	hear(mod)
	hear(o)

	// Test moving and disconnecting.
	mod.must(t, proto.Request{Type: "voice_move", Target: cara.key, ChannelID: other})
	if ev := cara.waitEvent(t, "voice_moved"); ev.Channel != other {
		t.Fatalf("Cara should be told where she went: %+v", ev)
	}
	if _, ch := state(cara.key); ch != other {
		t.Fatal("Cara should be in the other channel")
	}
	mod.must(t, proto.Request{Type: "voice_disconnect", Target: cara.key})
	if ev := cara.waitEvent(t, "voice_moved"); ev.Channel != "" {
		t.Fatalf("disconnected: %+v", ev)
	}
	if _, ch := state(cara.key); ch != "" {
		t.Fatal("Cara should be out of voice")
	}

	// A locked channel lets nobody new in, except those who may lock it; moving still works.
	ben.refused(t, proto.Request{Type: "voice_lock", ChannelID: room, On: true}, "no permission")
	mod.must(t, proto.Request{Type: "voice_lock", ChannelID: room, On: true})
	locked := false
	for _, n := range cara.must(t, proto.Request{Type: "channels"}).Channels {
		locked = locked || (n.ID == room && n.Locked)
	}
	if !locked {
		t.Fatal("the channel should show as locked")
	}
	cara.refused(t, proto.Request{Type: "voice_join", ChannelID: room}, "locked")
	ben.must(t, proto.Request{Type: "voice_join", ChannelID: room}) // He is already in it.
	cara.must(t, proto.Request{Type: "voice_join", ChannelID: other})
	mod.must(t, proto.Request{Type: "voice_move", Target: cara.key, ChannelID: room})
	mod.must(t, proto.Request{Type: "voice_lock", ChannelID: room, On: false})

	// Previews go to everyone in the channel, not only viewers.
	ben.must(t, proto.Request{Type: "stream_start"})
	ben.s.Post(proto.Request{Type: "stream", Stream: &proto.StreamPacket{Channel: room, Kind: proto.StreamPreview, Seq: 1, Version: 1, Nonce: []byte{1}, Cipher: []byte{2}}})
	select {
	case r := <-mod.s.Voice:
		if r.Stream == nil || r.Stream.Kind != proto.StreamPreview || r.Stream.From != ben.key {
			t.Fatalf("got %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the mod should get Ben's preview without watching")
	}
}

// TestVoiceModMigration checks that "Moderate Voice" becomes the five permissions.
func TestVoiceModMigration(t *testing.T) {
	g := &Server{roles: []proto.Role{{ID: "r", Perms: []string{proto.PermSpeak, "moderate_voice"}}},
		DataDir: t.TempDir()}
	g.chat.nodes = []proto.ChannelNode{{ID: "c", Overrides: []proto.Override{{Role: "r", Deny: []string{"moderate_voice"}}}}}
	g.migrateVoiceMod()
	if p := g.roles[0].Perms; len(p) != 6 || contains(p, "moderate_voice") || !contains(p, proto.PermLockChannel) {
		t.Fatalf("role perms: %v", p)
	}
	if d := g.chat.nodes[0].Overrides[0].Deny; len(d) != 5 || !contains(d, proto.PermMuteMembers) {
		t.Fatalf("override: %v", d)
	}
}
