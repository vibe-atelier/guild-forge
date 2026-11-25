package ui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// These are building blocks for the form screens (Create User, Load User, unlock).

// panel is a rounded dark box holding a form, centred on the page.
func panel(width float32, content ...fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(colPanel)
	bg.CornerRadius = 22
	bg.StrokeColor = colLine
	bg.StrokeWidth = 1
	box := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(30, 30, 34, 34), form(content...)))
	return narrow(width, box)
}

// heading is a screen's title.
func heading(s string) fyne.CanvasObject { return text(s, 26, colText, true) }

// para is wrapped, dimmed text.
func para(s string) fyne.CanvasObject { return wrapText(s, colDim) }

// hintText is a small faint note under an input.
func hintText(s string) fyne.CanvasObject { return wrapText(s, colFaint) }

// wrapText is wrapped text in a colour with no inner padding, so its left
// edge lines up with field labels, inputs and headings.
func wrapText(s string, c color.Color) fyne.CanvasObject {
	_, o := wrapLabel(s, c)
	return o
}

// wrapLabel is wrapText, also giving the label so its words can change.
func wrapLabel(s string, c color.Color) (*widget.Label, fyne.CanvasObject) {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	return l, container.New(textPad{}, newTint(l, tightTheme{Theme: appTheme, text: c}))
}

// field is a labelled input: a small label, the input, and an optional
// hint under it. Hints are sentences, so they end with a full stop.
func field(label string, input fyne.CanvasObject, hint string) fyne.CanvasObject {
	// The label and hint start where the text inside the box starts.
	box := stack(fieldGap, text(label, 13, colDim, true), input)
	if hint != "" {
		box.Add(hintText(sentence(hint)))
	}
	return box
}

// boxInset is how far the text inside an input box sits from its edge.
const boxInset = 10

// errorText shows a problem in red (empty when there is none).
type errorText struct {
	label *widget.Label
	box   fyne.CanvasObject
}

func newErrorText() *errorText {
	l := widget.NewLabel("")
	l.Wrapping = fyne.TextWrapWord
	box := container.New(textPad{}, newTint(l, tightTheme{Theme: appTheme, text: colDanger}))
	box.Hide()
	return &errorText{label: l, box: box}
}

// Set shows err as a sentence, or hides the line when err is nil.
func (e *errorText) Set(err error) {
	if err == nil {
		e.label.SetText("")
		e.box.Hide()
		return
	}
	e.label.SetText(sentence(err.Error()))
	e.box.Show()
}

// Run does step away from the window, so the window keeps responding. Back
// on the window's own thread, it shows step's error, if any, or else calls
// then.
func (e *errorText) Run(step func() error, then func()) {
	go func() {
		err := step()
		fyne.Do(func() {
			if err != nil {
				e.Set(err)
				return
			}
			then()
		})
	}()
}

// sentence starts a message with a capital letter and ends it with a full stop.
func sentence(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	s = string(r)
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "?") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "…") {
		s += "."
	}
	return s
}

// primary is the main button of a form.
func primary(label string, tap func()) *widget.Button {
	b := widget.NewButton(label, tap)
	b.Importance = widget.HighImportance
	return b
}

// backButton goes back to the previous screen.
func backButton(tap func()) fyne.CanvasObject {
	b := widget.NewButtonWithIcon("Back", backIcon, tap)
	b.Importance = widget.LowImportance
	return container.NewHBox(b)
}

// ---- password strength meter ----

type meter struct {
	bg, fill *canvas.Rectangle
	frac     float32
	box      *fyne.Container
}

func newMeter() *meter {
	m := &meter{bg: canvas.NewRectangle(colRaised), fill: canvas.NewRectangle(colDanger)}
	m.bg.CornerRadius, m.fill.CornerRadius = 2, 2
	m.box = container.New(m, m.bg, m.fill)
	return m
}

func (m *meter) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(10, 4) }

