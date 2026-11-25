package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
)

// This file shows pictures and files in channels, and custom emoji.

// ---- emoji pictures ----------------------------------------------------------------------

// emojiRes returns an emoji's picture, or nil while it is fetched (the chat
// is drawn again when it arrives).
func (u *UI) emojiRes(hash string) fyne.Resource {
	if u.emojiCache == nil {
		u.emojiCache = map[string]fyne.Resource{}
	}
	if r, ok := u.emojiCache[hash]; ok {
		return r
	}
	if u.fetching == nil {
		u.fetching = map[string]bool{}
	}
	if u.fetching["e"+hash] {
		return nil
	}
	u.fetching["e"+hash] = true
	go func() {
		path, err := u.guilds.EmojiFile(hash)
		var data []byte
		if err == nil {
			data, err = os.ReadFile(path)
		}
		fyne.Do(func() {
			delete(u.fetching, "e"+hash)
			if err != nil {
				return
			}
			u.emojiCache[hash] = fyne.NewStaticResource("emoji-"+hash, data)
			u.redrawChat()
		})
	}()
	return nil
}

func (u *UI) redrawChat() {
	if u.chat != nil {
		u.chat.shown = ""
		u.drawMessages(false)
	}
	u.redraw()
}

func emojiImage(res fyne.Resource, size float32) *canvas.Image {
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(size, size))
	return img
}

// emojiOnly reports whether the text is nothing but custom emoji (shown big).
func emojiOnly(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, t := range guildconn.EmojiTokens(s) {
		s = strings.Replace(s, t[0], "", 1)
	}
	return strings.TrimSpace(s) == ""
}

// shortEmoji shows custom emoji tokens as ":name:" inside text.
func shortEmoji(s string) string {
	for _, t := range guildconn.EmojiTokens(s) {
		s = strings.Replace(s, t[0], ":"+t[1]+":", 1)
	}
	return s
}

// ---- a channel message's body -------------------------------------------------------------

// messageBody draws a message's text: files and pictures, big emoji, or
// Markdown with names for mentions.
func (u *UI) messageBody(c *chatState, m *guildconn.ChatView) fyne.CanvasObject {
	plain := func(s string) fyne.CanvasObject { return u.markdownFlush(shortEmoji(c.withNames(s, true))) }
	// These are polls, spoilers and forwarded messages.
	if x := u.messageExtras(c, m, plain); x != nil {
		return x
	}
	if f, ok := guildconn.ParseFileRef(m.Text); ok {
		return u.fileBlock(c, f)
	}
	if emojiOnly(m.Text) {
		row := container.NewHBox()
		for _, t := range guildconn.EmojiTokens(m.Text) {
			if res := u.emojiRes(t[2]); res != nil {
				row.Add(emojiImage(res, 44))
			} else {
				row.Add(text(":"+t[1]+":", 14, colDim, false))
			}
		}
		return row
	}
	return u.markdownFlush(shortEmoji(c.withNames(m.Text, true)))
}

// fileBlock shows a picture (fetched and decrypted on this computer) or a
// file to open.
func (u *UI) fileBlock(c *chatState, f guildconn.FileRef) fyne.CanvasObject {
	if u.files == nil {
		u.files = map[string]string{}
	}
	path := u.files[f.Hash]
	if path == "" && !u.fetching["f"+f.Hash] {
		if u.fetching == nil {
			u.fetching = map[string]bool{}
		}
		u.fetching["f"+f.Hash] = true
		gid, ch := c.guild, c.channel
		go func() {
			var p string
			gc, err := u.guilds.Get(gid)
			if err == nil {
				p, err = gc.OpenFile(ch, f)
			}
			fyne.Do(func() {
				delete(u.fetching, "f"+f.Hash)
				if err == nil {
					u.files[f.Hash] = p
					u.redrawChat()
				}
			})
		}()
	}
	open := widget.NewButton(fmt.Sprintf("📎 %s (%s)", f.Name, sizeText(f.Size)), func() {
		if path != "" {
			u.app.OpenURL(&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)})
		}
	})
	open.Importance = widget.LowImportance
	if path == "" {
		open.Disable()
	}
	low := strings.ToLower(f.Name)
	if path != "" && (strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".gif")) {
		if data, err := os.ReadFile(path); err == nil {
			img := canvas.NewImageFromResource(fyne.NewStaticResource(f.Hash, data))
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(320, 220))
			return container.NewVBox(container.NewHBox(img), container.NewHBox(open))
		}
	}
	return container.NewHBox(open)
}

