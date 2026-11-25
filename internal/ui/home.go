package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/user"
)

// This file is the home screen: the rail, the side bar with you at the bottom,
// and the Friends page.

// statusNames are the statuses' names. "Invisible" and "Offline" are how other people can look;
// you pick from statusChoices only.
var statusNames = map[string]string{"Online": "Online", "Idle": "Idle", "DND": "Do Not Disturb", "Invisible": "Offline", "Offline": "Offline"}

var statusChoices = user.Statuses

// statusColour gives the traffic-light colours, and grey for offline.
func statusColour(s string) color.Color {
	switch s {
	case "Online":
		return colOnline
	case "Idle":
		return colYellow
	case "DND":
		return colRed
	}
	return rgb(0x6b, 0x72, 0x80)
}

// home holds the parts of the home screen that change while it is shown.
type home struct {
	rail         *fyne.Container // These are the guild buttons.
	side         *fyne.Container
	main         *fyne.Container
	views        []guildconn.GuildView
	sig, railSig string          // It is what is drawn now (see refreshHome).
	voiceSig     string          // It is the voice parts drawn now.
	voiceBox     *fyne.Container // It is the bar while in voice.
	meVoice      *fyne.Container // It holds mute and deafen, by your name.
	sideBox      *fyne.Container // It is the side bar, with the bars at its bottom.

	mu      sync.Mutex
	pending bool // A refresh is already on its way.
}

// enterHome starts the user's guild connections and shows the home screen.
func (u *UI) enterHome() {
	device, err := guildconn.DeviceID(u.ConfigDir)
	if err != nil {
		u.Log.Printf("no computer ID: %v", err)
	}
	u.selected, u.settings, u.homeView, u.friendsTab = "", nil, "", 0
	// Invisible is gone: anyone who had it is Online again.
	if !has(statusChoices, u.user.Profile.OnlineStatus) {
		u.user.SetStatus("Online")
	}
	u.home = &home{rail: container.NewVBox(), side: container.NewStack(), main: container.NewStack()}
	u.guilds = guildconn.New(u.user, device, u.guildsChanged)
	u.guilds.OnNotice(u.onNotice)
	u.startVoice()
	u.watchPushToTalk()
	u.home.voiceBox, u.home.meVoice = container.NewStack(), container.NewStack()
	// A slow tick, so things like "typing…" clear by themselves.
	go func(h *home) {
		for u.home == h {
			time.Sleep(2 * time.Second)
			fyne.Do(func() {
				if u.home == h {
					u.refreshHome()
				}
			})
		}
	}(u.home)

	homeBtn := newRailButton(icon(logoIcon, 28), func() { u.selectGuild("") })
	u.homeBtn = homeBtn
	add := newRailButton(icon(plusIcon, 22), u.openAddGuild)
	add.dashed = true
	sep := canvas.NewRectangle(colLineStrong)
	sep.SetMinSize(fyne.NewSize(28, 2))
	sep.CornerRadius = 1

	railBG := canvas.NewRectangle(colBG)
	railLine := canvas.NewRectangle(colLine)
	railLine.SetMinSize(fyne.NewSize(1, 1))
	railCol := container.NewVBox(gap(4), homeBtn, container.NewCenter(sep), u.home.rail, add)
	rail := container.NewStack(railBG, container.NewBorder(nil, nil, nil, railLine, container.NewVScroll(railCol)))

	sideBG := canvas.NewRectangle(colPanel)
	sideLine := canvas.NewRectangle(colLine)
	sideLine.SetMinSize(fyne.NewSize(1, 1))
	u.home.sideBox = container.NewBorder(nil, container.NewVBox(u.home.voiceBox, u.meBar()), nil, sideLine, u.home.side)
	side := container.NewStack(sideBG, u.home.sideBox)

	left := container.NewBorder(nil, nil, container.New(fixedWidth{72}, rail), nil,
		container.New(fixedWidth{260}, side))
	u.show(container.NewBorder(nil, nil, left, nil, u.home.main))
	u.refreshHome()
}

// guildsChanged is called by the guild connections (from their own
// threads) whenever something changes. Changes often come in bursts, so
// the screen is redrawn at most a few times a second.
func (u *UI) guildsChanged() {
	h := u.home
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.pending {
		h.mu.Unlock()
		return
	}
	h.pending = true
	h.mu.Unlock()
	time.AfterFunc(150*time.Millisecond, func() {
		h.mu.Lock()
		h.pending = false
		h.mu.Unlock()
		fyne.Do(func() {
			if u.home == h {
				u.refreshHome()
			}
		})
	})
}

