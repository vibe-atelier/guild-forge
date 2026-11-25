package ui

// This file shows auctions in the Shop: the lines on an auctioned item, and
// the Bid window.

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

var saleTypes = []string{"Market", "Auction"}

// auctionLines are an auction's highest bidder and end, after its
// description.
func auctionLines(g guildconn.GuildView, it proto.ShopItem) fyne.CanvasObject {
	line := func(label, value string) fyne.CanvasObject {
		return container.NewHBox(text(label, 13.5, colText, true), text(value, 13.5, colDim, false))
	}
	box := stack(4)
	switch {
	case it.Ended && it.Bidder != "":
		box.Add(line("Winner", nameOf(g, it.Bidder)))
	case it.Ended:
		box.Add(text("Ended without bids.", 13.5, colDim, false))
	case it.Bidder != "":
		box.Add(line("Highest Bidder", nameOf(g, it.Bidder)))
	default:
		box.Add(line("Starting Bid", coins(g, it.Price)))
	}
	if t, err := time.Parse(time.RFC3339, it.Ends); err == nil && !it.Ended {
		box.Add(line("Auction Ends", auctionTime(t)))
	}
	return box
}

// openBid is the Bid window: your balance, the current bid, and the
// amount to bid.
func (u *UI) openBid(g guildconn.GuildView, it proto.ShopItem) {
	var p *popup
	balance := text("…", 14, colDim, false)
	u.call(g.ID, proto.Request{Type: "wallet"}, func(r proto.Response, err error) {
		if err == nil && r.Wallet != nil {
			balance.Text = coins(g, r.Wallet.Balance)
			balance.Refresh()
		}
	})
	current := coins(g, it.Price) + " (starting bid)"
	least := it.Price
	if it.Bidder != "" {
		current = coins(g, it.Bid) + ", " + nameOf(g, it.Bidder)
		least = it.Bid + 1
	}
	amount := widget.NewEntry()
	amount.SetPlaceHolder(strconv.FormatInt(least, 10))
	problem := newErrorText()
	var place *widget.Button
	place = primary("Bid", func() {
		problem.Set(nil)
		n, err := parseAmount(amount.Text)
		switch {
		case err != nil:
			problem.Set(errors.New("the bid: " + err.Error()))
			return
		case n < least:
			problem.Set(fmt.Errorf("bid at least %s", coins(g, least)))
			return
		}
		place.Disable()
		u.call(g.ID, proto.Request{Type: "bid", ItemID: it.ID, Amount: n}, func(_ proto.Response, err error) {
			place.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Bid Placed")
		})
	})
	amount.OnSubmitted = func(string) { place.OnTapped() }
	row := func(label string, value fyne.CanvasObject) fyne.CanvasObject {
		// The text stays in the text column.
		return container.New(textPad{}, container.NewBorder(nil, nil, text(label, 14, colText, true), value))
	}
	p = u.openPopup(440, panelTitle("Bid"), text(it.Name, 15, colDim, true),
		row("Your Balance", balance), row("Current Bid", text(current, 14, colDim, false)),
		field("Bid Amount", amount, "Your bid is held from your wallet, and comes back if someone bids more."),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), place))
	u.win.Canvas().Focus(amount)
}

// auctionTime is when an auction ends, with the time zone:
// "Wednesday, 25 Nov :: 20:00 IST".
func auctionTime(t time.Time) string { return t.Local().Format("Monday, 2 Jan :: 15:04 MST") }

// auctionResult goes where the price was once an auction has ended, until
// staff remove the item.
func (u *UI) auctionResult(g guildconn.GuildView, it proto.ShopItem) fyne.CanvasObject {
	if it.Bidder == "" {
		return text("Unsold", 14, colFaint, true)
	}
	return container.NewVBox(container.NewHBox(layout.NewSpacer(), text("Sold", 14, colOnline, true)),
		container.NewHBox(layout.NewSpacer(), container.NewCenter(u.coinIcon(g, 20)), container.NewCenter(text(groupDigits(it.Bid), 16, colText, true))))
}
