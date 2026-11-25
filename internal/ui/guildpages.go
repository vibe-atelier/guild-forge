package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // This gives time zones on every computer.

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
	"nexus/internal/proto"
)

// This file has the guild pages: Calendar, Threads and Wiki,
// pictures in Markdown, and the Rules-change notice.

// ---- loading a page's data ----------------------------------------------------------

// pageLoad returns the data for key, loading it (again) when it is missing
// or the guild's pages changed since. nil means "loading".
func (u *UI) pageLoad(g guildconn.GuildView, key string, req proto.Request, pick func(proto.Response) any) any {
	if u.pageKey == key && u.pageRev == g.PagesRev {
		return u.pageData
	}
	want := fmt.Sprint(key, "@", g.PagesRev)
	if u.loading == want {
		if u.pageKey == key {
			return u.pageData // Show the old data while the new data comes.
		}
		return nil
	}
	u.loading = want
	rev, gid := g.PagesRev, g.ID
	u.call(gid, req, func(r proto.Response, err error) {
		if u.loading == want {
			u.loading = ""
		}
		if u.selected != gid {
			return
		}
		if err != nil {
			u.pageData = err
		} else {
			u.pageData = pick(r)
		}
		u.pageKey, u.pageRev = key, rev
		u.pageSig = fmt.Sprint(time.Now().UnixNano())
		u.redraw()
	})
	if u.pageKey == key {
		return u.pageData
	}
	return nil
}

// openPageArg opens a page about one thing (a post, an event, a Wiki page).
func (u *UI) openPageArg(page, arg string) {
	u.leaveChat()
	u.page, u.pageArg, u.pageSig = page, arg, ""
	u.redraw()
}

func nameOf(g guildconn.GuildView, key string) string {
	if m := memberOf(g, key); m != nil && shownName(*m) != "" {
		return shownName(*m)
	}
	if len(key) > 6 {
		return shortID(key)
	}
	return "someone"
}

func backTo(u *UI, page string) fyne.CanvasObject {
	b := widget.NewButtonWithIcon("Back", backIcon, func() { u.openPage(page) })
	b.Importance = widget.LowImportance
	return container.NewHBox(b)
}

func loadingOr(v any) fyne.CanvasObject {
	if err, ok := v.(error); ok {
		return para(sentence(err.Error()))
	}
	return para("Loading…")
}

// ---- Markdown with guild pictures and links ------------------------------------------

var wikiLink = regexp.MustCompile(`\[\[([^\]\n]{1,120})\]\]`)
var channelLink = regexp.MustCompile(`(^|[\s(])#([a-z0-9][a-z0-9_-]{0,49})`)

// guildMarkdown shows page Markdown: pictures from the guild (saved on this
// computer first), [[Wiki]] and #channel links.
func (u *UI) guildMarkdown(g guildconn.GuildView, s string) fyne.CanvasObject {
	// A picture on a line of its own is drawn as a picture block; the text
	// around it is Markdown. (A picture inside a line shows as its name.)
	box := container.NewVBox()
	var chunk []string
	flush := func() {
		if t := strings.TrimSpace(strings.Join(chunk, "\n")); t != "" {
			box.Add(flushMarkdown(u.guildText(g, t))) // It is in line with the titles.
		}
		chunk = nil
	}
	for _, line := range strings.Split(s, "\n") {
		m := mdImage.FindStringSubmatch(strings.TrimSpace(line))
		if m != nil && m[0] == strings.TrimSpace(line) {
			if hash, ok := strings.CutPrefix(m[2], "nexus:"); ok {
				flush()
				if path := u.pictureFile(g.ID, hash); path != "" {
					if data, err := os.ReadFile(path); err == nil {
						img := canvas.NewImageFromResource(fyne.NewStaticResource(hash, data))
						img.FillMode = canvas.ImageFillContain
						img.SetMinSize(fyne.NewSize(360, 240))
						box.Add(container.NewHBox(img))
						continue
					}
				}
				box.Add(text("Loading picture…", 12.5, colFaint, false))
				continue
			}
		}
		chunk = append(chunk, line)
	}
	flush()
	return box
}

// guildText is Markdown text with [[Wiki]] and #channel links.
func (u *UI) guildText(g guildconn.GuildView, s string) *widget.RichText {
	s = wikiLink.ReplaceAllStringFunc(s, func(m string) string {
		title := strings.TrimSpace(m[2 : len(m)-2])
		return "[" + title + "](nexus-wiki:" + url.PathEscape(title) + ")"
	})
	s = channelLink.ReplaceAllStringFunc(s, func(m string) string {
		parts := channelLink.FindStringSubmatch(m)
		for _, n := range g.Channels {
			if n.Kind == proto.KindChannel && n.Name == parts[2] {
				return parts[1] + "[#" + parts[2] + "](nexus-channel:" + n.ID + ")"
			}
		}
		return m
	})
	return u.markdown(s)
}

