package guild

import (
	"testing"

	"nexus/internal/proto"
)

// TestCurrency tests the guild currency, wallets and the Shop.
func TestCurrency(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	ben := member(t, addr, "pc-b")
	cara := member(t, addr, "pc-c")

	// Off until staff turn it on; only Manage Currency may.
	ben.refused(t, proto.Request{Type: "wallet"}, "no currency yet")
	ben.refused(t, proto.Request{Type: "currency_update", Currency: &proto.CurrencySettings{Enabled: true}}, "no Manage Currency")
	o.must(t, proto.Request{Type: "currency_update", Currency: &proto.CurrencySettings{Enabled: true, Name: "Pixels", Shop: true}, Image: fakePNG})
	if g := ben.must(t, proto.Request{Type: "about"}).Guild; g.Currency == nil || g.Currency.Name != "Pixels" || g.Currency.IconHash == "" {
		t.Fatalf("public currency: %+v", g.Currency)
	}

	// Make and give coins.
	ben.refused(t, proto.Request{Type: "mint", Amount: 100}, "no Manage Currency")
	o.refused(t, proto.Request{Type: "mint", Amount: 0}, "zero")
	o.must(t, proto.Request{Type: "mint", Amount: 1000})
	o.refused(t, proto.Request{Type: "give", Target: ben.key, Amount: 1001}, "more than the bank")
	o.must(t, proto.Request{Type: "give", Target: ben.key, Amount: 300})

	// Transfer coins between members.
	cara.refused(t, proto.Request{Type: "transfer", Target: ben.key, Amount: 1}, "Cara has nothing")
	ben.refused(t, proto.Request{Type: "transfer", Target: ben.key, Amount: 1}, "to yourself")
	ben.refused(t, proto.Request{Type: "transfer", Target: cara.key, Amount: -5}, "negative")
	ben.must(t, proto.Request{Type: "transfer", Target: cara.key, Amount: 50})
	if w := cara.must(t, proto.Request{Type: "wallet"}).Wallet; w.Balance != 50 || len(w.History) != 1 || w.History[0].From != ben.key {
		t.Fatalf("Cara's wallet: %+v", w)
	}

	// The Shop: coins go back to the bank.
	ben.refused(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Portrait", Price: 100}}, "no Manage Currency")
	item := o.must(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Portrait", Price: 100, Description: "A **portrait**"}}).Message
	cara.refused(t, proto.Request{Type: "buy", ItemID: item}, "Cara has 50")
	ben.must(t, proto.Request{Type: "buy", ItemID: item})
	if l := cara.must(t, proto.Request{Type: "shop"}).ShopItems; len(l) != 1 {
		t.Fatalf("shop: %+v", l)
	}

	// Balances are private; staff see all, and the bank and pool.
	ben.refused(t, proto.Request{Type: "balances"}, "not staff")
	if c := ben.must(t, proto.Request{Type: "currency"}).Currency; c.Bank != 0 || c.Pool != 0 {
		t.Fatal("members don't see the bank")
	}
	r := o.must(t, proto.Request{Type: "balances"})
	if r.Balances[ben.key] != 150 || r.Balances[cara.key] != 50 || len(r.Ledger) != 4 {
		t.Fatalf("balances: %+v, %d lines", r.Balances, len(r.Ledger))
	}
	if c := o.must(t, proto.Request{Type: "currency"}).Currency; c.Bank != 800 || c.Pool != 200 {
		t.Fatalf("bank %d pool %d, want 800 and 200", c.Bank, c.Pool)
	}

	// Close the Shop.
	o.must(t, proto.Request{Type: "currency_update", Currency: &proto.CurrencySettings{Enabled: true, Name: "Pixels"}})
	ben.refused(t, proto.Request{Type: "buy", ItemID: item}, "the Shop is closed")
}

// TestShopRoles checks roles bought in the Shop, equipped and swapped.
func TestShopRoles(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "sr-o")
	ben := member(t, addr, "sr-b")
	cara := member(t, addr, "sr-c")
	o.must(t, proto.Request{Type: "currency_update", Currency: &proto.CurrencySettings{Enabled: true, Shop: true}})
	o.must(t, proto.Request{Type: "mint", Amount: 1000})
	o.must(t, proto.Request{Type: "give", Target: ben.key, Amount: 300})
	o.must(t, proto.Request{Type: "give", Target: cara.key, Amount: 300})

	vip := o.must(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "VIP", Rank: 1}}).Message
	red := o.must(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "Red", Rank: 1, Color: "#ef4444"}}).Message
	o.refused(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Red Name", Price: 50, Type: proto.ShopRole, RoleID: red}}, "not Purchasable yet")
	o.must(t, proto.Request{Type: "role_put", Role: &proto.Role{ID: red, Name: "Red", Rank: 1, Color: "#ef4444", Purchasable: true, BuyableBy: []string{vip}}})
	blue := o.must(t, proto.Request{Type: "role_put", Role: &proto.Role{Name: "Blue", Rank: 1, Color: "#3b82f6", Purchasable: true, Exclusive: []string{red, "nonsense"}}}).Message
	redItem := o.must(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Red Name", Price: 50, Type: proto.ShopRole, RoleID: red}}).Message
	blueItem := o.must(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Blue Name", Price: 50, Type: proto.ShopRole, RoleID: blue}}).Message
	o.refused(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Again", Price: 5, Type: proto.ShopRole, RoleID: blue}}, "a role sold twice")
	o.refused(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Odd", Price: 5, Type: "car"}}, "an unknown kind")

	roles := func(c *chatter) map[string]bool {
		got := map[string]bool{}
		for _, m := range c.call(t, proto.Request{Type: "members"}).Members {
			if m.UserKey == c.key {
				for _, r := range m.Roles {
					got[r] = true
				}
			}
		}
		return got
	}
	item := func(c *chatter, id string) proto.ShopItem {
		for _, it := range c.must(t, proto.Request{Type: "shop"}).ShopItems {
			if it.ID == id {
				return it
			}
		}
		t.Fatalf("item %s missing", id)
		return proto.ShopItem{}
	}

	// Accessible To: Red is for VIPs only.
	if !item(ben, redItem).Locked {
		t.Fatal("Red should be locked for Ben")
	}
	ben.refused(t, proto.Request{Type: "buy", ItemID: redItem}, "not a VIP")
	o.must(t, proto.Request{Type: "member_roles", Target: ben.key, Roles: []string{vip}})
	ben.must(t, proto.Request{Type: "buy", ItemID: redItem})
	ben.refused(t, proto.Request{Type: "buy", ItemID: redItem}, "bought twice")
	if it := item(ben, redItem); !it.Owned || !it.Equipped || !roles(ben)[red] {
		t.Fatalf("Red after buying: %+v", it)
	}

	// Blue is exclusive with Red: equipping one takes the other off.
	ben.refused(t, proto.Request{Type: "equip", ItemID: blueItem}, "not bought")
	ben.must(t, proto.Request{Type: "buy", ItemID: blueItem})
	if r := roles(ben); !r[blue] || r[red] || !r[vip] {
		t.Fatalf("after Blue: %v", r)
	}
	ben.must(t, proto.Request{Type: "equip", ItemID: redItem})
	if r := roles(ben); r[blue] || !r[red] {
		t.Fatalf("after equipping Red: %v", r)
	}
	ben.must(t, proto.Request{Type: "unequip", ItemID: redItem})
	if it := item(ben, redItem); !it.Owned || it.Equipped || roles(ben)[red] {
		t.Fatalf("after unequipping Red: %+v", it)
	}
	if w := ben.must(t, proto.Request{Type: "wallet"}).Wallet; w.Balance != 200 {
		t.Fatalf("Ben paid twice: balance %d", w.Balance)
	}

	// Deleting a role takes its item out of the Shop.
	o.must(t, proto.Request{Type: "role_delete", RoleID: red})
	for _, it := range cara.must(t, proto.Request{Type: "shop"}).ShopItems {
		if it.ID == redItem {
			t.Fatal("Red's item is still in the Shop")
		}
	}
}
