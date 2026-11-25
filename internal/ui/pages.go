package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// This file has the guild's other pages: Rules and Join Requests.

// openPage switches to a guild page and loads what it needs.
func (u *UI) openPage(page string) {
	u.leaveChat()
	u.page, u.pageSig, u.pageArg = page, "", ""
	if page == "rules" || page == "requests" {
		u.pageData, u.pageKey = nil, "" // These pages load their own data.
	}
	u.redraw()
	switch page {
	case "rules":
		u.loadPage("rules", proto.Request{Type: "rules"}, func(r proto.Response) any { return r.Rules })
	case "requests":
		u.loadPage("requests", proto.Request{Type: "requests"}, func(r proto.Response) any { return r.People })
	}
}

// loadPage asks the guild for a page's data and redraws when it arrives.
func (u *UI) loadPage(page string, req proto.Request, pick func(proto.Response) any) {
	id := u.selected
	u.call(id, req, func(r proto.Response, err error) {
		if u.selected != id || u.page != page {
			return // The window moved on meanwhile.
		}
		if err != nil {
			u.pageData = err
		} else {
			u.pageData = pick(r)
		}
		u.pageSig = fmt.Sprint(time.Now().UnixNano())
		u.redraw()
	})
}

func (u *UI) guildMain(g guildconn.GuildView) fyne.CanvasObject {
	switch u.page {
	case "members":
		return u.membersPage(g)
	case "rules":
		if g.Join != nil && g.Join.RulesEnabled {
			return u.rulesPage(g)
		}
	case "requests":
		if can(g, proto.PermApproveJoins) {
			return u.requestsPage(g)
		}
	case "chat":
		if u.chat != nil {
			return u.chatPage(g)
		}
	case "calendar":
		if g.Pages.Calendar || can(g, proto.PermManagePages) {
			return u.calendarPage(g)
		}
	case "event":
		return u.eventPage(g)
	case "threads":
		if g.Pages.Threads || can(g, proto.PermManagePages) {
			return u.threadsPage(g)
		}
	case "post":
		return u.postPage(g)
	case "wiki":
		if g.Pages.Wiki || can(g, proto.PermManagePages) {
			return u.wikiPage(g)
		}
	case "wikipage":
		return u.wikiOnePage(g)
	case "wallet":
		if g.Currency != nil {
			return u.walletPage(g)
		}
	case "shop":
		if g.Currency != nil && g.Currency.Shop {
			return u.shopPage(g)
		}
	case "balances":
		if g.Currency != nil && can(g, proto.PermManageCurrency) {
			return u.balancesPage(g)
		}
	case "voice":
		return u.voicePage(g)
	case "share":
		return u.sharePage(g)
	case "editor":
		if u.editor != nil && u.editor.guild == g.ID {
			return u.editorPage(g)
		}
	}
	u.page = "about"
	return u.guildPage(g)
}

// pageFrame is a page with a title bar and scrolling content.
func pageFrame(ic fyne.Resource, title string, parts ...fyne.CanvasObject) fyne.CanvasObject {
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(14, 14, 24, 24), container.NewHBox(icon(ic, 18), text(title, 16, colText, true))))
	body := container.New(layout.NewCustomPaddedLayout(20, 30, 24, 24), narrowTop(760, container.NewVBox(parts...)))
	return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, container.NewVScroll(body))
}

func (u *UI) rulesPage(g guildconn.GuildView) fyne.CanvasObject {
	var body fyne.CanvasObject
	switch d := u.pageData.(type) {
	case nil:
		body = para("Loading…")
	case error:
		body = para(sentence(d.Error()))
	case string:
		if d == "" {
			body = para("No rules yet.")
		} else {
			body = u.guildMarkdown(g, d) // The Rules can have pictures.
		}
	}
	back := widget.NewButtonWithIcon("Back", backIcon, func() { u.openPage("about") })
	back.Importance = widget.LowImportance
	return pageFrame(shieldIcon, "Rules", container.NewHBox(back), card(24, body))
}

func (u *UI) requestsPage(g guildconn.GuildView) fyne.CanvasObject {
	list := stack(4) // This is a column, so text lines up.
	switch d := u.pageData.(type) {
	case nil:
		list.Add(para("Loading…"))
	case error:
		list.Add(para(sentence(d.Error())))
	case []proto.PersonInfo:
		if len(d) != g.Requests {
			// The number changed since loading: load again.
			defer u.loadPage("requests", proto.Request{Type: "requests"}, func(r proto.Response) any { return r.People })
		}
		if len(d) == 0 {
			list.Add(para("Nobody is waiting."))
		}
		for _, p := range d {
			p := p
			act := func(kind, done string) func() {
				return func() {
					u.call(g.ID, proto.Request{Type: kind, Target: p.UserKey}, func(_ proto.Response, err error) {
						if err != nil {
							u.toast(sentence(err.Error()))
							return
						}
						u.toast(done)
						u.openPage("requests")
					})
				}
			}
			asked := p.At
			if t, err := time.Parse(time.RFC3339, p.At); err == nil {
				asked = t.Local().Format("2 Jan 2006, 15:04")
			}
			list.Add(personLine(p, shortID(p.UserKey)+" · asked "+asked+accountFacts(p.Created, p.Guilds),
				widget.NewButton("Decline", act("decline", "Declined")), primary("Approve", act("approve", "Approved"))))
		}
	}
	return pageFrame(usersIcon, "Join Requests", card(22, list))
}

