package guild

import (
	"crypto/ed25519"
	"testing"
	"time"

	"nexus/internal/proto"
)

func (c *chatter) envelope(to, text string, live bool) *proto.DMEnvelope {
	d := &proto.DMEnvelope{From: c.key, To: to, Live: live, Nonce: []byte("123456789012"), Cipher: []byte(text)}
	d.Sig = ed25519.Sign(c.u.key, d.SignedPart())
	return d
}

func TestPrivateMessages(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	benUser := newTestUser()
	if _, r := joinAs(t, addr, benUser, proto.Request{}); r.Code != "joined" {
		t.Fatalf("join: %+v", r)
	}
	ben := connectChatter(t, addr, benUser, "pc-b")
	benKey := ben.key
	ben.s.Close() // Ben goes offline.

	// While Ben is offline, the guild keeps the sealed message for him.
	o.must(t, proto.Request{Type: "dm_send", DM: o.envelope(benKey, "sealed hello", false)})
	// Live envelopes (typing) are never kept.
	o.must(t, proto.Request{Type: "dm_send", DM: o.envelope(benKey, "typing", true)})
	// Nobody can send as someone else, or with a bad signature.
	fake := o.envelope(benKey, "x", false)
	fake.From = benKey
	o.refused(t, proto.Request{Type: "dm_send", DM: fake}, "sending as someone else")
	bad := o.envelope(benKey, "x", false)
	bad.Cipher = []byte("changed")
	o.refused(t, proto.Request{Type: "dm_send", DM: bad}, "the signature doesn't match")
	o.refused(t, proto.Request{Type: "dm_send", DM: o.envelope("NOTAMEMBER", "x", false)}, "not a member")

	// Ben comes back: one message waits; once saved and acknowledged it's gone.
	ben = connectChatter(t, addr, benUser, "pc-b")
	in := ben.must(t, proto.Request{Type: "dm_inbox"}).DMs
	if len(in) != 1 || string(in[0].Cipher) != "sealed hello" || in[0].From != o.key || in[0].ID == "" {
		t.Fatalf("inbox: %+v", in)
	}
	ben.must(t, proto.Request{Type: "dm_ack", DMIDs: []string{in[0].ID}})
	if left := ben.must(t, proto.Request{Type: "dm_inbox"}).DMs; len(left) != 0 {
		t.Fatalf("still waiting: %+v", left)
	}

	// While Ben is online, a message arrives straight away.
	o.must(t, proto.Request{Type: "dm_send", DM: o.envelope(benKey, "hi now", false)})
	if ev := ben.waitEvent(t, "dm"); ev.DM == nil || string(ev.DM.Cipher) != "hi now" {
		t.Fatalf("event: %+v", ev)
	}

	// After 30 days unread messages are dropped.
	g.mu.Lock()
	g.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	g.mu.Unlock()
	if left := ben.must(t, proto.Request{Type: "dm_inbox"}).DMs; len(left) != 0 {
		t.Fatalf("old messages should be dropped: %+v", left)
	}
}

func TestStatus(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	ben := member(t, addr, "pc-b")
	status := func() (string, bool) {
		for _, m := range o.call(t, proto.Request{Type: "members"}).Members {
			if m.UserKey == ben.key {
				return m.Status, m.Online
			}
		}
		t.Fatal("Ben is not listed")
		return "", false
	}
	if s, on := status(); s != "Online" || !on {
		t.Fatalf("status %q %v", s, on)
	}
	ben.must(t, proto.Request{Type: "set_status", Status: "DND"})
	o.waitEvent(t, "presence")
	if s, _ := status(); s != "DND" {
		t.Fatalf("status %q", s)
	}
	ben.must(t, proto.Request{Type: "set_status", Status: "Invisible"})
	if s, on := status(); s != "" || on {
		t.Fatalf("Invisible should look offline: %q %v", s, on)
	}
}
