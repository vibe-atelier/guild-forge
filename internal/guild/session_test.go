package guild

import (
	"bytes"
	"crypto/ed25519"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// signedIn connects as user from device and signs in.
func signedIn(t *testing.T, addr string, u testUser, device string, takeOver bool) (*proto.Session, proto.Response) {
	t.Helper()
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s := proto.NewSession(c)
	t.Cleanup(func() { s.Close() })
	r, err := s.Call(proto.Request{Type: "auth", UserKey: u.key.Public().(ed25519.PublicKey),
		Sig: ed25519.Sign(u.key, proto.AuthProof(s.SessionID(), device)), DeviceID: device, TakeOver: takeOver}, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}

// pairedGuild returns a running guild whose owner is the returned user.
func pairedGuild(t *testing.T) (*Server, string, testUser) {
	t.Helper()
	g, addr := startTestGuild(t)
	owner := newTestUser()
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g.StartBroadcast()
	otp, _, _ := g.NewOTP()
	if r := owner.pair(t, c, otp); !r.OK {
		t.Fatalf("pair: %+v", r)
	}
	return g, addr, owner
}

func addMember(g *Server, u testUser) string {
	key := guildstore.KeyText(u.key.Public().(ed25519.PublicKey))
	g.mu.Lock()
	g.members[key] = guildstore.Member{UserKey: key, Joined: time.Now()}
	g.saveMembersLocked()
	g.mu.Unlock()
	return key
}

func waitEvent(t *testing.T, s *proto.Session, want string) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events:
			if !ok {
				t.Fatalf("connection closed while waiting for %q", want)
			}
			if ev.Event == want {
				return
			}
		case <-timeout:
			t.Fatalf("no %q event", want)
		}
	}
}

func TestAuthAndOneComputerAtATime(t *testing.T) {
	_, addr, owner := pairedGuild(t)

	s1, r := signedIn(t, addr, owner, "pc-A", false)
	if !r.OK || !r.IsOwner {
		t.Fatalf("auth: %+v", r)
	}
	// A second computer is refused while the first is active.
	_, r = signedIn(t, addr, owner, "pc-B", false)
	if r.OK || r.Code != "elsewhere" {
		t.Fatalf("second computer: %+v", r)
	}
	// "Use here" moves the session; the first computer is told.
	s2, r := signedIn(t, addr, owner, "pc-B", true)
	if !r.OK {
		t.Fatalf("use here: %+v", r)
	}
	waitEvent(t, s1, "taken_over")
	if r, _ := s2.Call(proto.Request{Type: "ping"}, time.Second); !r.OK {
		t.Fatal("pc-B should be signed in")
	}
	// The same computer reconnecting just replaces its old connection.
	_, r = signedIn(t, addr, owner, "pc-B", false)
	if !r.OK {
		t.Fatalf("reconnect from the same computer: %+v", r)
	}

	// Strangers are not members until they join.
	_, r = signedIn(t, addr, newTestUser(), "pc-X", false)
	if r.OK || r.Code != "not_member" {
		t.Fatalf("stranger: %+v", r)
	}
	// A wrong signature is refused.
	c, _ := proto.Dial(addr, time.Second)
	s := proto.NewSession(c)
	defer s.Close()
	r, _ = s.Call(proto.Request{Type: "auth", UserKey: owner.key.Public().(ed25519.PublicKey), Sig: make([]byte, 64), DeviceID: "x"}, time.Second)
	if r.OK || r.Code != "bad_signature" {
		t.Fatalf("bad signature: %+v", r)
	}
	// Requests before signing in are refused, except public ones.
	if r, _ := s.Call(proto.Request{Type: "members"}, time.Second); r.OK || r.Code != "auth_required" {
		t.Fatalf("members before auth: %+v", r)
	}
	if r, _ := s.Call(proto.Request{Type: "about"}, time.Second); !r.OK || r.Guild.Name != guildstore.DefaultGuildName || r.Guild.MemberCount != 1 {
		t.Fatalf("about: %+v", r.Guild)
	}
}