// pictureFile returns the saved copy of a guild picture, fetching it if
// needed (then the page is drawn again).
func (u *UI) pictureFile(gid, hash string) string {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(hash) {
		return ""
	}
	path := filepath.Join(u.user.Dir, "media", "guilds", gid+"-pictures", hash)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if u.fetching == nil {
		u.fetching = map[string]bool{}
	}
	if u.fetching[hash] {
		return ""
	}
	u.fetching[hash] = true
	u.call(gid, proto.Request{Type: "image_get", Hash: hash}, func(r proto.Response, err error) {
		delete(u.fetching, hash)
		if err != nil || len(r.Data) == 0 {
			return
		}
		os.MkdirAll(filepath.Dir(path), 0o700)
		if os.WriteFile(path, r.Data, 0o600) == nil {
			u.redraw()
		}
	})
	return ""
}

// openInside follows a Nexus link: a Wiki page, a channel, or a post.
func (u *UI) openInside(link *url.URL) {
	what := link.Opaque
	if what == "" {
		what = strings.TrimPrefix(link.Path, "/")
	}
	what, _ = url.PathUnescape(what)
	switch link.Scheme {
	case "nexus-wiki":
		u.openPageArg("wikipage", what)
	case "nexus-channel":
		u.openChat(what)
	case "nexus-user": // It is an @mention in the open guild.
		if g := u.findGuild(u.selected); g != nil {
			if m := memberOf(*g, what); m != nil && !m.IsMe {
				u.openMember(*g, *m)
			}
		}
	case "nexus-post": // It is a link like guild/post.
		gid, post, ok := strings.Cut(what, "/")
		if !ok || u.findGuild(gid) == nil {
			u.toast("That post is in a guild you're not in.")
			return
		}
		u.selectGuild(gid)
		u.openPageArg("post", post)
	}
}

// pictureButton lets a writer add a picture: it is uploaded to the guild
// and its Markdown is added to the text.
func (u *UI) pictureButton(gid string, entry *widget.Entry, problem *errorText) *widget.Button {
	var b *widget.Button
	b = widget.NewButton("Add Picture", func() {
		go func() {
			path, err := choosePicture("Choose a picture")
			if err != nil || path == "" {
				if err != nil {
					fyne.Do(func() { problem.Set(err) })
				}
				return
			}
			data, err := os.ReadFile(path)
			if err == nil {
				data, err = imaging.FitPicture(data, 1600, 1900*1024)
			}
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
					return
				}
				b.Disable()
				u.call(gid, proto.Request{Type: "image_put", Image: data}, func(r proto.Response, err error) {
					b.Enable()
					if err != nil {
						problem.Set(err)
						return
					}
					alt := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
					t := entry.Text
					if t != "" && !strings.HasSuffix(t, "\n") {
						t += "\n"
					}
					entry.SetText(t + "![" + alt + "](nexus:" + r.Message + ")\n")
				})
			})
		}()
	})
	b.Importance = widget.LowImportance
	return b
}

// ---- the Pages window ---------------------------------------------------------------

func (u *UI) openPagesSettings(g guildconn.GuildView) {
	var p *popup
	cal, calBox := option("Calendar", "A calendar of the guild's events.", g.Pages.Calendar)
	thr, thrBox := option("Threads", "Posts with comments and likes, like a forum.", g.Pages.Threads)
	wiki, wikiBox := option("Wiki", "Pages of knowledge, linked to each other.", g.Pages.Wiki)
	problem := newErrorText()
	p = u.openPanel("pages", 480, text("Pages", 22, colText, true), para("Turn the guild's pages on or off. They appear under About in the side bar."),
		calBox, thrBox, wikiBox, problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), primary("Save", func() {
			ps := proto.PageSettings{Calendar: cal.Checked, Threads: thr.Checked, Wiki: wiki.Checked}
			u.call(g.ID, proto.Request{Type: "update_pages", Pages: &ps}, func(_ proto.Response, err error) {
				if err != nil {
					problem.Set(err)
					return
				}
				p.Close()
				u.toast("Pages saved")
			})
		})))
}

// ---- Calendar ----------------------------------------------------

