package guild

import (
	"testing"
	"time"

	"nexus/internal/proto"
)

// TestVoice checks voice channels, joining, and packets being passed on.
func TestVoice(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "v-o")
	ben := member(t, addr, "v-b")
	cara := member(t, addr, "v-c")

	room := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge Room"}}).Message
	text := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "chat"}}).Message
	ben.refused(t, proto.Request{Type: "voice_join", ChannelID: text}, "a text channel")
	ben.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	cara.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	o.must(t, proto.Request{Type: "voice_join", ChannelID: room})

	inRoom := func() map[string]proto.VoiceMember {
		got := map[string]proto.VoiceMember{}
		for _, n := range o.must(t, proto.Request{Type: "channels"}).Channels {
			if n.ID == room {
				if n.Name != "Lounge Room" {
					t.Fatalf("voice channel names keep their case: %q", n.Name)
				}
				for _, v := range n.Voice {
					got[v.User] = v
				}
			}
		}
		return got
	}
	if len(inRoom()) != 3 {
		t.Fatalf("in the room: %+v", inRoom())
	}

	send := func(c *chatter, seq uint32) {
		c.s.Post(proto.Request{Type: "voice", Voice: &proto.VoicePacket{Channel: room, Version: 1, Seq: seq, Nonce: []byte{1}, Cipher: []byte{2, 3}}})
	}
	hear := func(c *chatter, from string, seq uint32) {
		t.Helper()
		select {
		case r := <-c.s.Voice:
			if r.Voice.From != from || r.Voice.Seq != seq {
				t.Fatalf("heard %+v", r.Voice)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("nothing heard (seq %d)", seq)
		}
	}
	quiet := func(c *chatter) {
		t.Helper()
		select {
		case r := <-c.s.Voice:
			t.Fatalf("should hear nothing, heard %+v", r.Voice)
		case <-time.After(300 * time.Millisecond):
		}
	}

	send(ben, 1)
	hear(o, ben.key, 1)
	hear(cara, ben.key, 1)
	quiet(ben) // You don't hear your own voice back.

	// Deafened people hear nothing; muted people send nothing.
	cara.must(t, proto.Request{Type: "voice_state", Deafened: true})
	if !inRoom()[cara.key].Deafened || !inRoom()[cara.key].Muted {
		t.Fatal("deafened means muted too")
	}
	send(ben, 2)
	hear(o, ben.key, 2)
	quiet(cara)
	send(cara, 3)
	quiet(o)

	// Test leaving, and going away.
	ben.must(t, proto.Request{Type: "voice_leave"})
	if _, in := inRoom()[ben.key]; in {
		t.Fatal("Ben left")
	}
	cara.s.Close()
	time.Sleep(200 * time.Millisecond)
	if _, in := inRoom()[cara.key]; in {
		t.Fatal("Cara's connection ended, so she left voice")
	}
}

// TestStream checks going live, watching, and pictures being passed only to viewers.
func TestStream(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "s-o")
	ben := member(t, addr, "s-b")
	cara := member(t, addr, "s-c")
	room := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge"}}).Message

	ben.refused(t, proto.Request{Type: "stream_start"}, "not in voice")
	for _, c := range []*chatter{o, ben, cara} {
		c.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	}
	o.must(t, proto.Request{Type: "stream_start"})
	live := false
	for _, n := range ben.must(t, proto.Request{Type: "channels"}).Channels {
		for _, v := range n.Voice {
			if v.User == o.key && v.Live {
				live = true
			}
		}
	}
	if !live {
		t.Fatal("the owner should show as live")
	}
	ben.refused(t, proto.Request{Type: "stream_watch", Target: cara.key}, "Cara isn't live")
	ben.must(t, proto.Request{Type: "stream_watch", Target: o.key})
	o.waitEvent(t, "stream_key") // The sharer is asked for a full picture.

	send := func(seq uint32) {
		o.s.Post(proto.Request{Type: "stream", Stream: &proto.StreamPacket{Channel: room, Kind: proto.StreamVideo, Key: true, Seq: seq, Version: 1, Nonce: []byte{1}, Cipher: []byte{2}}})
	}
	send(1)
	select {
	case r := <-ben.s.Voice:
		if r.Stream == nil || r.Stream.From != o.key || r.Stream.Seq != 1 {
			t.Fatalf("Ben got %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ben, watching, should get the picture")
	}
	select {
	case r := <-cara.s.Voice:
		t.Fatalf("Cara isn't watching but got %+v", r)
	case <-time.After(300 * time.Millisecond):
	}

	// Stopping ends watching too.
	o.must(t, proto.Request{Type: "stream_stop"})
	send(2)
	select {
	case r := <-ben.s.Voice:
		t.Fatalf("nothing after stopping, got %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
}
