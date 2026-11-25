package ui

// This file has the guild's currency, your Wallet, the Bank, the Shop and the
// /transfer and /balance commands.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
	"nexus/internal/proto"
)

var walletIcon = svg("wallet.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linejoin="round"><path d="M4 7.5A2.5 2.5 0 0 1 6.5 5H18v3"/><rect x="4" y="7.5" width="16.5" height="12" rx="2.5"/><circle cx="16" cy="13.5" r="1.3" fill="#8b8b96"/></g>
</svg>`)

var shopIcon = svg("shop.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
<g fill="none" stroke="#8b8b96" stroke-width="1.8" stroke-linejoin="round"><path d="M4 9.5 5.5 4.5h13L20 9.5"/><path d="M4 9.5c0 1.5 1.2 2.5 2.7 2.5S9.3 11 9.3 9.5c0 1.5 1.2 2.5 2.7 2.5s2.7-1 2.7-2.5c0 1.5 1.2 2.5 2.6 2.5S20 11 20 9.5"/><path d="M5.5 12v7.5h13V12M10 19.5v-4.5h4v4.5"/></g>
</svg>`)

// ---- showing amounts ---------------------------------------------------------------

func currencyName(g guildconn.GuildView) string {
	if g.Currency != nil && g.Currency.Name != "" {
		return g.Currency.Name
	}
	return "Coins"
}

// coins is an amount with its currency: "1,250 Pixels".
func coins(g guildconn.GuildView, n int64) string {
	return groupDigits(n) + " " + currencyName(g)
}

func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// parseAmount reads "1,250" or "1250".
func parseAmount(s string) (int64, error) {
	n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 10, 64)
	if err != nil || n < 1 {
		return 0, errors.New("type a whole number of at least 1")
	}
	return n, nil
}

// coinIcon is the currency's icon, or 🪙.
func (u *UI) coinIcon(g guildconn.GuildView, size float32) fyne.CanvasObject {
	if g.Currency != nil && g.Currency.IconHash != "" {
		if path := u.pictureFile(g.ID, g.Currency.IconHash); path != "" {
			if res := faceResource(path); res != nil {
				img := canvas.NewImageFromResource(res)
				img.FillMode = canvas.ImageFillContain
				img.SetMinSize(fyne.NewSize(size, size))
				return container.NewGridWrap(fyne.NewSize(size, size), img)
			}
		}
	}
	return text("🪙", size*0.8, colText, false)
}

// memberChoices are the other members, by the name shown in the guild.
func memberChoices(g guildconn.GuildView) (names []string, keys map[string]string) {
	keys = map[string]string{}
	for _, m := range g.Members {
		if m.IsMe {
			continue
		}
		n := shownName(m)
		if n == "" {
			n = shortID(m.UserKey)
		}
		if _, dup := keys[n]; dup {
			n += " (" + shortID(m.UserKey) + ")"
		}
		keys[n] = m.UserKey
		names = append(names, n)
	}
	sort.Strings(names)
	return names, keys
}

// ---- Wallet page ---------------------------------------------------------------------

func (u *UI) walletPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "wallet", proto.Request{Type: "wallet"}, func(r proto.Response) any { return r.Wallet })
	w, ok := data.(*proto.Wallet)
	tools := container.NewHBox(layout.NewSpacer())
	if can(g, proto.PermManageCurrency) {
		tools.Add(widget.NewButton("Bank", func() { u.openBank(g) }))
	}
	tools.Add(primary("Transfer", func() { u.openSendCoins(g, "", 0) }))
	if !ok || w == nil {
		return pageFrame(walletIcon, "Wallet", tools, loadingOr(data))
	}
	balance := text(groupDigits(w.Balance), 34, colText, true)
	head := card(24, container.NewVBox(
		text("YOUR BALANCE", 11.5, colFaint, true),
		container.NewHBox(container.NewCenter(u.coinIcon(g, 40)), container.NewCenter(balance), container.NewVBox(layout.NewSpacer(), text(currencyName(g), 16, colDim, false))),
		text("Only you, and people who manage the currency, can see this.", 12.5, colFaint, false),
	))
	list := container.NewVBox(text("HISTORY", 11.5, colFaint, true))
	if len(w.History) == 0 {
		list.Add(para("Nothing yet. Coins come from the guild's staff, or from other members."))
	}
	me := g.Me
	for _, e := range w.History {
		list.Add(u.ledgerRow(g, e, me))
	}
	return pageFrame(walletIcon, "Wallet", tools, head, gap(12), card(18, list))
}

