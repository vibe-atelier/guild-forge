package guild

import (
	"testing"

	"nexus/internal/e2e"
	"nexus/internal/proto"
)

// member joins the guild and signs in.
func member(t *testing.T, addr, device string) *chatter {
	t.Helper()
	u := newTestUser()
	if _, r := joinAs(t, addr, u, proto.Request{}); r.Code != "joined" {
		t.Fatalf("join: %+v", r)
	}
	return connectChatter(t, addr, u, device)
}

func (c *chatter) must(t *testing.T, req proto.Request) proto.Response {
	t.Helper()
	r := c.call(t, req)
	if !r.OK {
		t.Fatalf("%s: %+v", req.Type, r)
	}
	return r
}

func (c *chatter) refused(t *testing.T, req proto.Request, why string) {
	t.Helper()
	if r := c.call(t, req); r.OK {
		t.Fatalf("%s should be refused: %s", req.Type, why)
	}
}

func makeRole(t *testing.T, o *chatter, name string, rank int, perms ...string) string {
	t.Helper()
	return o.must(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: name, Rank: rank, Perms: perms}}).Message
}

// TestParallelRanks checks roles of equal rank: Mod and Architect share a rank, so neither can
// act on the other; both can act on lower ranks.
func TestParallelRanks(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	mod, arch, ben := member(t, addr, "pc-m"), member(t, addr, "pc-a"), member(t, addr, "pc-b")

	modRole := makeRole(t, o, "Mod", 5, proto.PermKick, proto.PermBan, proto.PermManageRoles)
	archRole := makeRole(t, o, "Architect", 5, proto.PermManageChannels)
	helper := makeRole(t, o, "Helper", 2, proto.PermKick)
	o.must(t, proto.Request{Type: "member_roles", Target: mod.key, Roles: []string{modRole}})
	o.must(t, proto.Request{Type: "member_roles", Target: arch.key, Roles: []string{archRole}})
	o.must(t, proto.Request{Type: "member_roles", Target: ben.key, Roles: []string{helper}})

	arch.refused(t, proto.Request{Type: "kick", Target: ben.key}, "the Architect has no Kick")
	mod.refused(t, proto.Request{Type: "kick", Target: arch.key}, "same rank")
	mod.refused(t, proto.Request{Type: "member_roles", Target: arch.key, Roles: nil}, "same rank")
	ben.refused(t, proto.Request{Type: "kick", Target: mod.key}, "a lower rank can't kick a higher one")
	ben.refused(t, proto.Request{Type: "kick", Target: o.key}, "nobody can kick the owner")

	arch.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "plans"}})
	mod.refused(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "x"}}, "the Mod has no Manage Channels")
	arch.refused(t, proto.Request{Type: "update_guild", Name: strp("New")}, "no Manage Guild")

	// Roles can be made only below your own rank, and only with permissions you have.
	mod.refused(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "Peer", Rank: 5}}, "same rank as the Mod")
	mod.refused(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "Builder", Rank: 3, Perms: []string{proto.PermManageChannels}}}, "the Mod lacks that permission")
	trainee := mod.must(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "Trainee", Rank: 3, Perms: []string{proto.PermKick}}}).Message
	mod.refused(t, proto.Request{Type: "role_put", Role: &proto.Role{ID: archRole, Name: "Architect", Rank: 4}}, "can't change an equal role")

	// The Mod can give the lower role to Ben (Helper, rank 2) and kick him.
	mod.must(t, proto.Request{Type: "member_roles", Target: ben.key, Roles: []string{helper, trainee}})
	mod.must(t, proto.Request{Type: "kick", Target: ben.key})
	if r := o.call(t, proto.Request{Type: "roles"}); len(r.Roles) != 5 || r.Roles[len(r.Roles)-1].ID != proto.EveryoneRole {
		t.Fatalf("roles: %+v", r.Roles)
	}
	o.refused(t, proto.Request{Type: "role_delete", RoleID: proto.EveryoneRole}, "Everyone can't be deleted")
}

// TestHiddenChannel checks that a channel hidden from Everyone isn't listed,
// can't be read, and its key goes only to those who may read it. Losing
// access replaces the key.
func TestHiddenChannel(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	mod, ben := member(t, addr, "pc-m"), member(t, addr, "pc-b")
	modRole := makeRole(t, o, "Mod", 5)
	o.must(t, proto.Request{Type: "member_roles", Target: mod.key, Roles: []string{modRole}})

	staff := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindChannel, Name: "staff",
		Overrides: []proto.Override{
			{Role: proto.EveryoneRole, Deny: []string{proto.PermViewChannel}},
			{Role: modRole, Allow: []string{proto.PermViewChannel}},
		}}}).Message

	listed := func(c *chatter) bool {
		for _, n := range c.call(t, proto.Request{Type: "channels"}).Channels {
			if n.ID == staff {
				return true
			}
		}
		return false
	}
	if listed(ben) || !listed(mod) || !listed(o) {
		t.Fatal("staff should be listed for the owner and the Mod only")
	}
	ben.refused(t, proto.Request{Type: "history", ChannelID: staff}, "Ben can't read staff")
	mod.must(t, proto.Request{Type: "history", ChannelID: staff})

	st := func() proto.KeyState {
		for _, k := range o.call(t, proto.Request{Type: "key_states"}).Keys {
			if k.Channel == staff {
				return k
			}
		}
		t.Fatal("no key state")
		return proto.KeyState{}
	}
	if v := st().Viewers; len(v) != 2 || contains(v, ben.key) {
		t.Fatalf("viewers: %v", v)
	}

	// The owner's app makes key 1; a box for Ben is dropped by the guild.
	key, _ := e2e.NewChannelKey()
	var boxes []proto.KeyBox
	for u, pub := range o.members(t) {
		b, _ := e2e.SealKey(pub, key)
		boxes = append(boxes, proto.KeyBox{Channel: staff, Version: 1, To: u, Box: b})
	}
	o.must(t, proto.Request{Type: "put_keys", Boxes: boxes})
	for _, b := range ben.call(t, proto.Request{Type: "my_keys"}).Boxes {
		if b.Channel == staff {
			t.Fatal("Ben must not get the staff key")
		}
	}
	if mod.myKey(t, staff, 1) == nil {
		t.Fatal("the Mod should have the staff key")
	}

	// Taking the Mod role away replaces the key.
	o.must(t, proto.Request{Type: "member_roles", Target: mod.key, Roles: nil})
	if !st().Retired {
		t.Fatal("the key should be retired when someone loses access")
	}
	mod.refused(t, proto.Request{Type: "history", ChannelID: staff}, "no longer a Mod")

	// Rules on a channel need Manage Roles.
	mod.refused(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{ID: staff, Kind: proto.KindChannel, Name: "staff"}}, "no Manage Channels")
}

func strp(s string) *string { return &s }
