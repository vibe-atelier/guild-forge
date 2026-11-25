package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// This file is the channel list (categories, folders, channels)
// and the owner's channel editor.

// ---- folded groups, remembered on this computer --------------------

func (u *UI) collapsedPath() string { return filepath.Join(u.ConfigDir, "collapsed.json") }

// isCollapsedIn reports whether a category is folded. The guild keeps your folded categories; this
// computer's copy is used until it has given them.
func (u *UI) isCollapsedIn(g guildconn.GuildView, id string) bool {
	if gc, err := u.guilds.Get(g.ID); err == nil {
		if list, ok := gc.Collapsed(); ok {
			return slices.Contains(list, id)
		}
	}
	return u.isCollapsed(id)
}

func (u *UI) toggleCollapsedIn(g guildconn.GuildView, id string) {
	now := !u.isCollapsedIn(g, id)
	u.isCollapsed(id)
	if now != u.collapsed[id] {
		u.toggleCollapsed(id) // This computer's copy changes too.
	}
	if gc, err := u.guilds.Get(g.ID); err == nil {
		if list, ok := gc.Collapsed(); ok {
			list = slices.DeleteFunc(list, func(s string) bool { return s == id })
			if now {
				list = append(list, id)
			}
			gc.SetCollapsed(list)
		}
	}
	u.redraw()
}

func (u *UI) isCollapsed(id string) bool {
	if u.collapsed == nil {
		u.collapsed = map[string]bool{}
		if data, err := os.ReadFile(u.collapsedPath()); err == nil {
			json.Unmarshal(data, &u.collapsed)
		}
	}
	return u.collapsed[id]
}

func (u *UI) toggleCollapsed(id string) {
	u.isCollapsed(id)
	if u.collapsed[id] {
		delete(u.collapsed, id)
	} else {
		u.collapsed[id] = true
	}
	data, _ := json.Marshal(u.collapsed)
	os.WriteFile(u.collapsedPath(), data, 0o600)
	u.redraw()
}

// ---- the tree -------------------------------------------------------------

func (u *UI) channelTree(g guildconn.GuildView) fyne.CanvasObject {
	head := container.NewBorder(nil, nil, nil, nil, text("CHANNELS", 11.5, colFaint, true))
	if can(g, proto.PermManageChannels) {
		add := widget.NewButton("+", func() { u.openChannelEditor(g, nil) })
		add.Importance = widget.LowImportance
		head = container.NewBorder(nil, nil, nil, add, text("CHANNELS", 11.5, colFaint, true))
	}
	box := container.NewVBox(container.New(layout.NewCustomPaddedLayout(14, 2, 8, 0), head))
	if len(g.Channels) == 0 {
		msg := "No channels yet."
		if can(g, proto.PermManageChannels) {
			msg = "No channels yet. Click + to make one."
		}
		box.Add(container.New(layout.NewCustomPaddedLayout(0, 0, 8, 8), hintText(msg)))
		return box
	}
	// Staff drag rows to a new place.
	var drag *chanDrag
	if can(g, proto.PermManageChannels) {
		drag = &chanDrag{nodes: map[string]proto.ChannelNode{}, drop: func(id, under string, place int) {
			u.call(g.ID, proto.Request{Type: "channel_move", ChannelID: id, Under: under, Place: place}, func(_ proto.Response, err error) {
				if err != nil {
					u.toast(sentence(err.Error()))
				}
			})
		}}
		for _, n := range g.Channels {
			drag.nodes[n.ID] = n.ChannelNode
		}
	}
	// more[i] says whether more rows follow at level i, so its line goes on
	// down past this row.
	var draw func(parent string, more []bool, top bool)
	draw = func(parent string, more []bool, top bool) {
		var kids []guildconn.ChannelView
		for _, n := range g.Channels {
			if n.Parent == parent {
				kids = append(kids, n)
			}
		}
		for k, n := range kids {
			n := n
			last := k == len(kids)-1
			var row fyne.CanvasObject
			if n.Kind == proto.KindChannel {
				row = u.channelRow(g, n)
			} else if n.Kind == proto.KindVoice {
				row = u.voiceRow(g, n)
			} else {
				open := !u.isCollapsedIn(g, n.ID)
				arrow := "▸"
				if open {
					arrow = "▾"
				}
				toggle := func() { u.toggleCollapsedIn(g, n.ID) }
				if n.Kind == proto.KindCategory {
					r := newTreeRow(nil, arrow+" "+strings.ToUpper(n.Name), toggle, u.channelGear(g, n.ChannelNode))
					r.bold = true
					row = r
				} else {
					row = newTreeRow(dirIcon, n.Name+"  "+arrow, toggle, u.channelGear(g, n.ChannelNode))
				}
			}
			if tr := findTreeRow(row); tr != nil && drag != nil {
				tr.drag, tr.nodeID = drag, n.ID
				drag.rows = append(drag.rows, tr)
			}
			if !top {
				row = container.NewBorder(nil, nil, newTreeGuide(more, last), nil, row)
			}
			box.Add(row)
			if !proto.IsRoom(n.Kind) && !u.isCollapsedIn(g, n.ID) {
				if top {
					draw(n.ID, nil, false)
				} else {
					draw(n.ID, append(append([]bool{}, more...), !last), false)
				}
			}
		}
	}
	draw("", nil, true)
	return box
}

