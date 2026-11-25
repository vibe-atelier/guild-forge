package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
)

// This file shows friends (called contacts in the code). The
// home side bar lists them for private messages; the Friends page lists
// all of them by status, with Requests and Blocked tabs.

// tapRow is any content that can be clicked, with a hover background.
type tapRow struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
	onMenu  func(*fyne.PointEvent) // It is called on a right-click, when set.
	active  bool
	base    color.Color // It is the background when not hovered (nil means the side bar's).
	bg      *canvas.Rectangle
}

func newTapRow(content fyne.CanvasObject, active bool, tap func()) *tapRow {
	r := &tapRow{content: content, onTap: tap, active: active}
	r.ExtendBaseWidget(r)
	return r
}

func (r *tapRow) rest() {
	r.bg.FillColor = colPanel
	if r.base != nil {
		r.bg.FillColor = r.base
	}
	if r.active {
		r.bg.FillColor = colHover
	}
}

func (r *tapRow) CreateRenderer() fyne.WidgetRenderer {
	r.bg = canvas.NewRectangle(colPanel)
	r.bg.CornerRadius = 10
	r.rest()
	return widget.NewSimpleRenderer(container.NewStack(r.bg, container.New(layout.NewCustomPaddedLayout(6, 6, 8, 8), r.content)))
}

func (r *tapRow) Tapped(*fyne.PointEvent) { r.onTap() }

// TappedSecondary opens the row's menu on a right-click, if it has one.
func (r *tapRow) TappedSecondary(ev *fyne.PointEvent) {
	if r.onMenu != nil {
		r.onMenu(ev)
	}
}

func (r *tapRow) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (r *tapRow) MouseMoved(*desktop.MouseEvent) {}
func (r *tapRow) MouseIn(*desktop.MouseEvent) {
	r.bg.FillColor = colRaised
	r.bg.Refresh()
}
func (r *tapRow) MouseOut() {
	r.rest()
	r.bg.Refresh()
}

// contactStatus is how a friend looks: Online, Idle, DND or Offline.
func contactStatus(c guildconn.ContactView) string {
	if !c.Online {
		return "Offline"
	}
	if c.Status == "" || c.Status == "Invisible" {
		return "Online"
	}
	return c.Status
}

// contactFace is a picture with a status dot (grey when offline).
func contactFace(c guildconn.ContactView, size float32) fyne.CanvasObject {
	dot := statusDot(contactStatus(c), size*0.3)
	pic := container.NewStack(avatar(c.Name, c.UserID, size), container.NewWithoutLayout(dot))
	dot.Move(fyne.NewPos(size*0.72, size*0.72))
	return pic
}

// badge is the verification mark after a name.
func badge(verified string) fyne.CanvasObject {
	pill := func(s string, c color.Color) fyne.CanvasObject {
		r, g, b, _ := c.RGBA()
		bg := canvas.NewRectangle(color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 38})
		bg.CornerRadius = 6
		return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(2, 2, 7, 7), text(s, 11, c, true)))
	}
	switch verified {
	case "verified":
		return pill("✔ Verified", colOnline)
	case "changed":
		return pill("⚠ Key Changed", colIdle)
	}
	return nil
}

func (u *UI) contactsSig() string {
	if u.guilds == nil {
		return ""
	}
	return fmt.Sprintf("%+v", u.guilds.Contacts())
}

// contactLine is the second line under a friend's name.
func contactLine(c guildconn.ContactView) string {
	switch {
	case c.Typing:
		return "Typing…"
	case !c.Reachable:
		return "No shared guild"
	}
	return statusNames[contactStatus(c)]
}

// countBadge is a small red number.
func countBadge(n int) fyne.CanvasObject {
	bg := canvas.NewRectangle(colDanger)
	bg.CornerRadius = 9
	return container.NewCenter(container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(1, 1, 6, 6), text(fmt.Sprint(n), 11, rgb(255, 255, 255), true))))
}

