package guild

import (
	"crypto/ed25519"
	"sync"
	"testing"
	"time"

	"nexus/internal/e2e"
	"nexus/internal/proto"
)

// keyedRoom makes a channel whose key the owner and every member already
// have, and returns it with the key.
func keyedRoom(t *testing.T, o *chatter, name string) (string, []byte) {
	t.Helper()
	ch := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: name}}).Message
	key, _ := e2e.NewChannelKey()
	var boxes []proto.KeyBox
	for u, pub := range o.members(t) {
		b, _ := e2e.SealKey(pub, key)
		boxes = append(boxes, proto.KeyBox{Channel: ch, Version: 1, To: u, Box: b})
	}
	o.must(t, proto.Request{Type: "put_keys", Boxes: boxes})
	return ch, key
}

// sealed makes a signed message ready to send.
func (c *chatter) sealed(channel string, key []byte, text string) *proto.ChatMessage {
	nonce, sealed, _ := e2e.Encrypt(key, channel, 1, []byte(text))
	m := &proto.ChatMessage{Channel: channel, Author: c.key, Version: 1, Nonce: nonce, Cipher: sealed}
	m.Sig = ed25519.Sign(c.u.key, m.SignedPart())
	return m
}

func (c *chatter) find(t *testing.T, channel, id string) *proto.ChatMessage {
	t.Helper()
	for _, m := range c.must(t, proto.Request{Type: "history", ChannelID: channel}).Messages {
		if m.ID == id {
			return &m
		}
	}
	return nil
}

