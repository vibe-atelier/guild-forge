package guild

// This file has the guild's currency, wallets and Shop. Like the
// pages, the host can read these.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	currencyFile = "currency.json"
	balancesFile = "balances.json"
	ledgerFile   = "ledger.json"
	shopFile     = "shop.json"
	ownedFile    = "owned.json" // This file holds the role items each member bought.

	maxCoins       = 1_000_000_000_000 // This is the most one movement can be.
	maxBank        = 1_000_000_000_000_000
	maxLedger      = 20000 // This is how many movements are kept.
	maxWalletLines = 200
	maxShopItems   = 200
	maxIcon        = 256 << 10
)

type currencyState struct {
	settings proto.CurrencySettings
	balances map[string]int64
	ledger   []proto.LedgerEntry // The ledger is oldest first.
	shop     []proto.ShopItem
	owned    map[string][]string // It maps each member to the IDs of the role items they bought.
}

func (g *Server) loadCurrency() error {
	c := &g.currency
	c.balances = map[string]int64{}
	c.owned = map[string][]string{}
	for _, f := range []struct {
		name string
		v    any
	}{{currencyFile, &c.settings}, {balancesFile, &c.balances}, {ledgerFile, &c.ledger}, {shopFile, &c.shop}, {ownedFile, &c.owned}} {
		if err := guildstore.LoadPage(g.DataDir, f.name, f.v); err != nil {
			return err
		}
	}
	return nil
}

// saveCurrencyLocked writes what changed: settings and balances always,
// the ledger when moved is true.
func (g *Server) saveCurrencyLocked(moved bool) error {
	c := &g.currency
	if err := guildstore.SavePage(g.DataDir, currencyFile, c.settings); err != nil {
		return err
	}
	if err := guildstore.SavePage(g.DataDir, balancesFile, c.balances); err != nil {
		return err
	}
	if moved {
		if len(c.ledger) > maxLedger {
			c.ledger = append([]proto.LedgerEntry{}, c.ledger[len(c.ledger)-maxLedger:]...)
		}
		if err := guildstore.SavePage(g.DataDir, ledgerFile, c.ledger); err != nil {
			return err
		}
	}
	g.notifyLater(g.allConnsLocked(), "pages_changed:wallet")
	return nil
}

func (g *Server) recordLocked(e proto.LedgerEntry) {
	e.ID, e.Time = newID(), g.nowText()
	g.currency.ledger = append(g.currency.ledger, e)
}

// currencyView is what a member may see of the settings.
func (g *Server) currencyViewLocked(user string) *proto.CurrencySettings {
	s := g.currency.settings
	if !g.hasLocked(user, proto.PermManageCurrency) {
		s.Bank, s.Pool = 0, 0
	}
	return &s
}

// publicCurrencyLocked goes with the About information, when it is on.
func (g *Server) publicCurrencyLocked() *proto.CurrencySettings {
	s := g.currency.settings
	if !s.Enabled {
		return nil
	}
	s.Bank, s.Pool = 0, 0
	return &s
}

func checkAmount(n int64) error {
	if n < 1 || n > maxCoins {
		return errors.New("the amount must be a whole number from 1 to 1,000,000,000,000")
	}
	return nil
}

func (g *Server) needCurrencyLocked() error {
	if !g.currency.settings.Enabled {
		return errors.New("this guild has no currency")
	}
	return nil
}

// currencyUpdate changes the name, icon, and whether the currency and Shop
// are on (Manage Currency).
func (g *Server) currencyUpdate(user string, in *proto.CurrencySettings, icon []byte) error {
	if in == nil {
		return errors.New("missing settings")
	}
	name := strings.Join(strings.Fields(in.Name), " ")
	if utf8.RuneCountInString(name) > 24 {
		return errors.New("the currency name can be at most 24 characters")
	}
	if len(icon) > 0 && (len(icon) > maxIcon || !isPicture(icon)) {
		return errors.New("the icon must be PNG, JPG or GIF, up to 256 KB")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageCurrency); err != nil {
		return err
	}
	s := &g.currency.settings
	s.Enabled, s.Name, s.Shop = in.Enabled, name, in.Shop
	if len(icon) > 0 {
		h, err := guildstore.SaveBlob(g.DataDir, imagesDir, icon)
		if err != nil {
			return err
		}
		s.IconHash = h
	} else if in.IconHash == "" {
		s.IconHash = ""
	}
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	return g.saveCurrencyLocked(false)
}

