package ui

// This file shows the chat features from Telegram in the window:
// polls and quizzes, spoilers, forwarded messages, pins, disappearing and
// scheduled messages, and Edit History.

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

var pinIcon = svg("pin.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><path d="M9 4h6l-1 5 3 3v2H7v-2l3-3z"/><path d="M12 14v6"/></g></svg>`)
var clockIcon = svg("clock.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/></g></svg>`)

// ---- spoilers ------------------------------------------------------------------------------

var spoilerRe = regexp.MustCompile(`\|\|(.+?)\|\|`)

// hideSpoilers covers ||spoiler|| text with blocks until it is shown.
func hideSpoilers(s string) (string, bool) {
	found := false
	out := spoilerRe.ReplaceAllStringFunc(s, func(m string) string {
		found = true
		n := len([]rune(m)) - 4
		return strings.Repeat("▒", max(3, min(n, 40)))
	})
	return out, found
}

// showSpoilers just drops the bars.
func showSpoilers(s string) string { return spoilerRe.ReplaceAllString(s, "$1") }

// ---- extras on a message ---------------------------------------------------------------------

// messageExtras draws a forwarded message's origin, a poll, and spoilers;
// it returns nil when the plain text is all there is.
func (u *UI) messageExtras(c *chatState, m *guildconn.ChatView, plain func(string) fyne.CanvasObject) fyne.CanvasObject {
	var parts []fyne.CanvasObject
	if f := m.Forward; f != nil {
		parts = append(parts, text("Forwarded from "+f.Name+", "+f.Where, 12.5, colDim, false))
	}
	if m.Poll != nil {
		parts = append(parts, u.pollBlock(c, m))
	} else if m.Text != "" {
		shown := m.Text
		if hidden, has := hideSpoilers(m.Text); has {
			if c.spoilers[m.ID] {
				shown = showSpoilers(m.Text)
			} else {
				shown = hidden
				id := m.ID
				show := widget.NewButton("Show Spoiler", func() {
					c.spoilers[id] = true
					c.shown = ""
					u.drawMessages(false)
				})
				show.Importance = widget.LowImportance
				parts = append(parts, plain(shown), container.NewHBox(show))
				shown = ""
			}
		} else if m.Forward == nil {
			return nil
		}
		if shown != "" {
			parts = append(parts, plain(shown))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	if m.Forward != nil { // A bar runs down the left, like a quote.
		edge := canvas.NewRectangle(colLine)
		edge.SetMinSize(fyne.NewSize(3, 1))
		return container.NewBorder(nil, nil, container.NewHBox(edge, gap(1)), nil, container.NewVBox(parts...))
	}
	return container.NewVBox(parts...)
}

// pollBlock is a poll: the question, then a button and bar for each choice.
func (u *UI) pollBlock(c *chatState, m *guildconn.ChatView) fyne.CanvasObject {
	p := m.Poll
	kind := "Poll"
	if p.Quiz {
		kind = "Quiz"
	}
	box := container.NewVBox(text(kind, 11.5, colFaint, true), wrapText(p.Question, colText))
	id, ch := m.ID, c.channel
	for i, choice := range p.Choices {
		i, choice := i, choice
		frac := float32(0)
		if p.Total > 0 {
			frac = float32(choice.Count) / float32(p.Total)
		}
		label := choice.Text
		switch {
		case choice.Right:
			label = "✓ " + label
		case choice.Mine:
			label = "• " + label
		}
		var fill color.Color = colHover
		if choice.Mine {
			fill = pollMine
		}
		if p.Quiz && p.Voted && choice.Mine && !choice.Right {
			fill = pollWrong
		}
		bar := canvas.NewRectangle(fill)
		bar.CornerRadius = 6
		count := text(fmt.Sprintf("%d", choice.Count), 12.5, colDim, false)
		row := container.NewStack(container.New(&barFill{frac: frac}, bar),
			container.New(layout.NewCustomPaddedLayout(6, 6, 10, 10), container.NewBorder(nil, nil, nil, count, text(label, 14, colText, false))))
		var tap fyne.CanvasObject = row
		if !(p.Quiz && p.Voted) { // A quiz answer stays.
			choiceTo := i
			if choice.Mine {
				choiceTo = -1 // Clicking your vote again takes it back.
			}
			t := newTapRow(row, false, func() {
				u.chatAction(func(gc *guildconn.Conn) error { return gc.Vote(ch, id, choiceTo) })
			})
			t.base = colPanel
			tap = t
		}
		box.Add(tap)
	}
	votes := "votes"
	if p.Total == 1 {
		votes = "vote"
	}
	box.Add(text(fmt.Sprintf("%d %s", p.Total, votes), 12, colFaint, false))
	return container.New(fixedWidth{420}, box)
}

// barFill sizes its one object to a part of the width.
type barFill struct{ frac float32 }

func (b *barFill) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(1, 1) }
func (b *barFill) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	objs[0].Resize(fyne.NewSize(s.Width*max(0, min(1, b.frac)), s.Height))
	objs[0].Move(fyne.NewPos(0, 0))
}

// extraLine is the small line under a message: pinned, and when it
// disappears.
func extraLine(m *guildconn.ChatView) fyne.CanvasObject {
	var bits []fyne.CanvasObject
	if m.Pinned {
		bits = append(bits, icon(pinIcon, 13), text("Pinned", 11.5, colFaint, false))
	}
	if m.Expires != "" {
		if t, err := time.Parse(time.RFC3339, m.Expires); err == nil {
			bits = append(bits, icon(clockIcon, 13), text("Disappears "+atText(t.Local()), 11.5, colFaint, false))
		}
	}
	if len(bits) == 0 {
		return nil
	}
	return container.NewHBox(bits...)
}

// atText is a time like "today at 18:00" or "on Mon 23 Nov at 09:30".
func atText(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "today at " + t.Format("15:04")
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "tomorrow at " + t.Format("15:04")
	}
	return "on " + t.Format("Mon 2 Jan") + " at " + t.Format("15:04")
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// ---- the message menu's extras --------------------------------------------------------------

// moreMessageItems are Pin, Forward and Edit History for a message.
func (u *UI) moreMessageItems(m *guildconn.ChatView) []*fyne.MenuItem {
	c := u.chat
	g := u.findGuild(c.guild)
	var items []*fyne.MenuItem
	if g != nil && canIn(*g, c.channel, proto.PermPinMessages) {
		label := "Pin"
		if m.Pinned {
			label = "Unpin"
		}
		id, ch, on := m.ID, c.channel, !m.Pinned
		items = append(items, fyne.NewMenuItem(label, func() {
			u.chatAction(func(gc *guildconn.Conn) error { return gc.Pin(ch, id, on) })
		}))
	}
	if !m.Locked && !m.Bad && m.Poll == nil {
		items = append(items, fyne.NewMenuItem("Forward", func() { u.openForward(*m) }))
	}
	if m.EditCount > 0 {
		items = append(items, fyne.NewMenuItem("Edit History", func() { u.openEditHistory(*m) }))
	}
	return items
}

// ---- the message box's extras --------------------------------------------------------------

var disappearNames = []string{"Off", "1 Hour", "1 Day", "1 Week"}
var disappearTimes = []time.Duration{0, time.Hour, 24 * time.Hour, 7 * 24 * time.Hour}

// moreButton is the clock by the message box: polls, scheduling and
// disappearing messages.
func (u *UI) moreButton() *widget.Button {
	var b *widget.Button
	b = widget.NewButtonWithIcon("", clockIcon, func() {
		c := u.chat
		if c == nil {
			return
		}
		disappear := fyne.NewMenuItem("Disappearing Message", nil)
		for i, name := range disappearNames {
			d := disappearTimes[i]
			it := fyne.NewMenuItem(name, func() { c.disappear = d; u.showExtras() })
			it.Checked = c.disappear == d
			disappear.ChildMenu = appendItem(disappear.ChildMenu, it)
		}
		items := []*fyne.MenuItem{
			fyne.NewMenuItem("Create Poll", func() { u.openPollEditor() }),
			fyne.NewMenuItem("Schedule Message", func() { u.openSchedule() }),
			disappear,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Scheduled Messages", func() { u.openScheduledList() }),
			fyne.NewMenuItem("Pinned Messages", func() { u.openPinned() }),
		}
		menu := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.win.Canvas())
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(b)
		menu.ShowAtPosition(pos.Add(fyne.NewPos(0, -menu.MinSize().Height-4)))
	})
	b.Importance = widget.LowImportance
	return b
}

// showExtras shows what the next message will do (disappear, be
// scheduled) in a line above the message box.
func (u *UI) showExtras() {
	c := u.chat
	if c == nil || c.extras == nil {
		return
	}
	var lines []string
	if c.disappear > 0 {
		for i, d := range disappearTimes {
			if d == c.disappear {
				lines = append(lines, "Your messages disappear after "+strings.ToLower(disappearNames[i])+".")
			}
		}
	}
	if !c.scheduleAt.IsZero() {
		lines = append(lines, "Your next message will be sent "+atText(c.scheduleAt)+".")
	}
	if len(lines) == 0 {
		c.extras.Hide()
		return
	}
	c.extrasText.SetText(strings.Join(lines, " "))
	c.extras.Show()
	if c.view != nil {
		c.view.Refresh()
	}
}

// sendOptions are the extras for the next message (scheduling is used once).
func (c *chatState) sendOptions() guildconn.SendOptions {
	o := guildconn.SendOptions{Disappear: c.disappear, At: c.scheduleAt}
	c.scheduleAt = time.Time{}
	return o
}

// ---- pages ---------------------------------------------------------------------------------

// backToChat closes an editor page and shows the channel again.
func (u *UI) backToChat(p *popup, channel string) {
	p.Close()
	u.openChat(channel)
}

// openPollEditor is the Create Poll page.
func (u *UI) openPollEditor() {
	c := u.chat
	gid, ch := c.guild, c.channel
	question := widget.NewEntry()
	question.SetPlaceHolder("Where should we meet?")
	choices := []*widget.Entry{}
	list := form()
	answer := widget.NewSelect(nil, nil)
	quiz := widget.NewCheck("", nil)
	answerField := field("Right Answer", answer, "Shown to each person once they have answered.")
	answerField.Hide()
	refreshAnswers := func() {
		var opts []string
		for i := range choices {
			opts = append(opts, fmt.Sprintf("Choice %d", i+1))
		}
		sel := answer.SelectedIndex()
		answer.Options = opts
		if sel < 0 || sel >= len(opts) {
			sel = 0
		}
		answer.SetSelectedIndex(sel)
		answer.Refresh()
	}
	addChoice := func() {
		e := widget.NewEntry()
		choices = append(choices, e)
		list.Add(field(fmt.Sprintf("Choice %d", len(choices)), e, ""))
		refreshAnswers()
	}
	addChoice()
	addChoice()
	var more *widget.Button
	more = widget.NewButtonWithIcon("Add Choice", plusIcon, func() {
		addChoice()
		if len(choices) >= 10 {
			more.Disable()
		}
	})
	quiz.OnChanged = func(on bool) {
		if on {
			answerField.Show()
		} else {
			answerField.Hide()
		}
	}
	problem := newErrorText()
	var p *popup
	var send *widget.Button
	send = primary("Send Poll", func() {
		problem.Set(nil)
		var texts []string
		for _, e := range choices {
			texts = append(texts, e.Text)
		}
		right := -1
		if quiz.Checked {
			right = answer.SelectedIndex()
		}
		q, o := question.Text, c.sendOptions()
		send.Disable()
		go func() {
			gc, err := u.guilds.Get(gid)
			if err == nil {
				err = gc.SendPoll(ch, q, texts, right, o)
			}
			fyne.Do(func() {
				send.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				u.backToChat(p, ch)
			})
		}()
	})
	p = u.openEditor(chatIcon, "Create Poll", heading("Create Poll"),
		field("Question", question, ""), list, container.NewHBox(more),
		settingList("", settingRow("Quiz Mode", "One choice is right. Answers can't be changed.", quiz)),
		answerField, problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { u.backToChat(p, ch) }), send))
}

