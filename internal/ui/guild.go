package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
	"nexus/internal/proto"
)

// This file shows a guild's pages.

func waitShort() { time.Sleep(300 * time.Millisecond) }

// findGuild returns the current view of a guild, or nil.
func (u *UI) findGuild(id string) *guildconn.GuildView {
	if u.guilds == nil {
		return nil
	}
	for _, g := range u.guilds.Views() {
		if g.ID == id {
			g := g
			return &g
		}
	}
	return nil
}

// call sends a request to a guild in the background and reports the
// result on the window's thread.
func (u *UI) call(id string, req proto.Request, done func(proto.Response, error)) {
	go func() {
		var r proto.Response
		gc, err := u.guilds.Get(id)
		if err == nil {
			r, err = gc.Call(req)
		}
		fyne.Do(func() { done(r, err) })
	}()
}

// ---- side bar -------------------------------------------------------------

func (u *UI) guildSide(g guildconn.GuildView) fyne.CanvasObject {
	// Settings is a page of its own, opened from the gear.
	gear := widget.NewButtonWithIcon("", gearIcon, func() { u.openGuildSettings(g, "") })
	gear.Importance = widget.LowImportance
	head := container.NewBorder(nil, nil, nil, gear, container.NewVBox(layout.NewSpacer(), text(g.Name, 16, colText, true), layout.NewSpacer()))
	nav := container.NewVBox()
	item := func(page, label string, ic fyne.Resource) {
		b := widget.NewButtonWithIcon(label, ic, func() { u.openPage(page) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		if u.page == page {
			b.Importance = widget.MediumImportance
		}
		nav.Add(b)
	}
	item("about", "About", infoIcon)
	item("members", "Members", usersIcon)
	if g.Join != nil && g.Join.RulesEnabled {
		item("rules", "Rules", shieldIcon)
	}
	if g.Pages.Calendar {
		item("calendar", "Calendar", calendarIcon)
	}
	if g.Pages.Threads {
		item("threads", "Threads", threadsIcon)
	}
	if g.Pages.Wiki {
		item("wiki", "Wiki", wikiIcon)
	}
	if g.Currency != nil {
		item("wallet", "Wallet", walletIcon)
		if g.Currency.Shop {
			item("shop", "Shop", shopIcon)
		}
		if can(g, proto.PermManageCurrency) {
			item("balances", "Balances", walletIcon)
		}
	}
	if can(g, proto.PermApproveJoins) {
		label := "Join Requests"
		if g.Requests > 0 {
			label = fmt.Sprintf("Join Requests (%d)", g.Requests)
		}
		item("requests", label, userPlusIcon)
	}
	// The pages and channels scroll when they don't fit, and keep their place
	// when drawn again (people starting to talk redraw them).
	sc := container.NewVScroll(container.NewVBox(
		container.New(layout.NewCustomPaddedLayout(8, 0, 8, 8), nav),
		container.New(layout.NewCustomPaddedLayout(0, 8, 8, 8), u.channelTree(g)),
	))
	if u.sideScroll.guild == g.ID {
		sc.Offset = u.sideScroll.at
	}
	sc.OnScrolled = func(p fyne.Position) { u.sideScroll.guild, u.sideScroll.at = g.ID, p }
	return container.NewBorder(container.NewVBox(
		container.New(layout.NewCustomPaddedLayout(10, 8, 16, 8), head),
		sideLineH()), nil, nil, nil, sc)
}

// ---- About page -----------------------------------------------------------
//
// The About page is the guild's front door: its picture, name and
// About text, with the Rules button at the right. Members are on their own
// page, and settings on the settings page. Staff also see a strip of what
// needs them (join requests waiting) and a Server Settings button.

func (u *UI) guildPage(g guildconn.GuildView) fyne.CanvasObject {
	parts := []fyne.CanvasObject{}
	if n := u.rulesNotice(g); n != nil {
		parts = append(parts, n, gap(12))
	}
	if n := u.guildNotice(g); n != nil {
		parts = append(parts, n, gap(12))
	}
	if t := u.transferBanner(g); t != nil {
		parts = append(parts, t, gap(12))
	}
	if s := u.staffStrip(g); s != nil {
		parts = append(parts, s, gap(12))
	}

	count := ""
	if g.ShowMemberCount && g.MemberCount >= 0 {
		count = fmt.Sprintf("%d %s", g.MemberCount, map[bool]string{true: "member", false: "members"}[g.MemberCount == 1])
	}
	copyAddr := widget.NewButtonWithIcon("Copy Address", copyIcon, func() {
		u.app.Clipboard().SetContent(g.Address)
		u.toast("Address copied. Send it to people you want to invite.")
	})
	head := container.NewBorder(nil, nil, container.NewHBox(u.guildPicture(g, 88), gap(1)),
		container.NewVBox(layout.NewSpacer(), copyAddr, layout.NewSpacer()),
		container.NewVBox(layout.NewSpacer(), text(g.Name, 26, colText, true), text(count, 13.5, colDim, false), layout.NewSpacer()))
	var about fyne.CanvasObject
	switch {
	case g.About != "":
		about = u.markdownFlush(g.About)
	case can(g, proto.PermManageGuild):
		about = para("No About text yet. Add one in Server Settings, under Overview.")
	default:
		about = para("No About text yet.")
	}
	aboutBox := container.NewVBox(head, gap(14), about)
	// The Rules button opens the separate Rules page, at the
	// right like every other button.
	if g.Join != nil && g.Join.RulesEnabled && (g.Join.ShowRulesButton || g.Join.RequireRules) {
		aboutBox.Add(buttonsRow(widget.NewButtonWithIcon("Rules", shieldIcon, func() { u.openPage("rules") })))
	}
	parts = append(parts, card(22, aboutBox))
	return pageFrame(infoIcon, "About", parts...)
}

// staffStrip shows staff what needs them, with a way to the settings.
func (u *UI) staffStrip(g guildconn.GuildView) fyne.CanvasObject {
	cats := u.guildCats(g)
	staff := false
	for _, c := range cats {
		if c.group != "YOU" {
			staff = true
		}
	}
	if !staff && !can(g, proto.PermApproveJoins) {
		return nil
	}
	note := "You help run this guild."
	if g.IsOwner {
		note = "You own this guild."
	}
	var buttons []fyne.CanvasObject
	if can(g, proto.PermApproveJoins) && g.Requests > 0 {
		note += fmt.Sprintf(" %d %s waiting to join.", g.Requests, map[bool]string{true: "person is", false: "people are"}[g.Requests == 1])
		buttons = append(buttons, widget.NewButton("Join Requests", func() { u.openPage("requests") }))
	}
	if staff {
		buttons = append(buttons, primary("Server Settings", func() { u.openGuildSettings(g, "") }))
	}
	return card(16, container.NewBorder(nil, nil, container.NewHBox(icon(shieldIcon, 20), gap(2)), container.NewCenter(container.NewHBox(buttons...)),
		container.NewVBox(layout.NewSpacer(), text(note, 14, colText, false), layout.NewSpacer())))
}

// membersPage lists the members, in groups by role.
func (u *UI) membersPage(g guildconn.GuildView) fyne.CanvasObject {
	members := container.NewVBox()
	if len(g.Members) == 0 {
		msg := "No members."
		if g.Status != "online" {
			msg = "Members are shown when the guild is connected."
		}
		members.Add(para(msg))
	}
	group := func(title string, n int) {
		if len(members.Objects) > 0 {
			members.Add(gap(10))
		}
		members.Add(text(fmt.Sprintf("%s (%d)", strings.ToUpper(title), n), 11.5, colFaint, true))
		members.Add(gap(2))
	}
	// Roles shown as their own group come first, highest rank first.
	placed := map[string]bool{}
	for _, r := range g.Roles {
		if !r.Hoist || r.ID == proto.EveryoneRole {
			continue
		}
		var in []guildconn.MemberView
		for _, m := range g.Members {
			if !placed[m.UserKey] && has(m.Roles, r.ID) {
				in = append(in, m)
				placed[m.UserKey] = true
			}
		}
		if len(in) == 0 {
			continue
		}
		group(r.Name, len(in))
		for _, m := range in {
			members.Add(u.memberRow(g, m))
		}
	}
	var rest []guildconn.MemberView
	for _, m := range g.Members {
		if !placed[m.UserKey] {
			rest = append(rest, m)
		}
	}
	if len(rest) > 0 && len(placed) > 0 {
		group("Members", len(rest))
	}
	for _, m := range rest {
		members.Add(u.memberRow(g, m))
	}
	return pageFrame(usersIcon, "Members", card(22, members))
}

func (u *UI) memberRow(g guildconn.GuildView, m guildconn.MemberView) fyne.CanvasObject {
	name := shownName(m)
	if m.IsMe {
		if m.Nick == "" {
			name = u.user.Profile.DefaultName
		}
		name += " (you)"
	} else if name == "" {
		name = shortID(m.UserKey)
	}
	if m.IsOwner {
		name += "  👑"
	}
	sub := "Joined "
	if m.IsOwner {
		sub = "Owner · Joined "
	}
	if t, err := time.Parse(time.RFC3339, m.Joined); err == nil {
		sub += t.Local().Format("2 Jan 2006")
	}
	status := "Offline"
	if m.Online {
		status = "Online"
		if m.Status != "" && m.Status != "Invisible" {
			status = m.Status
		}
	}
	avName := m.Name
	if m.IsMe {
		avName = u.user.Profile.DefaultName
	} else if avName == "" {
		avName = m.UserKey[:2]
	}
	dot := statusDot(status, 11)
	pic := container.NewStack(avatar(avName, m.UserKey, 36), container.NewWithoutLayout(dot))
	dot.Move(fyne.NewPos(26, 26))
	var actions fyne.CanvasObject
	if !m.IsMe && g.Status == "online" { // Everyone gets contact tools; moderation needs permission.
		b := widget.NewButtonWithIcon("", dotsIcon, func() { u.openMember(g, m) })
		b.Importance = widget.LowImportance
		actions = b
	}
	return container.NewBorder(nil, nil, container.NewHBox(pic, gap(1)), actions,
		container.NewVBox(text(name, 14.5, nameColour(g, m.UserKey), true), text(sub, 12.5, colFaint, false)))
}

// guildPicture is the guild's picture (or initials) at a given size.
func (u *UI) guildPicture(g guildconn.GuildView, size float32) fyne.CanvasObject {
	if g.PictureHash != "" {
		if data, err := os.ReadFile(u.user.GuildPicturePath(g.ID)); err == nil {
			return pictureFromBytes("guild-"+g.PictureHash, data, size)
		}
	}
	return avatarSquare(g.Name, g.ID, size)
}

func pictureFromBytes(name string, data []byte, size float32) fyne.CanvasObject {
	img := canvas.NewImageFromResource(fyne.NewStaticResource(name, data))
	img.FillMode = canvas.ImageFillContain
	img.CornerRadius = size * 0.31
	img.SetMinSize(fyne.NewSize(size, size))
	return container.NewGridWrap(fyne.NewSize(size, size), img)
}

// guildNotice explains why a guild is grey, with what can be done.
func (u *UI) guildNotice(g guildconn.GuildView) fyne.CanvasObject {
	state := guildState(g)
	if state == "" {
		return nil
	}
	if g.Status == "offline" {
		state += ". Nexus keeps trying to reconnect."
	}
	l := wrapText(sentence(state), colIdle)
	var buttons []fyne.CanvasObject
	if g.Status == "elsewhere" {
		buttons = append(buttons, primary("Use Here", func() {
			if gc, err := u.guilds.Get(g.ID); err == nil {
				gc.UseHere()
				u.toast("Moving this guild to this computer…")
			}
		}))
	}
	switch g.Status {
	case "pending", "not_member", "declined", "removed", "banned":
		label := "Forget Guild"
		if g.Status == "pending" {
			label = "Cancel Request"
		}
		buttons = append(buttons, widget.NewButton(label, func() {
			if err := u.guilds.Forget(g.ID); err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.toast("Removed from your list")
			u.selectGuild("")
		}))
	}
	return card(16, container.NewBorder(nil, nil, nil, container.NewCenter(container.NewHBox(buttons...)), container.NewVBox(layout.NewSpacer(), l, layout.NewSpacer())))
}

// transferBanner shows an ownership offer.
func (u *UI) transferBanner(g guildconn.GuildView) fyne.CanvasObject {
	t := g.Transfer
	if t == nil {
		return nil
	}
	act := func(action, done string) func() {
		return func() {
			u.call(g.ID, proto.Request{Type: action}, func(_ proto.Response, err error) {
				if err != nil {
					u.toast(sentence(err.Error()))
					return
				}
				u.toast(done)
			})
		}
	}
	msg := "You offered ownership to " + shortID(t.To) + ". Waiting for them to accept."
	var buttons []fyne.CanvasObject
	if t.ToMe {
		msg = "The owner offers you ownership of this guild."
		buttons = append(buttons, widget.NewButton("Decline", act("transfer_decline", "Offer declined.")),
			primary("Accept", act("transfer_accept", "You are now the owner.")))
	}
	if t.FromMe {
		buttons = append(buttons, widget.NewButton("Cancel Offer", act("transfer_cancel", "Offer cancelled.")))
	}
	return card(16, container.NewBorder(nil, nil, nil, container.NewCenter(container.NewHBox(buttons...)), container.NewVBox(layout.NewSpacer(), wrapText(msg, colText), layout.NewSpacer())))
}

// ---- Server Settings (owner) ---------------------------------------

func (u *UI) openSettings(g guildconn.GuildView) {
	var p *popup
	name := widget.NewEntry()
	name.SetText(g.Name)
	about := widget.NewMultiLineEntry()
	about.SetText(g.About)
	about.Wrapping = fyne.TextWrapWord
	about.SetMinRowsVisible(14)
	counter := text("", 12, colFaint, false)
	countAbout := func(s string) {
		counter.Text = fmt.Sprintf("%d / 2000", utf8.RuneCountInString(s))
		counter.Refresh()
	}
	about.OnChanged = countAbout
	countAbout(g.About)
	showCount, showCountBox := option("Member Count", "Show how many members the guild has.", g.ShowMemberCount)
	defNotify := widget.NewSelect([]string{"Mentions Only", "All Messages"}, nil)
	defNotify.SetSelected("Mentions Only")
	if g.DefaultNotify == proto.NotifyAll {
		defNotify.SetSelected("All Messages")
	}
	problem := newErrorText()

	var newPicture []byte
	clearPicture := false
	picBox := container.NewStack(u.guildPicture(g, 72))
	choose := widget.NewButton("Change Picture", func() {
		go func() {
			path, err := choosePicture("Choose a picture for the guild")
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
					return
				}
				if path == "" {
					return
				}
				data, err := os.ReadFile(path)
				if err == nil && len(data) > 10<<20 {
					err = errors.New("The picture is larger than 10 MB.")
				}
				if err != nil {
					problem.Set(err)
					return
				}
				newPicture, clearPicture = data, false
				picBox.Objects = []fyne.CanvasObject{pictureFromBytes(fmt.Sprintf("new-%d", time.Now().UnixNano()), data, 72)}
				picBox.Refresh()
			})
		}()
	})
	remove := widget.NewButton("Remove", func() {
		newPicture, clearPicture = nil, true
		picBox.Objects = []fyne.CanvasObject{avatarSquare(name.Text, g.ID, 72)}
		picBox.Refresh()
	})

	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		n, a, show := name.Text, about.Text, showCount.Checked
		dn := proto.NotifyMentions
		if defNotify.Selected == "All Messages" {
			dn = proto.NotifyAll
		}
		req := proto.Request{Type: "update_guild", Name: &n, About: &a, ShowMemberCount: &show, ClearPicture: clearPicture, DefaultNotify: &dn}
		pic := newPicture
		save.Disable()
		go func() {
			var err error
			if len(pic) > 0 {
				req.Picture, err = imaging.ShrinkPicture(pic)
			}
			var gc *guildconn.Conn
			if err == nil {
				gc, err = u.guilds.Get(g.ID)
			}
			if err == nil {
				_, err = gc.Call(req)
			}
			fyne.Do(func() {
				save.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				p.Close()
				u.toast("Saved")
			})
		}()
	})
	cancel := widget.NewButton("Cancel", func() { p.Close() })

	picRow := container.NewBorder(nil, nil, container.NewHBox(picBox, gap(1)), nil,
		container.NewVBox(layout.NewSpacer(), container.NewHBox(choose, remove),
			text("PNG or JPG, up to 10 MB. Made square, 512×512.", 12, colFaint, false), layout.NewSpacer()))
	aboutHead := labelRow("About", counter)
	owner := "Nobody"
	if g.OwnerKey != "" {
		owner = shortID(g.OwnerKey)
		if g.OwnerKey == u.user.Profile.UserID {
			owner = "You"
		}
	}
	p = u.openPanel("overview", 600,
		panelTitle("Overview"),
		picRow,
		field("Name", name, ""),
		stack(fieldGap, aboutHead, about, hintText("Markdown works here: **bold**, *italic*, lists, links and headings.")),
		showCountBox,
		field("Default Notifications", defNotify, "Pop-ups for members who haven't chosen for themselves."),
		problem.box,
		buttonsRow(cancel, save),
		settingList("DETAILS",
			settingRow("Address", g.Address, nil),
			settingRow("Guild ID", g.ID, nil),
			settingRow("Owner", owner, nil)),
	)
}