// treeGuide draws the lines that show which category or folder a row is in: a
// line down for each level, and a short line across to the row.
type treeGuide struct {
	more []bool
	last bool
}

const guideStep = 14

func newTreeGuide(more []bool, last bool) fyne.CanvasObject {
	t := &treeGuide{more: more, last: last}
	var objs []fyne.CanvasObject
	line := func() *canvas.Rectangle {
		r := canvas.NewRectangle(colFaint) // The colour is bright enough to see.
		objs = append(objs, r)
		return r
	}
	for range more {
		line()
	}
	line() // This line goes down to this row.
	line() // This line goes across to it.
	return container.New(t, objs...)
}

func (t *treeGuide) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(float32(len(t.more)+1)*guideStep+4, 1)
}

func (t *treeGuide) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	x := func(level int) float32 { return 9 + float32(level)*guideStep }
	mid := s.Height / 2
	for i, m := range t.more {
		if m {
			objs[i].Move(fyne.NewPos(x(i), 0))
			objs[i].Resize(fyne.NewSize(2, s.Height))
		} else {
			objs[i].Resize(fyne.NewSize(0, 0))
		}
	}
	n := len(t.more)
	down := s.Height
	if t.last {
		down = mid
	}
	objs[n].Move(fyne.NewPos(x(n), 0))
	objs[n].Resize(fyne.NewSize(2, down))
	objs[n+1].Move(fyne.NewPos(x(n), mid-1))
	objs[n+1].Resize(fyne.NewSize(guideStep-4, 2))
}

// channelGear is what a row's gear does, or nil without Manage Channels.
func (u *UI) channelGear(g guildconn.GuildView, n proto.ChannelNode) func() {
	if !can(g, proto.PermManageChannels) {
		return nil
	}
	return func() { u.openChannelSettings(g, n, "") }
}

func (u *UI) channelRow(g guildconn.GuildView, n guildconn.ChannelView) fyne.CanvasObject {
	label := "#  " + n.Name
	if n.NoKey {
		label += "  🔒"
	}
	r := newTreeRow(nil, label, func() { u.openChat(n.ID) }, u.channelGear(g, n.ChannelNode))
	open := u.page == "chat" && u.chat != nil && u.chat.channel == n.ID
	r.active = open
	if open {
		r.colour = colText
	}
	if n.Mentions > 0 {
		bg := canvas.NewRectangle(colDanger)
		bg.CornerRadius = 9
		r.extra = append(r.extra, container.NewCenter(container.NewStack(bg,
			container.New(layout.NewCustomPaddedLayout(1, 1, 6, 6), text(fmt.Sprint(n.Mentions), 11, rgb(255, 255, 255), true)))))
	}
	if n.Unread && !open {
		// An unread channel gets a white marker and a bright, bold name.
		r.colour, r.bold = colText, true
		mark := canvas.NewRectangle(rgb(255, 255, 255))
		mark.CornerRadius = 2
		mark.SetMinSize(fyne.NewSize(4, 10))
		r.left = mark
	}
	return r
}

// ---- New Channel, and Channel Settings ----------------------------
//
// New Channel is a small window: kind, name, where it goes, topic. Changing
// one later happens on its Channel Settings page (the gear): Overview,
// Messages, Permissions, and Delete at the bottom.

var keepChoices = []string{"Inherit", "Forever", "7 Days", "30 Days", "90 Days", "1 Year"}
var keepDays = []int{-1, 0, 7, 30, 90, 365}

var kindNames = map[string]string{proto.KindChannel: "Channel", proto.KindVoice: "Voice Channel", proto.KindCategory: "Category", proto.KindFolder: "Folder"}