// personLine is a person with an avatar, a second line and buttons.
func personLine(p proto.PersonInfo, sub string, buttons ...fyne.CanvasObject) fyne.CanvasObject {
	name := p.Name
	if name == "" {
		name = shortID(p.UserKey)
	}
	av := name
	if p.Name == "" {
		av = p.UserKey[:2]
	}
	return container.NewBorder(nil, nil, container.NewHBox(avatar(av, p.UserKey, 36), gap(1)), container.NewHBox(buttons...),
		container.NewVBox(text(name, 14.5, colText, true), text(sub, 12.5, colFaint, false)))
}

// flow lays objects out left to right, wrapping onto new lines.
type flow struct {
	gap   float32
	width float32 // It is the last width laid out, to work out the height.
}

func (f *flow) place(objs []fyne.CanvasObject, width float32, move bool) float32 {
	x, y, line := float32(0), float32(0), float32(0)
	for _, o := range objs {
		s := o.MinSize()
		if x > 0 && x+s.Width > width {
			x, y, line = 0, y+line+f.gap, 0
		}
		if move {
			o.Resize(s)
			o.Move(fyne.NewPos(x, y))
		}
		x += s.Width + f.gap
		line = fyne.Max(line, s.Height)
	}
	return y + line
}

func (f *flow) MinSize(objs []fyne.CanvasObject) fyne.Size {
	w := f.width
	if w == 0 {
		w = 600
	}
	return fyne.NewSize(10, f.place(objs, w, false))
}

func (f *flow) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	f.width = s.Width
	f.place(objs, s.Width, true)
}

// leaveChat closes the open channel (the guild stops counting it as read).
func (u *UI) leaveChat() {
	if c := u.chat; c != nil {
		u.chat = nil
		if u.guilds != nil {
			if gc, err := u.guilds.Get(c.guild); err == nil {
				go gc.LeaveChannel()
			}
		}
	}
}

// ---- editor pages ------------------------------------------------------
//
// Editing a wiki page, an event, a post or a shop item, and the Bank, take
// the whole main area, like a settings page, instead of a pop-up window.
// The form is built once and kept, so redraws never lose typing.

type editorView struct {
	guild        string
	icon         fyne.Resource
	section      string
	body         fyne.CanvasObject
	back, argWas string // These are the page and item to go back to.
	chat         string // It is the channel to go back to, when opened from a chat.
	n            int
	close        func()
}

var editorCount int

// openEditor shows content as a page of the guild open now. Closing the
// returned popup goes back to the page it came from.
func (u *UI) openEditor(ic fyne.Resource, section string, content ...fyne.CanvasObject) *popup {
	back, arg := u.page, u.pageArg
	if u.editor != nil && u.page == "editor" { // One editor is opening another.
		back, arg = u.editor.back, u.editor.argWas
	}
	editorCount++
	e := &editorView{guild: u.selected, icon: ic, section: section, body: form(content...), back: back, argWas: arg, n: editorCount}
	if u.chat != nil && u.page == "chat" {
		e.chat = u.chat.channel
	} else if u.editor != nil && u.page == "editor" {
		e.chat = u.editor.chat
	}
	u.leaveChat()
	u.editor = e
	u.page, u.pageSig = "editor", fmt.Sprint("editor", e.n)
	p := &popup{embedded: true}
	p.reload = func() {
		if u.editor == e {
			u.editor = nil
			if e.chat != "" { // It was opened from a chat, so go back to it.
				u.openChat(e.chat)
				return
			}
			if e.back == "chat" || e.back == "editor" {
				e.back = "about"
			}
			u.openPageArg(e.back, e.argWas)
		}
	}
	e.close = p.Close
	u.redraw()
	return p
}

func (u *UI) editorPage(g guildconn.GuildView) fyne.CanvasObject {
	e := u.editor
	hideClose(e.body)
	return pageFrame(e.icon, e.section, backButton(e.close), e.body)
}

// accountFacts is " · account from 2 Jan 2026 · in 3 guilds", as the
// person's app said, or "" when unknown.
func accountFacts(created string, guilds int) string {
	s := ""
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		s += " · account from " + t.Local().Format("2 Jan 2006")
	}
	switch {
	case guilds == 1:
		s += " · in 1 guild"
	case guilds > 1:
		s += fmt.Sprintf(" · in %d guilds", guilds)
	}
	return s
}
