package guild

import (
	"testing"
	"time"

	"nexus/internal/proto"
)

// TestAuction checks that bids are held and handed back, that the clock ends
// an auction, and that a deleted auction refunds its bid.
func TestAuction(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	clock := &clockAt{t: time.Now()}
	g.mu.Lock()
	g.now = clock.now
	g.mu.Unlock()
	o := connectChatter(t, addr, owner, "au-o")
	ben := member(t, addr, "au-b")
	cara := member(t, addr, "au-c")
	o.must(t, proto.Request{Type: "currency_update", Currency: &proto.CurrencySettings{Enabled: true, Name: "Pixels", Shop: true}})
	o.must(t, proto.Request{Type: "mint", Amount: 1000})
	o.must(t, proto.Request{Type: "give", Target: ben.key, Amount: 300})
	o.must(t, proto.Request{Type: "give", Target: cara.key, Amount: 300})
	balance := func(c *chatter) int64 { return c.must(t, proto.Request{Type: "wallet"}).Wallet.Balance }

	ends := clock.now().Add(time.Hour).UTC().Format(time.RFC3339)
	o.refused(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Bad", Price: 10, Sale: proto.SaleAuction}}, "needs an end")
	id := o.must(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Golden Sprite", Price: 50, Sale: proto.SaleAuction, Ends: ends}}).Message
	ben.refused(t, proto.Request{Type: "buy", ItemID: id}, "auctions are bid on")
	ben.refused(t, proto.Request{Type: "bid", ItemID: id, Amount: 40}, "below the starting bid")
	ben.must(t, proto.Request{Type: "bid", ItemID: id, Amount: 50})
	if balance(ben) != 250 {
		t.Fatalf("Ben's bid is held: %d", balance(ben))
	}
	cara.refused(t, proto.Request{Type: "bid", ItemID: id, Amount: 50}, "not more than the current bid")
	cara.must(t, proto.Request{Type: "bid", ItemID: id, Amount: 51}) // Any higher amount is accepted.
	if balance(ben) != 300 || balance(cara) != 249 {
		t.Fatalf("outbid: Ben %d, Cara %d", balance(ben), balance(cara))
	}
	ben.refused(t, proto.Request{Type: "bid", ItemID: id, Amount: 400}, "more than he has")
	ben.must(t, proto.Request{Type: "bid", ItemID: id, Amount: 120})
	item := func() proto.ShopItem {
		for _, it := range ben.must(t, proto.Request{Type: "shop"}).ShopItems {
			if it.ID == id {
				return it
			}
		}
		return proto.ShopItem{}
	}
	if it := item(); it.Bid != 120 || it.Bidder != ben.key {
		t.Fatalf("highest: %+v", it)
	}
	// Staff can change the words, not the bidding.
	o.must(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{ID: id, Name: "Golden Sprite", Description: "Shiny.", Price: 1, Sale: proto.SaleAuction, Ends: ends}})
	if it := item(); it.Price != 50 || it.Bid != 120 || it.Description != "Shiny." {
		t.Fatalf("after editing: %+v", it)
	}
	clock.add(time.Hour + time.Minute)
	g.tick()
	if it := item(); !it.Ended || it.Bidder != ben.key {
		t.Fatalf("ended: %+v", it)
	}
	cara.refused(t, proto.Request{Type: "bid", ItemID: id, Amount: 200}, "it has ended")
	if balance(ben) != 180 || balance(cara) != 300 {
		t.Fatalf("at the end: Ben %d, Cara %d", balance(ben), balance(cara))
	}

	// Deleting an auction hands the held bid back.
	ends = clock.now().Add(time.Hour).UTC().Format(time.RFC3339)
	id2 := o.must(t, proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Silver Sprite", Price: 10, Sale: proto.SaleAuction, Ends: ends}}).Message
	cara.must(t, proto.Request{Type: "bid", ItemID: id2, Amount: 30})
	o.must(t, proto.Request{Type: "shop_delete", ItemID: id2})
	if balance(cara) != 300 {
		t.Fatalf("refunded: %d", balance(cara))
	}
}
