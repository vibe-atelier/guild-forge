package guild

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"strings"
	"testing"
	"time"

	"nexus/internal/e2e"
	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// chatter is a member with an encryption key, signed in.
type chatter struct {
	u   testUser
	enc *ecdh.PrivateKey
	s   *proto.Session
	key string
}

func connectChatter(t *testing.T, addr string, u testUser, device string) *chatter {
	t.Helper()
	enc, _ := ecdh.X25519().GenerateKey(rand.Reader)
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s := proto.NewSession(c)
	t.Cleanup(func() { s.Close() })
	r, _ := s.Call(proto.Request{Type: "auth", UserKey: u.key.Public().(ed25519.PublicKey),
		Sig: ed25519.Sign(u.key, proto.AuthProof(s.SessionID(), device)), DeviceID: device,
		EncKey: enc.PublicKey().Bytes(), UserName: "x"}, 2*time.Second)
	if !r.OK {
		t.Fatalf("auth: %+v", r)
	}
	return &chatter{u: u, enc: enc, s: s, key: guildstore.KeyText(u.key.Public().(ed25519.PublicKey))}
}

func (c *chatter) call(t *testing.T, req proto.Request) proto.Response {
	t.Helper()
	r, err := c.s.Call(req, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (c *chatter) waitEvent(t *testing.T, want string) proto.Response {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev := <-c.s.Events:
			if ev.Event == want {
				return ev
			}
		case <-timeout:
			t.Fatalf("no %q event", want)
		}
	}
}

// members returns user key -> encryption key.
func (c *chatter) members(t *testing.T) map[string][]byte {
	out := map[string][]byte{}
	for _, m := range c.call(t, proto.Request{Type: "members"}).Members {
		if len(m.EncKey) == 32 {
			out[m.UserKey] = m.EncKey
		}
	}
	return out
}

// myKey opens this member's box for a channel/version.
func (c *chatter) myKey(t *testing.T, channel string, version int) []byte {
	t.Helper()
	for _, b := range c.call(t, proto.Request{Type: "my_keys"}).Boxes {
		if b.Channel == channel && b.Version == version {
			k, err := e2e.OpenKey(c.enc, b.Box)
			if err != nil {
				t.Fatal(err)
			}
			return k
		}
	}
	return nil
}

func (c *chatter) say(t *testing.T, channel string, version int, key []byte, text string) proto.Response {
	t.Helper()
	nonce, sealed, _ := e2e.Encrypt(key, channel, version, []byte(text))
	m := proto.ChatMessage{Channel: channel, Author: c.key, Version: version, Nonce: nonce, Cipher: sealed}
	m.Sig = ed25519.Sign(c.u.key, m.SignedPart())
	return c.call(t, proto.Request{Type: "send", Msg: &m})
}

