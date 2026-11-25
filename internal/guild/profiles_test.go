package guild

import (
	"testing"
	"time"

	"nexus/internal/proto"
)

// TestProfilesAndNicknames tests members' profiles and nicknames.
func TestProfilesAndNicknames(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	ben := member(t, addr, "pc-b")
	cara := member(t, addr, "pc-c")

	// A picture, a banner and a bio, seen by everyone.
	ben.must(t, proto.Request{Type: "set_profile", Profile: &proto.MemberProfile{Bio: "I draw **sprites**"}, Picture: fakePNG, Banner: fakePNG})
	find := func(c *chatter, key string) proto.MemberInfo {
		for _, m := range c.must(t, proto.Request{Type: "members"}).Members {
			if m.UserKey == key {
				return m
			}
		}
		t.Fatal("member missing")
		return proto.MemberInfo{}
	}
	m := find(o, ben.key)
	if m.Bio != "I draw **sprites**" || m.PictureHash == "" || m.BannerHash != m.PictureHash {
		t.Fatalf("profile: %+v", m)
	}
	if d := o.must(t, proto.Request{Type: "image_get", Hash: m.PictureHash}).Data; string(d) != string(fakePNG) {
		t.Fatal("the picture should be served")
	}
	ben.refused(t, proto.Request{Type: "set_profile", Profile: &proto.MemberProfile{Bio: string(make([]byte, 191))}}, "bio too long")
	ben.refused(t, proto.Request{Type: "set_profile", Profile: &proto.MemberProfile{}, Picture: []byte("not a picture")}, "not a picture")
	ben.must(t, proto.Request{Type: "set_profile", Profile: &proto.MemberProfile{Bio: "", ClearBanner: true}})
	if m := find(o, ben.key); m.BannerHash != "" || m.PictureHash == "" {
		t.Fatalf("clearing the banner only: %+v", m)
	}

	// Everyone may change their own nickname; changing others' needs Manage Nicknames and a higher rank.
	ben.must(t, proto.Request{Type: "set_nick", Nick: strp("  Benny  ")})
	if m := find(cara, ben.key); m.Nick != "Benny" {
		t.Fatalf("nick: %+v", m)
	}
	cara.refused(t, proto.Request{Type: "set_nick", Target: ben.key, Nick: strp("Bob")}, "no Manage Nicknames")
	mod := makeRole(t, o, "Mod", 5, proto.PermManageNicknames)
	o.must(t, proto.Request{Type: "member_roles", Target: cara.key, Roles: []string{mod}})
	cara.must(t, proto.Request{Type: "set_nick", Target: ben.key, Nick: strp("Bob")})
	o.must(t, proto.Request{Type: "member_roles", Target: ben.key, Roles: []string{mod}})
	cara.refused(t, proto.Request{Type: "set_nick", Target: ben.key, Nick: strp("Bobby")}, "same rank")
	cara.refused(t, proto.Request{Type: "set_nick", Target: o.key, Nick: strp("Boss")}, "the owner")
	o.must(t, proto.Request{Type: "set_nick", Target: ben.key, Nick: strp("")})
	if m := find(o, ben.key); m.Nick != "" {
		t.Fatal("an empty nickname removes it")
	}
}

// TestSyncedSettings tests the settings each guild keeps for its members.
func TestSyncedSettings(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	ch := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "general"}}).Message

	o.must(t, proto.Request{Type: "settings_put", Settings: &proto.MemberSettings{
		Notify: map[string]string{ch: proto.NotifyAll, "guild": proto.NotifyMentions, "gone": proto.NotifyNothing},
		Reads:  map[string]string{ch: "m1"}, Collapsed: []string{"cat1"}}})
	o.refused(t, proto.Request{Type: "settings_put", Settings: &proto.MemberSettings{Notify: map[string]string{ch: "loud"}}}, "unknown choice")
	o.must(t, proto.Request{Type: "settings_put", Settings: &proto.MemberSettings{Reads: map[string]string{ch: "m2"}}})

	// Another computer gets them back.
	o.s.Close()
	time.Sleep(200 * time.Millisecond)
	o2 := connectChatter(t, addr, owner, "pc-o2")
	s := o2.must(t, proto.Request{Type: "settings_get"}).Settings
	if s == nil || s.Notify[ch] != proto.NotifyAll || s.Notify["guild"] != proto.NotifyMentions || s.Reads[ch] != "m2" ||
		len(s.Collapsed) != 1 || s.Notify["gone"] != "" {
		t.Fatalf("settings: %+v", s)
	}

	// Test the owner's default for the guild.
	o2.must(t, proto.Request{Type: "update_guild", DefaultNotify: strp(proto.NotifyAll)})
	if g := o2.must(t, proto.Request{Type: "about"}).Guild; g == nil || g.DefaultNotify != proto.NotifyAll {
		t.Fatalf("default: %+v", g)
	}
	o2.refused(t, proto.Request{Type: "update_guild", DefaultNotify: strp(proto.NotifyNothing)}, "not a default")
}
