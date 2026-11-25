package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// This file has small building blocks shared by the screens.

// text is a single line of text in a given size and colour.
func text(s string, size float32, c color.Color, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle.Bold = bold
	return t
}

// centred is text centred in its space.
func centred(t *canvas.Text) *canvas.Text {
	t.Alignment = fyne.TextAlignCenter
	return t
}

// gap is empty space of a fixed height.
func gap(h float32) fyne.CanvasObject {
	r := canvas.NewRectangle(color.Transparent)
	r.SetMinSize(fyne.NewSize(1, h))
	return r
}

// icon shows an SVG icon at a fixed size.
func icon(res fyne.Resource, size float32) *canvas.Image {
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(size, size))
	return img
}

// dim shows a widget with dimmed text.
func dim(o fyne.CanvasObject) fyne.CanvasObject {
	return newTint(o, colorTheme{Theme: appTheme, text: colDim})
}

// maxWidth centres its content and keeps it no wider than w.
type maxWidth struct{ w float32 }

func (l maxWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	s := objs[0].MinSize()
	if s.Width > l.w {
		s.Width = l.w
	}
	return s
}

func (l maxWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	o := objs[0]
	w := fyne.Min(l.w, size.Width)
	h := o.MinSize().Height
	o.Resize(fyne.NewSize(w, h))
	o.Move(fyne.NewPos((size.Width-w)/2, fyne.Max(0, (size.Height-h)/2)))
}

func narrow(w float32, o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(maxWidth{w}, o)
}

// ---- choice card: a large button with an icon, a title and two lines ----

type choiceCard struct {
	widget.BaseWidget
	icon  fyne.Resource
	title string
	lines []string
	onTap func()
	off   bool // A greyed-out card is shown, but does nothing.

	bg *canvas.Rectangle
}

func newChoiceCard(ic fyne.Resource, title string, lines []string, tap func()) *choiceCard {
	c := &choiceCard{icon: ic, title: title, lines: lines, onTap: tap}
	c.ExtendBaseWidget(c)
	return c
}

func (c *choiceCard) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(colPanel)
	c.bg.CornerRadius = 20
	c.bg.StrokeColor = colLineStrong
	c.bg.StrokeWidth = 1.5

	iconBG := canvas.NewRectangle(color.NRGBA{179, 13, 30, 26})
	iconBG.CornerRadius = 14
	iconBG.StrokeColor = color.NRGBA{179, 13, 30, 46}
	iconBG.StrokeWidth = 1
	iconBox := container.NewGridWrap(fyne.NewSize(46, 46), container.NewStack(iconBG, container.NewCenter(icon(c.icon, 24))))

	// Everything centred, so the card reads as one button.
	body := container.NewVBox(container.NewCenter(iconBox), gap(8), centred(text(c.title, 19, colText, true)), gap(2))
	for _, l := range c.lines {
		body.Add(centred(text(l, 14, colDim, false)))
	}
	var card fyne.CanvasObject = container.NewStack(c.bg, container.New(layout.NewCustomPaddedLayout(26, 28, 26, 26), body))
	if c.off {
		veil := canvas.NewRectangle(color.NRGBA{0, 0, 0, 150})
		veil.CornerRadius = 20
		card = container.NewStack(card, veil)
	}
	return widget.NewSimpleRenderer(card)
}

func (c *choiceCard) Tapped(*fyne.PointEvent) {
	if c.onTap != nil && !c.off {
		c.onTap()
	}
}

