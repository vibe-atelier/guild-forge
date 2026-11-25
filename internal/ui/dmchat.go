package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
)

// This file is the private chat with a contact.

type dmChat struct {
	contact string
	msgs    []guildconn.DMView
	shown   string

	replyTo, editing *guildconn.DMView
	lastTyping       time.Time

	view     fyne.CanvasObject
	head     *fyne.Container
	list     *fyne.Container
	scroll   *container.Scroll
	entry    *chatEntry
	bar      *fyne.Container
	barText  *widget.Label
	typing   *canvas.Text
	problem  *errorText
	atBottom bool
	spoilers map[string]bool // These are messages whose spoilers are shown.

	// Search: only in what is already on this computer.
	searchBar   *fyne.Container
	searchEntry *widget.Entry
	searchCount *canvas.Text
	rows        map[string]fyne.CanvasObject // It maps each message ID to its row, for Show Message.
	jumpTo      string                       // It is a message to scroll to after drawing.
}

// openDM shows the chat with a contact.
func (u *UI) openDM(id string) {
	u.leaveChat()
	if u.dm == nil || u.dm.contact != id {
		u.guilds.LeaveChat()
		u.dm = &dmChat{contact: id, atBottom: true}
	}
	u.page, u.pageSig = "", ""
	u.selected = "dm:" + id
	u.redraw()
	u.pollDM()
}

func (u *UI) contactView(id string) *guildconn.ContactView {
	for _, c := range u.guilds.Contacts() {
		if c.UserID == id {
			c := c
			return &c
		}
	}
	return nil
}

// pollDM reads the chat from disk and redraws it if it changed.
func (u *UI) pollDM() {
	d := u.dm
	if d == nil || u.guilds == nil {
		return
	}
	go func() {
		msgs, err := u.guilds.Chat(d.contact)
		fyne.Do(func() {
			if u.dm != d || d.view == nil {
				return
			}
			if err != nil {
				d.problem.Set(err)
				return
			}
			d.msgs = msgs
			u.drawDM()
		})
	}()
}

func (u *UI) dmPage(id string) fyne.CanvasObject {
	d := u.dm
	c := u.contactView(id)
	if d == nil || c == nil || c.State != guildconn.ContactActive {
		u.selected = ""
		return u.profilePage()
	}
	if d.view == nil {
		u.buildDM(d)
	}
	// The header shows the name, badge, status, and the contact's tools.
	status := "Offline"
	switch {
	case !c.Reachable:
		status = "No shared guild right now: messages can't be sent until you share one again."
	case c.Online:
		status = statusNames[c.Status]
		if status == "" {
			status = "Online"
		}
	}
	name := container.NewHBox(text(c.Name, 16, colText, true))
	if b := badge(c.Verified); b != nil {
		name.Add(gap(1))
		name.Add(container.NewCenter(b))
	}
	verify := u.verifyButton(*c)
	var more *widget.Button
	more = widget.NewButtonWithIcon("", dotsIcon, func() { u.contactMenu(*c, more) })
	more.Importance = widget.LowImportance
	find := newTipButton(u, "Search Messages", searchIcon, u.openDMSearch)
	find.Importance = widget.LowImportance
	d.head.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil, contactFace(*c, 30), container.NewHBox(find, verify, more),
		container.NewVBox(name, text(status, 11.5, colFaint, false)))}
	d.head.Refresh()
	if c.Reachable {
		d.entry.Enable()
		d.entry.SetPlaceHolder("Message " + c.Name)
	} else {
		d.entry.Disable()
		d.entry.SetPlaceHolder("You don't share a guild with " + c.Name + " right now.")
	}
	if c.Typing {
		d.typing.Text = c.Name + " is typing…"
	} else {
		d.typing.Text = ""
	}
	d.typing.Refresh()
	return d.view
}