// ledgerRow is one movement, as seen by me ("" me: as staff see it).
func (u *UI) ledgerRow(g guildconn.GuildView, e proto.LedgerEntry, me string) fyne.CanvasObject {
	var what string
	sign := ""
	switch e.Kind {
	case "mint":
		what = nameOf(g, e.By) + " made coins in the bank"
	case "give":
		what = "From the bank, given by " + nameOf(g, e.By)
		if me == "" {
			what = nameOf(g, e.By) + " gave " + nameOf(g, e.To)
		}
		sign = "+"
	case "transfer":
		switch me {
		case e.To:
			what, sign = "From "+nameOf(g, e.From), "+"
		case e.From:
			what, sign = "To "+nameOf(g, e.To), "−"
		default:
			what = nameOf(g, e.From) + " sent " + nameOf(g, e.To)
		}
	case "buy":
		what = "Bought " + e.Item
		if me == "" {
			what = nameOf(g, e.From) + " bought " + e.Item
		} else {
			sign = "−"
		}
	case "bid": // This is a bid.
		what = "Bid on " + e.Item
		if me == "" {
			what = nameOf(g, e.From) + " bid on " + e.Item
		} else {
			sign = "−"
		}
	case "refund":
		what = "Bid returned, " + e.Item
		if me == "" {
			what = "Bid returned to " + nameOf(g, e.To) + ", " + e.Item
		} else {
			sign = "+"
		}
	}
	amount := text(sign+groupDigits(e.Amount), 14.5, colText, true)
	if sign == "+" {
		amount.Color = colOnline
	}
	return container.NewBorder(nil, nil, nil, amount,
		container.NewVBox(text(what, 14, colText, false), text(msgTime(e.Time), 12, colFaint, false)))
}

// ---- transfers ----------------------------------------------------------------

// openSendCoins asks whom to pay and how much; target and amount may be
// filled in already (from /transfer or a member's window).
func (u *UI) openSendCoins(g guildconn.GuildView, target string, amount int64) {
	var p *popup
	names, keys := memberChoices(g)
	who := widget.NewSelect(names, nil)
	for n, k := range keys {
		if k == target {
			who.SetSelected(n)
		}
	}
	how := widget.NewEntry()
	how.SetPlaceHolder("50")
	if amount > 0 {
		how.SetText(strconv.FormatInt(amount, 10))
	}
	problem := newErrorText()
	next := primary("Send", func() {
		problem.Set(nil)
		key := keys[who.Selected]
		n, err := parseAmount(how.Text)
		if key == "" {
			err = errors.New("choose who to send them to")
		}
		if err != nil {
			problem.Set(err)
			return
		}
		p.Close()
		u.confirmTransfer(g, key, n)
	})
	p = u.openPopup(460, text("Transfer", 22, colText, true),
		para("Send "+currencyName(g)+" from your wallet to another member."),
		field("To", who, ""), field("Amount", how, ""), problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), next))
}

// confirmTransfer always asks before coins move.
func (u *UI) confirmTransfer(g guildconn.GuildView, key string, n int64) {
	u.confirm("Transfer", fmt.Sprintf("Send %s to %s?", coins(g, n), nameOf(g, key)), "Send", false, func() {
		u.call(g.ID, proto.Request{Type: "transfer", Target: key, Amount: n}, func(_ proto.Response, err error) {
			if err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.toast("Sent " + coins(g, n) + " to " + nameOf(g, key))
		})
	})
}

// ---- the Bank (Manage Currency) --------------------------------------------------------
//
// The Bank is a page of its own: what is in the bank and in
// circulation, making and giving coins, and the newest movements. Every
// member's balance has its own page, Balances.

