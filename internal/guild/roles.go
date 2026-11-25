package guild

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// This file handles roles and permissions.
//
// A member's guild-wide permissions are those of Everyone plus their roles. In
// a channel, each category, folder and channel on the way down can change that
// for a role: the closest rule wins. Ranks decide who may act on whom: only on
// people and roles ranked strictly below your highest role. The owner is above
// every rank and has every permission.

const (
	ownerRank = 1 << 30
	maxRoles  = 100 // A guild has at most 100 roles.
)

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type perms map[string]bool

func (p perms) add(list []string) {
	for _, x := range list {
		p[x] = true
	}
}

func (p perms) remove(list []string) {
	for _, x := range list {
		delete(p, x)
	}
}

func (p perms) list() []string {
	out := make([]string, 0, len(p))
	for x := range p {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

func (g *Server) loadRoles() error {
	r, err := guildstore.LoadRoles(g.DataDir)
	g.roles = r
	return err
}

func (g *Server) roleLocked(id string) *proto.Role {
	for i := range g.roles {
		if g.roles[i].ID == id {
			return &g.roles[i]
		}
	}
	return nil
}

// memberRolesLocked is Everyone plus the member's own roles.
func (g *Server) memberRolesLocked(user string) []*proto.Role {
	out := []*proto.Role{}
	if r := g.roleLocked(proto.EveryoneRole); r != nil {
		out = append(out, r)
	}
	for _, id := range g.members[user].Roles {
		if r := g.roleLocked(id); r != nil && id != proto.EveryoneRole {
			out = append(out, r)
		}
	}
	return out
}

func (g *Server) topRankLocked(user string) int {
	if g.isOwnerLocked(user) {
		return ownerRank
	}
	top := 0
	for _, r := range g.memberRolesLocked(user) {
		if r.Rank > top {
			top = r.Rank
		}
	}
	return top
}

// isAdminLocked reports whether user is the owner, or someone with Administrator.
func (g *Server) isAdminLocked(user string) bool {
	if g.isOwnerLocked(user) {
		return true
	}
	for _, r := range g.memberRolesLocked(user) {
		for _, p := range r.Perms {
			if p == proto.PermAdministrator {
				return true
			}
		}
	}
	return false
}

// guildPermsLocked is a member's guild-wide permissions.
func (g *Server) guildPermsLocked(user string) perms {
	p := perms{}
	if _, member := g.members[user]; !member && !g.isOwnerLocked(user) {
		return p
	}
	if g.isAdminLocked(user) {
		p.add(proto.GuildPerms)
		p.add(proto.ChannelPerms)
		g.muteLocked(user, p)
		return p
	}
	for _, r := range g.memberRolesLocked(user) {
		p.add(r.Perms)
	}
	g.muteLocked(user, p)
	return p
}

func (g *Server) hasLocked(user, perm string) bool { return g.guildPermsLocked(user)[perm] }

// chainLocked is the path from the top (category) down to node id.
func (g *Server) chainLocked(id string) []proto.ChannelNode {
	var chain []proto.ChannelNode
	for n := g.nodeLocked(id); n != nil && len(chain) < 8; {
		chain = append([]proto.ChannelNode{*n}, chain...)
		if n.Parent == "" {
			break
		}
		n = g.nodeLocked(n.Parent)
	}
	return chain
}

// channelPermsLocked is a member's permissions in a channel (or folder).
func (g *Server) channelPermsLocked(user, channel string) perms {
	p := g.guildPermsLocked(user)
	if len(p) == 0 || g.isAdminLocked(user) {
		return p
	}
	mine := map[string]bool{}
	for _, r := range g.memberRolesLocked(user) {
		mine[r.ID] = true
	}
	for _, n := range g.chainLocked(channel) {
		if n.InheritPerms {
			continue // The node uses the rules from above.
		}
		// Everyone's rule first, then the member's roles together.
		for _, o := range n.Overrides {
			if o.Role == proto.EveryoneRole {
				p.remove(o.Deny)
				p.add(o.Allow)
			}
		}
		var allow, deny []string
		for _, o := range n.Overrides {
			if o.Role != proto.EveryoneRole && mine[o.Role] {
				allow, deny = append(allow, o.Allow...), append(deny, o.Deny...)
			}
		}
		p.remove(deny)
		p.add(allow)
	}
	g.muteLocked(user, p)
	return p
}

func (g *Server) canLocked(user, channel, perm string) bool {
	return g.channelPermsLocked(user, channel)[perm]
}

// canSeeLocked reports whether user may read channel (its messages and its
// key).
func (g *Server) canSeeLocked(user, channel string) bool {
	return g.canLocked(user, channel, proto.PermViewChannel)
}

// viewersLocked lists the members who may read a channel.
func (g *Server) viewersLocked(channel string) []string {
	var out []string
	for u := range g.members {
		if g.canSeeLocked(u, channel) {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out
}

// outranksLocked reports whether actor may act on target.
func (g *Server) outranksLocked(actor, target string) bool {
	if g.isOwnerLocked(target) {
		return false
	}
	return g.topRankLocked(actor) > g.topRankLocked(target)
}

// needLocked returns an error unless user has perm guild-wide.
func (g *Server) needLocked(user, perm string) error {
	if g.hasLocked(user, perm) {
		return nil
	}
	return errors.New("you don't have permission to do that")
}

// ---- changing roles --------------------------------------------------------

// rolesView is what the "roles" request returns.
func (g *Server) rolesView(user string) ([]proto.Role, map[string][]string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	roles := append([]proto.Role{}, g.roles...)
	sort.SliceStable(roles, func(i, j int) bool { return roles[i].Rank > roles[j].Rank })
	my := map[string][]string{"": g.guildPermsLocked(user).list()}
	for _, n := range g.chat.nodes {
		my[n.ID] = g.channelPermsLocked(user, n.ID).list()
	}
	return roles, my
}

// snapshotViewersLocked remembers who can read each channel, so a change
// that takes someone's access away can replace the channel's key.
func (g *Server) snapshotViewersLocked() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, n := range g.chat.nodes {
		if proto.IsRoom(n.Kind) {
			set := map[string]bool{}
			for _, u := range g.viewersLocked(n.ID) {
				set[u] = true
			}
			out[n.ID] = set
		}
	}
	return out
}

// afterAccessChangeLocked replaces keys where someone lost access, asks for
// keys where someone gained it, and tells everyone the roles changed.
func (g *Server) afterAccessChangeLocked(before map[string]map[string]bool) {
	for ch, was := range before {
		now := map[string]bool{}
		for _, u := range g.viewersLocked(ch) {
			now[u] = true
		}
		for u := range was {
			if !now[u] {
				g.retireChannelKeyLocked(ch)
				break
			}
		}
	}
	g.requestKeysLocked()
	g.broadcastLocked("roles_changed")
}

func (g *Server) broadcastLocked(event string) {
	for _, s := range g.sessions {
		go s.conn.Send(proto.Response{Type: "event", OK: true, Event: event})
	}
}

func cleanPerms(in []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if !proto.IsPerm(p) {
			return nil, fmt.Errorf("unknown permission %q", p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

// rolePut creates or changes a role.
func (g *Server) rolePut(user string, in *proto.Role) (string, error) {
	if in == nil {
		return "", errors.New("no role sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageRoles); err != nil {
		return "", err
	}
	r := *in
	ps, err := cleanPerms(r.Perms)
	if err != nil {
		return "", err
	}
	r.Perms = ps
	// You can only give permissions you have yourself (unless Administrator).
	if !g.isAdminLocked(user) {
		mine := g.guildPermsLocked(user)
		for _, p := range r.Perms {
			if !mine[p] {
				return "", fmt.Errorf("you can't give a permission you don't have (%s)", p)
			}
		}
		for _, p := range r.Perms {
			if p == proto.PermAdministrator {
				return "", errors.New("only an Administrator can give Administrator")
			}
		}
	}
	if r.Color != "" && !colorRe.MatchString(r.Color) {
		return "", errors.New("the colour must look like #8b7bff")
	}
	top := g.topRankLocked(user)
	if r.ID == proto.EveryoneRole {
		old := g.roleLocked(proto.EveryoneRole)
		before := g.snapshotViewersLocked()
		old.Perms = r.Perms
		if err := guildstore.SaveRoles(g.DataDir, g.roles); err != nil {
			return "", err
		}
		g.afterAccessChangeLocked(before)
		return r.ID, nil
	}
	name := strings.TrimSpace(r.Name)
	if name == "" || utf8.RuneCountInString(name) > 32 {
		return "", errors.New("a role's name must be 1 to 32 characters")
	}
	r.Name = name
	g.cleanRoleLinksLocked(&r)
	if r.Rank < 1 {
		return "", errors.New("a role's rank must be 1 or more (Everyone is 0)")
	}
	if r.Rank >= top {
		return "", errors.New("you can only make roles ranked below your own highest role")
	}
	before := g.snapshotViewersLocked()
	if r.ID == "" {
		if len(g.roles) >= maxRoles {
			return "", errors.New("a guild can have at most 100 roles")
		}
		r.ID = newID()
		g.roles = append(g.roles, r)
	} else {
		old := g.roleLocked(r.ID)
		if old == nil {
			return "", errors.New("there is no such role")
		}
		if old.Rank >= top {
			return "", errors.New("you can only change roles ranked below your own highest role")
		}
		*old = r
	}
	if err := guildstore.SaveRoles(g.DataDir, g.roles); err != nil {
		return "", err
	}
	g.afterAccessChangeLocked(before)
	return r.ID, nil
}

// roleDelete removes a role from the guild and from everyone who had it.
func (g *Server) roleDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageRoles); err != nil {
		return err
	}
	if id == proto.EveryoneRole {
		return errors.New("the Everyone role can't be deleted")
	}
	r := g.roleLocked(id)
	if r == nil {
		return errors.New("there is no such role")
	}
	if r.Rank >= g.topRankLocked(user) {
		return errors.New("you can only delete roles ranked below your own highest role")
	}
	before := g.snapshotViewersLocked()
	kept := g.roles[:0]
	for _, x := range g.roles {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	g.roles = kept
	g.forgetRoleLocked(id)
	for k, m := range g.members {
		m.Roles = without(m.Roles, id)
		g.members[k] = m
	}
	for i := range g.chat.nodes {
		var o []proto.Override
		for _, x := range g.chat.nodes[i].Overrides {
			if x.Role != id {
				o = append(o, x)
			}
		}
		g.chat.nodes[i].Overrides = o
	}
	g.saveMembersLocked()
	guildstore.SaveChannels(g.DataDir, g.chat.nodes)
	if err := guildstore.SaveRoles(g.DataDir, g.roles); err != nil {
		return err
	}
	g.afterAccessChangeLocked(before)
	return nil
}

func without(list []string, x string) []string {
	var out []string
	for _, y := range list {
		if y != x {
			out = append(out, y)
		}
	}
	return out
}

// memberRoles gives target exactly the roles listed. Only roles ranked
// below the actor's highest can be added or taken away, and only for
// people ranked below them.
func (g *Server) memberRoles(user, target string, roles []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageRoles); err != nil {
		return err
	}
	m, ok := g.members[target]
	if !ok {
		return errors.New("they are not a member")
	}
	if !g.outranksLocked(user, target) {
		return errors.New("you can only change the roles of people ranked below you")
	}
	top := g.topRankLocked(user)
	want := map[string]bool{}
	for _, id := range roles {
		if id == proto.EveryoneRole {
			continue
		}
		if g.roleLocked(id) == nil {
			return errors.New("there is no such role")
		}
		want[id] = true
	}
	had := map[string]bool{}
	for _, id := range m.Roles {
		had[id] = true
	}
	for id := range want {
		if !had[id] && g.roleLocked(id).Rank >= top {
			return errors.New("you can only give roles ranked below your own highest role")
		}
	}
	for id := range had {
		if r := g.roleLocked(id); r != nil && !want[id] && r.Rank >= top {
			return errors.New("you can only take away roles ranked below your own highest role")
		}
	}
	before := g.snapshotViewersLocked()
	m.Roles = nil
	for _, r := range g.roles { // Keep the guild's order.
		if want[r.ID] {
			m.Roles = append(m.Roles, r.ID)
		}
	}
	g.members[target] = m
	g.saveMembersLocked()
	g.afterAccessChangeLocked(before)
	return nil
}

// rolesRequest handles the role requests. ok is false for other requests.
func (g *Server) rolesRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "roles":
		resp.Roles, resp.MyPerms = g.rolesView(user)
	case "role_put":
		resp.Message, err = g.rolePut(user, req.Role)
	case "role_delete":
		err = g.roleDelete(user, req.RoleID)
	case "member_roles":
		err = g.memberRoles(user, req.Target, req.Roles)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}