// reactionButton is a reaction under a message: a normal emoji, or a custom
// emoji's picture.
func (u *UI) reactionButton(emoji string, count int, mine bool, tap func()) *widget.Button {
	var b *widget.Button
	if _, hash, ok := guildconn.ParseEmoji(emoji); ok {
		if res := u.emojiRes(hash); res != nil {
			b = widget.NewButtonWithIcon(itoa(count), res, tap)
		} else {
			b = widget.NewButton("… "+itoa(count), tap)
		}
	} else {
		b = widget.NewButton(emoji+" "+itoa(count), tap)
	}
	if mine {
		b.Importance = widget.HighImportance
	}
	return b
}

// customReactions are menu items for your guilds' emoji.
func (u *UI) customReactions(gid string, react func(token string)) []*fyne.MenuItem {
	var items []*fyne.MenuItem
	for _, e := range u.guilds.UsableEmoji(gid) {
		e := e
		it := fyne.NewMenuItem(":"+e.Name+":", func() { react(e.Token()) })
		if res := u.emojiRes(e.Hash); res != nil {
			it.Icon = res
		}
		items = append(items, it)
		if len(items) >= 40 {
			break
		}
	}
	return items
}

// ---- the message box's File and Emoji buttons ---------------------------------------------

func (u *UI) sendChannelFile() {
	c := u.chat
	if c == nil {
		return
	}
	reply := ""
	if c.replyTo != nil {
		reply = c.replyTo.ID
	}
	go func() {
		path, err := chooseFile("Choose a picture or file to send")
		if err != nil || path == "" {
			if err != nil {
				fyne.Do(func() { c.problem.Set(err) })
			}
			return
		}
		data, err := os.ReadFile(path)
		var gc *guildconn.Conn
		if err == nil {
			gc, err = u.guilds.Get(c.guild)
		}
		if err == nil {
			c.atBottom = true
			err = gc.SendFile(c.channel, filepath.Base(path), data, reply)
		}
		fyne.Do(func() {
			if err != nil {
				c.problem.Set(err)
				return
			}
			u.cancelBar()
			u.loadMessages("")
		})
	}()
}

// openEmojiPicker is a panel just above the message box, like other chat
// apps: a search box, then the emoji of your guilds, then the
// normal emoji in groups. A click adds it to the message; clicking outside
// closes the panel. anchor is the emoji button (nil: the bottom left).
func (u *UI) openEmojiPicker(entry *widget.Entry, gid string, anchor fyne.CanvasObject) {
	var pop *widget.PopUp
	insert := func(s string) {
		t := entry.Text
		if t != "" && !strings.HasSuffix(t, " ") {
			t += " "
		}
		entry.SetText(t + s + " ")
		pop.Hide()
		u.win.Canvas().Focus(entry)
	}
	cell := func(face fyne.CanvasObject, tap func()) fyne.CanvasObject {
		r := newTapRow(container.NewCenter(face), false, tap)
		r.base = colMenu
		return r
	}
	search := widget.NewEntry()
	search.SetPlaceHolder("Search Emoji")
	body := container.NewVBox()
	var custom []guildconn.EmojiChoice
	// A friend chat ("") has only the normal emoji, because a friend may not have a guild's pictures.
	if gid != "" {
		custom = u.guilds.UsableEmoji(gid)
	}
	fill := func(q string) {
		q = strings.ToLower(strings.TrimSpace(q))
		body.RemoveAll()
		section := func(title string, cells []fyne.CanvasObject) {
			if len(cells) == 0 {
				return
			}
			body.Add(container.New(layout.NewCustomPaddedLayout(8, 2, 6, 0), text(title, 11, colFaint, true)))
			body.Add(container.NewGridWrap(fyne.NewSize(40, 40), cells...))
		}
		var mine []fyne.CanvasObject
		for _, e := range custom {
			e := e
			if q != "" && !strings.Contains(e.Name, q) {
				continue
			}
			var face fyne.CanvasObject = text(":"+e.Name+":", 10, colDim, false)
			if res := u.emojiRes(e.Hash); res != nil {
				face = emojiImage(res, 26)
			}
			mine = append(mine, cell(face, func() { insert(":" + e.Name + ":") }))
		}
		section("YOUR GUILDS", mine)
		for _, grp := range pickerGroups() {
			var cells []fyne.CanvasObject
			for _, e := range grp.list {
				e := e
				if q != "" && !strings.Contains(e.words, q) {
					continue
				}
				cells = append(cells, cell(text(e.char, 22, colText, false), func() { insert(e.char) }))
			}
			section(grp.title, cells)
		}
		if len(body.Objects) == 0 {
			body.Add(container.New(layout.NewCustomPaddedLayout(10, 0, 6, 0), para("No emoji match.")))
		}
		body.Refresh()
	}
	search.OnChanged = fill
	fill("")
	sc := container.NewVScroll(body)
	sc.SetMinSize(fyne.NewSize(10, 300))
	bg := canvas.NewRectangle(colMenu)
	bg.CornerRadius = 14
	bg.StrokeColor = colLineStrong
	bg.StrokeWidth = 1
	panel := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(10, 10, 10, 10),
		container.NewBorder(search, nil, nil, nil, sc)))
	size := fyne.NewSize(392, 400)
	pop = widget.NewPopUp(container.New(fixedWidth{size.Width}, panel), u.win.Canvas())
	ws := u.win.Canvas().Size()
	pos := fyne.NewPos(360, ws.Height-size.Height-90)
	if anchor != nil {
		if d := fyne.CurrentApp().Driver(); d != nil {
			a := d.AbsolutePositionForObject(anchor)
			if a.X > 0 || a.Y > 0 {
				pos = fyne.NewPos(a.X, a.Y-size.Height-8)
			}
		}
	}
	pop.ShowAtPosition(pos)
	pop.Resize(size)
	u.win.Canvas().Focus(search)
}