func (u *UI) openBank(g guildconn.GuildView) {
	var p *popup
	stats := container.NewStack(para("Loading…"))
	recent := stack(4) // This is a column, so text lines up.
	problem := newErrorText()
	var load func()
	load = func() {
		u.call(g.ID, proto.Request{Type: "currency"}, func(r proto.Response, err error) {
			if err != nil || r.Currency == nil {
				problem.Set(err)
				return
			}
			stat := func(v int64, label string) fyne.CanvasObject {
				return card(18, container.NewVBox(text(label, 11.5, colFaint, true),
					u.coinAmount(g, v, 26)))
			}
			stats.Objects = []fyne.CanvasObject{container.NewGridWithColumns(2,
				stat(r.Currency.Bank, "BANK BALANCE"), stat(r.Currency.Pool, "IN CIRCULATION"))}
			stats.Refresh()
		})
		u.call(g.ID, proto.Request{Type: "balances"}, func(r proto.Response, err error) {
			if err != nil {
				problem.Set(err)
				return
			}
			recent.RemoveAll()
			recent.Add(text("RECENT", 11.5, colFaint, true))
			if len(r.Ledger) == 0 {
				recent.Add(para("Nothing yet."))
			}
			for i, e := range r.Ledger {
				if i == 50 {
					break
				}
				recent.Add(u.ledgerRow(g, e, ""))
			}
			recent.Refresh()
		})
	}
	load()
	makeAmt := widget.NewEntry()
	makeAmt.SetPlaceHolder("1000")
	act := func(req func() (proto.Request, error), done string) func() {
		return func() {
			problem.Set(nil)
			r, err := req()
			if err != nil {
				problem.Set(err)
				return
			}
			u.call(g.ID, r, func(_ proto.Response, err error) {
				if err != nil {
					problem.Set(err)
					return
				}
				u.toast(done)
				load()
			})
		}
	}
	makeBtn := widget.NewButton("Make Coins", act(func() (proto.Request, error) {
		n, err := parseAmount(makeAmt.Text)
		return proto.Request{Type: "mint", Amount: n}, err
	}, "Coins made"))
	names, keys := memberChoices(g)
	who := widget.NewSelect(names, nil)
	who.PlaceHolder = "Member"
	giveAmt := widget.NewEntry()
	giveAmt.SetPlaceHolder("100")
	giveBtn := widget.NewButton("Give Coins", act(func() (proto.Request, error) {
		n, err := parseAmount(giveAmt.Text)
		if err == nil && keys[who.Selected] == "" {
			err = errors.New("choose a member")
		}
		return proto.Request{Type: "give", Target: keys[who.Selected], Amount: n}, err
	}, "Given"))
	p = u.openEditor(walletIcon, "Wallet", panelTitle("Bank"),
		para("New coins go into the bank. Giving moves them from the bank to a member, and buying in the Shop puts them back."),
		stats,
		field("Make Coins", container.NewBorder(nil, nil, nil, makeBtn, makeAmt), "How many new coins to put in the bank."),
		field("Give Coins", container.NewBorder(nil, nil, nil, giveBtn, container.NewGridWithColumns(2, who, giveAmt)), "From the bank to a member."),
		problem.box,
		card(18, recent))
	_ = p
}

// ---- Balances (Manage Currency) ------------------------------------------------

const balancesPerPage = 50

func (u *UI) balancesPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "balances", proto.Request{Type: "balances"}, func(r proto.Response) any { return r.Balances })
	all, ok := data.(map[string]int64)
	if !ok {
		return pageFrame(walletIcon, "Balances", loadingOr(data))
	}
	type row struct {
		name, key string
		n         int64
	}
	var rows []row
	var total int64
	for k, v := range all {
		rows = append(rows, row{nameOf(g, k), k, v})
		total += v
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return strings.ToLower(rows[i].name) < strings.ToLower(rows[j].name)
	})

	search := widget.NewEntry()
	search.SetPlaceHolder("Search Members")
	list := stack(4) // This is a column, so text lines up.
	count := text("", 12.5, colFaint, false)
	shown := balancesPerPage
	more := widget.NewButton("Show More", nil)
	var fill func()
	fill = func() {
		q := strings.ToLower(strings.TrimSpace(search.Text))
		list.RemoveAll()
		matched := 0
		for i, r := range rows {
			if q != "" && !strings.Contains(strings.ToLower(r.name), q) && !strings.Contains(strings.ToLower(r.key), q) {
				continue
			}
			matched++
			if matched > shown {
				continue
			}
			if list.Objects != nil && len(list.Objects) > 0 {
				list.Add(sideLineH())
			}
			list.Add(container.New(layout.NewCustomPaddedLayout(8, 8, 0, 0), container.NewBorder(nil, nil,
				container.NewHBox(container.New(fixedWidth{44}, text(fmt.Sprintf("%d.", i+1), 13, colFaint, false)), avatar(r.name, r.key, 28), gap(2)),
				text(groupDigits(r.n), 14.5, colText, true),
				container.NewVBox(layout.NewSpacer(), text(r.name, 14.5, colText, false), layout.NewSpacer()))))
		}
		if matched == 0 {
			list.Add(para("Nobody matches."))
			if len(rows) == 0 {
				list.Objects[0] = para("Nobody has any yet.")
			}
		}
		count.Text = fmt.Sprintf("Showing %s of %s.", groupDigits(int64(min(shown, matched))), groupDigits(int64(matched)))
		count.Refresh()
		if matched > shown {
			more.Show()
		} else {
			more.Hide()
		}
		list.Refresh()
	}
	more.OnTapped = func() { shown += balancesPerPage; fill() }
	search.OnChanged = func(string) { shown = balancesPerPage; fill() }
	fill()
	head := card(18, container.NewVBox(text("IN CIRCULATION", 11.5, colFaint, true),
		u.coinAmount(g, total, 26)))
	return pageFrame(walletIcon, "Balances", head, search, card(18, container.NewVBox(list)), container.NewBorder(nil, nil, nil, more, count))
}

