package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// Settings pages, laid out like Discord's: the side bar becomes a
// list of categories in groups, and the main area shows the chosen
// category. There are two: your own User Settings, and a guild's Server
// Settings, whose categories depend on what you are allowed to do (so
// members and staff see different pages).
//
// Each category's content is built by the same function that used to open
// a pop-up window; it calls openPanel with its category's key, and when
// that category is showing, the content goes into the page instead.

type settingsView struct {
	scope string // It is "" for User Settings, or the guild's ID.
	node  string // It is a channel, folder or category whose settings are shown.
	cat   string
	body  *fyne.Container
	n     int // It goes up whenever a category is drawn again.
}

type settingsCat struct {
	key, label, group string
	open              func()
}

// openPanel shows content on the settings page, as category key, when that
// category is showing, and as a pop-up window otherwise.
func (u *UI) openPanel(key string, width float32, content ...fyne.CanvasObject) *popup {
	s := u.settings
	if s == nil || s.cat != key || s.body == nil {
		return u.openPopup(width, content...)
	}
	n := s.n
	p := &popup{embedded: true}
	p.reload = func() {
		if u.settings == s && s.cat == key && s.n == n {
			u.openSettingsCat(key)
		}
	}
	for _, o := range content {
		hideClose(o)
	}
	s.body.Objects = []fyne.CanvasObject{form(content...)}
	s.body.Refresh()
	return p
}

// hideClose hides a window's Close button: a page has no window to close.
func hideClose(o fyne.CanvasObject) {
	switch v := o.(type) {
	case *widget.Button:
		if v.Text == "Close" {
			v.Hide()
		}
	case *fyne.Container:
		for _, c := range v.Objects {
			hideClose(c)
		}
	}
}

// openUserSettings opens your own settings on a category ("" = the first).
func (u *UI) openUserSettings(cat string) {
	u.leaveChat()
	u.dropJoin()
	u.selected = ""
	u.settings = &settingsView{}
	if cat == "" {
		cat = "account"
	}
	u.openSettingsCat(cat)
}

// openGuildSettings opens a guild's settings on a category ("" = the first
// one you may use).
func (u *UI) openGuildSettings(g guildconn.GuildView, cat string) {
	u.leaveChat()
	u.dropJoin()
	u.selected = g.ID
	u.settings = &settingsView{scope: g.ID}
	if cat == "" {
		cat = u.guildCats(g)[0].key
	}
	u.openSettingsCat(cat)
}

// closeSettings goes back to where you were.
func (u *UI) closeSettings() {
	u.settings = nil
	u.redraw()
}

// openSettingsCat draws a category again from fresh.
func (u *UI) openSettingsCat(cat string) {
	s := u.settings
	if s == nil {
		return
	}
	s.cat = cat
	s.n++
	s.body = container.NewStack(para("Loading…"))
	u.redraw()
	for _, c := range u.settingsCats() {
		if c.key == cat {
			c.open()
			return
		}
	}
}

func (u *UI) settingsCats() []settingsCat {
	if u.settings == nil {
		return nil
	}
	if u.settings.scope == "" {
		return u.userCats()
	}
	if u.settings.node != "" {
		if g, n, ok := u.nodeNow(u.settings.scope, u.settings.node); ok {
			return u.channelCats(g, n)
		}
		return nil
	}
	if g := u.findGuild(u.settings.scope); g != nil {
		return u.guildCats(*g)
	}
	return nil
}

func (u *UI) userCats() []settingsCat {
	return []settingsCat{
		{"account", "My Account", "ACCOUNT", u.accountPanel},
		{"profile", "Profile", "ACCOUNT", u.openEditProfile},
		{"privacy", "Privacy", "ACCOUNT", u.privacyPanel},
		{"notifications", "Notifications", "APP", u.soundsPanel},
		{"voice", "Voice", "APP", u.voicePanel},
		{"updates", "Check Updates", "APP", u.updatesPanel},
		{"licenses", "Licenses", "APP", u.licensesPanel},
	}
}

func (u *UI) guildCats(g guildconn.GuildView) []settingsCat {
	var cats []settingsCat
	add := func(ok bool, key, label, group string, open func(guildconn.GuildView)) {
		if ok {
			cats = append(cats, settingsCat{key, label, group, func() {
				if gv := u.findGuild(g.ID); gv != nil {
					open(*gv)
				}
			}})
		}
	}
	add(can(g, proto.PermManageGuild), "overview", "Overview", "GUILD", u.openSettings)
	add(can(g, proto.PermManageRoles), "roles", "Roles", "GUILD", u.openRoles)
	add(can(g, proto.PermManageGuild), "emoji", "Custom Emoji", "GUILD", u.openGuildEmoji)
	add(can(g, proto.PermManagePages), "pages", "Pages", "GUILD", u.openPagesSettings)
	add(can(g, proto.PermManageCurrency), "currency", "Currency", "GUILD", u.openCurrency)
	add(can(g, proto.PermManageSoundboard), "soundboard", "Soundboard", "GUILD", u.openSoundboard)
	add(can(g, proto.PermManageJoining), "whojoins", "Joining Conditions", "JOINING", func(g guildconn.GuildView) { u.openJoinSettings(g, "whojoins") })
	add(can(g, proto.PermManageJoining), "rulesset", "Rules", "JOINING", func(g guildconn.GuildView) { u.openJoinSettings(g, "rulesset") })
	add(can(g, proto.PermManageJoining), "questions", "CAPTCHA", "JOINING", u.openCaptcha)
	add(can(g, proto.PermBan), "bans", "Bans", "MODERATION", u.openBans)
	add(g.IsOwner, "ownership", "Ownership", "MODERATION", u.openTransfer)
	add(can(g, proto.PermChangeNickname) || can(g, proto.PermManageNicknames), "nickname", "Nickname", "YOU", u.openMyNickname)
	add(true, "notify", "Notifications", "YOU", func(g guildconn.GuildView) { u.openNotify(g, "", "") })
	return cats
}

