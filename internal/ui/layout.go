package ui

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// One layout rule for every form, settings page and window:
//
//   - Things are stacked top to bottom with the same gap between each pair:
//     formGap between fields, options and paragraphs; fieldGap inside a
//     field (label, box, hint). Hidden things take no room.
//   - All text (titles, paragraphs, labels, hints) starts where the text
//     inside an input box starts, boxInset from the edge. Only the edges of
//     boxes and buttons sit at the edge itself.
//   - Wrapped text is measured at the width it really gets, so a paragraph
//     on two lines never runs into what is under it.

const (
	formGap  = 16 // It is the gap between fields, options and paragraphs.
	fieldGap = 6  // It is the gap between a label, its box and its hint.
	titleGap = 6  // It is the gap between a title and the line under it.
)

// column is the layout: a gap between things, and text moved in to the
// text column when inset is on.
type column struct {
	gap   float32
	inset bool
	w     float32 // It is the width it was last laid out at.
	fixed float32 // It is the width a relayout was last asked for.
}

// form stacks a form's parts with the standard gap, text in the text column.
func form(objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(&column{gap: formGap, inset: true}, objs...)
}

// stack is form with a gap of your choice.
func stack(gap float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(&column{gap: gap, inset: true}, objs...)
}

// titled is a title with a line or two under it.
func titled(title fyne.CanvasObject, lines ...fyne.CanvasObject) *fyne.Container {
	return stack(titleGap, append([]fyne.CanvasObject{title}, lines...)...)
}

// isText says whether something is text that belongs in the text column.
func isText(o fyne.CanvasObject) bool {
	switch v := o.(type) {
	case *canvas.Text:
		return v.Alignment == fyne.TextAlignLeading // Centred text stays centred.
	case *fyne.Container:
		_, ok := v.Layout.(textPad)
		return ok
	}
	return false
}

func (c *column) place(o fyne.CanvasObject, w float32) (x, width float32) {
	if c.inset && isText(o) {
		return boxInset, w - boxInset
	}
	return 0, w
}

func (c *column) MinSize(objs []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		x, ow := c.place(o, c.w)
		if c.w > 0 && o.Size().Width != ow {
			o.Resize(fyne.NewSize(ow, o.Size().Height)) // This lets wrapped text measure its real height.
		}
		m := o.MinSize()
		w = fyne.Max(w, m.Width+x)
		h += m.Height
		n++
	}
	if n > 1 {
		h += c.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

func (c *column) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	c.w = s.Width
	var y float32
	for _, o := range objs {
		if !o.Visible() {
			continue
		}
		x, ow := c.place(o, s.Width)
		o.Resize(fyne.NewSize(ow, o.MinSize().Height))
		h := o.MinSize().Height // Measure again, now that wrapped text knows its width.
		o.Resize(fyne.NewSize(ow, h))
		o.Move(fyne.NewPos(x, y))
		y += h + c.gap
	}
	// Wrapped text came out taller (or shorter) than was planned for: have
	// the window lay things out again, once per width.
	if y > 0 && y-c.gap-s.Height > 1 && c.fixed != s.Width {
		c.fixed = s.Width
		askRelayout()
	}
}

// textPad holds wrapped text: Fyne measures unpadded wrapped text a little
// short, so it gets a little room under it. The column knows it as text.
type textPad struct{}

func (textPad) MinSize(objs []fyne.CanvasObject) fyne.Size {
	m := objs[0].MinSize()
	return fyne.NewSize(m.Width, m.Height+6)
}

func (textPad) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	objs[0].Resize(fyne.NewSize(s.Width, s.Height-6))
	objs[0].Move(fyne.NewPos(0, 1))
}

// ---- laying the window out again ---------------------------------------------

var (
	relayoutMu   sync.Mutex
	relayoutFunc func()
	relayoutDue  bool
)

// askRelayout lays the whole window out again, soon (at most once a frame).
func askRelayout() {
	relayoutMu.Lock()
	defer relayoutMu.Unlock()
	if relayoutFunc == nil || relayoutDue {
		return
	}
	relayoutDue = true
	f := relayoutFunc
	go fyne.Do(func() {
		relayoutMu.Lock()
		relayoutDue = false
		relayoutMu.Unlock()
		f()
	})
}

// watchLayout sets what askRelayout does for this window.
func (u *UI) watchLayout() {
	relayoutMu.Lock()
	defer relayoutMu.Unlock()
	relayoutFunc = func() {
		if c := u.win.Content(); c != nil {
			c.Refresh()
		}
		if top := u.win.Canvas().Overlays().Top(); top != nil {
			top.Refresh()
		}
	}
}

// plain stacks with a gap but leaves text where it is: for cards whose
// Markdown starts at the card's own edge, so notes line up with it.
func plain(gap float32, objs ...fyne.CanvasObject) *fyne.Container {
	return container.New(&column{gap: gap}, objs...)
}