func (u *UI) buildDM(d *dmChat) {
	d.head = container.NewStack()
	top := container.NewStack(canvas.NewRectangle(colBG), container.New(layout.NewCustomPaddedLayout(10, 10, 24, 16), d.head))
	d.list = container.NewVBox()
	d.scroll = container.NewVScroll(container.New(layout.NewCustomPaddedLayout(14, 8, 0, 0), d.list))
	d.scroll.OnScrolled = func(p fyne.Position) {
		d.atBottom = p.Y+d.scroll.Size().Height >= d.scroll.Content.MinSize().Height-40
	}
	d.typing = text("", 12.5, colDim, false)
	d.problem = newErrorText()
	d.entry = newChatEntry()
	d.entry.onSend = u.sendDM
	d.entry.onEsc = u.cancelDMBar
	d.entry.OnChanged = func(s string) {
		if s != "" && d.editing == nil && time.Since(d.lastTyping) > 3*time.Second {
			d.lastTyping = time.Now()
			go u.guilds.TypingDM(d.contact)
		}
	}
	send := primary("Send", u.sendDM)
	attach := widget.NewButtonWithIcon("", clipIcon, u.sendDMFile)
	attach.Importance = widget.LowImportance
	d.barText = widget.NewLabel("")
	x := widget.NewButtonWithIcon("", xIcon, u.cancelDMBar)
	x.Importance = widget.LowImportance
	d.bar = container.NewBorder(nil, nil, nil, x, dim(d.barText))
	d.bar.Hide()
	hint := text("Enter sends, Shift + Enter makes a new line. ||spoiler|| hides text until clicked. Only the two of you can read these messages.", 11.5, colFaint, false)
	var emojiBtn *widget.Button
	emojiBtn = widget.NewButtonWithIcon("", smileIcon, func() { u.openEmojiPicker(&d.entry.Entry, "", emojiBtn) })
	emojiBtn.Importance = widget.LowImportance
	composer := container.New(layout.NewCustomPaddedLayout(0, 12, 20, 20), container.NewVBox(d.bar,
		container.NewBorder(nil, nil, container.NewVBox(layout.NewSpacer(), container.NewHBox(attach, emojiBtn)), container.NewVBox(layout.NewSpacer(), send), d.entry), underButtons(hint)))
	bottom := container.NewVBox(d.problem.box, container.New(layout.NewCustomPaddedLayout(0, 0, 24, 24), d.typing), composer)
	// The search bar, under the header while searching.
	d.searchEntry = widget.NewEntry()
	d.searchEntry.SetPlaceHolder("Search Messages")
	d.searchEntry.OnChanged = func(string) { u.drawDM() }
	d.searchCount = text("", 12.5, colFaint, false)
	closeSearch := widget.NewButtonWithIcon("", xIcon, u.closeDMSearch)
	closeSearch.Importance = widget.LowImportance
	d.searchBar = container.New(layout.NewCustomPaddedLayout(8, 8, 24, 16), container.NewBorder(nil, nil, icon(searchIcon, 18),
		container.NewHBox(container.NewCenter(d.searchCount), closeSearch), d.searchEntry))
	d.searchBar.Hide()
	d.view = container.NewBorder(container.NewVBox(top, sideLineH(), d.searchBar), bottom, nil, nil, d.scroll)
	u.focusLater(d.entry)
}

// openDMSearch shows the search bar.
func (u *UI) openDMSearch() {
	d := u.dm
	if d == nil || d.searchBar == nil {
		return
	}
	d.searchBar.Show()
	d.view.Refresh() // Make room for the bar before it is drawn.
	u.win.Canvas().Focus(d.searchEntry)
}

// closeDMSearch hides the search bar and shows the whole chat again.
func (u *UI) closeDMSearch() {
	d := u.dm
	if d == nil || d.searchBar == nil {
		return
	}
	d.searchBar.Hide()
	d.view.Refresh()
	d.searchEntry.SetText("") // This redraws the chat.
	u.focusLater(d.entry)
}