var commonZones = []string{"UTC", "Asia/Kolkata", "Asia/Dubai", "Asia/Riyadh", "Asia/Tokyo",
	"Asia/Shanghai", "Asia/Singapore", "Europe/London", "Europe/Berlin", "Europe/Paris", "Europe/Istanbul",
	"Europe/Moscow", "Africa/Cairo", "America/New_York", "America/Chicago", "America/Denver",
	"America/Los_Angeles", "America/Sao_Paulo", "Australia/Sydney", "Pacific/Auckland"}

// localZone guesses this computer's time zone from the list.
func localZone() string {
	if n := time.Local.String(); n != "Local" && n != "" {
		return n
	}
	_, off := time.Now().Zone()
	for _, z := range commonZones {
		if loc, err := time.LoadLocation(z); err == nil {
			if _, o := time.Now().In(loc).Zone(); o == off {
				return z
			}
		}
	}
	return "UTC"
}

func zoneOf(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil && name != "" {
		return loc
	}
	return time.UTC
}

// whenText shows an event time in the zone it was written in.
func whenText(utc string, withTime bool, zone string) string {
	t, err := time.Parse(time.RFC3339, utc)
	if err != nil {
		return ""
	}
	t = t.In(zoneOf(zone))
	if !withTime {
		return t.Format("Mon 2 Jan 2006")
	}
	if zone == "" {
		zone = "UTC"
	}
	return t.Format("Mon 2 Jan 2006, 15:04") + " (" + zone + ")"
}

func (u *UI) calendarPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "events", proto.Request{Type: "events"}, func(r proto.Response) any { return r.Events })
	events, ok := data.([]proto.Event)
	if u.calMonth.IsZero() {
		now := time.Now()
		u.calMonth = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	}
	month := u.calMonth
	// Events in this month (by their own time zone's date).
	var mine []proto.Event
	marked := map[int]bool{}
	for _, e := range events {
		t, err := time.Parse(time.RFC3339, e.Start)
		if err != nil {
			continue
		}
		t = t.In(zoneOf(e.StartZone))
		if t.Year() == month.Year() && t.Month() == month.Month() {
			mine = append(mine, e)
			marked[t.Day()] = true
		}
	}
	move := func(n int) func() {
		return func() {
			u.calMonth = u.calMonth.AddDate(0, n, 0)
			u.pageSig = fmt.Sprint(time.Now().UnixNano())
			u.redraw()
		}
	}
	prev := widget.NewButton("‹", move(-1))
	next := widget.NewButton("›", move(1))
	today := widget.NewButton("Today", func() { u.calMonth = time.Time{}; u.pageSig = fmt.Sprint(time.Now().UnixNano()); u.redraw() })
	today.Importance = widget.LowImportance
	head := container.NewBorder(nil, nil, container.NewHBox(prev, next, today), nil, centred(text(month.Format("January 2006"), 20, colText, true)))
	if can(g, proto.PermManageEvents) {
		head = container.NewBorder(nil, nil, container.NewHBox(prev, next, today),
			primary("New Event", func() { u.openEventEditor(g, nil) }), centred(text(month.Format("January 2006"), 20, colText, true)))
	}
	grid := container.NewGridWithColumns(7)
	for _, d := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		grid.Add(centred(text(d, 12, colFaint, true)))
	}
	lead := (int(month.Weekday()) + 6) % 7 // The week starts on Monday.
	for i := 0; i < lead; i++ {
		grid.Add(gap(1))
	}
	days := month.AddDate(0, 1, -1).Day()
	now := time.Now()
	for d := 1; d <= days; d++ {
		b := widget.NewButton(fmt.Sprint(d), nil)
		b.Importance = widget.LowImportance
		if marked[d] {
			b.Importance = widget.HighImportance // It is a day with events.
		} else if now.Year() == month.Year() && now.Month() == month.Month() && now.Day() == d {
			b.Importance = widget.MediumImportance
		}
		grid.Add(b)
	}
	list := container.NewVBox(text("THIS MONTH", 11.5, colFaint, true))
	switch {
	case !ok:
		list.Add(loadingOr(data))
	case len(mine) == 0:
		list.Add(para("No events this month."))
	}
	for _, e := range mine {
		e := e
		when := whenText(e.Start, e.StartTime, e.StartZone)
		if e.End != "" {
			when += "  →  " + whenText(e.End, e.EndTime, e.EndZone)
		}
		list.Add(newTapRow(container.NewVBox(text(e.Title, 15, colText, true), text(when, 12.5, colDim, false)), false,
			func() { u.openPageArg("event", e.ID) }))
	}
	return pageFrame(calendarIcon, "Calendar", head, card(18, grid), gap(8), card(18, list))
}

