package ui

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// This file is a channel's chat: messages, replies, reactions, mentions,
// editing and deleting, typing.

var reactionEmoji = []string{"👍", "❤️", "😂", "🎉", "😮", "😢", "🙏", "🔥", "👀", "✅", "👏", "💯", "🤔", "😄", "😎", "🚀"}

var mentionToken = regexp.MustCompile(`<@([A-Z2-7]{52})>`)

// chatState is the open chat. It is built once per channel and updated in
// place, so typing and the scroll position survive live updates.
type chatState struct {
	guild, channel string
	msgs           []guildconn.ChatView
	more           bool
	shown          string // It is what the list shows now (to skip redraws).
	loading        bool

	replyTo, editing *guildconn.ChatView
	lastTyping       time.Time

	view          fyne.CanvasObject
	title, typing *canvas.Text
	list          *fyne.Container
	scroll        *container.Scroll
	older         *widget.Button
	entry         *chatEntry
	bar           *fyne.Container
	barText       *widget.Label
	mentions      *fyne.Container
	problem       *errorText
	members       []guildconn.MemberView
	names         map[string]string
	colours       map[string]color.Color // These are name colours from roles.
	unhidden      map[string]bool        // These are blocked people's messages, shown anyway.
	spoilers      map[string]bool        // These are messages whose spoilers are shown.
	pins          *widget.Button         // This is Pins, at the top.
	disappear     time.Duration          // The next messages disappear after this.
	scheduleAt    time.Time              // The next message is sent then.
	extras        *fyne.Container        // It is the line saying so, above the message box.
	extrasText    *widget.Label
	atBottom      bool
}

// openChat shows a channel.
func (u *UI) openChat(id string) {
	if u.chat != nil && u.chat.channel != id {
		if gc, err := u.guilds.Get(u.chat.guild); err == nil {
			gc.LeaveChannel()
		}
	}
	if u.chat == nil || u.chat.channel != id || u.chat.guild != u.selected {
		u.chat = &chatState{guild: u.selected, channel: id, atBottom: true, unhidden: map[string]bool{}, spoilers: map[string]bool{}}
	}
	u.page, u.pageSig = "chat", ""
	u.redraw()
	u.loadMessages("")
}

// loadMessages fetches the channel's messages (older ones when before is set).
func (u *UI) loadMessages(before string) {
	c := u.chat
	if c == nil || c.loading {
		return
	}
	c.loading = true
	go func() {
		var msgs []guildconn.ChatView
		var more bool
		gc, err := u.guilds.Get(c.guild)
		if err == nil {
			msgs, more, err = gc.History(c.channel, before)
		}
		fyne.Do(func() {
			c.loading = false
			if u.chat != c {
				return
			}
			if err != nil {
				c.problem.Set(err)
				return
			}
			c.msgs, c.more = msgs, more
			u.drawMessages(before == "")
		})
	}()
}

// chatPage is the chat for a guild view (reused while the channel is open).
func (u *UI) chatPage(g guildconn.GuildView) fyne.CanvasObject {
	c := u.chat
	var node *guildconn.ChannelView
	for i := range g.Channels {
		if g.Channels[i].ID == c.channel {
			node = &g.Channels[i]
		}
	}
	if node == nil {
		u.chat, u.page = nil, "about"
		return u.guildPage(g)
	}
	c.members = g.Members
	c.names = map[string]string{}
	c.colours = map[string]color.Color{}
	for _, m := range g.Members {
		n := shownName(m)
		if m.IsMe && m.Nick == "" {
			n = u.user.Profile.DefaultName
		}
		if n == "" {
			n = shortID(m.UserKey)
		}
		c.names[m.UserKey] = n
		c.colours[m.UserKey] = nameColour(g, m.UserKey)
	}
	if c.view == nil {
		u.buildChat(c)
	}
	title := "# " + node.Name
	if node.Topic != "" {
		title += "  ·  " + node.Topic
	}
	c.title.Text = title
	c.title.Refresh()
	if n := len(node.Pins); n > 0 {
		c.pins.SetText(fmt.Sprintf("Pins (%d)", n))
	} else {
		c.pins.SetText("Pins")
	}
	if until := myTimeout(g); until != "" {
		c.entry.Disable()
		c.entry.SetPlaceHolder("You are timed out until " + until + ".")
	} else if from := myWaitEnds(g); from != "" {
		c.entry.Disable()
		c.entry.SetPlaceHolder("New members can post from " + from + ".")
	} else if canIn(g, c.channel, proto.PermSendMessages) {
		c.entry.Enable()
		c.entry.SetPlaceHolder("Message #" + node.Name)
	} else {
		c.entry.Disable()
		c.entry.SetPlaceHolder("You can't send messages in this channel.")
	}
	who := g.Typing[c.channel]
	switch {
	case len(who) > 2:
		c.typing.Text = "Several people are typing…"
	default:
		c.typing.Text = strings.Join(who, " · ")
	}
	c.typing.Refresh()
	return c.view
}