func (m *meter) Layout(_ []fyne.CanvasObject, s fyne.Size) {
	m.bg.Resize(s)
	m.fill.Resize(fyne.NewSize(s.Width*m.frac, s.Height))
}

// Rate scores a password the same way as the HTML app.
func (m *meter) Rate(p string) {
	score := 0
	hasLower, hasUpper, hasDigit, hasOther := false, false, false, false
	for _, c := range p {
		switch {
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= '0' && c <= '9':
			hasDigit = true
		default:
			hasOther = true
		}
	}
	n := len([]rune(p))
	for _, ok := range []bool{n >= 8, n >= 12, hasLower && hasUpper, hasDigit, hasOther} {
		if ok {
			score++
		}
	}
	colours := []color.Color{colDanger, colDanger, colIdle, colIdle, colOnline, colOnline}
	m.fill.FillColor = colours[score]
	m.frac = 0
	if p != "" {
		m.frac = float32(max(12, score*20)) / 100
	}
	m.box.Refresh()
}

// ---- avatar: a node of a network, in the person's own colours ----------
//
// Without a picture, people and guilds get a small network drawing like the
// logo, with no letters. Each ID gets its own colours and its own
// turn of the drawing.

func avatar(name, id string, size float32) fyne.CanvasObject {
	if f := faceImage(id, size); f != nil {
		return f
	}
	return avatarShape(id, size, `<circle cx="50" cy="50" r="50" fill="url(#a)"/>`, personNode(id), "c")
}

// avatarSquare is the same with rounded corners (guilds without a picture).
func avatarSquare(name, id string, size float32) fyne.CanvasObject {
	return avatarShape(id, size, `<path d="M31 0H69A31 31 0 0 1 100 31V69A31 31 0 0 1 69 100H31A31 31 0 0 1 0 69V31A31 31 0 0 1 31 0Z" fill="url(#a)"/>`, guildNodes(id), "s")
}

// Fyne's SVG drawer handles plain shapes best, so the drawings use only
// lines and circles at worked-out places (no transforms, no opacity).

// at places a point of the drawing, shrunk a little towards the middle.
func at(x, y float64) (float64, float64) { return 50 + (x-50)*.8, 50 + (y-50)*.8 }

// personNode is one node with three links going out to smaller nodes.
func personNode(id string) string {
	turn := float64(hue(id) % 120)
	var links, dots strings.Builder
	cx, cy := at(50, 50)
	for i := 0; i < 3; i++ {
		a := (turn + float64(i)*120 - 90) * math.Pi / 180
		x, y := at(50+30*math.Cos(a), 50+30*math.Sin(a))
		fmt.Fprintf(&links, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ffffff" stroke-width="4.5" stroke-linecap="round"/>`, cx, cy, x, y)
		fmt.Fprintf(&dots, `<circle cx="%.1f" cy="%.1f" r="7.5" fill="#ffffff"/>`, x, y)
	}
	return links.String() + dots.String() + fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="10.5" fill="#ffffff"/>`, cx, cy)
}

// guildNodes is a hub with a ring of six linked nodes.
func guildNodes(id string) string {
	turn := float64(hue(id) % 60)
	var lines, dots strings.Builder
	var pts [][2]float64
	for i := 0; i < 6; i++ {
		a := (turn + float64(i)*60) * math.Pi / 180
		x, y := at(50+31*math.Cos(a), 50+31*math.Sin(a))
		pts = append(pts, [2]float64{x, y})
	}
	cx, cy := at(50, 50)
	for i, p := range pts {
		q := pts[(i+1)%6]
		fmt.Fprintf(&lines, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ffffff" stroke-width="2.5" stroke-linecap="round"/>`, p[0], p[1], q[0], q[1])
		if i%2 == 0 {
			fmt.Fprintf(&lines, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ffffff" stroke-width="4" stroke-linecap="round"/>`, cx, cy, p[0], p[1])
		}
		fmt.Fprintf(&dots, `<circle cx="%.1f" cy="%.1f" r="5.5" fill="#ffffff"/>`, p[0], p[1])
	}
	return lines.String() + dots.String() + fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="9" fill="#ffffff"/>`, cx, cy)
}

func avatarShape(id string, size float32, shape, drawing, kind string) fyne.CanvasObject {
	h := float64(hue(id))
	a, b := hsl(h, .70, .58), hsl(math.Mod(h+50, 360), .75, .45)
	src := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><defs><linearGradient id="a" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient></defs>%s%s</svg>`, a, b, shape, drawing)
	img := icon(svg("avatar3-"+kind+id+".svg", src), size)
	return container.NewGridWrap(fyne.NewSize(size, size), img)
}

