package guild

// This file handles roles bought in the Shop. A role marked Purchasable can be put in
// the Shop as a Role item. Members whose roles are on its "Accessible To"
// list (or everyone, when the list is empty) can buy it once; it is theirs
// to keep, and they can equip and unequip it. Roles listed as Mutually
// Exclusive can't be worn together: equipping one takes the other off. That
// lets a guild sell coloured names, for example.

import (
	"errors"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

func (g *Server) ownsLocked(user, itemID string) bool {
	for _, id := range g.currency.owned[user] {
		if id == itemID {
			return true
		}
	}
	return false
}

func (g *Server) hasRoleLocked(user, roleID string) bool {
	for _, id := range g.members[user].Roles {
		if id == roleID {
			return true
		}
	}
	return false
}

// mayBuyLocked reports whether the role's Accessible To list allows this member.
func (g *Server) mayBuyLocked(user string, r *proto.Role) bool {
	if len(r.BuyableBy) == 0 {
		return true
	}
	for _, id := range r.BuyableBy {
		if id == proto.EveryoneRole || g.hasRoleLocked(user, id) {
			return true
		}
	}
	return false
}

// buyableRoleLocked checks that a Role item can be bought by this member.
func (g *Server) buyableRoleLocked(user string, item *proto.ShopItem) (*proto.Role, error) {
	r := g.roleLocked(item.RoleID)
	switch {
	case r == nil || !r.Purchasable:
		return nil, errors.New("this role is not for sale any more")
	case g.ownsLocked(user, item.ID):
		return nil, errors.New("you already own this role. Equip it in the Shop")
	case !g.mayBuyLocked(user, r):
		return nil, errors.New("this role is not for sale to your roles")
	}
	return r, nil
}

// clashes reports whether two roles can't be worn together.
func clashes(a, b *proto.Role) bool {
	for _, id := range a.Exclusive {
		if id == b.ID {
			return true
		}
	}
	for _, id := range b.Exclusive {
		if id == a.ID {
			return true
		}
	}
	return false
}

// wearLocked gives a member a role, taking off the roles it clashes with.
func (g *Server) wearLocked(user string, r *proto.Role) {
	m, ok := g.members[user]
	if !ok {
		return
	}
	before := g.snapshotViewersLocked()
	var kept []string
	for _, id := range m.Roles {
		if o := g.roleLocked(id); o != nil && id != r.ID && clashes(r, o) {
			continue
		}
		if id != r.ID {
			kept = append(kept, id)
		}
	}
	want := map[string]bool{r.ID: true}
	for _, id := range kept {
		want[id] = true
	}
	m.Roles = nil
	for _, x := range g.roles { // Keep the guild's order.
		if want[x.ID] {
			m.Roles = append(m.Roles, x.ID)
		}
	}
	g.members[user] = m
	g.saveMembersLocked()
	g.afterAccessChangeLocked(before)
}

// equip wears (on) or takes off (off) a role the member bought.
func (g *Server) equip(user, itemID string, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needCurrencyLocked(); err != nil {
		return err
	}
	var item *proto.ShopItem
	for i := range g.currency.shop {
		if g.currency.shop[i].ID == itemID {
			item = &g.currency.shop[i]
		}
	}
	if item == nil || item.Type != proto.ShopRole {
		return errors.New("there is no such role in the Shop")
	}
	if !g.ownsLocked(user, itemID) {
		return errors.New("buy this role first")
	}
	r := g.roleLocked(item.RoleID)
	if r == nil {
		return errors.New("this role no longer exists")
	}
	if on {
		g.wearLocked(user, r)
		return nil
	}
	m := g.members[user]
	before := g.snapshotViewersLocked()
	m.Roles = without(m.Roles, r.ID)
	g.members[user] = m
	g.saveMembersLocked()
	g.afterAccessChangeLocked(before)
	return nil
}

// shopViewLocked is the Shop as one member sees it.
func (g *Server) shopViewLocked(user string) []proto.ShopItem {
	out := make([]proto.ShopItem, 0, len(g.currency.shop))
	for _, it := range g.currency.shop {
		if it.Type == proto.ShopRole {
			r := g.roleLocked(it.RoleID)
			if r == nil {
				continue
			}
			it.Owned = g.ownsLocked(user, it.ID)
			it.Equipped = it.Owned && g.hasRoleLocked(user, r.ID)
			it.Locked = !it.Owned && (!r.Purchasable || !g.mayBuyLocked(user, r))
		}
		out = append(out, it)
	}
	return out
}

// checkShopItemLocked checks an item's kind, and the role a Role item sells.
func (g *Server) checkShopItemLocked(user string, it *proto.ShopItem) error {
	switch it.Type {
	case "", proto.ShopOther:
		it.Type, it.RoleID = proto.ShopOther, ""
		return nil
	case proto.ShopRole:
	default:
		return errors.New("an item is a Role or Others")
	}
	r := g.roleLocked(it.RoleID)
	switch {
	case r == nil || r.ID == proto.EveryoneRole:
		return errors.New("choose the role this item gives")
	case !r.Purchasable:
		return errors.New("that role is not marked Purchasable. Tick it in the role's settings first")
	case r.Rank >= g.topRankLocked(user):
		return errors.New("you can only sell roles ranked below your own highest role")
	}
	for _, x := range g.currency.shop {
		if x.Type == proto.ShopRole && x.RoleID == r.ID && x.ID != it.ID {
			return errors.New("that role is already in the Shop")
		}
	}
	return nil
}

// cleanRoleLinksLocked checks a role's Accessible To and Mutually Exclusive
// lists: only roles that exist, never itself.
func (g *Server) cleanRoleLinksLocked(r *proto.Role) {
	keep := func(list []string, everyoneOK bool) []string {
		var out []string
		seen := map[string]bool{}
		for _, id := range list {
			if id == r.ID || seen[id] || g.roleLocked(id) == nil || (id == proto.EveryoneRole && !everyoneOK) {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		return out
	}
	r.BuyableBy = keep(r.BuyableBy, true)
	r.Exclusive = keep(r.Exclusive, false)
	if !r.Purchasable {
		r.BuyableBy = nil
	}
}

// forgetRoleLocked removes a deleted role from the other roles' lists and
// takes its item out of the Shop.
func (g *Server) forgetRoleLocked(id string) {
	for i := range g.roles {
		g.roles[i].BuyableBy = without(g.roles[i].BuyableBy, id)
		g.roles[i].Exclusive = without(g.roles[i].Exclusive, id)
	}
	kept := g.currency.shop[:0]
	changed := false
	for _, it := range g.currency.shop {
		if it.Type == proto.ShopRole && it.RoleID == id {
			changed = true
			continue
		}
		kept = append(kept, it)
	}
	g.currency.shop = kept
	if changed {
		guildstore.SavePage(g.DataDir, shopFile, g.currency.shop)
		g.notifyLater(g.allConnsLocked(), "pages_changed:shop")
	}
}