// searching is the search in the bar, or "" when not searching.
func (d *dmChat) searching() string {
	if d.searchBar == nil || !d.searchBar.Visible() {
		return ""
	}
	return strings.TrimSpace(d.searchEntry.Text)
}

// showMessage leaves the search and scrolls the chat to a message.
func (u *UI) showMessage(id string) {
	d := u.dm
	if d == nil {
		return
	}
	d.jumpTo = id
	u.closeDMSearch()
}

func (u *UI) drawDM() {
	d := u.dm
	query := d.searching()
	sig := fmt.Sprintf("%+v%v%q", d.msgs, d.spoilers, query)
	if sig == d.shown && d.jumpTo == "" {
		return
	}
	d.shown = sig
	stick := d.atBottom
	c := u.contactView(d.contact)
	theirName := "Them"
	if c != nil {
		theirName = c.Name
	}
	d.list.RemoveAll()
	d.rows = map[string]fyne.CanvasObject{}
	if query != "" {
		u.drawDMResults(query, theirName)
		return
	}
	if d.searchCount != nil && d.searchCount.Text != "" {
		d.searchCount.Text = ""
		d.searchBar.Refresh()
	}
	if len(d.msgs) == 0 {
		d.list.Add(container.NewCenter(text("No messages yet. Say hello!", 14, colFaint, false)))
	}
	var prevMine *bool
	var prevTime string
	for i := range d.msgs {
		m := &d.msgs[i]
		cont := prevMine != nil && *prevMine == m.Mine && m.ReplyTo == "" && sameStretch(prevTime, m.Time)
		row := u.dmRow(m, cont, theirName)
		d.rows[m.ID] = row
		d.list.Add(row)
		mine := m.Mine
		prevMine, prevTime = &mine, m.Time
	}
	d.list.Refresh()
	if id := d.jumpTo; id != "" {
		d.jumpTo = ""
		if row := d.rows[id]; row != nil {
			// The list sits 14 below the scroll's top (its padding).
			y := row.Position().Y + 14 - 12
			maxY := d.scroll.Content.MinSize().Height - d.scroll.Size().Height
			d.scroll.Offset = fyne.NewPos(0, max(0, min(y, maxY)))
			d.scroll.Refresh()
			d.atBottom = y >= maxY
			return
		}
	}
	if stick {
		d.scroll.ScrollToBottom()
	}
}

// drawDMResults lists the messages that match a search, each with the day
// it was sent and a button that shows it in the chat.
func (u *UI) drawDMResults(query, theirName string) {
	d := u.dm
	found := guildconn.SearchDMs(d.msgs, query)
	count := "No Results"
	switch {
	case len(found) == 1:
		count = "1 Result"
	case len(found) > 1:
		count = itoa(len(found)) + " Results"
	}
	if d.searchCount.Text != count {
		d.searchCount.Text = count
		d.searchBar.Refresh() // Measure the bar again for the new count.
	}
	if len(found) == 0 {
		d.list.Add(container.NewCenter(text("No messages match your search.", 14, colFaint, false)))
	}
	for i := len(found) - 1; i >= 0; i-- { // The results are newest first.
		m := found[i]
		id := m.ID
		show := widget.NewButton("Show Message", func() { u.showMessage(id) })
		show.Importance = widget.LowImportance
		day := text(searchDay(m.Time), 12, colFaint, false)
		d.list.Add(container.New(layout.NewCustomPaddedLayout(6, 0, 24, 16), container.NewBorder(nil, nil, nil, show, container.NewVBox(layout.NewSpacer(), day))))
		d.list.Add(u.dmRow(&found[i], false, theirName))
	}
	d.list.Refresh()
	d.scroll.ScrollToTop()
}

// searchDay is a result's date, such as "Monday, 2 Jan 2026".
func searchDay(rfc string) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return ""
	}
	return t.Local().Format("Monday, 2 Jan 2006")
}