// mint makes new coins into the bank (Manage Currency).
func (g *Server) mint(user string, n int64) error {
	if err := checkAmount(n); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageCurrency); err != nil {
		return err
	}
	if err := g.needCurrencyLocked(); err != nil {
		return err
	}
	s := &g.currency.settings
	if s.Bank+s.Pool+n > maxBank {
		return errors.New("that is more coins than a guild can have")
	}
	s.Bank += n
	g.recordLocked(proto.LedgerEntry{Kind: "mint", Amount: n, By: user})
	return g.saveCurrencyLocked(true)
}

// give pays a member from the bank (Manage Currency).
func (g *Server) give(user, target string, n int64) error {
	if err := checkAmount(n); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageCurrency); err != nil {
		return err
	}
	if err := g.needCurrencyLocked(); err != nil {
		return err
	}
	if _, ok := g.members[target]; !ok {
		return errors.New("there is no such member")
	}
	s := &g.currency.settings
	if s.Bank < n {
		return fmt.Errorf("the bank has only %d", s.Bank)
	}
	s.Bank -= n
	s.Pool += n
	g.currency.balances[target] += n
	g.recordLocked(proto.LedgerEntry{Kind: "give", To: target, Amount: n, By: user})
	return g.saveCurrencyLocked(true)
}

// sendCoins moves coins from one member to another.
func (g *Server) sendCoins(user, target string, n int64) error {
	if err := checkAmount(n); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needCurrencyLocked(); err != nil {
		return err
	}
	if target == user {
		return errors.New("you can't send coins to yourself")
	}
	if _, ok := g.members[target]; !ok {
		return errors.New("there is no such member")
	}
	b := g.currency.balances
	if b[user] < n {
		return fmt.Errorf("you have only %d", b[user])
	}
	b[user] -= n
	b[target] += n
	if b[user] == 0 {
		delete(b, user)
	}
	g.recordLocked(proto.LedgerEntry{Kind: "transfer", From: user, To: target, Amount: n})
	return g.saveCurrencyLocked(true)
}

// buy pays for a Shop item; the coins go back to the bank.
func (g *Server) buy(user, itemID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needCurrencyLocked(); err != nil {
		return err
	}
	if !g.currency.settings.Shop {
		return errors.New("the Shop is closed")
	}
	var item *proto.ShopItem
	for i := range g.currency.shop {
		if g.currency.shop[i].ID == itemID {
			item = &g.currency.shop[i]
		}
	}
	if item == nil {
		return errors.New("there is no such item")
	}
	if isAuction(item) {
		return errors.New("this item is auctioned: bid on it instead")
	}
	var role *proto.Role
	if item.Type == proto.ShopRole {
		var err error
		if role, err = g.buyableRoleLocked(user, item); err != nil {
			return err
		}
	}
	b := g.currency.balances
	if b[user] < item.Price {
		return fmt.Errorf("you have only %d", b[user])
	}
	b[user] -= item.Price
	if b[user] == 0 {
		delete(b, user)
	}
	s := &g.currency.settings
	s.Pool -= item.Price
	s.Bank += item.Price
	g.recordLocked(proto.LedgerEntry{Kind: "buy", From: user, Amount: item.Price, Item: item.Name})
	if role != nil { // The role is theirs to keep, and is worn straight away.
		g.currency.owned[user] = append(g.currency.owned[user], item.ID)
		if err := guildstore.SavePage(g.DataDir, ownedFile, g.currency.owned); err != nil {
			return err
		}
		g.wearLocked(user, role)
	}
	return g.saveCurrencyLocked(true)
}

// wallet is a member's balance and history.
func (g *Server) wallet(user string) (*proto.Wallet, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needCurrencyLocked(); err != nil {
		return nil, err
	}
	w := &proto.Wallet{Balance: g.currency.balances[user], History: []proto.LedgerEntry{}}
	for i := len(g.currency.ledger) - 1; i >= 0 && len(w.History) < maxWalletLines; i-- {
		if e := g.currency.ledger[i]; e.From == user || e.To == user {
			w.History = append(w.History, e)
		}
	}
	return w, nil
}

// allBalances is everyone's balance and the newest movements (Manage Currency).
func (g *Server) allBalances(user string) (map[string]int64, []proto.LedgerEntry, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageCurrency); err != nil {
		return nil, nil, err
	}
	out := make(map[string]int64, len(g.currency.balances))
	for k, v := range g.currency.balances {
		out[k] = v
	}
	var lines []proto.LedgerEntry
	for i := len(g.currency.ledger) - 1; i >= 0 && len(lines) < 500; i-- {
		lines = append(lines, g.currency.ledger[i])
	}
	return out, lines, nil
}

