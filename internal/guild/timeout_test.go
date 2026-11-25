package guild

import (
	"testing"
	"time"

	"nexus/internal/proto"
)

// TestTimeout tests timing members out.
func TestTimeout(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "to-o")
	ben := member(t, addr, "to-b")
	cara := member(t, addr, "to-c")

	can := func(c *chatter, perm string) bool {
		return has(c.must(t, proto.Request{Type: "roles"}).MyPerms[""], perm)
	}
	if !can(ben, proto.PermSendMessages) {
		t.Fatal("Ben should be able to write before")
	}
	cara.refused(t, proto.Request{Type: "timeout", Target: ben.key, Minutes: 10}, "no Timeout permission")
	ben.refused(t, proto.Request{Type: "timeout", Target: o.key, Minutes: 10}, "the owner")
	o.refused(t, proto.Request{Type: "timeout", Target: ben.key, Minutes: 60 * 24 * 40}, "too long")
	o.must(t, proto.Request{Type: "timeout", Target: ben.key, Minutes: 10})

	if can(ben, proto.PermSendMessages) || can(ben, proto.PermAddReactions) || can(ben, proto.PermPostThreads) || !can(ben, proto.PermViewChannel) {
		t.Fatal("a timed-out member reads but can't write")
	}
	shown := false
	for _, m := range o.must(t, proto.Request{Type: "members"}).Members {
		if m.UserKey == ben.key && m.TimeoutUntil != "" {
			shown = true
		}
	}
	if !shown {
		t.Fatal("the member list should show the timeout")
	}

	// It ends by itself.
	g.mu.Lock()
	g.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	g.mu.Unlock()
	if !can(ben, proto.PermSendMessages) {
		t.Fatal("the timeout should have ended")
	}

	// Lift it early.
	g.mu.Lock()
	g.now = time.Now
	g.mu.Unlock()
	o.must(t, proto.Request{Type: "timeout", Target: ben.key, Minutes: 10})
	o.must(t, proto.Request{Type: "timeout", Target: ben.key, Minutes: 0})
	if !can(ben, proto.PermSendMessages) {
		t.Fatal("lifting should give writing back")
	}
}

func has(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}

// TestWaitingPeriod checks that new members wait before posting.
func TestWaitingPeriod(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "wp-o")
	js := o.must(t, proto.Request{Type: "join_settings"}).JoinSettings
	js.PostWaitMinutes = 30
	o.must(t, proto.Request{Type: "update_join", Join: js})
	ben := member(t, addr, "wp-b")

	can := func(c *chatter, perm string) bool {
		return has(c.must(t, proto.Request{Type: "roles"}).MyPerms[""], perm)
	}
	if !can(o, proto.PermSendMessages) {
		t.Fatal("the owner never waits")
	}
	if can(ben, proto.PermSendMessages) || can(ben, proto.PermAddReactions) || !can(ben, proto.PermViewChannel) {
		t.Fatal("a new member reads but can't write yet")
	}
	if w := ben.must(t, proto.Request{Type: "about"}).Guild.Join.PostWaitMinutes; w != 30 {
		t.Fatalf("the app should know the Waiting Period, got %d", w)
	}

	// A role skips the wait.
	role := o.must(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "Trusted", Rank: 1}}).Message
	o.must(t, proto.Request{Type: "member_roles", Target: ben.key, Roles: []string{role}})
	if !can(ben, proto.PermSendMessages) {
		t.Fatal("a member with a role doesn't wait")
	}
	o.must(t, proto.Request{Type: "member_roles", Target: ben.key, Roles: nil})

	// And it ends by itself.
	g.mu.Lock()
	g.now = func() time.Time { return time.Now().Add(31 * time.Minute) }
	g.mu.Unlock()
	if !can(ben, proto.PermSendMessages) {
		t.Fatal("the Waiting Period should have ended")
	}
}