// openSchedule chooses when the next message is sent.
func (u *UI) openSchedule() {
	c := u.chat
	ch := c.channel
	start := time.Now().Add(time.Hour).Truncate(5 * time.Minute)
	if !c.scheduleAt.IsZero() {
		start = c.scheduleAt
	}
	date := widget.NewEntry()
	date.SetText(start.Format("2006-01-02"))
	clock := widget.NewEntry()
	clock.SetText(start.Format("15:04"))
	problem := newErrorText()
	var p *popup
	set := primary("Set Time", func() {
		problem.Set(nil)
		t, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(date.Text)+" "+strings.TrimSpace(clock.Text), time.Local)
		switch {
		case err != nil:
			problem.Set(fmt.Errorf("Write the date as YYYY-MM-DD and the time as HH:MM."))
			return
		case !t.After(time.Now().Add(time.Minute)):
			problem.Set(fmt.Errorf("Choose a time in the future."))
			return
		case t.After(time.Now().AddDate(1, 0, 0)):
			problem.Set(fmt.Errorf("A message can be scheduled at most a year ahead."))
			return
		}
		c.scheduleAt = t
		u.backToChat(p, ch)
		u.showExtras()
	})
	p = u.openEditor(clockIcon, "Schedule Message", heading("Schedule Message"),
		para("Write your message after choosing the time; Send then hands it to the guild, which posts it on time even if your computer is off. You can cancel it until then under Scheduled Messages."),
		container.NewGridWithColumns(2, field("Date", date, "YYYY-MM-DD, like 2026-12-31."), field("Time", clock, "HH:MM, like 18:30, your own time.")),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { u.backToChat(p, ch) }), set))
}