// settingsSig is what the settings page depends on, so it is drawn again
// only when that changes (typing is never lost to a redraw).
func (u *UI) settingsSig() string {
	s := u.settings
	sig := fmt.Sprintf("settings|%s|%s|%s|%d", s.scope, s.node, s.cat, s.n)
	for _, c := range u.settingsCats() {
		sig += "|" + c.key
	}
	if g := u.findGuild(s.scope); g != nil {
		sig += "|" + g.Name + g.Status
	}
	return sig
}

// settingsSide is the list of categories, in the side bar.
func (u *UI) settingsSide() fyne.CanvasObject {
	s := u.settings
	title := "User Settings"
	if g := u.findGuild(s.scope); g != nil {
		title = g.Name
	}
	if _, n, ok := u.nodeNow(s.scope, s.node); ok && s.node != "" {
		title = n.Name
		if n.Kind == proto.KindChannel {
			title = "#" + n.Name
		}
	}
	back := widget.NewButtonWithIcon("Back", backIcon, u.closeSettings)
	back.Importance = widget.LowImportance
	list := container.NewVBox(container.NewHBox(back),
		container.New(layout.NewCustomPaddedLayout(6, 0, 10, 8), text(title, 16, colText, true)))
	group := ""
	for _, c := range u.settingsCats() {
		c := c
		if c.group != group {
			group = c.group
			list.Add(container.New(layout.NewCustomPaddedLayout(14, 4, 10, 8), text(group, 11.5, colFaint, true)))
		}
		b := widget.NewButton(c.label, func() { u.openSettingsCat(c.key) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		if c.key == s.cat {
			b.Importance = widget.MediumImportance
		}
		list.Add(b)
	}
	list.Add(container.New(layout.NewCustomPaddedLayout(10, 10, 0, 0), sideLineH()))
	var last *widget.Button
	if g, n, ok := u.nodeNow(s.scope, s.node); ok && s.node != "" {
		last = widget.NewButton("Delete "+kindNames[n.Kind], func() { u.deleteNode(g, n) })
	} else if g := u.findGuild(s.scope); g != nil {
		gv := *g
		last = widget.NewButton("Leave Guild", func() { u.openLeave(gv) })
	} else {
		last = widget.NewButtonWithIcon("Lock", lockIcon, u.lock)
	}
	last.Alignment = widget.ButtonAlignLeading
	last.Importance = widget.LowImportance
	list.Add(newTint(last, colorTheme{Theme: appTheme, text: colDanger}))
	return container.NewVScroll(container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), list))
}

// settingsMain is the chosen category, with a close button at the top.
func (u *UI) settingsMain() fyne.CanvasObject {
	s := u.settings
	title := "User Settings"
	switch {
	case s.node != "":
		title = "Channel Settings"
		if _, n, ok := u.nodeNow(s.scope, s.node); ok {
			title = kindNames[n.Kind] + " Settings"
		}
	case s.scope != "":
		title = "Server Settings"
	}
	closeBtn := widget.NewButtonWithIcon("Close", xIcon, u.closeSettings)
	closeBtn.Importance = widget.LowImportance
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(8, 8, 24, 12),
			container.NewBorder(nil, nil, container.NewHBox(icon(gearIcon, 18), container.NewCenter(text(title, 16, colText, true))), closeBtn)))
	body := container.New(layout.NewCustomPaddedLayout(24, 40, 32, 32), narrowTop(740, s.body))
	return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, container.NewVScroll(body))
}

// ---- rows of settings --------------------------------------------------------

// settingRow is one setting in a list: its name and a line about it on the
// left, and its control (a switch or a button) on the right.
func settingRow(title, about string, control fyne.CanvasObject) fyne.CanvasObject {
	words := stack(2, text(title, 15, colText, true))
	if about != "" {
		words.Add(hintText(about)) // Callers write whole sentences, or a plain value like a date.
	}
	var right fyne.CanvasObject = layout.NewSpacer()
	if control != nil {
		right = container.NewCenter(control)
	}
	return container.New(layout.NewCustomPaddedLayout(12, 12, 0, 0), container.NewBorder(nil, nil, nil, right, words))
}

// settingList puts rows under a heading, with lines between them.
func settingList(heading string, rows ...fyne.CanvasObject) fyne.CanvasObject {
	box := stack(0)
	if heading != "" {
		box.Add(text(heading, 11.5, colFaint, true))
		box.Add(gap(4))
	}
	for i, r := range rows {
		if i > 0 {
			box.Add(sideLineH())
		}
		box.Add(r)
	}
	return box
}

// toggle is a switch for a setting row.
func toggle(on bool, changed func(bool)) *widget.Check {
	c := widget.NewCheck("", nil)
	c.SetChecked(on)
	c.OnChanged = changed
	return c
}

// panelTitle is a category's heading on a settings page.
func panelTitle(s string) fyne.CanvasObject { return text(s, 22, colText, true) }
