package ui

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// This file is the Roles window: the guild's roles, highest rank first, and an
// editor for each. Roles ranked at or above your own can't be changed.

// roleColours are the suggested colours: red, green, blue, then cyan,
// magenta and yellow.
var roleColours = []string{"", "#ef4444", "#22c55e", "#3b82f6", "#22d3ee", "#d946ef", "#facc15"}

func colourDot(hex string, size float32) fyne.CanvasObject {
	c, ok := parseHex(hex)
	if !ok {
		c = rgb(0x5a, 0x5a, 0x64)
	}
	d := canvas.NewCircle(c)
	return container.NewGridWrap(fyne.NewSize(size, size), d)
}

func (u *UI) openRoles(g guildconn.GuildView) {
	var p *popup
	top := myRank(g)
	count := map[string]int{}
	for _, m := range g.Members {
		for _, r := range m.Roles {
			count[r]++
		}
	}
	list := stack(4) // This is a column, so text lines up.
	for _, r := range g.Roles {
		r := r
		sub := fmt.Sprintf("Rank %d · %d %s", r.Rank, count[r.ID], map[bool]string{true: "member", false: "members"}[count[r.ID] == 1])
		if r.ID == proto.EveryoneRole {
			sub = "Rank 0 · everyone in the guild"
		}
		editable := r.ID == proto.EveryoneRole || r.Rank < top
		var right fyne.CanvasObject = text("Above you", 12.5, colFaint, false)
		if editable {
			right = widget.NewButton("Edit", func() { p.Close(); u.roleWindow(g, &r) })
		}
		list.Add(container.NewBorder(nil, nil, container.NewHBox(container.NewCenter(colourDot(r.Color, 14)), gap(1)), right,
			container.NewVBox(text(r.Name, 15, colText, true), text(sub, 12.5, colFaint, false))))
	}
	add := primary("New Role", func() { p.Close(); u.roleWindow(g, nil) })
	if len(g.Roles) >= 100 {
		add.Disable()
	}
	p = u.openPanel("roles", 560,
		container.NewBorder(nil, nil, nil, add, text("Roles", 22, colText, true)),
		para("Higher ranks can manage lower ones. Roles on the same rank can't touch each other."),
		list,
		buttonsRow(widget.NewButton("Close", func() { p.Close() })))
}