// openScheduledList lists your scheduled messages in this guild, with Cancel.
func (u *UI) openScheduledList() {
	c := u.chat
	gid := c.guild
	g := u.findGuild(gid)
	list := stack(0)
	problem := newErrorText()
	var fill func()
	fill = func() {
		go func() {
			gc, err := u.guilds.Get(gid)
			var items []guildconn.ScheduledView
			if err == nil {
				items, err = gc.Scheduled()
			}
			fyne.Do(func() {
				list.RemoveAll()
				if err != nil {
					problem.Set(err)
					return
				}
				if len(items) == 0 {
					list.Add(para("Nothing is scheduled."))
				}
				for i, s := range items {
					s := s
					where := ""
					if g != nil {
						for _, n := range g.Channels {
							if n.ID == s.Channel {
								where = "#" + n.Name + ", "
							}
						}
					}
					cancel := widget.NewButton("Cancel", func() {
						problem.Run(func() error { return gc.CancelScheduled(s.ID) }, fill)
					})
					if i > 0 {
						list.Add(sideLineH())
					}
					list.Add(settingRow(s.Text, where+"sent "+atText(s.At)+".", cancel))
				}
				list.Refresh()
			})
		}()
	}
	fill()
	u.openEditor(clockIcon, "Scheduled Messages", heading("Scheduled Messages"), list, problem.box)
}