// ---- the Currency window (Manage Currency) ----------------------------------------------

func (u *UI) openCurrency(g guildconn.GuildView) {
	var p *popup
	cur := proto.CurrencySettings{}
	if g.Currency != nil {
		cur = *g.Currency
	}
	on, onBox := option("Enable Currency", "Members get a Wallet and can send each other coins.", cur.Enabled)
	shop, shopBox := option("Enable Shop", "A Shop page where members spend coins on items you add.", cur.Shop)
	name := widget.NewEntry()
	name.SetText(cur.Name)
	name.SetPlaceHolder("Coins")
	problem := newErrorText()
	var newIcon []byte
	clearIcon := false
	iconBox := container.NewStack(u.coinIcon(g, 40))
	pick := widget.NewButton("Change Icon", func() {
		go func() {
			path, err := choosePicture("Choose an icon for the currency")
			var data []byte
			if err == nil && path != "" {
				data, err = os.ReadFile(path)
			}
			if err == nil && data != nil {
				data, err = imaging.FitPicture(data, 128, 250<<10)
			}
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
					return
				}
				if data != nil {
					newIcon, clearIcon = data, false
					iconBox.Objects = []fyne.CanvasObject{pictureFromBytes(fmt.Sprintf("icon-%d", len(data)), data, 40)}
					iconBox.Refresh()
				}
			})
		}()
	})
	remove := widget.NewButton("Remove", func() {
		newIcon, clearIcon = nil, true
		iconBox.Objects = []fyne.CanvasObject{text("🪙", 32, colText, false)}
		iconBox.Refresh()
	})
	save := primary("Save", func() {
		s := proto.CurrencySettings{Enabled: on.Checked, Name: name.Text, Shop: shop.Checked, IconHash: cur.IconHash}
		if clearIcon {
			s.IconHash = ""
		}
		u.call(g.ID, proto.Request{Type: "currency_update", Currency: &s, Image: newIcon}, func(_ proto.Response, err error) {
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Saved")
		})
	})
	p = u.openPanel("currency", 540, text("Currency", 22, colText, true),
		para("A currency for this guild only. It isn't real money."),
		onBox,
		field("Name", name, "Like Coins, Pixels or Gems."),
		container.NewHBox(iconBox, gap(1), container.NewVBox(layout.NewSpacer(), container.NewHBox(pick, remove), layout.NewSpacer())),
		shopBox, problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
}

// ---- the Shop -------------------------------------------------------------------