func (u *UI) buildChat(c *chatState) {
	c.title = text("", 16, colText, true)
	bell := widget.NewButton("Notifications", func() {
		for _, g := range u.home.views {
			if g.ID == c.guild {
				name := ""
				for _, n := range g.Channels {
					if n.ID == c.channel {
						name = n.Name
					}
				}
				u.openNotify(g, c.channel, name)
			}
		}
	})
	bell.Importance = widget.LowImportance
	// Pins sit at the top of the chat.
	c.pins = widget.NewButtonWithIcon("Pins", pinIcon, func() { u.openPinned() })
	c.pins.Importance = widget.LowImportance
	top := container.NewStack(canvas.NewRectangle(colBG), container.New(layout.NewCustomPaddedLayout(8, 8, 24, 16),
		container.NewBorder(nil, nil, nil, container.NewHBox(c.pins, bell), container.NewVBox(layout.NewSpacer(), c.title, layout.NewSpacer()))))
	c.list = container.NewVBox()
	c.older = widget.NewButton("Load Earlier", func() {
		if len(c.msgs) > 0 {
			c.atBottom = false
			u.loadMessages(c.msgs[0].ID)
		}
	})
	c.older.Importance = widget.LowImportance
	c.older.Hide()
	c.scroll = container.NewVScroll(container.New(layout.NewCustomPaddedLayout(14, 8, 0, 0),
		container.NewVBox(container.NewCenter(c.older), c.list)))
	c.scroll.OnScrolled = func(p fyne.Position) {
		c.atBottom = p.Y+c.scroll.Size().Height >= c.scroll.Content.MinSize().Height-40
	}
	c.typing = text("", 12.5, colDim, false)
	c.problem = newErrorText()

	c.entry = newChatEntry()
	c.entry.onSend = func() { u.sendMessage() }
	c.entry.onEsc = func() { u.cancelBar() }
	c.entry.OnChanged = func(s string) {
		u.suggestMentions(s)
		if s != "" && c.editing == nil && time.Since(c.lastTyping) > 3*time.Second {
			c.lastTyping = time.Now()
			if gc, err := u.guilds.Get(c.guild); err == nil {
				go gc.Typing(c.channel)
			}
		}
	}
	send := primary("Send", u.sendMessage)
	attach := widget.NewButtonWithIcon("", clipIcon, u.sendChannelFile)
	attach.Importance = widget.LowImportance
	var emojiBtn *widget.Button
	emojiBtn = widget.NewButtonWithIcon("", smileIcon, func() { u.openEmojiPicker(&c.entry.Entry, c.guild, emojiBtn) })
	emojiBtn.Importance = widget.LowImportance
	c.barText = widget.NewLabel("")
	cancelBar := widget.NewButtonWithIcon("", xIcon, func() { u.cancelBar() })
	cancelBar.Importance = widget.LowImportance
	c.bar = container.NewBorder(nil, nil, nil, cancelBar, dim(c.barText))
	c.bar.Hide()
	c.mentions = container.NewVBox()
	c.extrasText = widget.NewLabel("")
	c.extrasText.Wrapping = fyne.TextWrapWord
	clearExtras := widget.NewButtonWithIcon("", xIcon, func() { c.disappear, c.scheduleAt = 0, time.Time{}; u.showExtras() })
	clearExtras.Importance = widget.LowImportance
	c.extras = container.NewBorder(nil, nil, icon(clockIcon, 16), clearExtras, dim(c.extrasText))
	c.extras.Hide()
	more := u.moreButton()
	hint := text("Enter sends, and Shift + Enter makes a new line. Markdown works: **bold**, *italic*, `code`, > quote, - list, ||spoiler||.", 11.5, colFaint, false)
	composer := container.New(layout.NewCustomPaddedLayout(0, 12, 20, 20), container.NewVBox(
		c.mentions, c.bar, c.extras, container.NewBorder(nil, nil, container.NewVBox(layout.NewSpacer(), container.NewHBox(attach, emojiBtn, more)), container.NewVBox(layout.NewSpacer(), send), c.entry), underButtons(hint)))
	bottom := container.NewVBox(c.problem.box, container.New(layout.NewCustomPaddedLayout(0, 0, 24, 24), c.typing), composer)
	c.view = container.NewBorder(container.NewVBox(top, sideLineH()), bottom, nil, nil, c.scroll)
	u.focusLater(c.entry)
}