// shopPut adds or changes an item (Manage Currency).
func (g *Server) shopPut(user string, in *proto.ShopItem, image []byte) (string, error) {
	if in == nil {
		return "", errors.New("missing item")
	}
	name := strings.TrimSpace(in.Name)
	switch {
	case name == "" || utf8.RuneCountInString(name) > 64:
		return "", errors.New("an item needs a name of up to 64 characters")
	case utf8.RuneCountInString(in.Description) > 2000:
		return "", errors.New("the description can be at most 2000 characters")
	}
	if err := checkAmount(in.Price); err != nil {
		return "", errors.New("the price must be a whole number from 1 to 1,000,000,000,000")
	}
	if len(image) > 0 && (len(image) > maxImage || !isPicture(image)) {
		return "", errors.New("pictures must be PNG, JPG or GIF, up to 2 MB")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageCurrency); err != nil {
		return "", err
	}
	item := proto.ShopItem{ID: in.ID, Name: name, Description: strings.TrimSpace(in.Description), Price: in.Price, ImageHash: in.ImageHash, Type: in.Type, RoleID: in.RoleID,
		Sale: in.Sale, Ends: in.Ends}
	if err := g.checkShopItemLocked(user, &item); err != nil {
		return "", err
	}
	var old *proto.ShopItem
	if item.ID != "" {
		old = g.shopItemLocked(item.ID)
	}
	if err := g.checkAuctionLocked(&item, old); err != nil { // Auctions are checked too.
		return "", err
	}
	if len(image) > 0 {
		h, err := guildstore.SaveBlob(g.DataDir, imagesDir, image)
		if err != nil {
			return "", err
		}
		item.ImageHash = h
	}
	list := g.currency.shop
	if item.ID == "" {
		if len(list) >= maxShopItems {
			return "", fmt.Errorf("the Shop can have at most %d items", maxShopItems)
		}
		item.ID = newID()
		list = append(list, item)
	} else {
		found := false
		for i := range list {
			if list[i].ID == item.ID {
				list[i], found = item, true
			}
		}
		if !found {
			return "", errors.New("there is no such item")
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Price < list[j].Price })
	g.currency.shop = list
	g.notifyLater(g.allConnsLocked(), "pages_changed:shop")
	return item.ID, guildstore.SavePage(g.DataDir, shopFile, list)
}

func (g *Server) shopDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageCurrency); err != nil {
		return err
	}
	list := g.currency.shop
	for i := range list {
		if list[i].ID == id {
			g.refundAuctionLocked(&list[i]) // A held bid goes back.
			g.currency.shop = append(list[:i:i], list[i+1:]...)
			g.notifyLater(g.allConnsLocked(), "pages_changed:shop")
			return guildstore.SavePage(g.DataDir, shopFile, g.currency.shop)
		}
	}
	return errors.New("there is no such item")
}

// ---- requests -------------------------------------------------------------------------

func (g *Server) currencyRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "currency":
		g.mu.Lock()
		resp.Currency = g.currencyViewLocked(user)
		g.mu.Unlock()
	case "currency_update":
		err = g.currencyUpdate(user, req.Currency, req.Image)
	case "mint":
		err = g.mint(user, req.Amount)
	case "give":
		err = g.give(user, req.Target, req.Amount)
	case "transfer":
		err = g.sendCoins(user, req.Target, req.Amount)
	case "wallet":
		resp.Wallet, err = g.wallet(user)
	case "balances":
		resp.Balances, resp.Ledger, err = g.allBalances(user)
	case "shop":
		g.mu.Lock()
		if g.currency.settings.Enabled && g.currency.settings.Shop {
			resp.ShopItems = g.shopViewLocked(user)
		} else {
			err = errors.New("the Shop is closed")
		}
		g.mu.Unlock()
	case "shop_put":
		resp.Message, err = g.shopPut(user, req.ShopItem, req.Image)
	case "shop_delete":
		err = g.shopDelete(user, req.ItemID)
	case "buy":
		err = g.buy(user, req.ItemID)
	case "bid":
		err = g.bid(user, req.ItemID, req.Amount)
	case "equip", "unequip":
		err = g.equip(user, req.ItemID, req.Type == "equip")
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}