// selectGuild shows a guild ("" = home, which shows the Friends page). It
// also closes any settings page.
func (u *UI) selectGuild(id string) {
	if id != u.selected {
		u.page, u.pageSig, u.pageData, u.pageKey, u.pageArg = "about", "", nil, "", ""
		u.leaveChat()
		if u.dm != nil {
			u.dm = nil
			u.guilds.LeaveChat()
		}
	}
	if id == "" {
		u.homeView = ""
	}
	u.settings = nil
	u.dropJoin()
	u.selected = id
	u.refreshHome()
}

// refreshHome redraws the rail and whatever is selected.
func (u *UI) refreshHome() {
	h := u.home
	if h == nil || u.guilds == nil {
		return
	}
	h.views = u.guilds.Views()
	if u.learnFaces(h.views) {
		h.sig = "" // Someone's picture arrived or changed.
		if u.chat != nil {
			u.chat.shown = ""
			defer u.drawMessages(false)
		}
	}

	// Redraw only what changed, so scroll positions and typing are kept.
	railSig := fmt.Sprint(u.selected)
	for _, g := range h.views {
		railSig += "|" + g.ID + g.Name + g.Status + g.PictureHash
	}
	if railSig != h.railSig {
		h.railSig = railSig
		h.rail.RemoveAll()
		for _, g := range h.views {
			g := g
			b := newRailButton(u.guildFace(g), func() { u.selectGuild(g.ID) })
			b.active = g.ID == u.selected
			b.dimmed = g.Status != "online"
			b.u, b.tip, b.state = u, g.Name, guildStateText(g.Status) // The name and state are shown on hover.
			h.rail.Add(b)
		}
		h.rail.Refresh()
		u.homeBtn.active = u.selected == "" || strings.HasPrefix(u.selected, "dm:")
		u.homeBtn.Refresh()
	}

	var side, main func() fyne.CanvasObject
	var sig string
	switch {
	case u.settings != nil:
		if u.settings.node != "" {
			if _, _, ok := u.nodeNow(u.settings.scope, u.settings.node); !ok {
				u.settings = nil // The channel was deleted.
				u.refreshHome()
				return
			}
		}
		if u.settings.scope != "" && u.findGuild(u.settings.scope) == nil {
			u.settings = nil
			u.selectGuild("")
			return
		}
		sig = u.settingsSig()
		side, main = u.settingsSide, u.settingsMain
	case u.joining != nil:
		sig = u.joinSig()
		side, main = u.homeSide, u.joinPage
	case u.selected == "":
		sig = fmt.Sprintf("home|%s|%d|%s|%s|%s|%s|%d|%s", u.user.Profile.OnlineStatus, len(h.views), u.contactsSig(),
			u.user.Profile.DefaultName, u.user.Profile.Bio, u.homeView, u.friendsTab, u.user.Profile.ProfilePicture+u.user.Profile.Banner)
		side, main = u.homeSide, u.friendsPage
		if u.homeView == "profile" {
			main = u.profilePage
		}
	case strings.HasPrefix(u.selected, "dm:"):
		id := strings.TrimPrefix(u.selected, "dm:")
		sig = "dm|" + u.selected + "|" + u.contactsSig()
		side, main = u.homeSide, func() fyne.CanvasObject { return u.dmPage(id) }
		u.pollDM()
	default:
		var g *guildconn.GuildView
		for i := range h.views {
			if h.views[i].ID == u.selected {
				g = &h.views[i]
			}
		}
		if g == nil {
			u.selected = ""
			u.refreshHome()
			return
		}
		gv := *g
		sig = fmt.Sprintf("guild|%s|%s|%+v", u.page, u.pageSig, gv)
		side = func() fyne.CanvasObject { return u.guildSide(gv) }
		main = func() fyne.CanvasObject { return u.guildMain(gv) }
	}
	if u.page == "chat" && u.settings == nil {
		u.pollChat()
	}
	vs := u.voiceSig()
	if vs != h.voiceSig {
		h.voiceSig = vs
		h.voiceBox.Objects = []fyne.CanvasObject{u.voiceBar()}
		h.voiceBox.Refresh()
		h.meVoice.Objects = []fyne.CanvasObject{u.voiceButtons()}
		h.meVoice.Refresh()
		h.sideBox.Refresh() // The bar came or went, so lay the side bar out again.
		if sig == h.sig && u.settings == nil && u.selected != "" && !strings.HasPrefix(u.selected, "dm:") {
			// Only who is talking changed, so only the channel list and a voice page are redrawn.
			h.side.Objects = []fyne.CanvasObject{side()}
			h.side.Refresh()
			if u.page == "voice" {
				h.main.Objects = []fyne.CanvasObject{main()}
				h.main.Refresh()
			}
			return
		}
	}
	if sig == h.sig {
		return
	}
	h.sig = sig
	h.side.Objects = []fyne.CanvasObject{side()}
	h.main.Objects = []fyne.CanvasObject{main()}
	h.side.Refresh()
	h.main.Refresh()
}