// drawMessages redraws the message list if anything changed.
func (u *UI) drawMessages(toBottom bool) {
	c := u.chat
	sig := ""
	for _, m := range c.msgs {
		sig += m.ID + m.Text + m.Time
		if m.Edited {
			sig += "e"
		}
		if m.Deleted {
			sig += "d"
		}
		sig += fmt.Sprint(m.Pinned, m.Expires, m.EditCount, c.spoilers[m.ID])
		if m.Poll != nil {
			for _, ch := range m.Poll.Choices {
				sig += fmt.Sprint(ch.Count, ch.Mine, ch.Right)
			}
		}
		for _, r := range m.Reactions {
			sig += r.Emoji + string(rune('0'+r.Count))
			if r.Mine {
				sig += "m"
			}
		}
	}
	if sig == c.shown {
		return
	}
	c.shown = sig
	stick := c.atBottom || toBottom
	c.list.RemoveAll()
	if len(c.msgs) == 0 {
		c.list.Add(container.NewCenter(text("No messages yet. Say hello!", 14, colFaint, false)))
	}
	var prev *guildconn.ChatView
	for i := range c.msgs {
		m := &c.msgs[i]
		cont := prev != nil && prev.Author == m.Author && m.ReplyTo == "" && sameStretch(prev.Time, m.Time)
		c.list.Add(u.messageRow(c, m, cont))
		prev = m
	}
	if c.more {
		c.older.Show()
	} else {
		c.older.Hide()
	}
	c.list.Refresh()
	if stick {
		c.scroll.ScrollToBottom()
		c.atBottom = true
	}
}

// sameStretch reports whether two messages are within 5 minutes, so they are grouped under one name.
func sameStretch(a, b string) bool {
	ta, err1 := time.Parse(time.RFC3339, a)
	tb, err2 := time.Parse(time.RFC3339, b)
	return err1 == nil && err2 == nil && tb.Sub(ta) < 5*time.Minute
}

func msgTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	t = t.Local()
	if y, m, d := t.Date(); y == time.Now().Year() && m == time.Now().Month() && d == time.Now().Day() {
		return t.Format("15:04")
	}
	return t.Format("2 Jan, 15:04")
}

// withNames shows "<@USERID>" as "@Name" (bold when shown as Markdown).
func (c *chatState) withNames(s string, bold bool) string {
	return mentionToken.ReplaceAllStringFunc(s, func(t string) string {
		key := t[2 : len(t)-1]
		n, ok := c.names[key]
		if !ok {
			n = key[:6]
		}
		if bold {
			// Shown like a link; clicking it opens their member window.
			return "[@" + strings.NewReplacer("[", `\[`, "]", `\]`).Replace(n) + "](nexus-user:" + key + ")"
		}
		return "@" + n
	})
}