func (u *UI) shopPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "shop", proto.Request{Type: "shop"}, func(r proto.Response) any { return r.ShopItems })
	items, ok := data.([]proto.ShopItem)
	tools := container.NewHBox(layout.NewSpacer())
	staff := can(g, proto.PermManageCurrency)
	if staff {
		tools.Add(primary("New Item", func() { u.openShopItem(g, nil) }))
	}
	if !ok {
		return pageFrame(shopIcon, "Shop", tools, loadingOr(data))
	}
	parts := []fyne.CanvasObject{tools}
	if len(items) == 0 {
		msg := "Nothing for sale yet."
		if staff {
			msg = "Nothing for sale yet. Click New Item to add something."
		}
		parts = append(parts, para(msg))
	}
	call := func(req proto.Request, done string) {
		u.call(g.ID, req, func(_ proto.Response, err error) {
			if err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.toast(done)
		})
	}
	for _, it := range items {
		it := it
		var pic fyne.CanvasObject = gap(1)
		if it.ImageHash != "" {
			if path := u.pictureFile(g.ID, it.ImageHash); path != "" {
				if res := faceResource(path); res != nil {
					img := canvas.NewImageFromResource(res)
					img.FillMode = canvas.ImageFillContain
					img.CornerRadius = 8
					img.SetMinSize(fyne.NewSize(96, 96))
					pic = img
				}
			}
		}
		// The member can buy it, or equip a role they own.
		var act *widget.Button
		switch {
		case it.Equipped:
			act = widget.NewButton("Unequip", func() { call(proto.Request{Type: "unequip", ItemID: it.ID}, "Unequipped "+it.Name) })
		case it.Owned:
			act = primary("Equip", func() { call(proto.Request{Type: "equip", ItemID: it.ID}, "Equipped "+it.Name) })
		case it.Sale == proto.SaleAuction && it.Ended:
			act = nil // The auction is over.
		case it.Sale == proto.SaleAuction:
			act = primary("Bid", func() { u.openBid(g, it) }) // This is an auction.
			if it.Locked {
				act.Disable()
			}
		default:
			act = primary("Buy", func() {
				u.confirm("Buy", fmt.Sprintf("Buy %s for %s?", it.Name, coins(g, it.Price)), "Buy", false, func() {
					call(proto.Request{Type: "buy", ItemID: it.ID}, "Bought "+it.Name)
				})
			})
			if it.Locked {
				act.Disable()
			}
		}
		shown := it.Price
		if it.Sale == proto.SaleAuction && it.Bidder != "" {
			shown = it.Bid // The highest bid goes where the price would.
		}
		price := container.NewHBox(container.NewCenter(u.coinIcon(g, 20)), container.NewCenter(text(groupDigits(shown), 16, colText, true)))
		if it.Owned {
			price = container.NewHBox(text("Owned", 14, colOnline, true))
		}
		if it.Sale == proto.SaleAuction && it.Ended {
			price = container.NewHBox(u.auctionResult(g, it))
		}
		right := container.NewVBox(container.NewHBox(layout.NewSpacer(), price))
		if act != nil {
			right.Add(container.NewHBox(layout.NewSpacer(), act))
		}
		if staff {
			edit := widget.NewButton("Edit", func() { u.openShopItem(g, &it) })
			del := widget.NewButton("Delete", func() {
				u.confirm("Delete Item", "Delete \""+it.Name+"\" from the Shop?", "Delete", true, func() {
					call(proto.Request{Type: "shop_delete", ItemID: it.ID}, "Item deleted")
				})
			})
			del.Importance = widget.DangerImportance
			right.Add(container.NewHBox(layout.NewSpacer(), edit, del))
		}
		kind := "Item"
		if it.Type == proto.ShopRole {
			kind = "Role"
		}
		meta := kind
		if staff {
			meta += " · ID " + strings.ToUpper(it.ID)
		}
		body := container.NewVBox(text(it.Name, 17, colText, true), text(meta, 12, colFaint, false))
		if it.Description != "" {
			body.Add(u.guildMarkdown(g, it.Description))
		}
		if it.Sale == proto.SaleAuction {
			body.Add(auctionLines(g, it))
		}
		if it.Locked {
			body.Add(text("Not for sale to your roles.", 12.5, colFaint, false))
		}
		parts = append(parts, card(18, container.NewBorder(nil, nil, container.NewHBox(pic, gap(1)), right, body)))
	}
	parts = append(parts, text("Roles are yours as soon as you buy them. Staff deliver other items.", 12.5, colFaint, false))
	return pageFrame(shopIcon, "Shop", parts...)
}

// shopTypes are the kinds of item.
var shopTypes = []string{"Role", "Others"}