// homeSide is the home side bar: the Friends page, then private messages.
func (u *UI) homeSide() fyne.CanvasObject {
	var friends []guildconn.ContactView
	requests := 0
	for _, c := range u.guilds.Contacts() {
		switch c.State {
		case guildconn.ContactActive:
			friends = append(friends, c)
		case guildconn.ContactReceived:
			requests++
		}
	}
	// Most recent chats first, like Discord's list.
	sort.SliceStable(friends, func(i, j int) bool { return friends[i].LastChat.After(friends[j].LastChat) })

	var right fyne.CanvasObject
	if requests > 0 {
		right = countBadge(requests)
	}
	friendsRow := newTapRow(container.NewBorder(nil, nil, container.NewHBox(icon(usersIcon, 20), gap(4)), right,
		text("Friends", 15, colText, true)), u.selected == "" && u.homeView == "", func() { u.selectGuild("") })
	pad := func(o fyne.CanvasObject) fyne.CanvasObject {
		return container.New(layout.NewCustomPaddedLayout(0, 0, 8, 8), o)
	}
	box := container.NewVBox(gap(8), pad(friendsRow))
	add := widget.NewButton("+", u.openAddContact)
	add.Importance = widget.LowImportance
	box.Add(container.New(layout.NewCustomPaddedLayout(10, 2, 16, 8),
		container.NewBorder(nil, nil, nil, add, container.NewVBox(layout.NewSpacer(), text("DIRECT MESSAGES", 11.5, colFaint, true), layout.NewSpacer()))))
	if len(friends) == 0 {
		box.Add(container.New(layout.NewCustomPaddedLayout(4, 0, 16, 16), hintText("No friends yet. Add one from the Friends page.")))
	}
	for _, c := range friends {
		c := c
		nameCol := colText
		if !c.Reachable {
			nameCol = colFaint // There is no shared guild right now.
		}
		var right fyne.CanvasObject
		if c.Unread > 0 {
			right = countBadge(c.Unread)
		}
		content := container.NewBorder(nil, nil, container.NewHBox(contactFace(c, 34), gap(1)), right,
			container.NewVBox(text(c.Name, 14, nameCol, true), text(contactLine(c), 11.5, colFaint, false)))
		row := newTapRow(content, u.selected == "dm:"+c.UserID, func() { u.openDM(c.UserID) })
		row.onMenu = func(ev *fyne.PointEvent) { u.chatListMenu(c, ev.AbsolutePosition) } // A right-click opens the chat's menu.
		box.Add(pad(row))
	}
	return container.NewVScroll(box)
}

// ---- the Friends page ---------------------------------------------------------

func (u *UI) friendsPage() fyne.CanvasObject {
	var received, sent, friends, blocked []guildconn.ContactView
	for _, c := range u.guilds.Contacts() {
		switch c.State {
		case guildconn.ContactReceived:
			received = append(received, c)
		case guildconn.ContactSent:
			sent = append(sent, c)
		case guildconn.ContactBlocked:
			blocked = append(blocked, c)
		default:
			friends = append(friends, c)
		}
	}
	reqLabel := "Requests"
	if len(received) > 0 {
		reqLabel = fmt.Sprintf("Requests (%d)", len(received))
	}
	addTab := newTabButton("", userPlusIcon, u.openAddContact)
	addTab.selected = u.friendsTab == 3 // Add Friend is a tab of its own.
	bar := tabBar(addTab, []string{"Contacts", reqLabel, "Blocked"}, u.friendsTab, func(i int) {
		u.friendsTab = i
		u.redraw()
	})
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(8, 8, 24, 24), bar))

	list := stack(4) // This is a column, so text lines up.
	switch u.friendsTab {
	case 3:
		list.Add(u.addFriendPane())
	case 1:
		u.requestsList(list, received, sent)
	case 2:
		u.blockedList(list, blocked)
	default:
		u.allFriends(list, friends)
	}
	if u.friendsTab == 0 && len(friends) == 0 {
		// Nothing to list: the note and its button sit in the middle.
		return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, narrow(400, list))
	}
	body := container.New(layout.NewCustomPaddedLayout(16, 30, 24, 24), narrowTop(760, list))
	return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, container.NewVScroll(body))
}