// roleWindow makes (r == nil) or changes a role.
func (u *UI) roleWindow(g guildconn.GuildView, r *proto.Role) {
	var p *popup
	back := func() {
		p.Close()
		if ng := u.findGuild(g.ID); ng != nil {
			u.openRoles(*ng)
		}
	}
	everyone := r != nil && r.ID == proto.EveryoneRole
	top := myRank(g)

	name := widget.NewEntry()
	rank := widget.NewEntry()
	colourEntry := widget.NewEntry()
	colourEntry.SetPlaceHolder("#8b7bff")
	hoist, hoistBox := option("Separate Group", "Show its members as their own group in the member list.", false)
	mention, mentionBox := option("Mentionable", "Anyone can @mention this role.", false)
	buyable, buyableBox := option("Purchasable", "Members can buy it in the Shop, once it is added there as a Role item.", r != nil && r.Purchasable)
	var own, buyersOld, clashOld []string
	if r != nil {
		own, buyersOld, clashOld = []string{r.ID}, r.BuyableBy, r.Exclusive
	}
	buyersList, buyers := roleTicks(g, buyersOld, own, true)
	buyersBox := field("Accessible To", buyersList, "Only these roles can buy it. Tick none to let every member buy it.")
	clashBox, clashes := roleTicks(g, clashOld, append(own, proto.EveryoneRole), false)
	buyable.OnChanged = func(on bool) {
		if on {
			buyersBox.Show()
		} else {
			buyersBox.Hide()
		}
	}
	buyable.OnChanged(buyable.Checked)
	checks := map[string]*widget.Check{}

	if r != nil {
		name.SetText(r.Name)
		rank.SetText(strconv.Itoa(r.Rank))
		colourEntry.SetText(r.Color)
		hoist.SetChecked(r.Hoist)
		mention.SetChecked(r.Mentionable)
	} else {
		rank.SetText(strconv.Itoa(min(1, top-1)))
	}
	swatches := container.NewHBox()
	for _, c := range roleColours {
		c := c
		label := "None"
		if c != "" {
			label = "●"
		}
		b := widget.NewButton(label, func() { colourEntry.SetText(c) })
		b.Importance = widget.LowImportance
		if c != "" {
			swatches.Add(container.NewStack(b, container.NewCenter(colourDot(c, 16))))
			b.SetText("   ")
		} else {
			swatches.Add(b)
		}
	}

	perms := form()
	mine := g.MyPerms[""]
	admin := g.IsOwner || has(mine, proto.PermAdministrator)
	for _, grp := range permGroups {
		perms.Add(text(grp.title, 11.5, colFaint, true))
		for _, pm := range grp.perms {
			c, box := option(permNames[pm], permHints[pm], r != nil && has(r.Perms, pm))
			// You can only give what you have; only an Administrator gives Administrator.
			if !admin && (!has(mine, pm) || pm == proto.PermAdministrator) {
				c.Disable()
			}
			checks[pm] = c
			perms.Add(box)
		}
	}

	problem := newErrorText()
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		role := proto.Role{Name: strings.TrimSpace(name.Text), Color: strings.TrimSpace(colourEntry.Text),
			Hoist: hoist.Checked, Mentionable: mention.Checked,
			Purchasable: buyable.Checked, BuyableBy: buyers(), Exclusive: clashes()}
		if r != nil {
			role.ID = r.ID
		}
		n, err := strconv.Atoi(strings.TrimSpace(rank.Text))
		if err != nil && !everyone {
			problem.Set(fmt.Errorf("The rank must be a number."))
			return
		}
		role.Rank = n
		for _, grp := range permGroups {
			for _, pm := range grp.perms {
				if checks[pm].Checked {
					role.Perms = append(role.Perms, pm)
				}
			}
		}
		if everyone {
			role.Name, role.Rank = "Everyone", 0
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "role_put", Role: &role}, func(_ proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			u.toast("Role saved")
			back()
		})
	})
	buttons := []fyne.CanvasObject{widget.NewButton("Back", back), save}
	if r != nil && !everyone {
		del := widget.NewButton("Delete", func() {
			u.confirm("Delete Role", "Delete \""+r.Name+"\"? Everyone who has it loses it.", "Delete", true, func() {
				u.call(g.ID, proto.Request{Type: "role_delete", RoleID: r.ID}, func(_ proto.Response, err error) {
					if err != nil {
						problem.Set(err)
						return
					}
					u.toast("Role deleted")
					back()
				})
			})
		})
		del.Importance = widget.DangerImportance
		buttons = append([]fyne.CanvasObject{del, layout.NewSpacer()}, buttons...)
	}

	title := "New Role"
	if r != nil {
		title = "Edit Role"
	}
	parts := []fyne.CanvasObject{text(title, 22, colText, true)}
	if everyone {
		parts = append(parts, para("Everyone in the guild has this role. Its permissions are the starting point for all members."))
	} else {
		rankHint := fmt.Sprintf("1 or more, below your own rank (%d). Equal ranks can't manage each other.", top)
		if g.IsOwner {
			rankHint = "1 or more. Higher ranks manage lower ones; equal ranks can't touch each other."
		}
		parts = append(parts,
			container.NewGridWithColumns(2, field("Name", name, ""), field("Rank", rank, rankHint)),
			field("Colour", container.NewHBox(container.New(fixedWidth{130}, colourEntry), swatches), "Names show in the colour of their highest coloured role."),
			hoistBox, mentionBox, buyableBox, buyersBox,
			field("Mutually Exclusive", clashBox, "Equipping this role takes these off, and the other way round."))
	}
	parts = append(parts, perms, problem.box, container.NewHBox(buttons...))
	p = u.openPanel("roles", 600, parts...)
}

// memberRolesBox lets you tick a member's roles (only roles below your own
// rank can be changed). It returns the box and a function that reads it.
func memberRolesBox(g guildconn.GuildView, m guildconn.MemberView) (fyne.CanvasObject, func() []string) {
	top := myRank(g)
	box := stack(0) // The window gives it its heading.
	var checks []*widget.Check
	var ids []string
	for _, r := range g.Roles {
		if r.ID == proto.EveryoneRole {
			continue
		}
		c := widget.NewCheck(r.Name, nil)
		c.SetChecked(has(m.Roles, r.ID))
		if r.Rank >= top {
			c.Disable() // The role is at or above your rank.
		}
		checks, ids = append(checks, c), append(ids, r.ID)
		box.Add(container.NewHBox(container.NewCenter(colourDot(r.Color, 12)), c))
	}
	if len(ids) == 0 {
		box.Add(text("No roles yet. Make some in Roles.", 12.5, colFaint, false))
	}
	return box, func() []string {
		var out []string
		for i, c := range checks {
			if c.Checked {
				out = append(out, ids[i])
			}
		}
		return out
	}
}

// ---- channel rules --------------------------------------

var ruleChoices = []string{"Inherit", "Allow", "Deny"}