func (u *UI) openShopItem(g guildconn.GuildView, it *proto.ShopItem) {
	var p *popup
	item := proto.ShopItem{}
	if it != nil {
		item = *it
	}
	name := widget.NewEntry()
	name.SetText(item.Name)
	price := widget.NewEntry()
	price.SetPlaceHolder("100")
	if item.Price > 0 {
		price.SetText(strconv.FormatInt(item.Price, 10))
	}
	desc := widget.NewMultiLineEntry()
	desc.Wrapping = fyne.TextWrapWord
	desc.SetMinRowsVisible(6)
	desc.SetText(item.Description)
	problem := newErrorText()

	// The Type is a role the buyer gets straight away, or something staff deliver.
	var roleNames, roleIDs []string
	for _, r := range g.Roles {
		if r.Purchasable && r.ID != proto.EveryoneRole {
			roleNames = append(roleNames, r.Name)
			roleIDs = append(roleIDs, r.ID)
		}
	}
	role := widget.NewSelect(roleNames, func(s string) {
		if strings.TrimSpace(name.Text) == "" {
			name.SetText(s)
		}
	})
	role.PlaceHolder = "Choose Role"
	for i, id := range roleIDs {
		if id == item.RoleID {
			role.SetSelectedIndex(i)
		}
	}
	roleHint := "Only roles marked Purchasable are listed."
	if len(roleNames) == 0 {
		roleHint = "No role is marked Purchasable yet. Tick Purchasable in a role's settings first."
	}
	roleField := field("Role", role, roleHint)
	kind := widget.NewSelect(shopTypes, func(s string) {
		if s == "Role" {
			roleField.Show()
		} else {
			roleField.Hide()
		}
	})
	if item.Type == proto.ShopRole {
		kind.SetSelected("Role")
	} else {
		kind.SetSelected("Others")
	}

	var newPic []byte
	picNote := text("No picture.", 12.5, colFaint, false)
	if item.ImageHash != "" {
		picNote.Text = "Has a picture."
	}
	pick := widget.NewButton("Choose Picture", func() {
		go func() {
			path, err := choosePicture("Choose a picture for the item")
			var data []byte
			if err == nil && path != "" {
				data, err = os.ReadFile(path)
			}
			if err == nil && data != nil {
				data, err = imaging.FitPicture(data, 512, 1800<<10)
			}
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
					return
				}
				if data != nil {
					newPic = data
					picNote.Text = "New picture chosen."
					picNote.Refresh()
				}
			})
		}()
	})
	// Sale Type: sold at a price, or auctioned from a starting bid
	// until a chosen time.
	endAt := time.Now().AddDate(0, 0, 1)
	endAt = time.Date(endAt.Year(), endAt.Month(), endAt.Day(), 18, 0, 0, 0, time.Local)
	if t, err := time.Parse(time.RFC3339, item.Ends); err == nil {
		endAt = t.Local()
	}
	endDate, endTime := widget.NewEntry(), widget.NewEntry()
	endDate.SetText(endAt.Format("2006-01-02"))
	endTime.SetText(endAt.Format("15:04"))
	ends := container.NewGridWithColumns(2, field("Auction Ends", endDate, "YYYY-MM-DD."), field("Time", endTime, "HH:MM, your own time."))
	priceBox := container.NewStack()
	sale := widget.NewSelect(saleTypes, nil)
	sale.OnChanged = func(s string) {
		label, hint := "Price", "In "+currencyName(g)+"."
		if s == "Auction" {
			label, hint = "Starting Bid", "The lowest first bid, in "+currencyName(g)+"."
			ends.Show()
		} else {
			ends.Hide()
		}
		priceBox.Objects = []fyne.CanvasObject{field(label, price, hint)}
		priceBox.Refresh()
	}
	if item.Sale == proto.SaleAuction {
		sale.SetSelected("Auction")
	} else {
		sale.SetSelected("Market")
	}
	underway := item.Sale == proto.SaleAuction && (item.Bidder != "" || item.Ended)
	if underway { // The bidding can't change any more.
		sale.Disable()
		price.Disable()
		endDate.Disable()
		endTime.Disable()
	}
	save := primary("Save", func() {
		problem.Set(nil)
		n, err := parseAmount(price.Text)
		if err != nil {
			problem.Set(errors.New("the price: " + err.Error()))
			return
		}
		item.Sale, item.Ends = proto.SaleMarket, ""
		if sale.Selected == "Auction" {
			t, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(endDate.Text)+" "+strings.TrimSpace(endTime.Text), time.Local)
			if err != nil {
				problem.Set(errors.New("write when it ends as YYYY-MM-DD and HH:MM"))
				return
			}
			item.Sale, item.Ends = proto.SaleAuction, t.UTC().Format(time.RFC3339)
		}
		item.Name, item.Description, item.Price = name.Text, desc.Text, n
		item.Type, item.RoleID = proto.ShopOther, ""
		if kind.Selected == "Role" {
			if role.SelectedIndex() < 0 {
				problem.Set(errors.New("choose the role this item gives"))
				return
			}
			item.Type, item.RoleID = proto.ShopRole, roleIDs[role.SelectedIndex()]
		}
		u.call(g.ID, proto.Request{Type: "shop_put", ShopItem: &item, Image: newPic}, func(_ proto.Response, err error) {
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Item saved")
		})
	})
	title := "New Item"
	parts := []fyne.CanvasObject{panelTitle(title)}
	if it != nil {
		id := text("Item ID "+strings.ToUpper(item.ID), 12.5, colFaint, false)
		id.TextStyle.Monospace = true
		parts = []fyne.CanvasObject{titled(panelTitle("Edit Item"), id)}
	}
	if underway {
		parts = append(parts, hintText("Bidding has started: only the name, description and picture can change."))
	}
	parts = append(parts,
		container.NewGridWithColumns(2, field("Type", kind, ""), roleField),
		container.NewGridWithColumns(2, field("Sale Type", sale, ""), priceBox),
		ends,
		field("Name", name, ""),
		field("Description", desc, "Markdown works here."),
		container.NewHBox(pick, container.NewCenter(picNote)), problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	p = u.openEditor(shopIcon, "Shop", parts...)
}

