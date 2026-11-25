package ui

import (
	"image/color"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
)

// This file is the rail: the column of round-cornered buttons on the far left (home,
// your guilds, add a guild).

// guildReasons say why a guild is grey (the same words as the HTML app).
var guildReasons = map[string]string{
	"connecting": "Connecting…",
	"elsewhere":  "Active on another computer",
	"not_member": "You are not a member any more",
	"wrong_key":  "Identity changed. This may be an impostor.",
	"pending":    "Waiting for the owner's approval",
	"declined":   "Your join request was declined",
	"removed":    "You were removed from this guild",
	"banned":     "You are banned from this guild",
}

// guildState is the line that explains a guild's state ("" when online).
func guildState(g guildconn.GuildView) string {
	switch g.Status {
	case "online":
		return ""
	case "offline":
		if g.Detail != "" {
			return g.Detail
		}
		return "Offline"
	}
	if r, ok := guildReasons[g.Status]; ok {
		return r
	}
	return g.Detail
}

// railButton is one square button in the rail.
type railButton struct {
	widget.BaseWidget
	face   fyne.CanvasObject
	active bool
	dimmed bool
	dashed bool // It is true for the "add" button.
	onTap  func()

	// Resting on it shows the guild's name and state beside it.
	u          *UI
	tip, state string
	pop        *widget.PopUp

	bg, pill, shade *canvas.Rectangle
}

func newRailButton(face fyne.CanvasObject, tap func()) *railButton {
	b := &railButton{face: face, onTap: tap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *railButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(colPanel)
	b.bg.CornerRadius = 16
	b.bg.StrokeWidth = 1
	b.shade = canvas.NewRectangle(color.NRGBA{0, 0, 0, 150}) // This greys out offline guilds.
	b.shade.CornerRadius = 16
	b.pill = canvas.NewRectangle(color.White)
	b.pill.CornerRadius = 2
	b.paint(false)
	square := container.NewGridWrap(fyne.NewSize(48, 48), container.NewStack(b.bg, container.NewCenter(b.face), b.shade))
	return &railRenderer{b: b, square: square}
}

func (b *railButton) paint(hover bool) {
	b.bg.FillColor, b.bg.StrokeColor = colPanel, colLine
	if hover || b.active {
		b.bg.FillColor = colRaised
	}
	if b.active {
		b.bg.StrokeColor = color.NRGBA{179, 13, 30, 90}
	}
	if b.dashed {
		b.bg.StrokeColor = color.NRGBA{34, 197, 94, 110}
		if hover {
			b.bg.FillColor = color.NRGBA{34, 197, 94, 30}
		}
	}
	if b.dimmed && !hover {
		b.shade.Show()
	} else {
		b.shade.Hide()
	}
	switch {
	case b.active:
		b.pill.Resize(fyne.NewSize(4, 36))
		b.pill.Show()
	case hover && !b.dashed:
		b.pill.Resize(fyne.NewSize(4, 18))
		b.pill.Show()
	default:
		b.pill.Hide()
	}
	b.pill.Move(fyne.NewPos(0, (48-b.pill.Size().Height)/2))
	b.bg.Refresh()
}

func (b *railButton) Tapped(*fyne.PointEvent) { b.hideTip(); b.onTap() }

func (b *railButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *railButton) MouseIn(*desktop.MouseEvent) {
	b.paint(true)
	canvas.Refresh(b)
	if b.u != nil && b.tip != "" && b.pop == nil {
		b.pop = showTipBeside(b.u, b, b.tip, b.state)
	}
}

func (b *railButton) hideTip() {
	if b.pop != nil {
		b.pop.Hide()
		b.pop = nil
	}
}

func (b *railButton) MouseMoved(*desktop.MouseEvent) {}

func (b *railButton) MouseOut() { b.hideTip(); b.paint(false); canvas.Refresh(b) }

// railRenderer puts the selection pill at the rail's left edge.
type railRenderer struct {
	b      *railButton
	square fyne.CanvasObject
}

func (r *railRenderer) Layout(s fyne.Size) {
	r.square.Resize(fyne.NewSize(48, 48))
	r.square.Move(fyne.NewPos((s.Width-48)/2, 0))
	ps := r.b.pill.Size()
	r.b.pill.Move(fyne.NewPos(0, (48-ps.Height)/2))
}

func (r *railRenderer) MinSize() fyne.Size           { return fyne.NewSize(72, 48) }
func (r *railRenderer) Refresh()                     { r.b.paint(false); r.Layout(r.b.Size()); canvas.Refresh(r.b) }
func (r *railRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.b.pill, r.square} }
func (r *railRenderer) Destroy()                     {}

// guildFace is a guild's picture, or its initials on its own colour.
func (u *UI) guildFace(g guildconn.GuildView) fyne.CanvasObject {
	if g.PictureHash != "" && u.user != nil {
		if data, err := os.ReadFile(u.user.GuildPicturePath(g.ID)); err == nil {
			img := canvas.NewImageFromResource(fyne.NewStaticResource("guild-"+g.PictureHash, data))
			img.FillMode = canvas.ImageFillContain
			img.CornerRadius = 15
			img.SetMinSize(fyne.NewSize(48, 48))
			return img
		}
	}
	return avatarSquare(g.Name, g.ID, 48)
}

// showTipBeside shows a guild's name, and its state under it, to the right
// of a rail button.
func showTipBeside(u *UI, o fyne.CanvasObject, name, state string) *widget.PopUp {
	c := fyne.CurrentApp().Driver().CanvasForObject(o)
	if c == nil || u.win == nil {
		return nil
	}
	bg := canvas.NewRectangle(colMenu)
	bg.CornerRadius = 6
	bg.StrokeColor = colLineStrong
	bg.StrokeWidth = 1
	words := container.NewVBox(text(name, 13.5, colText, true))
	if state != "" {
		words.Add(text(state, 12, colDim, false))
	}
	label := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(6, 6, 10, 10), words))
	pop := widget.NewPopUp(label, c)
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
	s := label.MinSize()
	pop.ShowAtPosition(fyne.NewPos(pos.X+o.Size().Width+10, pos.Y+(o.Size().Height-s.Height)/2))
	return pop
}

// guildStateText is a guild's state in a few words.
func guildStateText(status string) string {
	switch status {
	case "online":
		return "Online"
	case "connecting":
		return "Connecting…"
	case "elsewhere":
		return "Open Elsewhere"
	case "pending":
		return "Awaiting Approval"
	case "removed":
		return "Removed"
	case "declined":
		return "Request Declined"
	case "not_member":
		return "Membership Ended"
	}
	return "Offline"
}