func TestChannelsKeysAndMessages(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")

	// The owner builds a small tree.
	cat := o.call(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindCategory, Name: "General"}}).Message
	ch := o.call(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "Chat Room", Parent: cat}})
	if !ch.OK {
		t.Fatalf("channel: %+v", ch)
	}
	chID := ch.Message
	// The guild asks the owner's app for a first key.
	if ev := o.waitEvent(t, "keys_needed"); ev.Channel != chID {
		t.Fatalf("keys_needed for %q", ev.Channel)
	}
	list := o.call(t, proto.Request{Type: "channels"}).Channels
	if len(list) != 2 || list[1].Name != "chat-room" || !list[1].EffHistory {
		t.Fatalf("channels: %+v", list)
	}

	// The owner's app makes key 1 and seals it for every member with a key.
	key1, _ := e2e.NewChannelKey()
	var boxes []proto.KeyBox
	for u, pub := range o.members(t) {
		b, _ := e2e.SealKey(pub, key1)
		boxes = append(boxes, proto.KeyBox{Channel: chID, Version: 1, To: u, Box: b})
	}
	if r := o.call(t, proto.Request{Type: "put_keys", Boxes: boxes}); !r.OK {
		t.Fatalf("put_keys: %+v", r)
	}
	if got := o.myKey(t, chID, 1); string(got) != string(key1) {
		t.Fatal("the owner should be able to open their own box")
	}

	// A member joins; the guild asks the owner to hand them the key.
	ben := newTestUser()
	joinAs(t, addr, ben, proto.Request{})
	b := connectChatter(t, addr, ben, "pc-b")
	if ev := o.waitEvent(t, "keys_needed"); ev.Channel != chID {
		t.Fatal("the owner should be asked to hand over the key")
	}
	st := o.call(t, proto.Request{Type: "key_states"}).Keys[0]
	if st.Current != 1 || len(st.Missing) != 1 || st.Missing[0] != b.key {
		t.Fatalf("ledger: %+v", st)
	}
	bb, _ := e2e.SealKey(o.members(t)[b.key], key1)
	if r := o.call(t, proto.Request{Type: "put_keys", Boxes: []proto.KeyBox{{Channel: chID, Version: 1, To: b.key, Box: bb}}}); !r.OK {
		t.Fatalf("hand-over: %+v", r)
	}
	b.waitEvent(t, "keys_changed")
	if string(b.myKey(t, chID, 1)) != string(key1) {
		t.Fatal("Ben should now hold key 1")
	}

	// Ben writes; the owner gets it live and can read it. The guild can't.
	if r := b.say(t, chID, 1, key1, "hello **world**"); !r.OK || r.Chat.ID == "" {
		t.Fatalf("send: %+v", r)
	}
	ev := o.waitEvent(t, "message")
	plain, err := e2e.Decrypt(key1, chID, 1, ev.Chat.Nonce, ev.Chat.Cipher)
	if err != nil || string(plain) != "hello **world**" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
	if !ed25519.Verify(ben.key.Public().(ed25519.PublicKey), ev.Chat.SignedPart(), ev.Chat.Sig) {
		t.Fatal("the author's signature must check out")
	}
	raw, _ := guildstore.LoadMessages(g.DataDir, chID)
	if len(raw) != 1 || strings.Contains(string(raw[0].Cipher), "hello") {
		t.Fatal("the guild must store only encrypted text")
	}

	// A forged message (signed by someone else) is refused.
	nonce, sealed, _ := e2e.Encrypt(key1, chID, 1, []byte("fake"))
	fake := proto.ChatMessage{Channel: chID, Author: b.key, Version: 1, Nonce: nonce, Cipher: sealed}
	fake.Sig = ed25519.Sign(owner.key, fake.SignedPart())
	if r := b.call(t, proto.Request{Type: "send", Msg: &fake}); r.OK {
		t.Fatal("a message with someone else's signature must be refused")
	}

	// Reply, react, edit and delete.
	first := ev.Chat.ID
	nonce, sealed, _ = e2e.Encrypt(key1, chID, 1, []byte("hi Ben"))
	reply := proto.ChatMessage{Channel: chID, Author: o.key, Version: 1, Nonce: nonce, Cipher: sealed, ReplyTo: first}
	reply.Sig = ed25519.Sign(owner.key, reply.SignedPart())
	if r := o.call(t, proto.Request{Type: "send", Msg: &reply}); !r.OK || r.Chat.ReplyTo != first {
		t.Fatalf("reply: %+v", r)
	}
	if r := o.call(t, proto.Request{Type: "react", ChannelID: chID, MessageID: first, Emoji: "👍", On: true}); !r.OK {
		t.Fatal(r.Error)
	}
	nonce, sealed, _ = e2e.Encrypt(key1, chID, 1, []byte("hello everyone"))
	ed := proto.ChatMessage{ID: first, Channel: chID, Author: b.key, Version: 1, Nonce: nonce, Cipher: sealed}
	ed.Sig = ed25519.Sign(ben.key, ed.SignedPart())
	if r := o.call(t, proto.Request{Type: "edit", Msg: &ed}); r.OK {
		t.Fatal("only the author may edit")
	}
	if r := b.call(t, proto.Request{Type: "edit", Msg: &ed}); !r.OK {
		t.Fatalf("edit: %+v", r)
	}
	h := b.call(t, proto.Request{Type: "history", ChannelID: chID})
	if len(h.Messages) != 2 || h.Messages[0].Edited == "" || len(h.Messages[0].Reactions["👍"]) != 1 {
		t.Fatalf("history: %+v", h.Messages)
	}
	if r := b.call(t, proto.Request{Type: "delete_message", ChannelID: chID, MessageID: h.Messages[1].ID}); r.OK {
		t.Fatal("a member must not delete someone else's message")
	}
	if r := o.call(t, proto.Request{Type: "delete_message", ChannelID: chID, MessageID: first}); !r.OK {
		t.Fatal("the owner may delete any message")
	}

	// Kicking Ben retires the key; the guild asks for a new one.
	o.call(t, proto.Request{Type: "kick", Target: b.key})
	if ev := o.waitEvent(t, "keys_needed"); ev.Channel != chID {
		t.Fatal("a new key should be asked for after a member leaves")
	}
	st = o.call(t, proto.Request{Type: "key_states"}).Keys[0]
	if !st.Retired {
		t.Fatalf("key should be retired: %+v", st)
	}
	key2, _ := e2e.NewChannelKey()
	b2, _ := e2e.SealKey(o.enc.PublicKey().Bytes(), key2)
	if r := o.call(t, proto.Request{Type: "put_keys", Boxes: []proto.KeyBox{{Channel: chID, Version: 2, To: o.key, Box: b2}}}); !r.OK {
		t.Fatalf("new key 2: %+v", r)
	}
	st = o.call(t, proto.Request{Type: "key_states"}).Keys[0]
	if st.Current != 2 || st.Retired {
		t.Fatalf("after re-key: %+v", st)
	}
	// Making key 2 again (a race) is refused.
	if r := o.call(t, proto.Request{Type: "put_keys", Boxes: []proto.KeyBox{{Channel: chID, Version: 3, To: o.key, Box: b2}}}); r.OK {
		t.Fatal("a new key is only allowed when one is needed")
	}

	// Messages survive a restart.
	g2, err := NewServer(g.DataDir, nil)
	if err != nil || len(g2.chat.msgs[chID]) != 2 || !g2.chat.msgs[chID][0].Deleted {
		t.Fatalf("after restart: %v %+v", err, g2.chat.msgs[chID])
	}
}

