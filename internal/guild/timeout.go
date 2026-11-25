package guild

// Timeout: a member ranked below you can be silenced for a while
// without being removed. Until it ends they can still read, but can't write
// messages, react, post or comment in Threads, edit the Wiki, change their
// nickname or use voice. It ends by itself.

import (
	"errors"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const maxTimeoutMinutes = 28 * 24 * 60 // This is four weeks.

// mutedPerms are what a timed-out member loses.
var mutedPerms = []string{proto.PermSendMessages, proto.PermAddReactions, proto.PermPostThreads, proto.PermEditWiki,
	proto.PermManageEvents, proto.PermChangeNickname, proto.PermConnect, proto.PermSpeak, proto.PermStream, proto.PermPrioritySpeaker, proto.PermUseSoundboard}

func (g *Server) timedOutLocked(user string) bool {
	m, ok := g.members[user]
	return ok && g.now().Before(m.Timeout)
}

// muteLocked takes away what a timed-out member may not do.
func (g *Server) muteLocked(user string, p perms) {
	if g.timedOutLocked(user) {
		p.remove(mutedPerms)
	}
	if g.newcomerLocked(user) {
		p.remove(newcomerPerms)
	}
}

// timeoutTextLocked is when a member's timeout ends, or "" when there is none.
func (g *Server) timeoutTextLocked(m guildstore.Member) string {
	if !g.now().Before(m.Timeout) {
		return ""
	}
	return m.Timeout.UTC().Format(time.RFC3339)
}

// timeout silences target for minutes (0 lifts it).
func (g *Server) timeout(user, target string, minutes int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermTimeout); err != nil {
		return err
	}
	m, ok := g.members[target]
	switch {
	case !ok:
		return errors.New("that person is not a member")
	case target == user:
		return errors.New("you can't time yourself out")
	case !g.outranksLocked(user, target):
		return errors.New("you can only time out people ranked below you")
	case minutes < 0 || minutes > maxTimeoutMinutes:
		return errors.New("a timeout can be at most four weeks")
	}
	if minutes == 0 {
		m.Timeout = time.Time{}
		g.Log.Printf("%s: timeout lifted", target)
	} else {
		m.Timeout = g.now().Add(time.Duration(minutes) * time.Minute).UTC().Truncate(time.Second)
		g.Log.Printf("%s timed out for %d minutes", target, minutes)
	}
	g.members[target] = m
	g.saveMembersLocked()
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	if minutes > 0 { // Tell everyone again when it ends, so apps update.
		time.AfterFunc(time.Duration(minutes)*time.Minute+time.Second, func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.notifyLater(g.allConnsLocked(), "guild_updated")
		})
	}
	return nil
}

// ---- the Waiting Period -------------------------
//
// New members can read straight away, but can't write messages, react or
// post in Threads until the guild's Waiting Period has passed since they
// joined. The owner, Administrators and anyone given a role (other than
// Everyone) don't wait.

var newcomerPerms = []string{proto.PermSendMessages, proto.PermAddReactions, proto.PermPostThreads}

// newcomerLocked reports whether user is still in the Waiting Period.
func (g *Server) newcomerLocked(user string) bool {
	wait := g.join.PostWaitMinutes
	m, ok := g.members[user]
	if wait <= 0 || !ok || g.isOwnerLocked(user) || len(without(m.Roles, proto.EveryoneRole)) > 0 {
		return false
	}
	if !g.now().Before(m.Joined.Add(time.Duration(wait) * time.Minute)) {
		return false
	}
	for _, r := range g.memberRolesLocked(user) {
		for _, p := range r.Perms {
			if p == proto.PermAdministrator {
				return false
			}
		}
	}
	return true
}

// waitEndsLocked tells a new member's app when their Waiting Period ends.
func (g *Server) waitEndsLocked(user string) {
	wait := g.join.PostWaitMinutes
	if wait <= 0 {
		return
	}
	time.AfterFunc(time.Duration(wait)*time.Minute+time.Second, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.notifyLater(g.connsOfLocked(user), "guild_updated")
	})
}