// openPinned lists the channel's pinned messages.
func (u *UI) openPinned() {
	c := u.chat
	gid, ch := c.guild, c.channel
	g := u.findGuild(gid)
	list := stack(0)
	problem := newErrorText()
	var fill func()
	fill = func() {
		go func() {
			gc, err := u.guilds.Get(gid)
			var msgs []guildconn.ChatView
			if err == nil {
				msgs, err = gc.Pinned(ch)
			}
			fyne.Do(func() {
				list.RemoveAll()
				if err != nil {
					problem.Set(err)
					return
				}
				if len(msgs) == 0 {
					list.Add(para("Nothing is pinned here yet."))
				}
				for i, m := range msgs {
					m := m
					body := m.Text
					if m.Poll != nil {
						body = "Poll: " + m.Poll.Question
					}
					body, _ = hideSpoilers(body)
					var unpin fyne.CanvasObject
					if g != nil && canIn(*g, ch, proto.PermPinMessages) {
						unpin = widget.NewButton("Unpin", func() {
							problem.Run(func() error { return gc.Pin(ch, m.ID, false) }, fill)
						})
					}
					if i > 0 {
						list.Add(sideLineH())
					}
					list.Add(settingRow(m.Name+", "+msgTime(m.Time), firstLine(body, 200), unpin))
				}
				list.Refresh()
			})
		}()
	}
	fill()
	u.openEditor(pinIcon, "Pinned Messages", heading("Pinned Messages"), list, problem.box)
}

// openEditHistory shows a message's earlier texts.
func (u *UI) openEditHistory(m guildconn.ChatView) {
	c := u.chat
	gid, ch := c.guild, c.channel
	gc, err := u.guilds.Get(gid)
	var versions []guildconn.EarlierView
	if err == nil {
		versions, err = gc.EditHistory(ch, m.ID)
	}
	list := stack(0)
	if err != nil {
		list.Add(wrapText(sentence(err.Error()), colDanger))
	}
	for i, v := range versions {
		label := "Earlier"
		if i == len(versions)-1 {
			label = "Now"
		}
		when := ""
		if t, err := time.Parse(time.RFC3339Nano, v.Time); err == nil {
			when = ", " + atText(t.Local())
		}
		if i > 0 {
			list.Add(sideLineH())
		}
		shown, _ := hideSpoilers(v.Text)
		list.Add(container.New(layout.NewCustomPaddedLayout(12, 12, 0, 0), stack(4, text(label+when, 15, colText, true), container.New(textPad{}, u.markdownFlush(shown)))))
	}
	u.openEditor(chatIcon, "Edit History", heading("Edit History"), list)
}