// openChannelRules edits the permission rules of a category, folder or channel.
func (u *UI) openChannelRules(g guildconn.GuildView, node proto.ChannelNode) {
	var p *popup
	type rule struct {
		role    string
		selects map[string]*widget.Select
	}
	var rules []*rule
	roleName := map[string]string{}
	for _, r := range g.Roles {
		roleName[r.ID] = r.Name
	}
	top := myRank(g)
	rankOfRole := map[string]int{}
	for _, r := range g.Roles {
		rankOfRole[r.ID] = r.Rank
	}

	box := container.NewVBox()
	var draw func()
	addRule := func(o proto.Override) {
		rl := &rule{role: o.Role, selects: map[string]*widget.Select{}}
		for _, pm := range proto.ChannelPerms {
			s := widget.NewSelect(ruleChoices, nil)
			s.SetSelectedIndex(0)
			if has(o.Allow, pm) {
				s.SetSelectedIndex(1)
			} else if has(o.Deny, pm) {
				s.SetSelectedIndex(2)
			}
			if o.Role != proto.EveryoneRole && rankOfRole[o.Role] >= top {
				s.Disable() // The role is at or above your rank.
			}
			rl.selects[pm] = s
		}
		rules = append(rules, rl)
	}
	for _, o := range node.Overrides {
		addRule(o)
	}
	inherit, inheritBox := option("Inherit Rules", "Use the rules of the folder or category above, and ignore the ones below.", node.InheritPerms)

	picker := widget.NewSelect(nil, nil)
	picker.PlaceHolder = "Add Rule For…"
	var pickIDs []string
	fillPicker := func() {
		var labels []string
		pickIDs = nil
		for _, r := range g.Roles {
			taken := false
			for _, rl := range rules {
				taken = taken || rl.role == r.ID
			}
			if !taken && (r.ID == proto.EveryoneRole || r.Rank < top) {
				labels, pickIDs = append(labels, r.Name), append(pickIDs, r.ID)
			}
		}
		picker.Options = labels
		picker.ClearSelected()
	}
	picker.OnChanged = func(string) {
		i := picker.SelectedIndex()
		if i < 0 || i >= len(pickIDs) {
			return
		}
		addRule(proto.Override{Role: pickIDs[i]})
		draw()
	}
	draw = func() {
		box.RemoveAll()
		if len(rules) == 0 {
			box.Add(para("No rules here: everyone has their roles' permissions."))
		}
		for i, rl := range rules {
			i, rl := i, rl
			remove := widget.NewButtonWithIcon("", xIcon, func() {
				rules = append(rules[:i:i], rules[i+1:]...)
				draw()
			})
			remove.Importance = widget.LowImportance
			grid := container.NewGridWithColumns(2)
			for _, pm := range proto.ChannelPerms {
				grid.Add(container.NewBorder(nil, nil, container.New(fixedWidth{140}, text(permNames[pm], 13.5, colDim, false)), nil, rl.selects[pm]))
			}
			box.Add(card(14, container.NewVBox(container.NewBorder(nil, nil, nil, remove, text(roleName[rl.role], 15, colText, true)), grid)))
		}
		fillPicker()
		box.Refresh()
	}
	draw()

	problem := newErrorText()
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		n := node
		n.InheritPerms = inherit.Checked
		n.Overrides = nil
		for _, rl := range rules {
			o := proto.Override{Role: rl.role}
			for _, pm := range proto.ChannelPerms {
				switch rl.selects[pm].SelectedIndex() {
				case 1:
					o.Allow = append(o.Allow, pm)
				case 2:
					o.Deny = append(o.Deny, pm)
				}
			}
			n.Overrides = append(n.Overrides, o)
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "channel_put", Node: &n}, func(_ proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Permissions saved")
		})
	})
	parts := []fyne.CanvasObject{text("Permissions: "+node.Name, 22, colText, true),
		para("Allow or deny permissions for a role here. The closest rule wins: a channel's rule beats its folder's, which beats its category's.")}
	if node.Parent != "" {
		parts = append(parts, inheritBox)
	}
	parts = append(parts, box, picker, problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	p = u.openPanel("chanperms", 640, parts...)
}

// roleTicks is a box of the guild's roles to tick, for Accessible To and
// Mutually Exclusive. It returns the box and what is ticked.
func roleTicks(g guildconn.GuildView, ticked, leaveOut []string, everyone bool) (fyne.CanvasObject, func() []string) {
	var checks []*widget.Check
	var ids []string
	grid := container.NewGridWithColumns(3)
	for _, r := range g.Roles {
		if has(leaveOut, r.ID) || (r.ID == proto.EveryoneRole && !everyone) {
			continue
		}
		c := widget.NewCheck(r.Name, nil)
		c.SetChecked(has(ticked, r.ID))
		checks = append(checks, c)
		ids = append(ids, r.ID)
		grid.Add(flushCheck(c))
	}
	var body fyne.CanvasObject = grid
	if len(checks) == 0 {
		body = hintText("There are no other roles yet.")
	}
	read := func() []string {
		var out []string
		for i, c := range checks {
			if c.Checked {
				out = append(out, ids[i])
			}
		}
		return out
	}
	return outlined(stack(0, body)), read
}

// outlined draws a thin rounded line around a box of choices.
func outlined(o fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colPanel)
	bg.CornerRadius = 12
	bg.StrokeColor = colLine
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(6, 6, 0, 8), o))
}