func (u *UI) dmRow(m *guildconn.DMView, cont bool, theirName string) fyne.CanvasObject {
	name, id := theirName, u.dm.contact
	if m.Mine {
		name, id = u.user.Profile.DefaultName, u.user.Profile.UserID
	}
	var body fyne.CanvasObject
	switch {
	case m.Deleted:
		body = text("This message was deleted.", 14, colFaint, false)
	case m.File != "":
		body = u.fileBody(m)
	default:
		body = u.dmBody(*m)
	}
	parts := []fyne.CanvasObject{}
	if m.ReplyTo != "" {
		rt := m.ReplyText
		if rt == "" {
			rt = "(a message not here)"
		}
		parts = append(parts, container.NewHBox(icon(replyIcon, 14), text(firstLine(rt, 80), 12.5, colFaint, false)))
	}
	if !cont {
		parts = append(parts, container.NewHBox(text(name, 14.5, colText, true), text(msgTime(m.Time), 12, colFaint, false)))
	}
	parts = append(parts, body)
	if m.Edited && !m.Deleted {
		parts = append(parts, text("(edited)", 11.5, colFaint, false))
	}
	if len(m.Reactions) > 0 {
		var rs []fyne.CanvasObject
		for _, r := range m.Reactions {
			r := r
			b := widget.NewButton(r.Emoji+" "+itoa(r.Count), func() { u.reactDM(m, r.Emoji, !r.Mine) })
			if r.Mine {
				b.Importance = widget.HighImportance
			}
			rs = append(rs, b)
		}
		parts = append(parts, container.New(&flow{gap: 4}, rs...))
	}
	var left fyne.CanvasObject = gap(1)
	if !cont {
		left = avatar(name, id, 40)
	}
	var tools fyne.CanvasObject
	if !m.Deleted {
		var b *widget.Button
		b = widget.NewButtonWithIcon("", dotsIcon, func() { u.dmMenu(m, b) })
		b.Importance = widget.LowImportance
		tools = container.NewVBox(b)
		if m.Mine { // Your messages get ticks, as in other messaging apps.
			tools = container.NewHBox(container.NewVBox(gap(4), icon(ticksFor(*m), 18)), container.NewVBox(b))
		}
	}
	row := container.NewBorder(nil, nil, container.NewHBox(container.New(fixedWidth{40}, container.NewVBox(left)), gap(1)), tools, container.NewVBox(parts...))
	return container.New(layout.NewCustomPaddedLayout(2, 2, 24, 16), row)
}

// fileBody shows a picture inline, or a file to open.
func (u *UI) fileBody(m *guildconn.DMView) fyne.CanvasObject {
	full := filepath.Join(u.user.Dir, m.FilePath)
	open := widget.NewButton(fmt.Sprintf("📎 %s (%s)", m.File, sizeText(m.FileSize)), func() {
		u.app.OpenURL(&url.URL{Scheme: "file", Path: filepath.ToSlash(full)})
	})
	open.Importance = widget.LowImportance
	low := strings.ToLower(m.File)
	if strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") {
		if data, err := os.ReadFile(full); err == nil {
			img := canvas.NewImageFromResource(fyne.NewStaticResource(m.ID+"-"+m.File, data))
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(320, 220))
			return container.NewVBox(container.NewHBox(img), container.NewHBox(open))
		}
	}
	return container.NewHBox(open)
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