// ---- Transfer Ownership -------------------------------------------

func (u *UI) openTransfer(g guildconn.GuildView) {
	var p *popup
	var keys, labels []string
	for _, m := range g.Members {
		if m.IsMe {
			continue
		}
		n := m.Name
		if n == "" {
			n = shortID(m.UserKey)
		}
		keys = append(keys, m.UserKey)
		labels = append(labels, n+"  ("+shortID(m.UserKey)+")")
	}
	problem := newErrorText()
	offer := primary("Offer Ownership", nil)
	offer.Disable()
	pick := widget.NewRadioGroup(labels, func(string) { offer.Enable() })
	offer.OnTapped = func() {
		target := ""
		for i, l := range labels {
			if l == pick.Selected {
				target = keys[i]
			}
		}
		if target == "" {
			return
		}
		offer.Disable()
		u.call(g.ID, proto.Request{Type: "transfer_offer", Target: target}, func(_ proto.Response, err error) {
			offer.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Offer sent. They must accept it.")
		})
	}
	var list fyne.CanvasObject = pick
	if len(labels) == 0 {
		list = para("There are no other members yet.")
	}
	p = u.openPanel("ownership", 480,
		text("Ownership", 22, colText, true),
		para("To hand the guild over, pick a member and offer it to them. They become the owner when they accept. Until then, you stay the owner."),
		list,
		problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), offer),
	)
}