func TestChannelTreeRules(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	put := func(n proto.ChannelNode) proto.Response {
		return o.call(t, proto.Request{Type: "channel_put", Node: &n})
	}
	cat := put(proto.ChannelNode{Kind: proto.KindCategory, Name: "Cat"}).Message
	if r := put(proto.ChannelNode{Kind: proto.KindCategory, Name: "Inner", Parent: cat}); r.OK {
		t.Fatal("categories can't be nested")
	}
	if r := put(proto.ChannelNode{Kind: proto.KindFolder, Name: "Loose"}); r.OK {
		t.Fatal("a folder needs a category or folder")
	}
	f1 := put(proto.ChannelNode{Kind: proto.KindFolder, Name: "F1", Parent: cat}).Message
	f2 := put(proto.ChannelNode{Kind: proto.KindFolder, Name: "F2", Parent: f1}).Message
	f3 := put(proto.ChannelNode{Kind: proto.KindFolder, Name: "F3", Parent: f2}).Message
	if r := put(proto.ChannelNode{Kind: proto.KindFolder, Name: "F4", Parent: f3}); r.OK {
		t.Fatal("folders are at most 3 deep")
	}
	keep := 30
	no := false
	put(proto.ChannelNode{ID: cat, Kind: proto.KindCategory, Name: "Cat", KeepDays: &keep, History: &no})
	ch := put(proto.ChannelNode{Kind: proto.KindChannel, Name: "deep", Parent: f3}).Message
	for _, n := range o.call(t, proto.Request{Type: "channels"}).Channels {
		if n.ID == ch && (n.EffKeepDays != 30 || n.EffHistory) {
			t.Fatalf("settings should come from the category: %+v", n)
		}
	}
	// Deleting the category removes everything inside it.
	if r := o.call(t, proto.Request{Type: "channel_delete", ChannelID: cat}); !r.OK {
		t.Fatal(r.Error)
	}
	if n := o.call(t, proto.Request{Type: "channels"}).Channels; len(n) != 0 {
		t.Fatalf("left over: %+v", n)
	}
	// Members can't manage channels.
	u := newTestUser()
	joinAs(t, addr, u, proto.Request{})
	m := connectChatter(t, addr, u, "pc-u")
	if r := m.call(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "x"}}); r.OK {
		t.Fatal("only the owner manages channels")
	}
}

// TestChannelMove checks dragging a channel to a new place.
func TestChannelMove(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	put := func(n proto.ChannelNode) string {
		return o.call(t, proto.Request{Type: "channel_put", Node: &n}).Message
	}
	cat := put(proto.ChannelNode{Kind: proto.KindCategory, Name: "Cat"})
	a := put(proto.ChannelNode{Kind: proto.KindChannel, Name: "a", Parent: cat})
	b := put(proto.ChannelNode{Kind: proto.KindChannel, Name: "b", Parent: cat})
	c := put(proto.ChannelNode{Kind: proto.KindChannel, Name: "c", Parent: cat})
	order := func(parent string) []string {
		var out []string
		for _, n := range o.call(t, proto.Request{Type: "channels"}).Channels {
			if n.Parent == parent && n.Kind == proto.KindChannel {
				out = append(out, n.Name)
			}
		}
		return out
	}
	move := func(id, under string, place int) proto.Response {
		return o.call(t, proto.Request{Type: "channel_move", ChannelID: id, Under: under, Place: place})
	}
	if r := move(c, cat, 0); !r.OK {
		t.Fatal(r.Error)
	}
	if got := fmt.Sprint(order(cat)); got != "[c a b]" {
		t.Fatalf("c first: %s", got)
	}
	if r := move(c, cat, 99); !r.OK || fmt.Sprint(order(cat)) != "[a b c]" {
		t.Fatalf("c last: %s %s", r.Error, fmt.Sprint(order(cat)))
	}
	if r := move(a, "", 0); !r.OK || fmt.Sprint(order("")) != "[a]" || fmt.Sprint(order(cat)) != "[b c]" {
		t.Fatalf("a to the top: %s", r.Error)
	}
	if r := move(cat, a, 0); r.OK {
		t.Fatal("a category stays at the top")
	}
	if r := move(b, a, 0); r.OK {
		t.Fatal("nothing goes inside a channel")
	}
}