// allFriends lists every friend, grouped by status (online ones first) and
// in name order within each group.
func (u *UI) allFriends(list *fyne.Container, friends []guildconn.ContactView) {
	if len(friends) == 0 {

		list.Add(centred(text("No friends yet.", 15, colDim, false)))
		list.Add(gap(6))
		list.Add(container.NewCenter(primary("Add Friend", u.openAddContact)))
		return
	}
	order := []string{"Online", "Idle", "DND", "Offline"}
	groups := map[string][]guildconn.ContactView{}
	for _, c := range friends {
		s := contactStatus(c)
		groups[s] = append(groups[s], c)
	}
	for _, s := range order {
		in := groups[s]
		if len(in) == 0 {
			continue
		}
		sort.Slice(in, func(i, j int) bool { return strings.ToLower(in[i].Name) < strings.ToLower(in[j].Name) })
		list.Add(container.New(layout.NewCustomPaddedLayout(12, 6, 0, 0),
			text(fmt.Sprintf("%s (%d)", strings.ToUpper(statusNames[s]), len(in)), 11.5, colFaint, true)))
		for i, c := range in {
			c := c
			if i > 0 {
				list.Add(sideLineH())
			}
			name := container.NewHBox(text(c.Name, 15, colText, true))
			if b := badge(c.Verified); b != nil {
				name.Add(gap(1))
				name.Add(container.NewCenter(b))
			}
			msg := widget.NewButtonWithIcon("", chatIcon, func() { u.openDM(c.UserID) })
			var more *widget.Button
			more = widget.NewButtonWithIcon("", dotsIcon, func() { u.contactMenu(c, more) })
			row := container.NewBorder(nil, nil, container.NewHBox(contactFace(c, 38), gap(2)), container.NewCenter(container.NewHBox(msg, more)),
				container.NewVBox(layout.NewSpacer(), name, text(contactLine(c), 12.5, colFaint, false), layout.NewSpacer()))
			list.Add(newTapRow(row, false, func() { u.openDM(c.UserID) }))
		}
	}
}

// requestsList shows requests to you (Accept, Decline) and from you (Cancel).
func (u *UI) requestsList(list *fyne.Container, received, sent []guildconn.ContactView) {
	if len(received)+len(sent) == 0 {
		list.Add(gap(30))
		list.Add(centred(text("No friend requests.", 15, colDim, false)))
		return
	}
	row := func(c guildconn.ContactView, sub string, buttons ...fyne.CanvasObject) fyne.CanvasObject {
		return container.New(layout.NewCustomPaddedLayout(8, 8, 0, 0), container.NewBorder(nil, nil,
			container.NewHBox(avatar(c.Name, c.UserID, 38), gap(2)), container.NewCenter(container.NewHBox(buttons...)),
			container.NewVBox(layout.NewSpacer(), text(c.Name, 15, colText, true), text(sub, 12.5, colFaint, false), layout.NewSpacer())))
	}
	if len(received) > 0 {
		list.Add(container.New(layout.NewCustomPaddedLayout(12, 6, 0, 0), text(fmt.Sprintf("RECEIVED (%d)", len(received)), 11.5, colFaint, true)))
	}
	for i, c := range received {
		c := c
		if i > 0 {
			list.Add(sideLineH())
		}
		accept := primary("Accept", func() {
			u.contactAction(func() error { return u.guilds.AcceptContact(c.UserID) }, "You are now friends with "+c.Name+".")
		})
		decline := widget.NewButton("Decline", func() {
			u.contactAction(func() error { return u.guilds.DeclineContact(c.UserID) }, "Request declined.")
		})
		list.Add(row(c, "Wants to be your friend.", decline, accept))
	}
	if len(sent) > 0 {
		list.Add(container.New(layout.NewCustomPaddedLayout(18, 6, 0, 0), text(fmt.Sprintf("SENT (%d)", len(sent)), 11.5, colFaint, true)))
	}
	for i, c := range sent {
		c := c
		if i > 0 {
			list.Add(sideLineH())
		}
		cancel := widget.NewButton("Cancel", func() {
			u.contactAction(func() error { return u.guilds.DeclineContact(c.UserID) }, "Request cancelled.")
		})
		list.Add(row(c, "Waiting for them to accept.", cancel))
	}
}