// placeBoxes holds the Parent Category and Parent Folder lists.
type placeBoxes struct {
	category, folder  *widget.Select
	catIDs, folderIDs []string
	box               fyne.CanvasObject
}

// newPlaceBoxes fills the two lists from the guild's channels. self is the
// folder being edited (it and what is inside it can't be its own parent).
func newPlaceBoxes(g guildconn.GuildView, self string) *placeBoxes {
	pb := &placeBoxes{category: widget.NewSelect(nil, nil), folder: widget.NewSelect(nil, nil)}
	byID := map[string]guildconn.ChannelView{}
	for _, n := range g.Channels {
		byID[n.ID] = n
	}
	inside := func(id, top string) bool { // It reports whether id is at or under top.
		for id != "" {
			if id == top {
				return true
			}
			id = byID[id].Parent
		}
		return false
	}
	categoryOf := func(id string) string {
		for id != "" {
			if byID[id].Kind == proto.KindCategory {
				return id
			}
			id = byID[id].Parent
		}
		return ""
	}
	var path func(id string) string
	path = func(id string) string {
		n := byID[id]
		if p := byID[n.Parent]; p.Kind == proto.KindFolder {
			return path(n.Parent) + " › " + n.Name
		}
		return n.Name
	}
	labels := []string{"None"}
	pb.catIDs = []string{""}
	for _, n := range g.Channels {
		if n.Kind == proto.KindCategory {
			labels, pb.catIDs = append(labels, n.Name), append(pb.catIDs, n.ID)
		}
	}
	pb.category.Options = labels
	pb.category.OnChanged = func(string) {
		cat := pb.catIDs[max(0, pb.category.SelectedIndex())]
		labels := []string{"None"}
		pb.folderIDs = []string{""}
		for _, n := range g.Channels {
			if n.Kind != proto.KindFolder || cat == "" || categoryOf(n.ID) != cat || (self != "" && inside(n.ID, self)) {
				continue
			}
			labels, pb.folderIDs = append(labels, path(n.ID)), append(pb.folderIDs, n.ID)
		}
		pb.folder.Options = labels
		pb.folder.SetSelectedIndex(0)
		if len(labels) == 1 {
			pb.folder.Disable()
		} else {
			pb.folder.Enable()
		}
	}
	pb.category.SetSelectedIndex(0)
	pb.box = container.NewGridWithColumns(2, field("Parent Category", pb.category, ""), field("Parent Folder", pb.folder, ""))
	return pb
}

// set shows where a node is now.
func (pb *placeBoxes) set(g guildconn.GuildView, parent string) {
	byID := map[string]guildconn.ChannelView{}
	for _, n := range g.Channels {
		byID[n.ID] = n
	}
	cat, folder := "", ""
	if p, ok := byID[parent]; ok {
		if p.Kind == proto.KindFolder {
			folder = parent
		}
		for id := parent; id != ""; id = byID[id].Parent {
			if byID[id].Kind == proto.KindCategory {
				cat = id
			}
		}
	}
	for i, id := range pb.catIDs {
		if id == cat {
			pb.category.SetSelectedIndex(i)
		}
	}
	for i, id := range pb.folderIDs {
		if id == folder {
			pb.folder.SetSelectedIndex(i)
		}
	}
}

// parent is the chosen place: the folder if one is chosen, else the category.
func (pb *placeBoxes) parent() string {
	if i := pb.folder.SelectedIndex(); i > 0 && i < len(pb.folderIDs) {
		return pb.folderIDs[i]
	}
	if i := pb.category.SelectedIndex(); i > 0 && i < len(pb.catIDs) {
		return pb.catIDs[i]
	}
	return ""
}