// redraw forces the next refreshHome to rebuild the page.
func (u *UI) redraw() {
	if u.home != nil {
		u.home.sig = ""
		u.refreshHome()
	}
}

// ---- side bar -------------------------------------------------------------

func sideLineH() fyne.CanvasObject {
	r := canvas.NewRectangle(colLine)
	r.SetMinSize(fyne.NewSize(1, 1))
	return r
}

// meBar is you, at the bottom of the side bar: your picture, name and
// status (click them for your profile), then Settings and Lock.
func (u *UI) meBar() fyne.CanvasObject {
	p := u.user.Profile
	u.meStatus = text(statusNames[p.OnlineStatus], 12.5, colDim, false)
	u.meDot = statusDot(p.OnlineStatus, 11)
	pic := container.NewStack(avatar(p.DefaultName, p.UserID, 36), container.NewWithoutLayout(u.meDot))
	u.meDot.Move(fyne.NewPos(26, 26))
	words := container.NewVBox(layout.NewSpacer(), fitText(p.DefaultName, true, colText), u.meStatus, layout.NewSpacer())
	me := newTapRow(container.NewBorder(nil, nil, container.NewHBox(pic, gap(1)), nil, words), false, func() {
		u.selectGuild("")
		u.homeView = "profile"
		u.redraw()
	})
	me.base = rgb(0x05, 0x05, 0x07)
	// Its name shows on hover.
	gearBtn := newTipButton(u, "User Settings", gearIcon, func() { u.openUserSettings("") })
	gearBtn.Importance = widget.LowImportance
	lockBtn := newTipButton(u, "Lock", lockIcon, u.lock)
	lockBtn.Importance = widget.LowImportance
	bg := canvas.NewRectangle(rgb(0x05, 0x05, 0x07))
	buttons := newTint(container.NewHBox(u.home.meVoice, gearBtn, lockBtn), compactTheme{appTheme})
	// The buttons are compact, and long names end in ….
	row := container.NewBorder(nil, nil, nil, container.NewCenter(container.NewHBox(gap(4), buttons)), me)
	return container.NewStack(bg, container.NewBorder(sideLineH(), nil, nil, nil,
		container.New(layout.NewCustomPaddedLayout(6, 6, 6, 6), row)))
}

// statusDot is a small coloured circle with a dark ring.
func statusDot(status string, size float32) *canvas.Circle {
	c := canvas.NewCircle(statusColour(status))
	c.StrokeColor = rgb(0x05, 0x05, 0x07)
	c.StrokeWidth = 2
	c.Resize(fyne.NewSize(size, size))
	return c
}

// statusPicker is a drop-down list of the three statuses.
func (u *UI) statusPicker() fyne.CanvasObject {
	cur := u.user.Profile.OnlineStatus
	var b *widget.Button
	b = widget.NewButtonWithIcon(statusNames[cur]+"   ▾", dotIcon(statusColour(cur)), func() {
		var items []*fyne.MenuItem
		for _, st := range statusChoices {
			st := st
			it := fyne.NewMenuItem(statusNames[st], func() { u.setStatus(st) })
			it.Icon = dotIcon(statusColour(st))
			it.Checked = st == cur
			items = append(items, it)
		}
		m := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.win.Canvas())
		m.ShowAtPosition(fyne.CurrentApp().Driver().AbsolutePositionForObject(b).Add(fyne.NewPos(0, b.Size().Height+4)))
	})
	return container.NewHBox(b)
}