func (u *UI) eventPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "events", proto.Request{Type: "events"}, func(r proto.Response) any { return r.Events })
	events, ok := data.([]proto.Event)
	if !ok {
		return pageFrame(calendarIcon, "Calendar", backTo(u, "calendar"), loadingOr(data))
	}
	var e *proto.Event
	for i := range events {
		if events[i].ID == u.pageArg {
			e = &events[i]
		}
	}
	if e == nil {
		return pageFrame(calendarIcon, "Calendar", backTo(u, "calendar"), para("This event is gone."))
	}
	parts := []fyne.CanvasObject{text(e.Title, 26, colText, true),
		text("Starts: "+whenText(e.Start, e.StartTime, e.StartZone), 14, colDim, false)}
	if e.End != "" {
		parts = append(parts, text("Ends: "+whenText(e.End, e.EndTime, e.EndZone), 14, colDim, false))
	}
	if e.Details != "" {
		parts = append(parts, gap(8), u.guildMarkdown(g, e.Details))
	}
	tools := container.NewHBox()
	if can(g, proto.PermManageEvents) {
		ev := *e
		tools.Add(widget.NewButton("Edit", func() { u.openEventEditor(g, &ev) }))
		del := widget.NewButton("Delete", func() {
			u.confirm("Delete Event", "Delete \""+ev.Title+"\"?", "Delete", true, func() {
				u.call(g.ID, proto.Request{Type: "event_delete", ItemID: ev.ID}, func(_ proto.Response, err error) {
					if err != nil {
						u.toast(sentence(err.Error()))
						return
					}
					u.openPage("calendar")
				})
			})
		})
		del.Importance = widget.DangerImportance
		tools.Add(del)
	}
	return pageFrame(calendarIcon, "Calendar", container.NewBorder(nil, nil, backTo(u, "calendar"), tools), card(24, container.NewVBox(parts...)))
}

// openEventEditor makes (e == nil) or changes an event.
func (u *UI) openEventEditor(g guildconn.GuildView, e *proto.Event) {
	var p *popup
	title := widget.NewEntry()
	startDate, startTime := widget.NewEntry(), widget.NewEntry()
	endDate, endTime := widget.NewEntry(), widget.NewEntry()
	startDate.SetPlaceHolder("2026-12-05")
	endDate.SetPlaceHolder("Optional")
	startTime.SetPlaceHolder("18:30")
	endTime.SetPlaceHolder("Optional")
	startZone, endZone := widget.NewSelect(commonZones, nil), widget.NewSelect(commonZones, nil)
	details := widget.NewMultiLineEntry()
	details.SetMinRowsVisible(8)
	details.Wrapping = fyne.TextWrapWord
	problem := newErrorText()
	zone := localZone()
	if !has(commonZones, zone) {
		startZone.Options = append([]string{zone}, commonZones...)
		endZone.Options = startZone.Options
	}
	startZone.SetSelected(zone)
	endZone.SetSelected(zone)
	fill := func(utc string, withTime bool, z string, d, tm *widget.Entry, s *widget.Select) {
		if utc == "" {
			return
		}
		t, err := time.Parse(time.RFC3339, utc)
		if err != nil {
			return
		}
		if z == "" {
			z = "UTC"
		}
		t = t.In(zoneOf(z))
		d.SetText(t.Format("2006-01-02"))
		if withTime {
			tm.SetText(t.Format("15:04"))
		}
		if !has(s.Options, z) {
			s.Options = append([]string{z}, s.Options...)
		}
		s.SetSelected(z)
	}
	if e != nil {
		title.SetText(e.Title)
		details.SetText(e.Details)
		fill(e.Start, e.StartTime, e.StartZone, startDate, startTime, startZone)
		fill(e.End, e.EndTime, e.EndZone, endDate, endTime, endZone)
	}
	when := func(d, tm, z string) (string, bool, error) {
		d, tm = strings.TrimSpace(d), strings.TrimSpace(tm)
		layout, val := "2006-01-02", d
		if tm != "" {
			layout, val = "2006-01-02 15:04", d+" "+tm
		}
		t, err := time.ParseInLocation(layout, val, zoneOf(z))
		if err != nil {
			return "", false, errors.New("dates look like 2026-12-05 and times like 18:30")
		}
		return t.UTC().Format(time.RFC3339), tm != "", nil
	}
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		ev := proto.Event{Title: title.Text, Details: details.Text, StartZone: startZone.Selected}
		if e != nil {
			ev.ID = e.ID
		}
		var err error
		if ev.Start, ev.StartTime, err = when(startDate.Text, startTime.Text, startZone.Selected); err != nil {
			problem.Set(err)
			return
		}
		if strings.TrimSpace(endDate.Text) != "" {
			ev.EndZone = endZone.Selected
			if ev.End, ev.EndTime, err = when(endDate.Text, endTime.Text, endZone.Selected); err != nil {
				problem.Set(err)
				return
			}
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "event_put", Event: &ev}, func(r proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Event saved")
			u.openPageArg("event", r.Message)
		})
	})
	heading := "New Event"
	if e != nil {
		heading = "Edit Event"
	}
	p = u.openEditor(calendarIcon, "Calendar", panelTitle(heading),
		field("Title", title, ""),
		container.NewGridWithColumns(3, field("Start Date", startDate, ""), field("Start Time", startTime, ""), field("Time Zone", startZone, "")),
		container.NewGridWithColumns(3, field("End Date", endDate, ""), field("End Time", endTime, ""), field("Time Zone", endZone, "")),
		field("Details", details, "Markdown: **bold**, lists, links and pictures."),
		container.NewHBox(u.pictureButton(g.ID, details, problem)),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	u.win.Canvas().Focus(title)
}

