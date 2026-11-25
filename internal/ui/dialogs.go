package ui

import (
	"image/color"
	"strings"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// This file has pop-up windows inside the app window, and short notes ("toasts").
// (Fyne's own dialog package needs an extra library; these are plain Fyne.)

// popup is an open pop-up window.
type popup struct {
	pop    *widget.PopUp
	inner  fyne.CanvasObject // It is the content, inside a scroll.
	scroll *container.Scroll
	c      fyne.Canvas
	width  float32
	closed bool

	// When embedded, it is shown inside a settings page instead (openPanel). Closing
	// it draws the category again from fresh.
	embedded bool
	reload   func()
}

func (p *popup) Close() {
	if p.embedded {
		if !p.closed {
			p.closed = true
			p.reload()
		}
		return
	}
	p.closed = true
	p.pop.Hide()
}

// fit resizes the pop-up to its content and keeps it centred.
func (p *popup) fit() {
	if p.embedded {
		return
	}
	cs := p.c.Size()
	h := fyne.Min(p.inner.MinSize().Height+48, cs.Height-40)
	s := fyne.NewSize(p.width, h)
	if p.pop.Content.Size() == s {
		return
	}
	p.scroll.SetMinSize(fyne.NewSize(10, h-48))
	p.pop.Resize(s)
	p.pop.Move(fyne.NewPos((cs.Width-s.Width)/2, (cs.Height-s.Height)/2))
}

// openPopup shows content in a rounded box over a darkened window. Clicking
// outside does not close it (so typing is never lost by accident).
func (u *UI) openPopup(width float32, content ...fyne.CanvasObject) *popup {
	bg := canvas.NewRectangle(colMenu)
	bg.CornerRadius = 20
	bg.StrokeColor = colLineStrong
	bg.StrokeWidth = 1
	inner := form(content...)
	sc := container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 0, 26, 26), inner))
	box := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(24, 24, 0, 0), sc))
	sized := container.New(fixedWidth{width}, box)
	p := &popup{pop: widget.NewModalPopUp(sized, u.win.Canvas()), c: u.win.Canvas(), width: width, inner: inner, scroll: sc}
	p.pop.Show()
	p.fit()
	// Content can grow (a preview, an error): keep the window fitting.
	go func() {
		for !p.closed {
			time.Sleep(200 * time.Millisecond)
			fyne.Do(func() {
				if !p.closed {
					p.fit()
				}
			})
		}
	}()
	return p
}

// buttonsRow puts buttons at the right, the main one last, with a little
// room above them so they never touch the text over them.
func buttonsRow(buttons ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(gap(8), container.NewHBox(append([]fyne.CanvasObject{layout.NewSpacer()}, buttons...)...))
}

// confirm asks a yes/no question; yes runs only if confirmed.
func (u *UI) confirm(title, question, yesLabel string, danger bool, yes func()) {
	var p *popup
	ok := widget.NewButton(yesLabel, func() { p.Close(); yes() })
	ok.Importance = widget.HighImportance
	if danger {
		ok.Importance = widget.DangerImportance
	}
	cancel := widget.NewButton("Cancel", func() { p.Close() })
	p = u.openPopup(440, text(title, 20, colText, true), para(question), buttonsRow(cancel, ok))
}

// toast shows a short note at the bottom of the window for a few seconds.
func (u *UI) toast(msg string) {
	msg = toastText(msg)
	bg := canvas.NewRectangle(color.NRGBA{0x18, 0x18, 0x1e, 245})
	bg.CornerRadius = 12
	bg.StrokeColor = colLineStrong
	bg.StrokeWidth = 1
	note := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(10, 10, 16, 16), centred(text(msg, 14, colText, false))))
	pop := widget.NewPopUp(note, u.win.Canvas())
	s := note.MinSize()
	ws := u.win.Canvas().Size()
	// Centred on the page, beside the side bar.
	left, width := float32(0), ws.Width
	if u.home != nil && u.home.main != nil && fyne.CurrentApp().Driver().CanvasForObject(u.home.main) == u.win.Canvas() {
		left = fyne.CurrentApp().Driver().AbsolutePositionForObject(u.home.main).X
		width = u.home.main.Size().Width
	}
	pop.ShowAtPosition(fyne.NewPos(left+(width-s.Width)/2, ws.Height-s.Height-28))
	time.AfterFunc(2600*time.Millisecond, func() { fyne.Do(pop.Hide) })
}

// toastText follows the wording rules: a short phrase is in Title
// Case with no full stop ("Role Saved"); anything longer is a sentence.
func toastText(msg string) string {
	msg = strings.TrimSpace(msg)
	core := strings.TrimRight(msg, ".")
	words := strings.Fields(core)
	if len(words) > 0 && len(words) <= 4 && !strings.ContainsAny(core, ".,:;!?…") {
		for i, w := range words {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
		return strings.Join(words, " ")
	}
	return sentence(msg)
}