// clockAt lets a test move the guild's time.
type clockAt struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clockAt) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clockAt) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// TestTelegramFeatures checks polls and quizzes, pins, Slow Mode, Edit History,
// disappearing and scheduled messages.
func TestTelegramFeatures(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	clock := &clockAt{t: time.Now()}
	g.mu.Lock()
	g.now = clock.now
	g.mu.Unlock()
	o := connectChatter(t, addr, owner, "tg-o")
	ben := member(t, addr, "tg-b")
	room, key := keyedRoom(t, o, "general")

	// A poll allows one vote each, which can be changed or taken back.
	poll := o.sealed(room, key, "Lunch? / Rice / Noodles")
	poll.Poll = &proto.PollInfo{Choices: 2}
	id := o.must(t, proto.Request{Type: "send", Msg: poll}).Chat.ID
	ben.must(t, proto.Request{Type: "poll_vote", ChannelID: room, MessageID: id, Choice: 0})
	ben.must(t, proto.Request{Type: "poll_vote", ChannelID: room, MessageID: id, Choice: 1})
	o.must(t, proto.Request{Type: "poll_vote", ChannelID: room, MessageID: id, Choice: 1})
	ben.refused(t, proto.Request{Type: "poll_vote", ChannelID: room, MessageID: id, Choice: 5}, "no such choice")
	if v := o.find(t, room, id).Votes; len(v[0]) != 0 || len(v[1]) != 2 {
		t.Fatalf("votes: %v", v)
	}
	// A quiz: the first answer stays.
	quiz := o.sealed(room, key, "2+2? / 3 / 4")
	quiz.Poll = &proto.PollInfo{Choices: 2, Quiz: true}
	qid := o.must(t, proto.Request{Type: "send", Msg: quiz}).Chat.ID
	ben.must(t, proto.Request{Type: "poll_vote", ChannelID: room, MessageID: qid, Choice: 0})
	ben.refused(t, proto.Request{Type: "poll_vote", ChannelID: room, MessageID: qid, Choice: 1}, "quiz answers stay")
	bad := o.sealed(room, key, "one choice")
	bad.Poll = &proto.PollInfo{Choices: 1}
	o.refused(t, proto.Request{Type: "send", Msg: bad}, "a poll needs two choices")

	// Pinning needs Pin Messages (the owner has every permission).
	ben.refused(t, proto.Request{Type: "pin", ChannelID: room, MessageID: id, On: true}, "Ben can't pin")
	o.must(t, proto.Request{Type: "pin", ChannelID: room, MessageID: id, On: true})
	o.must(t, proto.Request{Type: "pin", ChannelID: room, MessageID: qid, On: true})
	pins := func() []string {
		for _, n := range ben.must(t, proto.Request{Type: "channels"}).Channels {
			if n.ID == room {
				return n.Pins
			}
		}
		return nil
	}
	if p := pins(); len(p) != 2 || p[0] != qid {
		t.Fatalf("pins, newest first: %v", p)
	}
	o.must(t, proto.Request{Type: "delete_message", ChannelID: room, MessageID: qid})
	if p := pins(); len(p) != 1 || p[0] != id {
		t.Fatalf("a deleted message is unpinned: %v", p)
	}

	// Edit History: the earlier text is kept, sealed.
	first := o.must(t, proto.Request{Type: "send", Msg: o.sealed(room, key, "helo")}).Chat
	fixed := o.sealed(room, key, "hello")
	fixed.ID = first.ID
	o.must(t, proto.Request{Type: "edit", Msg: fixed})
	m := o.find(t, room, first.ID)
	if len(m.Earlier) != 1 {
		t.Fatalf("earlier: %+v", m.Earlier)
	}
	if text, err := e2e.Decrypt(key, room, 1, m.Earlier[0].Nonce, m.Earlier[0].Cipher); err != nil || string(text) != "helo" {
		t.Fatalf("the earlier text should open to %q, got %q %v", "helo", text, err)
	}

	// Slow Mode: Ben waits; the owner doesn't.
	o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{ID: room, Kind: proto.KindChannel, Name: "general", Slow: 30}})
	ben.must(t, proto.Request{Type: "send", Msg: ben.sealed(room, key, "one")})
	ben.refused(t, proto.Request{Type: "send", Msg: ben.sealed(room, key, "two")}, "Slow Mode")
	o.must(t, proto.Request{Type: "send", Msg: o.sealed(room, key, "staff are exempt")})
	clock.add(31 * time.Second)
	ben.must(t, proto.Request{Type: "send", Msg: ben.sealed(room, key, "two")})
	o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{ID: room, Kind: proto.KindChannel, Name: "general"}})

	// A disappearing message is gone once its time is up.
	brief := o.sealed(room, key, "this goes away")
	brief.Expires = clock.now().Add(time.Hour).UTC().Format(time.RFC3339)
	bid := o.must(t, proto.Request{Type: "send", Msg: brief}).Chat.ID
	long := o.sealed(room, key, "too long")
	long.Expires = clock.now().Add(60 * 24 * time.Hour).UTC().Format(time.RFC3339)
	o.refused(t, proto.Request{Type: "send", Msg: long}, "at most four weeks")
	g.tick()
	if m := o.find(t, room, bid); m == nil || m.Gone {
		t.Fatal("not gone before its time")
	}
	clock.add(time.Hour + time.Minute)
	g.tick()
	if m := o.find(t, room, bid); m == nil || !m.Gone || len(m.Cipher) != 0 {
		t.Fatalf("it should be gone, with its text: %+v", m)
	}

	// A scheduled message is held by the guild, can be cancelled, and is posted on time.
	at := func(d time.Duration) string { return clock.now().Add(d).UTC().Format(time.RFC3339) }
	ben.refused(t, proto.Request{Type: "schedule", Msg: ben.sealed(room, key, "past"), At: at(-time.Minute)}, "in the past")
	later := ben.must(t, proto.Request{Type: "schedule", Msg: ben.sealed(room, key, "good morning"), At: at(2 * time.Hour)}).Message
	gone := ben.must(t, proto.Request{Type: "schedule", Msg: ben.sealed(room, key, "never mind"), At: at(3 * time.Hour)}).Message
	if list := ben.must(t, proto.Request{Type: "scheduled"}).Scheduled; len(list) != 2 || list[0].ID != later {
		t.Fatalf("scheduled: %+v", list)
	}
	if n := len(o.must(t, proto.Request{Type: "scheduled"}).Scheduled); n != 0 {
		t.Fatal("others' scheduled messages are not yours to see")
	}
	o.refused(t, proto.Request{Type: "schedule_cancel", ItemID: gone}, "not the owner's")
	ben.must(t, proto.Request{Type: "schedule_cancel", ItemID: gone})
	count := func() int { return len(o.must(t, proto.Request{Type: "history", ChannelID: room}).Messages) }
	before := count()
	clock.add(2*time.Hour + time.Minute)
	g.tick()
	if count() != before+1 {
		t.Fatal("the scheduled message should be posted on time")
	}
	clock.add(2 * time.Hour)
	g.tick()
	if count() != before+1 {
		t.Fatal("the cancelled one should never be posted")
	}
	if n := len(ben.must(t, proto.Request{Type: "scheduled"}).Scheduled); n != 0 {
		t.Fatalf("%d still scheduled", n)
	}
}