// ---- Threads ---------------------------------------------------

func voteButtons(likes, dislikes, mine int, vote func(int)) fyne.CanvasObject {
	like := widget.NewButton(fmt.Sprintf("👍 %d", likes), func() {
		if mine == 1 {
			vote(0)
		} else {
			vote(1)
		}
	})
	dislike := widget.NewButton(fmt.Sprintf("👎 %d", dislikes), func() {
		if mine == -1 {
			vote(0)
		} else {
			vote(-1)
		}
	})
	like.Importance, dislike.Importance = widget.LowImportance, widget.LowImportance
	if mine == 1 {
		like.Importance = widget.HighImportance
	}
	if mine == -1 {
		dislike.Importance = widget.HighImportance
	}
	return container.NewHBox(like, dislike)
}

func (u *UI) vote(g guildconn.GuildView, req proto.Request) {
	u.call(g.ID, req, func(_ proto.Response, err error) {
		if err != nil {
			u.toast(sentence(err.Error()))
		}
	})
}

func (u *UI) threadsPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "threads", proto.Request{Type: "threads"}, func(r proto.Response) any { return r.Posts })
	posts, ok := data.([]proto.Post)
	var head fyne.CanvasObject = layout.NewSpacer()
	if can(g, proto.PermPostThreads) {
		head = container.NewHBox(layout.NewSpacer(), primary("New Post", func() { u.openPostEditor(g, nil) }))
	}
	parts := []fyne.CanvasObject{head}
	switch {
	case !ok:
		parts = append(parts, loadingOr(data))
	case len(posts) == 0:
		parts = append(parts, para("No posts yet."))
	}
	for _, p := range posts {
		p := p
		if p.Deleted && p.Comments == 0 {
			continue
		}
		by := nameOf(g, p.Author) + " · " + msgTime(p.Time)
		body := container.NewVBox(text(p.Title, 17, colText, true), text(by, 12, colFaint, false))
		if p.Body != "" {
			ex := widget.NewLabel(firstLine(p.Body, 160))
			ex.Wrapping = fyne.TextWrapWord
			body.Add(dim(ex))
		}
		body.Add(container.NewHBox(
			voteButtons(p.Likes, p.Dislikes, p.MyVote, func(v int) { u.vote(g, proto.Request{Type: "vote", ItemID: p.ID, Vote: v}) }),
			text(fmt.Sprintf("💬 %d", p.Comments), 13, colDim, false)))
		parts = append(parts, card(4, newTapRow(body, false, func() { u.openPageArg("post", p.ID) })), gap(8))
	}
	return pageFrame(threadsIcon, "Threads", parts...)
}

type postData struct {
	post     *proto.Post
	comments []proto.Comment
}