// blockedList shows blocked people, with Unblock.
func (u *UI) blockedList(list *fyne.Container, blocked []guildconn.ContactView) {
	list.Add(container.New(layout.NewCustomPaddedLayout(4, 10, 0, 0),
		hintText("Blocked people can't send you private messages or friend requests, and their guild messages are collapsed for you. They aren't told.")))
	if len(blocked) == 0 {
		list.Add(gap(20))
		list.Add(centred(text("Nobody is blocked.", 15, colDim, false)))
		return
	}
	for i, c := range blocked {
		c := c
		if i > 0 {
			list.Add(sideLineH())
		}
		unblock := widget.NewButton("Unblock", func() { u.guilds.Unblock(c.UserID); u.toast("Unblocked."); u.redraw() })
		list.Add(container.New(layout.NewCustomPaddedLayout(8, 8, 0, 0), container.NewBorder(nil, nil,
			container.NewHBox(avatar(c.Name, c.UserID, 38), gap(2)), container.NewCenter(unblock),
			container.NewVBox(layout.NewSpacer(), text(c.Name, 15, colText, true), text(shortID(c.UserID), 12.5, colFaint, false), layout.NewSpacer()))))
	}
}

// contactAction runs a friend action in the background.
func (u *UI) contactAction(do func() error, done string) {
	go func() {
		err := do()
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.toast(done)
			u.redraw()
		})
	}()
}

// openAddContact shows the Add Friend tab of the Friends page.
func (u *UI) openAddContact() {
	u.selectGuild("")
	u.homeView = ""
	u.friendsTab = 3
	u.redraw()
}

// addFriendPane asks for a user ID to send a friend request to.
func (u *UI) addFriendPane() fyne.CanvasObject {
	id := widget.NewEntry()
	id.SetPlaceHolder("User ID")
	problem := newErrorText()
	var send *widget.Button
	send = primary("Send Request", func() {
		problem.Set(nil)
		send.Disable()
		v := strings.TrimSpace(id.Text)
		go func() {
			err := u.guilds.RequestContact(v)
			fyne.Do(func() {
				send.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				u.toast("Request sent.")
				u.friendsTab = 1
				u.redraw()
			})
		}()
	})
	id.OnSubmitted = func(string) { send.OnTapped() }
	u.focusLater(id)
	return form(
		titled(panelTitle("Add Friend"), para("Ask them for their user ID (on their profile page), or click ⋯ next to them in a guild's member list. You can only add people you share a guild with, and they must accept.")),
		container.NewBorder(nil, nil, nil, send, id),
		problem.box)
}

// blockPerson asks first, then blocks.
func (u *UI) blockPerson(id, name string) {
	u.confirm("Block "+name, "They won't be able to send you private messages or friend requests, and their guild messages will be collapsed. They won't be told.", "Block", true, func() {
		u.guilds.Block(id, name)
		if u.selected == "dm:"+id {
			u.selectGuild("")
		}
		u.toast("Blocked.")
		u.redraw()
	})
}

// chatListMenu is the right-click menu of a private chat in the side bar.
func (u *UI) chatListMenu(c guildconn.ContactView, at fyne.Position) {
	menu := widget.NewPopUpMenu(fyne.NewMenu("",
		fyne.NewMenuItem("Clear Chat", func() { u.openClearChat(c) })), u.win.Canvas())
	menu.ShowAtPosition(at)
}

// openClearChat asks before deleting your copy of a chat, with Erase
// Media for its pictures and files.
func (u *UI) openClearChat(c guildconn.ContactView) {
	var p *popup
	erase, eraseBox := option("Erase Media", "Also delete the pictures and files sent in this chat.", false)
	clear := widget.NewButton("Clear Chat", func() {
		media := erase.Checked
		p.Close()
		u.contactAction(func() error { return u.guilds.ClearChat(c.UserID, media) }, "Chat Cleared")
		if u.dm != nil && u.dm.contact == c.UserID {
			u.pollDM() // The open chat shows the change.
		}
	})
	clear.Importance = widget.DangerImportance
	cancel := widget.NewButton("Cancel", func() { p.Close() })
	p = u.openPopup(440, text("Clear Chat", 20, colText, true),
		para("Delete your copy of the messages with "+c.Name+"? This cannot be undone. Their copy is not affected, and they stay your friend."),
		eraseBox, buttonsRow(cancel, clear))
}