func tinyPNG(t *testing.T, w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUpdateGuildOwnerOnly(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	member := newTestUser()
	addMember(g, member)

	so, _ := signedIn(t, addr, owner, "pc-A", false)
	sm, r := signedIn(t, addr, member, "pc-M", false)
	if !r.OK || r.IsOwner {
		t.Fatalf("member auth: %+v", r)
	}

	name, about := "  Pixel Club  ", "We make pixel art.\nEveryone welcome."
	show := false
	pic := tinyPNG(t, 64, 64)
	if r, _ := so.Call(proto.Request{Type: "update_guild", Name: &name, About: &about, Picture: pic, ShowMemberCount: &show}, time.Second); !r.OK {
		t.Fatalf("update: %+v", r)
	}
	waitEvent(t, sm, "guild_updated")
	r, _ = sm.Call(proto.Request{Type: "about"}, time.Second)
	gi := r.Guild
	if gi.Name != "Pixel Club" || gi.About != about || gi.MemberCount != -1 || len(gi.Picture) == 0 || gi.PictureHash == "" {
		t.Fatalf("about after update: %+v", gi)
	}
	// The picture is not sent again when the app already has it.
	r, _ = sm.Call(proto.Request{Type: "about", PictureHash: gi.PictureHash}, time.Second)
	if len(r.Guild.Picture) != 0 {
		t.Fatal("picture sent again")
	}
	// The settings are saved: a restarted guild has the same ones.
	g2, err := NewServer(g.DataDir, nil)
	if err != nil || g2.profile.Name != "Pixel Club" || len(g2.picture) == 0 {
		t.Fatalf("not saved: %+v %v", g2.profile, err)
	}

	// A member cannot change settings; bad input is refused.
	if r, _ := sm.Call(proto.Request{Type: "update_guild", Name: &name}, time.Second); r.OK {
		t.Fatal("a member must not change the settings")
	}
	empty := "  "
	if r, _ := so.Call(proto.Request{Type: "update_guild", Name: &empty}, time.Second); r.OK {
		t.Fatal("an empty name must be refused")
	}
	if r, _ := so.Call(proto.Request{Type: "update_guild", Picture: tinyPNG(t, 600, 600)}, time.Second); r.OK || !strings.Contains(r.Error, "512") {
		t.Fatalf("a too-big picture must be refused: %+v", r)
	}
	if r, _ := so.Call(proto.Request{Type: "update_guild", Picture: []byte("not a picture")}, time.Second); r.OK {
		t.Fatal("a non-picture must be refused")
	}
	if r, _ := so.Call(proto.Request{Type: "update_guild", ClearPicture: true}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	if r, _ := so.Call(proto.Request{Type: "about"}, time.Second); r.Guild.PictureHash != "" {
		t.Fatal("picture not removed")
	}
}

func TestTransferOwnership(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	member := newTestUser()
	mkey := addMember(g, member)
	so, _ := signedIn(t, addr, owner, "pc-A", false)
	sm, _ := signedIn(t, addr, member, "pc-M", false)

	// Only the owner can offer; the member must accept.
	if r, _ := sm.Call(proto.Request{Type: "transfer_offer", Target: mkey}, time.Second); r.OK {
		t.Fatal("a member must not offer ownership")
	}
	if r, _ := so.Call(proto.Request{Type: "transfer_offer", Target: "NOBODY"}, time.Second); r.OK {
		t.Fatal("offering to a non-member must fail")
	}
	if r, _ := so.Call(proto.Request{Type: "transfer_offer", Target: mkey}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	waitEvent(t, sm, "transfer_changed")
	if g.Status().OwnerKey == mkey {
		t.Fatal("ownership must not move before accepting")
	}
	// Decline, then offer again and accept.
	if r, _ := sm.Call(proto.Request{Type: "transfer_decline"}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	so.Call(proto.Request{Type: "transfer_offer", Target: mkey}, time.Second)
	if r, _ := sm.Call(proto.Request{Type: "transfer_accept"}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	waitEvent(t, so, "owner_changed")
	if g.Status().OwnerKey != mkey || guildstore.OwnerKey(g.DataDir) != mkey {
		t.Fatal("ownership did not move")
	}
	// The old owner is now a normal member.
	name := "Mine"
	if r, _ := so.Call(proto.Request{Type: "update_guild", Name: &name}, time.Second); r.OK {
		t.Fatal("the old owner must not change settings any more")
	}
}

func TestOwnerLeavesAndResetOwner(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	member := newTestUser()
	addMember(g, member)
	so, _ := signedIn(t, addr, owner, "pc-A", false)
	sm, _ := signedIn(t, addr, member, "pc-M", false)

	if r, _ := so.Call(proto.Request{Type: "leave"}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	waitEvent(t, sm, "owner_changed")
	if s := g.Status(); s.HasOwner || guildstore.OwnerKey(g.DataDir) != "" {
		t.Fatal("the guild should have no owner after the owner left")
	}
	if _, r := signedIn(t, addr, owner, "pc-A", false); r.Code != "not_member" {
		t.Fatalf("the old owner should no longer be a member: %+v", r)
	}
	// It can be paired again.
	if _, err := g.StartBroadcast(); err != nil {
		t.Fatal(err)
	}

	// reset_owner on a guild that has an owner again.
	g2, _, _ := pairedGuild(t)
	if err := g2.ResetOwner(); err != nil {
		t.Fatal(err)
	}
	if g2.Status().HasOwner || len(g2.members) != 1 {
		t.Fatal("reset_owner should remove the owner but keep members")
	}
}