// openChannelEditor makes a new channel, category or folder (node == nil),
// or opens an existing one's settings page.
func (u *UI) openChannelEditor(g guildconn.GuildView, node *proto.ChannelNode) {
	if node != nil {
		u.openChannelSettings(g, *node, "")
		return
	}
	var p *popup
	// These are the kinds a node can be.
	kinds := []string{"Text Channel", "Voice Channel", "Category", "Folder"}
	kindValues := []string{proto.KindChannel, proto.KindVoice, proto.KindCategory, proto.KindFolder}
	kind := widget.NewSelect(kinds, nil)
	name := widget.NewEntry()
	name.SetPlaceHolder("Enter Name")
	topic := widget.NewEntry()
	topic.SetPlaceHolder("Optional")
	place := newPlaceBoxes(g, "")
	topicBox := field("Topic", topic, "")
	problem := newErrorText()
	kind.OnChanged = func(string) {
		k := kindValues[max(0, kind.SelectedIndex())]
		if k == proto.KindCategory {
			place.box.Hide()
		} else {
			place.box.Show()
		}
		if k == proto.KindChannel {
			topicBox.Show()
		} else {
			topicBox.Hide()
		}
	}
	kind.SetSelectedIndex(0)
	var save *widget.Button
	save = primary("Create", func() {
		problem.Set(nil)
		k := kindValues[max(0, kind.SelectedIndex())]
		n := proto.ChannelNode{Kind: k, Name: name.Text, Topic: topic.Text, Order: len(g.Channels)}
		if k != proto.KindCategory {
			n.Parent = place.parent()
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "channel_put", Node: &n}, func(r proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast(kindNames[k] + " made.")
			if k == proto.KindChannel && r.Message != "" {
				u.openChat(r.Message)
			}
		})
	})
	name.OnSubmitted = func(string) { save.OnTapped() }
	p = u.openPopup(540, text("New Channel", 22, colText, true),
		field("Kind", kind, ""), field("Name", name, ""), place.box, topicBox,
		hintText("More settings, like how long messages are kept and who can see it, are on its settings page. Click its gear."),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	u.win.Canvas().Focus(name)
}

// openChannelSettings opens the settings page of a channel, folder or
// category, at one of its tabs ("" = Overview).
func (u *UI) openChannelSettings(g guildconn.GuildView, n proto.ChannelNode, cat string) {
	u.leaveChat()
	u.selected = g.ID
	u.settings = &settingsView{scope: g.ID, node: n.ID}
	if cat == "" {
		cat = "chanoverview"
	}
	u.openSettingsCat(cat)
}

// nodeNow is a channel node as the guild has it now.
func (u *UI) nodeNow(gid, id string) (guildconn.GuildView, proto.ChannelNode, bool) {
	g := u.findGuild(gid)
	if g == nil {
		return guildconn.GuildView{}, proto.ChannelNode{}, false
	}
	for _, n := range g.Channels {
		if n.ID == id {
			return *g, n.ChannelNode, true
		}
	}
	return *g, proto.ChannelNode{}, false
}

func (u *UI) channelCats(g guildconn.GuildView, n proto.ChannelNode) []settingsCat {
	group := strings.ToUpper(kindNames[n.Kind])
	cats := []settingsCat{
		{"chanoverview", "Overview", group, func() { u.channelOverview(g.ID, n.ID) }},
	}
	if n.Kind != proto.KindVoice { // A voice channel has no messages.
		cats = append(cats, settingsCat{"chanmessages", "Messages", group, func() { u.channelMessages(g.ID, n.ID) }})
	}
	if can(g, proto.PermManageRoles) {
		cats = append(cats, settingsCat{"chanperms", "Permissions", group, func() {
			if gv, nv, ok := u.nodeNow(g.ID, n.ID); ok {
				u.openChannelRules(gv, nv)
			}
		}})
	}
	return cats
}

// saveNode sends a changed node to the guild.
func (u *UI) saveNode(gid string, n proto.ChannelNode, problem *errorText, save *widget.Button, done func()) {
	save.Disable()
	u.call(gid, proto.Request{Type: "channel_put", Node: &n}, func(_ proto.Response, err error) {
		save.Enable()
		if err != nil {
			problem.Set(err)
			return
		}
		done()
		u.toast("Saved.")
	})
}

func (u *UI) channelOverview(gid, id string) {
	g, node, ok := u.nodeNow(gid, id)
	if !ok {
		return
	}
	var p *popup
	name := widget.NewEntry()
	name.SetText(node.Name)
	topic := widget.NewEntry()
	topic.SetText(node.Topic)
	topic.SetPlaceHolder("Optional")
	self := ""
	if node.Kind == proto.KindFolder {
		self = node.ID
	}
	place := newPlaceBoxes(g, self)
	place.set(g, node.Parent)
	problem := newErrorText()
	parts := []fyne.CanvasObject{text("Overview", 22, colText, true), field("Name", name, "")}
	if node.Kind != proto.KindCategory {
		parts = append(parts, place.box)
	}
	if node.Kind == proto.KindChannel {
		parts = append(parts, field("Topic", topic, "Shown at the top of the chat."))
	}
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		n := node
		n.Name, n.Topic = name.Text, topic.Text
		if n.Kind != proto.KindCategory {
			n.Parent = place.parent()
		}
		u.saveNode(gid, n, problem, save, func() { p.Close() })
	})
	parts = append(parts, problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	p = u.openPanel("chanoverview", 560, parts...)
}