// ---- slash commands in chat -------------------------------------------------------

// chatCommand handles a message starting with "/"; it reports whether the
// text was a command (and so must not be sent).
func (u *UI) chatCommand(g guildconn.GuildView, txt string) (bool, error) {
	t := strings.TrimSpace(txt)
	if !strings.HasPrefix(t, "/") || strings.HasPrefix(t, "//") {
		return false, nil
	}
	cmd, rest, _ := strings.Cut(t, " ")
	switch strings.ToLower(cmd) {
	case "/balance":
		if g.Currency == nil {
			return true, errors.New("this guild has no currency")
		}
		u.call(g.ID, proto.Request{Type: "wallet"}, func(r proto.Response, err error) {
			if err != nil || r.Wallet == nil {
				u.toast(sentence(fmt.Sprint(err)))
				return
			}
			u.toast("You have " + coins(g, r.Wallet.Balance))
		})
		return true, nil
	case "/transfer":
		if g.Currency == nil {
			return true, errors.New("this guild has no currency")
		}
		key, amount, err := parseTransfer(g, rest)
		if err != nil {
			return true, err
		}
		u.confirmTransfer(g, key, amount)
		return true, nil
	}
	return true, errors.New("unknown command. Try /transfer @name 50 or /balance. To send a message that starts with /, start it with //")
}

// parseTransfer reads "@name 50"; names may have spaces, so the longest
// member name that fits wins.
func parseTransfer(g guildconn.GuildView, rest string) (string, int64, error) {
	rest = strings.TrimSpace(rest)
	usage := errors.New("write it like this: /transfer @name 50")
	if !strings.HasPrefix(rest, "@") {
		return "", 0, usage
	}
	rest = rest[1:]
	low := strings.ToLower(rest)
	key, best := "", 0
	for _, m := range g.Members {
		if m.IsMe {
			continue
		}
		for _, n := range []string{m.Nick, m.Name} {
			if n != "" && len(n) > best && strings.HasPrefix(low, strings.ToLower(n)+" ") {
				key, best = m.UserKey, len(n)
			}
		}
	}
	if key == "" {
		return "", 0, errors.New("there is no other member with that name here")
	}
	n, err := parseAmount(rest[best:])
	if err != nil {
		return "", 0, usage
	}
	return key, n, nil
}

// coinAmount is the coin and a number, centred on each other.
func (u *UI) coinAmount(g guildconn.GuildView, n int64, size float32) fyne.CanvasObject {
	return container.NewHBox(container.NewCenter(u.coinIcon(g, size)), container.NewCenter(text(groupDigits(n), size, colText, true)))
}