func (c *choiceCard) Cursor() desktop.Cursor {
	if c.off {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (c *choiceCard) MouseIn(*desktop.MouseEvent) {
	if c.off {
		return
	}
	c.bg.FillColor = colRaised
	c.bg.StrokeColor = color.NRGBA{179, 13, 30, 180}
	c.bg.Refresh()
}

func (c *choiceCard) MouseMoved(*desktop.MouseEvent) {}

func (c *choiceCard) MouseOut() {
	c.bg.FillColor = colPanel
	c.bg.StrokeColor = colLineStrong
	c.bg.Refresh()
}

// ---- tab bar: tabs that clearly look like separate tabs ----------
//
// The tabs sit in one outlined strip with a line between each, and the
// chosen tab is filled, so they never read as plain buttons.

type tabButton struct {
	widget.BaseWidget
	label    string
	ic       fyne.Resource
	selected bool
	onTap    func()
	bg       *canvas.Rectangle
	words    *canvas.Text
}

func newTabButton(label string, ic fyne.Resource, tap func()) *tabButton {
	t := &tabButton{label: label, ic: ic, onTap: tap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tabButton) CreateRenderer() fyne.WidgetRenderer {
	t.bg = canvas.NewRectangle(color.Transparent)
	t.bg.CornerRadius = 8
	row := container.NewHBox()
	if t.ic != nil {
		row.Add(container.NewCenter(icon(t.ic, 16)))
	}
	if t.label != "" {
		t.words = text(t.label, 14, colDim, true)
		row.Add(container.NewCenter(t.words))
	}
	t.paint(false)
	return widget.NewSimpleRenderer(container.NewStack(t.bg, container.New(layout.NewCustomPaddedLayout(6, 6, 14, 14), row)))
}

func (t *tabButton) paint(hover bool) {
	if t.bg == nil {
		return
	}
	t.bg.FillColor = color.Transparent
	c := colDim
	switch {
	case t.selected:
		t.bg.FillColor, c = colAccent, rgb(0x07, 0x07, 0x0a)
	case hover:
		t.bg.FillColor, c = colHover, colText
	}
	if t.words != nil {
		t.words.Color = c
		t.words.Refresh()
	}
	t.bg.Refresh()
}

func (t *tabButton) Tapped(*fyne.PointEvent) {
	if t.onTap != nil {
		t.onTap()
	}
}
func (t *tabButton) Cursor() desktop.Cursor         { return desktop.PointerCursor }
func (t *tabButton) MouseIn(*desktop.MouseEvent)    { t.paint(true) }
func (t *tabButton) MouseMoved(*desktop.MouseEvent) {}
func (t *tabButton) MouseOut()                      { t.paint(false) }

// tabBar draws the strip. lead, when not nil, is an extra button at the very
// left (like Add Friend). pick is called with the chosen tab's number.
func tabBar(lead *tabButton, names []string, selected int, pick func(int)) fyne.CanvasObject {
	row := container.NewHBox()
	divider := func() fyne.CanvasObject {
		r := canvas.NewRectangle(colLineStrong)
		r.SetMinSize(fyne.NewSize(1, 18))
		return container.NewCenter(r)
	}
	if lead != nil {
		row.Add(lead)
		row.Add(divider())
	}
	var all []*tabButton
	for i, n := range names {
		i := i
		b := newTabButton(n, nil, func() {
			for k, o := range all {
				o.selected = k == i
				o.paint(false)
			}
			pick(i)
		})
		b.selected = i == selected
		all = append(all, b)
		if i > 0 {
			row.Add(divider())
		}
		row.Add(b)
	}
	bg := canvas.NewRectangle(colPanel)
	bg.CornerRadius = 11
	bg.StrokeColor = colLineStrong
	bg.StrokeWidth = 1
	strip := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(3, 3, 3, 3), row))
	return container.NewHBox(strip)
}

// fitText is one line of text that ends in "…" when there isn't room, so
// it never runs under what is beside it. It is cut again each time
// it is laid out, so it is right at any width.
func fitText(s string, bold bool, c color.Color) fyne.CanvasObject {
	t := text(s, 14, c, bold)
	return container.New(&fitLayout{full: s, t: t}, t)
}

type fitLayout struct {
	full string
	t    *canvas.Text
}

func (f *fitLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(20, fyne.MeasureText("Ag", f.t.TextSize, f.t.TextStyle).Height)
}

func (f *fitLayout) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	shown := f.full
	fits := func(x string) bool { return fyne.MeasureText(x, f.t.TextSize, f.t.TextStyle).Width <= s.Width }
	if !fits(shown) {
		r := []rune(f.full)
		for len(r) > 0 && !fits(string(r)+"…") {
			r = r[:len(r)-1]
		}
		shown = strings.TrimSpace(string(r)) + "…"
	}
	if f.t.Text != shown {
		f.t.Text = shown
		f.t.Refresh()
	}
	f.t.Resize(fyne.NewSize(s.Width, s.Height))
	f.t.Move(fyne.NewPos(0, 0))
}
