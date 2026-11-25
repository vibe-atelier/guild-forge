package guild

// This file runs auctions in the Shop. Staff put an item up with a starting
// bid and an end time. A bid is at least the starting bid, then any amount
// above the current one; it is taken from the bidder's wallet and held by
// the bank, and handed straight back when someone bids more. When the time
// is up the guild's clock ends it: the highest bidder has already paid, and
// a Role is theirs at once (other items are delivered by staff).

import (
	"errors"
	"fmt"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	minAuction = time.Minute
	maxAuction = 90 * 24 * time.Hour
)

func isAuction(it *proto.ShopItem) bool { return it.Sale == proto.SaleAuction }

// checkAuctionLocked checks a new or changed auction's sale details, keeping
// the bidding of one already under way.
func (g *Server) checkAuctionLocked(item *proto.ShopItem, old *proto.ShopItem) error {
	if item.Sale != proto.SaleAuction {
		item.Sale, item.Ends = "", ""
		if old != nil && isAuction(old) && old.Bidder != "" {
			return errors.New("this auction has bids; delete it to stop it")
		}
		return nil
	}
	// The auction is under way: only the words may change.
	if old != nil && isAuction(old) && (old.Bidder != "" || old.Ended) {
		item.Price, item.Ends, item.Bid, item.Bidder, item.Ended = old.Price, old.Ends, old.Bid, old.Bidder, old.Ended
		return nil
	}
	t, err := time.Parse(time.RFC3339, item.Ends)
	if err != nil {
		return errors.New("choose when the auction ends")
	}
	switch d := t.Sub(g.now()); {
	case d < minAuction:
		return errors.New("an auction must end in the future")
	case d > maxAuction:
		return errors.New("an auction can last at most 90 days")
	}
	item.Ends = t.UTC().Format(time.RFC3339)
	item.Bid, item.Bidder, item.Ended = 0, "", false
	return nil
}

// bid places a bid of amount on an auction.
func (g *Server) bid(user, itemID string, amount int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needCurrencyLocked(); err != nil {
		return err
	}
	if !g.currency.settings.Shop {
		return errors.New("the Shop is closed")
	}
	item := g.shopItemLocked(itemID)
	switch {
	case item == nil || !isAuction(item):
		return errors.New("there is no such auction")
	case item.Ended || g.auctionOverLocked(item):
		return errors.New("this auction has ended")
	case item.Type == proto.ShopRole && g.ownsLocked(user, item.ID):
		return errors.New("you already own it")
	}
	if item.Type == proto.ShopRole {
		if _, err := g.buyableRoleLocked(user, item); err != nil {
			return err
		}
	}
	if err := checkAmount(amount); err != nil {
		return errors.New("bid a whole number of coins")
	}
	switch {
	case item.Bidder == "" && amount < item.Price:
		return fmt.Errorf("the starting bid is %d", item.Price)
	case item.Bidder != "" && amount <= item.Bid:
		return fmt.Errorf("bid more than %d", item.Bid)
	}
	b := g.currency.balances
	have := b[user]
	if item.Bidder == user {
		have += item.Bid // When you raise your own bid, the coins already held count.
	}
	if have < amount {
		return fmt.Errorf("you have only %d", have)
	}
	s := &g.currency.settings
	if item.Bidder != "" { // The last bid goes straight back.
		b[item.Bidder] += item.Bid
		s.Pool += item.Bid
		s.Bank -= item.Bid
		g.recordLocked(proto.LedgerEntry{Kind: "refund", To: item.Bidder, Amount: item.Bid, Item: item.Name})
	}
	b[user] -= amount
	if b[user] == 0 {
		delete(b, user)
	}
	s.Pool -= amount
	s.Bank += amount
	g.recordLocked(proto.LedgerEntry{Kind: "bid", From: user, Amount: amount, Item: item.Name})
	item.Bid, item.Bidder = amount, user
	if err := guildstore.SavePage(g.DataDir, shopFile, g.currency.shop); err != nil {
		return err
	}
	g.notifyLater(g.allConnsLocked(), "pages_changed:shop")
	return g.saveCurrencyLocked(true)
}

func (g *Server) shopItemLocked(id string) *proto.ShopItem {
	for i := range g.currency.shop {
		if g.currency.shop[i].ID == id {
			return &g.currency.shop[i]
		}
	}
	return nil
}

func (g *Server) auctionOverLocked(item *proto.ShopItem) bool {
	t, err := time.Parse(time.RFC3339, item.Ends)
	return err == nil && !t.After(g.now())
}

// endAuctionsLocked ends auctions whose time is up (from the clock).
func (g *Server) endAuctionsLocked() {
	changed := false
	for i := range g.currency.shop {
		item := &g.currency.shop[i]
		if !isAuction(item) || item.Ended || !g.auctionOverLocked(item) {
			continue
		}
		item.Ended, changed = true, true
		if item.Bidder == "" {
			continue // The auction ends unsold.
		}
		if item.Type == proto.ShopRole {
			if r := g.roleLocked(item.RoleID); r != nil {
				g.currency.owned[item.Bidder] = append(g.currency.owned[item.Bidder], item.ID)
				guildstore.SavePage(g.DataDir, ownedFile, g.currency.owned)
				g.wearLocked(item.Bidder, r)
			}
		}
		g.Log.Printf("auction %q won for %d", item.Name, item.Bid)
	}
	if changed {
		guildstore.SavePage(g.DataDir, shopFile, g.currency.shop)
		g.saveCurrencyLocked(false)
		g.notifyLater(g.allConnsLocked(), "pages_changed:shop")
	}
}

// refundAuctionLocked hands a held bid back (the auction was deleted).
func (g *Server) refundAuctionLocked(item *proto.ShopItem) {
	if !isAuction(item) || item.Ended || item.Bidder == "" {
		return
	}
	g.currency.balances[item.Bidder] += item.Bid
	s := &g.currency.settings
	s.Pool += item.Bid
	s.Bank -= item.Bid
	g.recordLocked(proto.LedgerEntry{Kind: "refund", To: item.Bidder, Amount: item.Bid, Item: item.Name})
	g.saveCurrencyLocked(true)
}