// forwardTarget is somewhere a message can be forwarded to.
type forwardTarget struct {
	label          string
	guild, channel string
	contact        string
}

// openForward chooses where to forward a message: a channel in any of
// your guilds, or a friend.
func (u *UI) openForward(m guildconn.ChatView) {
	c := u.chat
	fromGuild, ch := c.guild, c.channel
	where := "a guild"
	if g := u.findGuild(fromGuild); g != nil {
		where = g.Name
		for _, n := range g.Channels {
			if n.ID == ch {
				where += " / #" + n.Name
			}
		}
	}
	msgText := c.withNames(m.Text, false)
	if m.Forward != nil { // It was forwarded again, so keep who wrote it first.
		m.Name, where = m.Forward.Name, m.Forward.Where
	}
	body := guildconn.ForwardText(msgText, m.Name, where, m.Time)

	var targets []forwardTarget
	for _, g := range u.guilds.Views() {
		for _, n := range g.Channels {
			if n.Kind == proto.KindChannel && !n.NoKey && canIn(g, n.ID, proto.PermSendMessages) {
				targets = append(targets, forwardTarget{label: g.Name + " / #" + n.Name, guild: g.ID, channel: n.ID})
			}
		}
	}
	for _, f := range u.guilds.Contacts() {
		if f.State == "contact" {
			targets = append(targets, forwardTarget{label: "Friend: " + f.Name, contact: f.UserID})
		}
	}
	var names []string
	for _, t := range targets {
		names = append(names, t.label)
	}
	to := widget.NewSelect(names, nil)
	if len(names) > 0 {
		to.SetSelectedIndex(0)
	}
	preview, _ := hideSpoilers(msgText)
	problem := newErrorText()
	var p *popup
	var send *widget.Button
	send = primary("Forward", func() {
		problem.Set(nil)
		i := to.SelectedIndex()
		if i < 0 || i >= len(targets) {
			problem.Set(fmt.Errorf("Choose where to forward it."))
			return
		}
		t := targets[i]
		send.Disable()
		go func() {
			var err error
			if t.contact != "" {
				if body == "" {
					err = fmt.Errorf("that message is too long to forward")
				} else {
					err = u.guilds.SendDM(t.contact, body, "")
				}
			} else {
				var gc *guildconn.Conn
				if gc, err = u.guilds.Get(t.guild); err == nil {
					err = gc.Forward(t.channel, body)
				}
			}
			fyne.Do(func() {
				send.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				p.Close()
				u.toast("Forwarded to " + t.label)
			})
		}()
	})
	p = u.openPopup(560, panelTitle("Forward"), // It is a window over the chat.
		card(14, form(text(m.Name, 14, colText, true), container.New(textPad{}, u.markdownFlush(preview)))),
		field("Forward To", to, "The message shows who wrote it and where."),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), send))
}

// These are the poll bar colours: your vote, and a wrong quiz answer.
var (
	pollMine  = color.NRGBA{179, 13, 30, 110}
	pollWrong = color.NRGBA{242, 95, 107, 80}
)

// dmBody draws a friend message: forwarded ones show where they came from,
// and spoilers stay hidden until Show Spoiler.
func (u *UI) dmBody(m guildconn.DMView) fyne.CanvasObject {
	b := guildconn.ParseBody(m.Text)
	d := u.dm
	var body fyne.CanvasObject
	if hidden, has := hideSpoilers(b.Text); has && (d == nil || !d.spoilers[m.ID]) {
		id := m.ID
		show := widget.NewButton("Show Spoiler", func() {
			if u.dm != nil {
				if u.dm.spoilers == nil {
					u.dm.spoilers = map[string]bool{}
				}
				u.dm.spoilers[id] = true
				u.drawDM()
			}
		})
		show.Importance = widget.LowImportance
		body = container.NewVBox(u.markdownFlush(hidden), container.NewHBox(show))
	} else {
		body = u.markdownFlush(showSpoilers(b.Text))
	}
	if b.Forward == nil {
		return body
	}
	edge := canvas.NewRectangle(colLine)
	edge.SetMinSize(fyne.NewSize(3, 1))
	return container.NewBorder(nil, nil, container.NewHBox(edge, gap(1)), nil,
		container.NewVBox(text("Forwarded from "+b.Forward.Name+", "+b.Forward.Where, 12.5, colDim, false), body))
}