// ---- Your Profile: how others see you -------------------------------------

func (u *UI) profilePage() fyne.CanvasObject {
	usr := u.user
	p := usr.Profile
	friends := 0
	for _, c := range u.guilds.Contacts() {
		if c.State == guildconn.ContactActive {
			friends++
		}
	}
	guilds := len(u.home.views)

	// The banner is in the user's own colours.
	h := float64(hue(p.UserID))
	banner := icon(svg("banner-"+p.UserID+".svg", fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 120" preserveAspectRatio="none"><defs><linearGradient id="b" x1="0" y1="0" x2="1" y2="0.4"><stop offset="0" stop-color="%s"/><stop offset=".6" stop-color="%s"/><stop offset="1" stop-color="#000"/></linearGradient></defs><rect width="800" height="120" fill="url(#b)"/></svg>`,
		hsl(h, .70, .30), hsl(math.Mod(h+60, 360), .80, .22))), 1)
	banner.FillMode = canvas.ImageFillStretch
	banner.SetMinSize(fyne.NewSize(10, 110))
	var bannerObj fyne.CanvasObject = banner
	if b := u.profileBanner(110); b != nil {
		bannerObj = b
	}

	copyBtn := widget.NewButtonWithIcon(shortID(p.UserID), copyIcon, nil)
	copyBtn.OnTapped = func() {
		u.app.Clipboard().SetContent(p.UserID)
		copyBtn.SetText("Copied")
		time.AfterFunc(1500*time.Millisecond, func() { fyne.Do(func() { copyBtn.SetText(shortID(p.UserID)) }) })
	}
	edit := widget.NewButtonWithIcon("Edit Profile", gearIcon, func() { u.openUserSettings("profile") })

	stat := func(value, label string) fyne.CanvasObject {
		return container.NewVBox(text(value, 18, colText, true), text(label, 12, colFaint, false))
	}
	stats := container.NewGridWithColumns(3,
		stat(fmt.Sprint(friends), "Friends"),
		stat(fmt.Sprint(guilds), "Guilds"),
		stat(p.CreationDate.Local().Format("2 Jan 2006"), "Member Since"))

	head := container.NewBorder(nil, nil, container.NewHBox(avatar(p.DefaultName, p.UserID, 88), gap(1), gap(1)),
		container.NewVBox(layout.NewSpacer(), edit, layout.NewSpacer()),
		container.NewVBox(layout.NewSpacer(), text(p.DefaultName, 26, colText, true), container.NewHBox(copyBtn), layout.NewSpacer()))
	inner := container.NewVBox(head, gap(10))
	if p.Bio != "" {
		inner.Add(u.markdownFlush(p.Bio))
		inner.Add(gap(10))
	}
	inner.Add(field("Status", u.statusPicker(), ""))
	inner.Add(gap(16))
	inner.Add(stats)
	profile := card(0, container.NewVBox(bannerObj,
		container.New(layout.NewCustomPaddedLayout(16, 22, 24, 24), inner)))

	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(14, 14, 24, 24), text("Your Profile", 16, colText, true)))
	body := container.New(layout.NewCustomPaddedLayout(20, 30, 24, 24), narrowTop(760, profile))
	return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, container.NewVScroll(body))
}

// setStatus saves your online status and redraws.
func (u *UI) setStatus(s string) {
	if err := u.user.SetStatus(s); err != nil {
		u.Log.Printf("status: %v", err)
		return
	}
	if u.guilds != nil {
		u.guilds.SetStatus(s)
	}
	u.meStatus.Text = statusNames[s]
	u.meStatus.Refresh()
	u.meDot.FillColor = statusColour(s)
	u.meDot.Refresh()
	u.redraw()
}

// ---- User Settings categories ---------------------------------------