func (u *UI) postPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "post:"+u.pageArg, proto.Request{Type: "thread_get", ItemID: u.pageArg},
		func(r proto.Response) any { return postData{r.Post, r.Comments} })
	pd, ok := data.(postData)
	if !ok || pd.post == nil {
		return pageFrame(threadsIcon, "Threads", backTo(u, "threads"), loadingOr(data))
	}
	p := *pd.post
	tools := container.NewHBox()
	link := widget.NewButton("Copy Link", func() {
		u.app.Clipboard().SetContent("nexus-post:" + g.ID + "/" + p.ID)
		u.toast("Link copied: paste it in a chat")
	})
	link.Importance = widget.LowImportance
	tools.Add(link)
	if p.Author == u.user.Profile.UserID && !p.Deleted {
		tools.Add(widget.NewButton("Edit", func() { u.openPostEditor(g, &p) }))
	}
	if !p.Deleted && (p.Author == u.user.Profile.UserID || can(g, proto.PermManagePages)) {
		del := widget.NewButton("Delete", func() {
			u.confirm("Delete Post", "Delete \""+p.Title+"\"? Its comments stay.", "Delete", true, func() {
				u.call(g.ID, proto.Request{Type: "post_delete", ItemID: p.ID}, func(_ proto.Response, err error) {
					if err != nil {
						u.toast(sentence(err.Error()))
					}
				})
			})
		})
		del.Importance = widget.DangerImportance
		tools.Add(del)
	}
	by := nameOf(g, p.Author) + " · " + msgTime(p.Time)
	if p.Edited {
		by += " · edited"
	}
	postBox := container.NewVBox(text(p.Title, 24, colText, true), text(by, 12.5, colFaint, false))
	if p.Body != "" {
		postBox.Add(gap(6))
		postBox.Add(u.guildMarkdown(g, p.Body))
	}
	postBox.Add(voteButtons(p.Likes, p.Dislikes, p.MyVote, func(v int) { u.vote(g, proto.Request{Type: "vote", ItemID: p.ID, Vote: v}) }))

	// Comments are shown as a tree.
	comments := container.NewVBox(text(fmt.Sprintf("COMMENTS (%d)", p.Comments), 11.5, colFaint, true))
	if can(g, proto.PermPostThreads) && !p.Deleted {
		box := widget.NewEntry() // It is one line, the same height as Comment.
		box.SetPlaceHolder("Write Comment")
		send := primary("Comment", nil)
		send.OnTapped = func() {
			if strings.TrimSpace(box.Text) == "" {
				return
			}
			send.Disable()
			u.call(g.ID, proto.Request{Type: "comment_put", Comment: &proto.Comment{Post: p.ID, Body: box.Text}}, func(_ proto.Response, err error) {
				send.Enable()
				if err != nil {
					u.toast(sentence(err.Error()))
					return
				}
				box.SetText("")
			})
		}
		box.OnSubmitted = func(string) { send.OnTapped() }
		comments.Add(container.NewBorder(nil, nil, nil, send, box))
	}
	children := map[string][]proto.Comment{}
	for _, c := range pd.comments {
		children[c.Parent] = append(children[c.Parent], c)
	}
	// Replies hang under their comment, with a line down from its picture
	// that joins them, like the channel tree.
	var draw func(parent string, depth int) fyne.CanvasObject
	draw = func(parent string, depth int) fyne.CanvasObject {
		box := container.NewVBox()
		for _, c := range children[parent] {
			c := c
			row := container.New(layout.NewCustomPaddedLayout(4, 4, 0, 0), u.commentRow(g, p, c))
			if len(children[c.ID]) == 0 {
				box.Add(row)
				continue
			}
			sub := draw(c.ID, depth+1)
			if depth < 6 {
				sub = container.NewBorder(nil, nil, threadLine(), nil, sub)
			}
			box.Add(container.NewVBox(row, sub))
		}
		return box
	}
	comments.Add(draw("", 0))
	return pageFrame(threadsIcon, "Threads", container.NewBorder(nil, nil, backTo(u, "threads"), tools),
		card(24, postBox), gap(12), card(20, comments))
}