func (u *UI) messageRow(c *chatState, m *guildconn.ChatView, cont bool) fyne.CanvasObject {
	var body fyne.CanvasObject
	switch {
	case m.Deleted:
		body = text("This message was deleted.", 14, colFaint, false)
	case m.Locked:
		body = text("🔒 Encrypted with a key you don't have.", 14, colFaint, false)
	case m.Bad:
		body = text("⚠ This message's signature doesn't match its author.", 14, colDanger, false)
	default:
		if u.guilds.IsBlocked(m.Author) && !c.unhidden[m.ID] {
			// A blocked person's message is collapsed.
			id := m.ID
			show := widget.NewButton("Show", func() {
				c.unhidden[id] = true
				c.shown = ""
				u.drawMessages(false)
			})
			show.Importance = widget.LowImportance
			body = container.NewHBox(container.NewCenter(text("Message from someone you blocked.", 13.5, colFaint, false)), show)
		} else {
			body = u.messageBody(c, m)
		}
	}
	parts := []fyne.CanvasObject{}
	if m.ReplyTo != "" {
		rt := m.ReplyText
		if rt == "" {
			rt = "(message not loaded)"
		}
		parts = append(parts, container.NewHBox(icon(replyIcon, 14), text(m.ReplyName, 12.5, colDim, true), text(firstLine(rt, 80), 12.5, colFaint, false)))
	}
	if !cont {
		name := m.Name
		if name == "" {
			name = shortID(m.Author)
		}
		nc := c.colours[m.Author]
		if nc == nil {
			nc = colText
		}
		parts = append(parts, container.NewHBox(text(name, 14.5, nc, true), text(msgTime(m.Time), 12, colFaint, false)))
	}
	parts = append(parts, body)
	if m.Edited && !m.Deleted {
		parts = append(parts, text("(edited)", 11.5, colFaint, false))
	}
	// It shows whether a message is pinned or disappearing.
	if line := extraLine(m); line != nil && !m.Deleted {
		parts = append(parts, line)
	}
	if len(m.Reactions) > 0 {
		var rs []fyne.CanvasObject
		for _, r := range m.Reactions {
			r := r
			b := u.reactionButton(r.Emoji, r.Count, r.Mine, func() { u.react(m, r.Emoji, !r.Mine) })
			rs = append(rs, b)
		}
		parts = append(parts, container.New(&flow{gap: 4}, rs...))
	}
	var left fyne.CanvasObject = gap(1)
	if !cont {
		left = avatar(m.Name, m.Author, 40)
	}
	leftBox := container.New(fixedWidth{40}, container.NewVBox(left))
	var tools fyne.CanvasObject
	if !m.Deleted {
		var b *widget.Button
		b = widget.NewButtonWithIcon("", dotsIcon, func() { u.messageMenu(m, b) })
		b.Importance = widget.LowImportance
		tools = container.NewVBox(b)
	}
	row := container.NewBorder(nil, nil, container.NewHBox(leftBox, gap(1)), tools, container.NewVBox(parts...))
	padded := container.New(layout.NewCustomPaddedLayout(2, 2, 24, 16), row)
	mentioned := !m.Mine && strings.Contains(m.Text, "<@"+u.user.Profile.UserID+">")
	if mentioned {
		bg := canvas.NewRectangle(colAmberSoft)
		edge := canvas.NewRectangle(colIdle)
		edge.SetMinSize(fyne.NewSize(3, 1))
		return container.NewStack(bg, container.NewBorder(nil, nil, edge, nil, padded))
	}
	return padded
}