// accountPanel is My Account: who you are, and your password.
func (u *UI) accountPanel() {
	usr := u.user
	p := usr.Profile
	mono := func(s string) fyne.CanvasObject {
		l := widget.NewLabel(s)
		l.Wrapping = fyne.TextWrapBreak
		l.TextStyle.Monospace = true
		// The text stays in the text column.
		return container.New(textPad{}, newTint(l, tightTheme{Theme: appTheme, text: colDim}))
	}
	copyID := widget.NewButtonWithIcon("Copy", copyIcon, func() {
		u.app.Clipboard().SetContent(p.UserID)
		u.toast("ID Copied")
	})
	rec := "No recovery code yet. If you forget your password, the account is lost."
	recLabel := "Make Code"
	if usr.HasRecoveryCode() {
		rec, recLabel = "You have a recovery code. Making a new one stops the old one working.", "New Code"
	}
	padded := func(o fyne.CanvasObject) fyne.CanvasObject {
		return container.New(layout.NewCustomPaddedLayout(12, 12, 0, 0), o)
	}
	u.openPanel("account", 600, panelTitle("My Account"),
		settingList("IDENTITY",
			padded(container.NewBorder(nil, nil, nil, container.NewCenter(copyID),
				stack(2, text("User ID", 15, colText, true), mono(p.UserID)))),
			padded(stack(2, text("Folder", 15, colText, true), mono(usr.Dir))),
			settingRow("Created", p.CreationDate.Local().Format("2 January 2006"), nil),
		),
		gap(20),
		settingList("SECURITY",
			settingRow("Password", "Change the password that unlocks this user.", widget.NewButton("Change", u.openChangePassword)),
			settingRow("Recovery Code", rec, widget.NewButton(recLabel, u.openRecoveryCode)),
		),
	)
}

// privacyPanel holds read receipts and the blocked list.
func (u *UI) privacyPanel() {
	p := u.user.Profile
	receipts := toggle(!p.HideReadReceipts, func(on bool) {
		if err := u.guilds.SetReadReceipts(on); err != nil {
			u.toast(sentence(err.Error()))
		}
	})
	blocked := 0
	for _, c := range u.guilds.Contacts() {
		if c.State == guildconn.ContactBlocked {
			blocked++
		}
	}
	about := "Nobody is blocked."
	switch {
	case blocked == 1:
		about = "1 person is blocked."
	case blocked > 1:
		about = fmt.Sprintf("%d people are blocked.", blocked)
	}
	u.openPanel("privacy", 600, panelTitle("Privacy"),
		settingList("",
			settingRow("Read Receipts", "Friends see when you have read their messages. If you turn this off, you don't send them.", receipts),
			settingRow("Blocked", about, widget.NewButton("Show", func() { u.selectGuild(""); u.friendsTab = 2; u.redraw() })),
		),
	)
}

// soundsPanel holds the app's notification settings.
func (u *UI) soundsPanel() {
	sounds := toggle(!u.user.Profile.MuteSounds, func(on bool) {
		if err := u.user.SetMuteSounds(!on); err != nil {
			u.toast(sentence(err.Error()))
		}
	})
	u.openPanel("notifications", 600, panelTitle("Notifications"),
		settingList("",
			settingRow("Sounds", "Play a short sound for new messages and mentions.", sounds),
			settingRow("Pop-ups", "Each guild has its own choice, in its Server Settings under Notifications. Channels can have their own too.", nil),
		),
	)
}

// card is a rounded panel.
func card(pad float32, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colPanel)
	bg.CornerRadius = 20
	bg.StrokeColor = colLine
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(pad, pad, pad, pad), content))
}

// dotIcon is a small filled circle (for the status buttons).
func dotIcon(c color.Color) fyne.Resource {
	r, g, b, _ := c.RGBA()
	hex := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	return svg("dot-"+hex+".svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="5" fill="`+hex+`"/></svg>`)
}

// fixedWidth gives its content an exact width and the full height.
type fixedWidth struct{ w float32 }

func (l fixedWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(l.w, objs[0].MinSize().Height)
}

func (l fixedWidth) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	objs[0].Resize(fyne.NewSize(l.w, s.Height))
	objs[0].Move(fyne.NewPos(0, 0))
}

// narrowTop is like narrow, but sits at the top instead of the middle.
type maxWidthTop struct{ w float32 }

func (l maxWidthTop) MinSize(objs []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(fyne.Min(l.w, objs[0].MinSize().Width), objs[0].MinSize().Height)
}

func (l maxWidthTop) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	w := fyne.Min(l.w, s.Width)
	objs[0].Resize(fyne.NewSize(w, objs[0].MinSize().Height))
	objs[0].Move(fyne.NewPos((s.Width-w)/2, 0))
}

func narrowTop(w float32, o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(maxWidthTop{w}, o)
}