func (u *UI) dmMenu(m *guildconn.DMView, anchor fyne.CanvasObject) {
	react := fyne.NewMenuItem("React", nil)
	for _, e := range reactionEmoji {
		e := e
		react.ChildMenu = appendItem(react.ChildMenu, fyne.NewMenuItem(e, func() { u.reactDM(m, e, true) }))
	}
	items := []*fyne.MenuItem{react, fyne.NewMenuItem("Reply", func() {
		u.dm.replyTo, u.dm.editing = m, nil
		u.dm.barText.SetText("Replying")
		u.dm.bar.Show()
		u.dm.view.Refresh()
		u.win.Canvas().Focus(u.dm.entry)
	})}
	if m.Mine && m.File == "" {
		items = append(items, fyne.NewMenuItem("Edit", func() {
			u.dm.editing, u.dm.replyTo = m, nil
			u.dm.barText.SetText("Editing your message. Esc cancels.")
			u.dm.bar.Show()
			u.dm.view.Refresh()
			u.dm.entry.SetText(m.Text)
			u.win.Canvas().Focus(u.dm.entry)
		}))
	}
	if m.Mine {
		items = append(items, fyne.NewMenuItem("Delete", func() {
			u.confirm("Delete Message", "Delete this message for both of you? This cannot be undone.", "Delete", true, func() {
				id, c := m.ID, u.dm.contact
				u.dmAction(func() error { return u.guilds.DeleteDM(c, id) })
			})
		}))
	}
	menu := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.win.Canvas())
	if anchor == nil {
		menu.ShowAtPosition(fyne.NewPos(1000, 160))
		return
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	menu.ShowAtPosition(pos.Add(fyne.NewPos(-120, anchor.Size().Height)))
}

func (u *UI) dmAction(do func() error) {
	go func() {
		err := do()
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.pollDM()
		})
	}()
}

func (u *UI) reactDM(m *guildconn.DMView, emoji string, on bool) {
	id, c := m.ID, u.dm.contact
	u.dmAction(func() error { return u.guilds.ReactDM(c, id, emoji, on) })
}

func (u *UI) cancelDMBar() {
	d := u.dm
	if d == nil {
		return
	}
	if d.editing != nil {
		d.entry.SetText("")
	}
	d.replyTo, d.editing = nil, nil
	d.bar.Hide()
}

func (u *UI) sendDM() {
	d := u.dm
	if d == nil || strings.TrimSpace(d.entry.Text) == "" {
		return
	}
	txt, c, edit, reply := d.entry.Text, d.contact, d.editing, ""
	if d.replyTo != nil {
		reply = d.replyTo.ID
	}
	d.entry.SetText("")
	d.replyTo, d.editing = nil, nil
	d.bar.Hide()
	d.problem.Set(nil)
	d.atBottom = edit == nil
	go func() {
		var err error
		if edit != nil {
			err = u.guilds.EditDM(c, edit.ID, txt)
		} else {
			err = u.guilds.SendDM(c, txt, reply)
		}
		fyne.Do(func() {
			if err != nil {
				d.entry.SetText(txt)
				d.problem.Set(err)
				return
			}
			u.pollDM()
		})
	}()
}

func (u *UI) sendDMFile() {
	d := u.dm
	go func() {
		path, err := chooseFile("Choose a picture or file to send")
		if err != nil || path == "" {
			if err != nil {
				fyne.Do(func() { d.problem.Set(err) })
			}
			return
		}
		data, err := os.ReadFile(path)
		if err == nil {
			d.atBottom = true
			err = u.guilds.SendFile(d.contact, filepath.Base(path), data)
		}
		fyne.Do(func() {
			if err != nil {
				d.problem.Set(err)
				return
			}
			u.pollDM()
		})
	}()
}

// ---- the contact's tools --------------------------------------------------------

func (u *UI) contactMenu(c guildconn.ContactView, anchor fyne.CanvasObject) {
	items := []*fyne.MenuItem{
		u.verifyItem(c),
		fyne.NewMenuItem("Note", func() { u.openNote(c) }),
		fyne.NewMenuItem("Copy ID", func() { u.app.Clipboard().SetContent(c.UserID); u.toast("ID Copied") }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Remove Friend", func() {
			u.confirm("Remove "+c.Name, "Remove them from your friends? Your chat stays on this computer. To talk again, send a new friend request.", "Remove", true, func() {
				u.guilds.RemoveContact(c.UserID)
				u.selectGuild("")
			})
		}),
		fyne.NewMenuItem("Block", func() { u.blockPerson(c.UserID, c.Name) }),
	}
	menu := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.win.Canvas())
	pos := fyne.NewPos(1000, 60)
	if anchor != nil {
		pos = fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor).Add(fyne.NewPos(-140, anchor.Size().Height))
	}
	menu.ShowAtPosition(pos)
}