func (u *UI) commentRow(g guildconn.GuildView, p proto.Post, c proto.Comment) fyne.CanvasObject {
	head := container.NewHBox(text(nameOf(g, c.Author), 13.5, nameColour(g, c.Author), true), text(msgTime(c.Time), 11.5, colFaint, false))
	var body fyne.CanvasObject
	if c.Deleted {
		body = text("This comment was deleted.", 13.5, colFaint, false)
	} else {
		body = u.guildMarkdown(g, c.Body)
	}
	// Replying happens right under the comment, not in a window.
	replyBox := widget.NewMultiLineEntry()
	replyBox.SetPlaceHolder("Write Reply")
	replyBox.SetMinRowsVisible(2)
	replyProblem := newErrorText()
	replyArea := stack(fieldGap)
	replyArea.Hide()
	sendReply := primary("Reply", func() {
		if strings.TrimSpace(replyBox.Text) == "" {
			return
		}
		u.call(g.ID, proto.Request{Type: "comment_put", Comment: &proto.Comment{Post: p.ID, Parent: c.ID, Body: replyBox.Text}}, func(_ proto.Response, err error) {
			if err != nil {
				replyProblem.Set(err)
				return
			}
			replyBox.SetText("")
			replyArea.Hide()
		})
	})
	replyArea.Objects = []fyne.CanvasObject{replyBox, replyProblem.box,
		container.NewHBox(layout.NewSpacer(), widget.NewButton("Cancel", func() { replyArea.Hide() }), sendReply)}
	tools := container.NewHBox()
	if !c.Deleted {
		tools.Add(voteButtons(c.Likes, c.Dislikes, c.MyVote, func(v int) {
			u.vote(g, proto.Request{Type: "vote", Comment: &proto.Comment{Post: p.ID, ID: c.ID}, Vote: v})
		}))
		if can(g, proto.PermPostThreads) && !p.Deleted {
			reply := widget.NewButton("Reply", func() { replyArea.Show(); u.win.Canvas().Focus(replyBox) })
			reply.Importance = widget.LowImportance
			tools.Add(reply)
		}
		if c.Author == u.user.Profile.UserID || can(g, proto.PermManagePages) {
			del := widget.NewButton("Delete", func() {
				u.call(g.ID, proto.Request{Type: "comment_delete", Comment: &proto.Comment{Post: p.ID, ID: c.ID}}, func(_ proto.Response, err error) {
					if err != nil {
						u.toast(sentence(err.Error()))
					}
				})
			})
			del.Importance = widget.LowImportance
			tools.Add(del)
		}
	}
	return container.NewBorder(nil, nil, container.NewVBox(avatar(nameOf(g, c.Author), c.Author, 28)), nil,
		container.NewVBox(head, body, tools, replyArea))
}

func (u *UI) openPostEditor(g guildconn.GuildView, p *proto.Post) {
	var pop *popup
	title := widget.NewEntry()
	body := widget.NewMultiLineEntry()
	body.SetMinRowsVisible(10)
	body.Wrapping = fyne.TextWrapWord
	problem := newErrorText()
	heading := "New Post"
	if p != nil {
		heading = "Edit Post"
		title.SetText(p.Title)
		body.SetText(p.Body)
	}
	var save *widget.Button
	save = primary("Post", func() {
		problem.Set(nil)
		np := proto.Post{Title: title.Text, Body: body.Text}
		if p != nil {
			np.ID = p.ID
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "post_put", Post: &np}, func(r proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			pop.Close()
			u.openPageArg("post", r.Message)
		})
	})
	pop = u.openEditor(threadsIcon, "Threads", panelTitle(heading), field("Title", title, ""),
		field("Text", body, "Markdown: **bold**, lists, links and pictures."),
		container.NewHBox(u.pictureButton(g.ID, body, problem)), problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { pop.Close() }), save))
	u.win.Canvas().Focus(title)
}

// ---- Wiki ----------------------------------------------------------------

