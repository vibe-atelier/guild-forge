package ui

import (
	"image/color"
	"nexus/internal/proto"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// treeRow is one row of the channel list: a channel, folder or category.
// Its gear (for people who may change it) shows only while the mouse is
// over the row, like Discord.
type treeRow struct {
	widget.BaseWidget
	ic       fyne.Resource
	label    string
	bold     bool
	colour   color.Color
	active   bool
	extra    []fyne.CanvasObject // These are on the right side and always shown (the mention count).
	left     fyne.CanvasObject   // It is the left marker (unread).
	onTap    func()
	onGear   func()
	bg       *canvas.Rectangle
	gear     *hoverIcon
	over     bool
	overGear bool

	// These are for dragging to a new place, and are set by the channel tree for staff.
	drag     *chanDrag
	nodeID   string
	dropMark string // It is "before", "after" or "inside" while something is dragged over it.
	line     *canvas.Rectangle
}

func newTreeRow(ic fyne.Resource, label string, tap, gear func()) *treeRow {
	r := &treeRow{ic: ic, label: label, onTap: tap, onGear: gear, colour: colDim}
	r.ExtendBaseWidget(r)
	return r
}

func (r *treeRow) CreateRenderer() fyne.WidgetRenderer {
	r.bg = canvas.NewRectangle(color.Transparent)
	r.bg.CornerRadius = 8
	words := text(r.label, 14.5, r.colour, r.bold)
	left := container.NewHBox()
	if r.left != nil {
		left.Add(container.NewCenter(r.left))
	}
	if r.ic != nil {
		left.Add(container.NewCenter(icon(r.ic, 16)))
	}
	right := container.NewHBox(r.extra...)
	if r.onGear != nil {
		r.gear = newHoverIcon(gearIcon, r)
		r.gear.Hide()
		right.Add(container.NewCenter(r.gear))
	}
	r.paint()
	row := container.NewBorder(nil, nil, left, right, container.NewVBox(layout.NewSpacer(), words, layout.NewSpacer()))
	r.line = canvas.NewRectangle(colAccentLink)
	r.line.Hide()
	return widget.NewSimpleRenderer(container.NewStack(r.bg, container.New(layout.NewCustomPaddedLayout(7, 7, 8, 6), row), container.NewWithoutLayout(r.line)))
}

func (r *treeRow) paint() {
	switch {
	case r.dropMark == "inside":
		r.bg.FillColor = colHover
	case r.active:
		r.bg.FillColor = colRaised
	case r.over || r.overGear:
		r.bg.FillColor = colHover
	default:
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	if r.gear != nil {
		if r.over || r.overGear {
			r.gear.Show()
		} else {
			r.gear.Hide()
		}
	}
}

func (r *treeRow) Tapped(*fyne.PointEvent)        { r.onTap() }
func (r *treeRow) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (r *treeRow) MouseMoved(*desktop.MouseEvent) {}
func (r *treeRow) MouseIn(*desktop.MouseEvent)    { r.over = true; r.paint() }

// MouseOut waits a moment: moving onto the gear also leaves the row.
func (r *treeRow) MouseOut() {
	r.over = false
	time.AfterFunc(60*time.Millisecond, func() { fyne.Do(r.paint) })
}

// hoverIcon is a small clickable icon that keeps its row lit while the
// mouse is on it.
type hoverIcon struct {
	widget.BaseWidget
	res fyne.Resource
	row *treeRow
}

func newHoverIcon(res fyne.Resource, row *treeRow) *hoverIcon {
	h := &hoverIcon{res: res, row: row}
	h.ExtendBaseWidget(h)
	return h
}

func (h *hoverIcon) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.New(layout.NewCustomPaddedLayout(2, 2, 4, 4), icon(h.res, 16)))
}
func (h *hoverIcon) Tapped(*fyne.PointEvent)        { h.row.onGear() }
func (h *hoverIcon) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (h *hoverIcon) MouseMoved(*desktop.MouseEvent) {}
func (h *hoverIcon) MouseIn(*desktop.MouseEvent)    { h.row.overGear = true; h.row.paint() }
func (h *hoverIcon) MouseOut() {
	h.row.overGear = false
	time.AfterFunc(60*time.Millisecond, func() { fyne.Do(h.row.paint) })
}

// ---- dragging channels into a new order ---------------------------

// chanDrag is one side bar's draggable rows, top to bottom, and what is
// being dragged.
type chanDrag struct {
	rows   []*treeRow
	nodes  map[string]proto.ChannelNode
	from   *treeRow
	target *treeRow
	zone   string
	drop   func(id, under string, place int)
}

// showMark draws the drop line above or below the row, or lights it up for
// "inside".
func (r *treeRow) showMark(zone string) {
	if r.dropMark == zone {
		return
	}
	r.dropMark = zone
	if r.line != nil {
		w := r.Size().Width
		switch zone {
		case "before":
			r.line.Move(fyne.NewPos(4, 0))
			r.line.Resize(fyne.NewSize(w-8, 2))
			r.line.Show()
		case "after":
			r.line.Move(fyne.NewPos(4, r.Size().Height-2))
			r.line.Resize(fyne.NewSize(w-8, 2))
			r.line.Show()
		default:
			r.line.Hide()
		}
	}
	r.paint()
}

func (r *treeRow) Dragged(ev *fyne.DragEvent) {
	d := r.drag
	if d == nil {
		return
	}
	d.from = r
	var hit *treeRow
	zone := ""
	for _, row := range d.rows {
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(row)
		h := row.Size().Height
		if ev.AbsolutePosition.Y < pos.Y || ev.AbsolutePosition.Y >= pos.Y+h {
			continue
		}
		hit = row
		zone = "before"
		if ev.AbsolutePosition.Y >= pos.Y+h/2 {
			zone = "after"
			if n := d.nodes[row.nodeID]; !proto.IsRoom(n.Kind) {
				zone = "inside" // It is the lower half of a category or folder.
			}
		}
		break
	}
	if hit == r {
		hit, zone = nil, ""
	}
	if d.target != nil && d.target != hit {
		d.target.showMark("")
	}
	d.target, d.zone = hit, zone
	if hit != nil {
		hit.showMark(zone)
	}
}

func (r *treeRow) DragEnd() {
	d := r.drag
	if d == nil || d.from == nil {
		return
	}
	target, zone := d.target, d.zone
	d.from, d.target, d.zone = nil, nil, ""
	if target == nil {
		return
	}
	target.showMark("")
	moved, ok := d.nodes[r.nodeID]
	if !ok {
		return
	}
	t := d.nodes[target.nodeID]
	if moved.Kind == proto.KindCategory {
		// Categories stay at the top: drop beside the category the target is in.
		for t.Parent != "" {
			t = d.nodes[t.Parent]
		}
		if zone == "inside" {
			zone = "after"
		}
	}
	if zone == "inside" {
		d.drop(moved.ID, t.ID, 0)
		return
	}
	// Its place among the target's brothers and sisters, without itself.
	place := 0
	for _, row := range d.rows {
		n := d.nodes[row.nodeID]
		if n.ID == moved.ID || n.Parent != t.Parent {
			continue
		}
		if n.ID == t.ID {
			break
		}
		place++
	}
	if zone == "after" {
		place++
	}
	d.drop(moved.ID, t.Parent, place)
}