// ---- Leave Guild --------------------------------------------------

func (u *UI) openLeave(g guildconn.GuildView) {
	var p *popup
	problem := newErrorText()
	leave := widget.NewButton("Leave Guild", nil)
	leave.Importance = widget.DangerImportance
	parts := []fyne.CanvasObject{text("Leave "+g.Name+"?", 22, colText, true)}
	if g.IsOwner {
		warn := wrapText("You are the owner. If you leave, the guild has no owner until its host broadcasts it and someone pairs again. To hand it over instead, use Ownership in Server Settings.", colIdle)
		confirmName := widget.NewEntry()
		confirmName.SetPlaceHolder(g.Name)
		leave.Disable()
		confirmName.OnChanged = func(s string) {
			if s == g.Name {
				leave.Enable()
			} else {
				leave.Disable()
			}
		}
		parts = append(parts, warn,
			field("Confirm", confirmName, "Type the guild's name to confirm."))
	} else {
		parts = append(parts, para("You can join again later if the guild lets you in."))
	}
	leave.OnTapped = func() {
		leave.Disable()
		u.call(g.ID, proto.Request{Type: "leave"}, func(_ proto.Response, err error) {
			if err != nil {
				leave.Enable()
				problem.Set(err)
				return
			}
			u.guilds.Remove(g.ID)
			u.user.RemoveGuild(g.ID)
			p.Close()
			u.toast("You left " + g.Name + ".")
			u.selectGuild("")
		})
	}
	parts = append(parts, problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), leave))
	p = u.openPopup(480, parts...)
}