func (u *UI) wikiPage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "wiki", proto.Request{Type: "wiki_list"}, func(r proto.Response) any { return r.Wikis })
	pages, ok := data.([]proto.WikiPage)
	var head fyne.CanvasObject = layout.NewSpacer()
	if can(g, proto.PermEditWiki) {
		head = container.NewHBox(layout.NewSpacer(), primary("New Page", func() { u.openWikiEditor(g, nil) }))
	}
	list := stack(4) // This is a column, so text lines up.
	switch {
	case !ok:
		list.Add(loadingOr(data))
	case len(pages) == 0:
		list.Add(para("No pages yet."))
	}
	group := "\x00"
	for _, w := range pages {
		w := w
		if w.Group != group {
			group = w.Group
			title := strings.ToUpper(group)
			if title == "" {
				title = "PAGES"
			}
			list.Add(gap(6))
			list.Add(text(title, 11.5, colFaint, true))
		}
		b := widget.NewButtonWithIcon(w.Title, wikiIcon, func() { u.openPageArg("wikipage", w.ID) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		list.Add(b)
	}
	return pageFrame(wikiIcon, "Wiki", head, card(18, list))
}

func (u *UI) wikiOnePage(g guildconn.GuildView) fyne.CanvasObject {
	data := u.pageLoad(g, "wikipage:"+u.pageArg, proto.Request{Type: "wiki_get", ItemID: u.pageArg},
		func(r proto.Response) any { return r.Wiki })
	w, ok := data.(*proto.WikiPage)
	if !ok || w == nil {
		return pageFrame(wikiIcon, "Wiki", backTo(u, "wiki"), loadingOr(data))
	}
	tools := container.NewHBox()
	if can(g, proto.PermEditWiki) {
		page := *w
		tools.Add(widget.NewButton("Edit", func() { u.openWikiEditor(g, &page) }))
		del := widget.NewButton("Delete", func() {
			u.confirm("Delete Page", "Delete \""+page.Title+"\"?", "Delete", true, func() {
				u.call(g.ID, proto.Request{Type: "wiki_delete", ItemID: page.ID}, func(_ proto.Response, err error) {
					if err != nil {
						u.toast(sentence(err.Error()))
						return
					}
					u.openPage("wiki")
				})
			})
		})
		del.Importance = widget.DangerImportance
		tools.Add(del)
	}
	meta := "Updated " + msgTime(w.Updated) + " by " + nameOf(g, w.Author)
	return pageFrame(wikiIcon, "Wiki", container.NewBorder(nil, nil, backTo(u, "wiki"), tools),
		card(24, container.NewVBox(text(w.Title, 26, colText, true), text(meta, 12, colFaint, false), gap(6), u.guildMarkdown(g, w.Body))))
}

func (u *UI) openWikiEditor(g guildconn.GuildView, w *proto.WikiPage) {
	var p *popup
	title, group := widget.NewEntry(), widget.NewEntry()
	group.SetPlaceHolder("Optional")
	body := widget.NewMultiLineEntry()
	body.SetMinRowsVisible(14)
	body.Wrapping = fyne.TextWrapWord
	problem := newErrorText()
	heading := "New Page"
	if w != nil {
		heading = "Edit Page"
		title.SetText(w.Title)
		group.SetText(w.Group)
		body.SetText(w.Body)
	}
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		np := proto.WikiPage{Title: title.Text, Group: group.Text, Body: body.Text}
		if w != nil {
			np.ID = w.ID
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "wiki_put", Wiki: &np}, func(r proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.openPageArg("wikipage", r.Message)
		})
	})
	p = u.openEditor(wikiIcon, "Wiki", panelTitle(heading),
		container.NewGridWithColumns(2, field("Title", title, ""), field("Heading", group, "Pages are listed under their heading.")),
		field("Text", body, "Markdown. [[Page Title]] links to another page, #channel-name to a channel."),
		container.NewHBox(u.pictureButton(g.ID, body, problem)), problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	u.win.Canvas().Focus(title)
}

// ---- the Rules-change notice ----------------------------------------------------

func (u *UI) dismissedPath() string { return filepath.Join(u.ConfigDir, "notices.json") }

func (u *UI) dismissed() map[string]string {
	m := map[string]string{}
	if data, err := os.ReadFile(u.dismissedPath()); err == nil {
		json.Unmarshal(data, &m)
	}
	return m
}

// rulesNotice tells a member who joined by agreeing that the Rules changed.
func (u *UI) rulesNotice(g guildconn.GuildView) fyne.CanvasObject {
	if g.Join == nil || g.Join.RulesUpdated == "" {
		return nil
	}
	var me *guildconn.MemberView
	for i := range g.Members {
		if g.Members[i].IsMe {
			me = &g.Members[i]
		}
	}
	if me == nil || !me.AgreedRules || g.IsOwner || g.Join.RulesUpdated <= me.Joined {
		return nil
	}
	// Shown once: it is marked as seen the first time, and stays
	// up until the app is closed. Nothing needs clicking.
	key := u.user.Profile.UserID + "/" + g.ID
	updated := g.Join.RulesUpdated
	if u.noticeShown[key] != updated {
		if u.dismissed()[key] == updated {
			return nil
		}
		m := u.dismissed()
		m[key] = updated
		data, _ := json.Marshal(m)
		os.WriteFile(u.dismissedPath(), data, 0o600)
		if u.noticeShown == nil {
			u.noticeShown = map[string]string{}
		}
		u.noticeShown[key] = updated
	}
	when := g.Join.RulesUpdated
	if t, err := time.Parse(time.RFC3339, when); err == nil {
		when = t.Local().Format("2 January 2006")
	}
	l := wrapText("The Rules of "+g.Name+" were updated on "+when+". You agreed to the Rules when you joined; by staying a member, you agree to the new Rules too.", colText)
	read := widget.NewButtonWithIcon("Rules", shieldIcon, func() { u.openPage("rules") })
	return card(16, container.NewBorder(nil, nil, nil, container.NewCenter(read), plain(titleGap, text("RULES UPDATED", 11.5, colIdle, true), l)))
}

// threadLine is the line down the left of a comment's replies, starting
// under the comment's picture.
func threadLine() fyne.CanvasObject {
	l := canvas.NewRectangle(colLineStrong)
	return container.New(threadRail{}, l)
}

type threadRail struct{}

func (threadRail) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(28, 1) }

func (threadRail) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	objs[0].Move(fyne.NewPos(13, 0))
	objs[0].Resize(fyne.NewSize(2, s.Height-6))
}