func itoa(n int) string { return strconv.Itoa(n) }

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " …"
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// messageMenu offers React, Reply, Edit and Delete for one message.
func (u *UI) messageMenu(m *guildconn.ChatView, anchor fyne.CanvasObject) {
	c := u.chat
	g := u.findGuild(c.guild)
	var items []*fyne.MenuItem
	if !m.Locked && !m.Bad {
		react := fyne.NewMenuItem("React", nil)
		for _, e := range reactionEmoji {
			e := e
			react.ChildMenu = appendItem(react.ChildMenu, fyne.NewMenuItem(e, func() { u.react(m, e, true) }))
		}
		for _, it := range u.customReactions(c.guild, func(token string) { u.react(m, token, true) }) {
			react.ChildMenu = appendItem(react.ChildMenu, it)
		}
		items = append(items, react, fyne.NewMenuItem("Reply", func() { u.startReply(m) }))
	}
	if m.Mine && !m.Locked && !m.Bad && m.Poll == nil && m.Forward == nil {
		items = append(items, fyne.NewMenuItem("Edit", func() { u.startEdit(m) }))
	}
	items = append(items, u.moreMessageItems(m)...)
	if m.Mine || (g != nil && canIn(*g, c.channel, proto.PermManageMessages)) {
		items = append(items, fyne.NewMenuItem("Delete", func() {
			u.confirm("Delete Message", "Delete this message? This cannot be undone.", "Delete", true, func() {
				u.chatAction(func(gc *guildconn.Conn) error { return gc.Delete(c.channel, m.ID) })
			})
		}))
	}
	if len(items) == 0 {
		return
	}
	menu := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.win.Canvas())
	if anchor == nil { // This is only for pictures.
		menu.ShowAtPosition(fyne.NewPos(1000, 160))
		return
	}
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	menu.ShowAtPosition(pos.Add(fyne.NewPos(-120, anchor.Size().Height)))
}

func appendItem(m *fyne.Menu, it *fyne.MenuItem) *fyne.Menu {
	if m == nil {
		return fyne.NewMenu("", it)
	}
	m.Items = append(m.Items, it)
	return m
}

// chatAction runs a chat action in the background and reports problems.
func (u *UI) chatAction(do func(gc *guildconn.Conn) error) {
	c := u.chat
	go func() {
		gc, err := u.guilds.Get(c.guild)
		if err == nil {
			err = do(gc)
		}
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.loadMessages("")
		})
	}()
}

func (u *UI) react(m *guildconn.ChatView, emoji string, on bool) {
	id, ch := m.ID, u.chat.channel
	u.chatAction(func(gc *guildconn.Conn) error { return gc.React(ch, id, emoji, on) })
}

// ---- composer ---------------------------------------------------------------

func (u *UI) startReply(m *guildconn.ChatView) {
	c := u.chat
	c.editing, c.replyTo = nil, m
	c.barText.SetText("Replying to " + m.Name)
	c.bar.Show()
	c.view.Refresh() // Lay the message box out again, with the bar above it.
	u.win.Canvas().Focus(c.entry)
}

func (u *UI) startEdit(m *guildconn.ChatView) {
	c := u.chat
	c.replyTo, c.editing = nil, m
	c.barText.SetText("Editing your message. Esc cancels.")
	c.bar.Show()
	c.view.Refresh()
	c.entry.SetText(c.withNames(m.Text, false))
	u.win.Canvas().Focus(c.entry)
}

func (u *UI) cancelBar() {
	c := u.chat
	if c.editing != nil {
		c.entry.SetText("")
	}
	c.replyTo, c.editing = nil, nil
	c.bar.Hide()
	c.view.Refresh()
}

func (u *UI) sendMessage() {
	c := u.chat
	if c == nil {
		return
	}
	txt := c.entry.Text
	if strings.TrimSpace(txt) == "" {
		return
	}
	c.problem.Set(nil)
	if c.editing == nil {
		for _, g := range u.home.views {
			if g.ID != c.guild {
				continue
			}
			if done, err := u.chatCommand(g, txt); done {
				if err != nil {
					c.problem.Set(err)
					return
				}
				c.entry.SetText("")
				return
			}
		}
		if t := strings.TrimSpace(txt); strings.HasPrefix(t, "//") {
			txt = t[1:] // Typing "//" sends a message that starts with "/".
		}
	}
	ch, reply, edit := c.channel, "", c.editing
	if c.replyTo != nil {
		reply = c.replyTo.ID
	}
	c.entry.SetText("")
	c.replyTo, c.editing = nil, nil
	c.bar.Hide()
	c.mentions.RemoveAll()
	c.atBottom = edit == nil
	var opts guildconn.SendOptions
	if edit == nil {
		opts = c.sendOptions() // These are disappearing and scheduled messages.
	}
	go func() {
		gc, err := u.guilds.Get(c.guild)
		if err == nil {
			if edit != nil {
				err = gc.Edit(ch, edit.ID, txt)
			} else {
				err = gc.SendWith(ch, txt, reply, opts)
			}
		}
		fyne.Do(func() {
			if err != nil {
				c.entry.SetText(txt) // Give the text back.
				c.problem.Set(err)
				if !opts.At.IsZero() {
					c.scheduleAt = opts.At
				}
				u.showExtras()
				return
			}
			u.showExtras()
			if !opts.At.IsZero() {
				u.toast("Scheduled for " + atText(opts.At))
			}
			u.loadMessages("")
		})
	}()
}