// openVerify shows your code for them, and a field for theirs.
func (u *UI) openVerify(c guildconn.ContactView) {
	var p *popup
	mine := u.guilds.MyCode(c.UserID)
	code := widget.NewEntry()
	code.SetPlaceHolder("ABC-DEF-GHJ")
	problem := newErrorText()
	verify := primary("Verify", func() {
		problem.Set(nil)
		if err := u.guilds.Verify(c.UserID, code.Text); err != nil {
			problem.Set(err)
			return
		}
		p.Close()
		u.toast(c.Name + " is verified")
		u.redraw()
	})
	code.OnSubmitted = func(string) { verify.OnTapped() }
	big := text(mine, 30, colText, true)
	big.TextStyle.Monospace = true
	state := "Not verified yet."
	switch c.Verified {
	case "verified":
		state = "Verified. Their keys haven't changed since."
	case "changed":
		state = "Warning: their keys changed since you verified them. Verify again before trusting them."
	}
	p = u.openPopup(520, text("Verify "+c.Name, 22, colText, true), para(state),
		stack(fieldGap, text("Your Code", 13, colDim, true), container.NewCenter(big)),
		para("Tell "+c.Name+" this code by another way (in person, or on a call), so they can verify you."),
		field("Their Code", code, "Ask "+c.Name+" for the code their app shows for you, and type it here."),
		problem.box, buttonsRow(widget.NewButton("Close", func() { p.Close() }), verify))
	u.win.Canvas().Focus(code)
}

func (u *UI) openNote(c guildconn.ContactView) {
	var p *popup
	note := widget.NewMultiLineEntry()
	note.SetText(c.Note)
	note.SetMinRowsVisible(4)
	p = u.openPopup(480, text("Note", 22, colText, true), para("A private note about "+c.Name+". Only you can see it."),
		note, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), primary("Save", func() {
			u.guilds.SetNote(c.UserID, note.Text)
			p.Close()
			u.toast("Note saved")
		})))
}

// Ticks on your messages: one grey tick when sent, two grey ticks
// when their app received it, two red ticks when they read it.
var (
	tickSent = svg("tick-sent.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M5 12.5l4 4 9-9" fill="none" stroke="#8e8e9a" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`)
	tickGot  = svg("tick-got.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#8e8e9a" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12.5l4 4 9-9"/><path d="M11.5 15.5l1 1 9-9"/></g></svg>`)
	tickRead = svg("tick-read.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#f04a55" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12.5l4 4 9-9"/><path d="M11.5 15.5l1 1 9-9"/></g></svg>`)
)

func ticksFor(m guildconn.DMView) fyne.Resource {
	switch {
	case m.Read:
		return tickRead
	case m.Got:
		return tickGot
	}
	return tickSent
}

// verifyButton is Verify, or Remove Verification once verified.
func (u *UI) verifyButton(c guildconn.ContactView) *widget.Button {
	b := widget.NewButton("Verify", func() { u.openVerify(c) })
	if c.Verified == "verified" {
		b = widget.NewButton("Remove Verification", func() { u.unverify(c) })
	}
	b.Importance = widget.LowImportance
	return b
}

func (u *UI) verifyItem(c guildconn.ContactView) *fyne.MenuItem {
	if c.Verified == "verified" {
		return fyne.NewMenuItem("Remove Verification", func() { u.unverify(c) })
	}
	return fyne.NewMenuItem("Verify", func() { u.openVerify(c) })
}

func (u *UI) unverify(c guildconn.ContactView) {
	u.confirm("Remove Verification", "Remove "+c.Name+"'s verification? You can verify them again with your codes.", "Remove", true, func() {
		u.guilds.Unverify(c.UserID)
		u.toast("Verification Removed")
		u.redraw()
	})
}