func (u *UI) channelMessages(gid, id string) {
	_, node, ok := u.nodeNow(gid, id)
	if !ok {
		return
	}
	var p *popup
	history := widget.NewSelect([]string{"Inherit", "Visible", "Hidden"}, nil)
	history.SetSelectedIndex(0)
	if node.History != nil {
		if *node.History {
			history.SetSelectedIndex(1)
		} else {
			history.SetSelectedIndex(2)
		}
	}
	keep := widget.NewSelect(append([]string{}, keepChoices...), nil)
	days := append([]int{}, keepDays...)
	keep.SetSelectedIndex(0)
	if node.KeepDays != nil {
		found := false
		for i, d := range days {
			if d == *node.KeepDays {
				keep.SetSelectedIndex(i)
				found = true
			}
		}
		if !found {
			keep.Options = append(keep.Options, fmt.Sprintf("%d Days", *node.KeepDays))
			days = append(days, *node.KeepDays)
			keep.SetSelectedIndex(len(keep.Options) - 1)
		}
	}
	// Slow Mode is for text channels only.
	slow := widget.NewSelect(append([]string{}, slowChoices...), nil)
	slow.SetSelectedIndex(0)
	for i, s := range slowSeconds {
		if s == node.Slow {
			slow.SetSelectedIndex(i)
		}
	}
	problem := newErrorText()
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		n := node
		n.History, n.KeepDays = nil, nil
		n.Slow = slowSeconds[max(0, slow.SelectedIndex())]
		switch history.SelectedIndex() {
		case 1:
			t := true
			n.History = &t
		case 2:
			f := false
			n.History = &f
		}
		if i := keep.SelectedIndex(); i > 0 && i < len(days) {
			d := days[i]
			n.KeepDays = &d
		}
		u.saveNode(gid, n, problem, save, func() { p.Close() })
	})
	inherit := "Inherit uses the setting of the folder or category it is in."
	if node.Kind == proto.KindCategory {
		inherit = "Inherit uses the guild's normal setting."
	}
	p = u.openPanel("chanmessages", 560, text("Messages", 22, colText, true),
		field("Past Messages", history, "Can people who join later read the messages sent before they joined?"),
		field("Keep Messages", keep, "How long messages are kept. Older ones are deleted by themselves."),
		hintText(inherit),
		slowField(node, slow),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
}

// deleteNode asks, then deletes a channel, folder or category.
func (u *UI) deleteNode(g guildconn.GuildView, n proto.ChannelNode) {
	what := "all its messages"
	if n.Kind != proto.KindChannel {
		what = "everything inside it, with all messages"
	}
	u.confirm("Delete "+kindNames[n.Kind], fmt.Sprintf("Delete \"%s\" and %s? This cannot be undone.", n.Name, what), "Delete", true, func() {
		u.call(g.ID, proto.Request{Type: "channel_delete", ChannelID: n.ID}, func(_ proto.Response, err error) {
			if err != nil {
				u.toast(sentence(err.Error()))
				return
			}
			u.closeSettings()
			u.toast("Deleted.")
		})
	})
}

// These are the Slow Mode choices.
var (
	slowChoices = []string{"Off", "5 Seconds", "10 Seconds", "30 Seconds", "1 Minute", "5 Minutes", "15 Minutes", "1 Hour", "6 Hours"}
	slowSeconds = []int{0, 5, 10, 30, 60, 300, 900, 3600, 21600}
)

// slowField is Slow Mode, for text channels only (hidden otherwise).
func slowField(node proto.ChannelNode, slow *widget.Select) fyne.CanvasObject {
	f := field("Slow Mode", slow, "How often each member may send a message here. Those who manage messages are exempt.")
	if node.Kind != proto.KindChannel {
		f.Hide()
	}
	return f
}

// findTreeRow is the side bar row inside obj (a voice channel's row has its
// members under it).
func findTreeRow(obj fyne.CanvasObject) *treeRow {
	switch o := obj.(type) {
	case *treeRow:
		return o
	case *fyne.Container:
		for _, c := range o.Objects {
			if r := findTreeRow(c); r != nil {
				return r
			}
		}
	}
	return nil
}