// suggestMentions lists members whose name starts with what follows "@".
func (u *UI) suggestMentions(s string) {
	c := u.chat
	c.mentions.RemoveAll()
	defer func() { // Lay the message box out again, with the list above it.
		c.mentions.Refresh()
		if c.view != nil {
			c.view.Refresh()
		}
	}()
	at := strings.LastIndexByte(s, '@')
	if at < 0 || (at > 0 && !strings.ContainsAny(s[at-1:at], " \n")) {
		return
	}
	word := s[at+1:]
	if strings.ContainsAny(word, " \n") {
		return
	}
	shown := 0
	for _, m := range c.members {
		name := c.names[m.UserKey]
		if m.IsMe || !strings.HasPrefix(strings.ToLower(name), strings.ToLower(word)) || shown >= 6 {
			continue
		}
		shown++
		n := name
		b := widget.NewButton("@"+n, func() {
			c.entry.SetText(s[:at] + "@" + n + " ")
			c.entry.CursorColumn = len([]rune(c.entry.Text))
			c.mentions.RemoveAll()
			u.win.Canvas().Focus(c.entry)
		})
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		c.mentions.Add(b)
	}
}

// chatEntry is the message box: Enter sends, Shift + Enter makes a new
// line, Esc cancels a reply or an edit.
type chatEntry struct {
	widget.Entry
	shift  bool
	onSend func()
	onEsc  func()
}

func newChatEntry() *chatEntry {
	e := &chatEntry{}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	e.SetMinRowsVisible(1)
	return e
}

func (e *chatEntry) KeyDown(k *fyne.KeyEvent) {
	if k.Name == desktop.KeyShiftLeft || k.Name == desktop.KeyShiftRight {
		e.shift = true
	}
	e.Entry.KeyDown(k)
}

func (e *chatEntry) KeyUp(k *fyne.KeyEvent) {
	if k.Name == desktop.KeyShiftLeft || k.Name == desktop.KeyShiftRight {
		e.shift = false
	}
	e.Entry.KeyUp(k)
}

func (e *chatEntry) TypedKey(k *fyne.KeyEvent) {
	switch {
	case (k.Name == fyne.KeyReturn || k.Name == fyne.KeyEnter) && !e.shift:
		if e.onSend != nil {
			e.onSend()
		}
		return
	case k.Name == fyne.KeyEscape && e.onEsc != nil:
		e.onEsc()
		return
	}
	e.Entry.TypedKey(k)
}

// pollChat picks up new messages (the guild connection keeps them; this
// reads them without asking the guild). Called on every live update.
func (u *UI) pollChat() {
	c := u.chat
	if c == nil || c.loading || c.view == nil {
		return
	}
	go func() {
		gc, err := u.guilds.Get(c.guild)
		if err != nil {
			return
		}
		msgs, more, err := gc.History(c.channel, "")
		if err != nil {
			return
		}
		fyne.Do(func() {
			if u.chat == c && !c.loading {
				c.msgs, c.more = msgs, more
				u.drawMessages(false)
			}
		})
	}()
}

// underButtons lines the note under the message box up with the first
// button's icon (the paper clip), not the button's edge.
func underButtons(o fyne.CanvasObject) fyne.CanvasObject {
	pad := theme.InnerPadding()
	return container.New(layout.NewCustomPaddedLayout(0, 0, pad, 0), o)
}