// hsl turns a hue/saturation/lightness colour into "#rrggbb".
func hsl(h, s, l float64) string {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return fmt.Sprintf("#%02x%02x%02x", int((r+m)*255+.5), int((g+m)*255+.5), int((b+m)*255+.5))
}

// ---- person row: one user in the Load User list ----

type personRow struct {
	widget.BaseWidget
	r        recentUser
	onTap    func()
	onForget func()
	bg       *canvas.Rectangle
	forget   *widget.Button
}

func newPersonRow(r recentUser, tap, forget func()) *personRow {
	p := &personRow{r: r, onTap: tap, onForget: forget}
	p.ExtendBaseWidget(p)
	return p
}

func (p *personRow) CreateRenderer() fyne.WidgetRenderer {
	p.bg = canvas.NewRectangle(colBG)
	p.bg.CornerRadius = 14
	p.bg.StrokeColor = colLine
	p.bg.StrokeWidth = 1
	name := text(p.r.Name, 15, colText, true)
	folder := widget.NewLabel(p.r.Folder)
	folder.Truncation = fyne.TextTruncateEllipsis
	words := container.NewVBox(layout.NewSpacer(), name, newTint(folder, tightTheme{Theme: appTheme, text: colFaint}), layout.NewSpacer())
	p.forget = widget.NewButtonWithIcon("", xIcon, p.onForget)
	p.forget.Importance = widget.LowImportance
	p.forget.Hide()
	row := container.NewBorder(nil, nil, container.NewCenter(avatar(p.r.Name, p.r.UserID, 40)), container.NewCenter(p.forget), words)
	return widget.NewSimpleRenderer(container.NewStack(p.bg, container.New(layout.NewCustomPaddedLayout(8, 8, 12, 8), row)))
}

func (p *personRow) Tapped(*fyne.PointEvent) { p.onTap() }

func (p *personRow) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (p *personRow) MouseIn(*desktop.MouseEvent) {
	p.bg.FillColor, p.bg.StrokeColor = colRaised, colLineStrong
	p.bg.Refresh()
	p.forget.Show()
}

func (p *personRow) MouseMoved(*desktop.MouseEvent) {}

func (p *personRow) MouseOut() {
	p.bg.FillColor, p.bg.StrokeColor = colBG, colLine
	p.bg.Refresh()
	p.forget.Hide()
}

// labelRow is a field's label with something at its right (a counter).
func labelRow(label string, right fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(0, 0, boxInset, 0),
		container.NewBorder(nil, nil, nil, right, text(label, 13, colDim, true)))
}

// twoSteps shows step1, then step2 once next succeeds (a new
// password is typed again on a second screen, which suits password
// managers and makes sure it is remembered). It returns the pair, and the
// functions that move between them.
func twoSteps(step1, step2 *fyne.Container) (both fyne.CanvasObject, forward, back func()) {
	step2.Hide()
	both = container.NewStack(step1, step2)
	forward = func() { step1.Hide(); step2.Show(); both.Refresh() }
	back = func() { step2.Hide(); step1.Show(); both.Refresh() }
	return both, forward, back
}