// ---- the guild's Emoji window (Manage Guild) ---------------------------------------------------

func (u *UI) openGuildEmoji(g guildconn.GuildView) {
	var p *popup
	gc, err := u.guilds.Get(g.ID)
	if err != nil {
		u.toast(sentence(err.Error()))
		return
	}
	list := stack(4) // This is a column, so text lines up.
	problem := newErrorText()
	var fill func()
	fill = func() {
		list.RemoveAll()
		emoji := gc.GuildEmoji()
		if len(emoji) == 0 {
			list.Add(para("No emoji yet."))
		}
		for _, e := range emoji {
			e := e
			var face fyne.CanvasObject = gap(1)
			if res := u.emojiRes(e.Hash); res != nil {
				face = emojiImage(res, 28)
			}
			del := widget.NewButtonWithIcon("", xIcon, func() {
				problem.Run(func() error { return gc.DeleteEmoji(e.Name) }, fill)
			})
			del.Importance = widget.LowImportance
			list.Add(container.NewBorder(nil, nil, container.NewHBox(face, gap(1)), del, text(":"+e.Name+":", 14, colText, false)))
		}
		list.Refresh()
	}
	fill()
	name := widget.NewEntry()
	name.SetPlaceHolder("pixel_heart")
	add := primary("Add Emoji", func() {
		problem.Set(nil)
		n := strings.ToLower(strings.TrimSpace(name.Text))
		go func() {
			path, err := choosePicture("Choose a picture for the emoji")
			if err != nil || path == "" {
				if err != nil {
					fyne.Do(func() { problem.Set(err) })
				}
				return
			}
			data, err := os.ReadFile(path)
			if err == nil {
				data, err = imaging.FitPicture(data, 128, 250*1024)
			}
			if err == nil {
				err = gc.PutEmoji(n, data)
			}
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
					return
				}
				name.SetText("")
				fill()
				u.toast("Emoji added")
			})
		}()
	})
	p = u.openPanel("emoji", 520, text("Custom Emoji", 22, colText, true),
		para(fmt.Sprintf("Up to 50 emoji. Members use them as :name: in messages and reactions, here and in their other guilds. This guild has %d.", len(gc.GuildEmoji()))),
		list, field("Name", container.NewBorder(nil, nil, nil, add, name), "2 to 32 small letters, digits or _. Then pick a picture."),
		problem.box, buttonsRow(widget.NewButton("Close", func() { p.Close() })))
}
