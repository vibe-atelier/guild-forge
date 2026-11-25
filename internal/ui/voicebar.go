package ui

// The voice page's look: square tiles, the channel's own
// buttons as icons in a row at the bottom of the window (each says what it
// does when the pointer rests on it), and the soundboard as square pictures
// behind a Soundboard button.

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

var (
	cameraIcon     = svg("camera.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><rect x="3" y="6.5" width="13" height="11" rx="2"/><path d="M16 10.5l5-3v9l-5-3z"/></g></svg>`)
	cameraOffIcon  = svg("camera-off.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#ff5a36" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="6.5" width="13" height="11" rx="2"/><path d="M16 10.5l5-3v9l-5-3zM3 3l18 18"/></g></svg>`)
	screenOffIcon  = svg("screen-off.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#ff5a36" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20M9 8l6 5M15 8l-6 5"/></g></svg>`)
	soundboardIcon = svg("soundboard.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><path d="M15.5 19.5v-5l4-1v5"/><circle cx="14.5" cy="19.5" r="1"/><circle cx="18.5" cy="18.5" r="1"/></g></svg>`)
)

// tipButton is an icon button whose name shows while the pointer rests on
// it (the voice page's buttons have no words).
type tipButton struct {
	widget.Button
	u   *UI
	tip string
	pop *widget.PopUp
}

func newTipButton(u *UI, tip string, ic fyne.Resource, tap func()) *tipButton {
	b := &tipButton{u: u, tip: tip}
	b.Icon, b.OnTapped = ic, tap
	b.ExtendBaseWidget(b)
	return b
}

func (b *tipButton) MouseIn(e *desktop.MouseEvent) {
	b.Button.MouseIn(e)
	b.showTip()
}

func (b *tipButton) MouseOut() {
	b.Button.MouseOut()
	b.hideTip()
}

func (b *tipButton) Tapped(e *fyne.PointEvent) {
	b.hideTip()
	b.Button.Tapped(e)
}

func (b *tipButton) showTip() { b.pop = showTipFor(b.u, b, b.tip) }
func (b *tipButton) hideTip() {
	if b.pop != nil {
		b.pop.Hide()
		b.pop = nil
	}
}

// showTipFor shows a small label above an object.
func showTipFor(u *UI, o fyne.CanvasObject, tip string) *widget.PopUp {
	if tip == "" || u.win == nil {
		return nil
	}
	c := fyne.CurrentApp().Driver().CanvasForObject(o)
	if c == nil {
		return nil
	}
	bg := canvas.NewRectangle(colMenu)
	bg.CornerRadius = 6
	bg.StrokeColor = colLineStrong
	bg.StrokeWidth = 1
	label := container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(4, 4, 8, 8), text(tip, 12.5, colText, false)))
	pop := widget.NewPopUp(label, c)
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(o)
	s := label.MinSize()
	pop.ShowAtPosition(fyne.NewPos(pos.X+(o.Size().Width-s.Width)/2, pos.Y-s.Height-6))
	return pop
}

// voiceControls is the row of icons at the bottom of a voice page.
func (u *UI) voiceControls(g guildconn.GuildView, n *guildconn.ChannelView) fyne.CanvasObject {
	m, d := u.voice.Muted()
	var row []fyne.CanvasObject
	add := func(tip string, ic fyne.Resource, tap func(), danger bool) {
		b := newTipButton(u, tip, ic, tap)
		if danger {
			b.Importance = widget.DangerImportance
		}
		row = append(row, b)
	}
	if m {
		add("Unmute", micOffIcon, func() { u.voice.SetMute(false) }, false)
	} else {
		add("Mute", micIcon, func() { u.voice.SetMute(true) }, false)
	}
	if d {
		add("Undeafen", headsetOffIcon, func() { u.voice.SetDeafen(false) }, false)
	} else {
		add("Deafen", headsetIcon, func() { u.voice.SetDeafen(true) }, false)
	}
	if name, _ := u.voice.Sharing(); name != "" {
		add("Stop Sharing", screenOffIcon, func() { go u.voice.StopSharing() }, false)
	} else if canIn(g, n.ID, proto.PermStream) {
		add("Share Screen", screenIcon, func() { u.openPageArg("share", n.ID) }, false)
	}
	if u.voice.CameraOn() {
		add("Stop Camera", cameraOffIcon, func() { go func() { u.voice.StopCamera(); fyne.Do(u.redraw) }() }, false)
	} else if canIn(g, n.ID, proto.PermStream) {
		add("Start Camera", cameraIcon, func() { u.startCamera() }, false)
	}
	if u.soundboardFor(g, n) != nil {
		tip := "Show Soundboard"
		if u.showBoard {
			tip = "Hide Soundboard"
		}
		add(tip, soundboardIcon, func() { u.showBoard = !u.showBoard; u.redraw() }, false)
	}
	more := newTipButton(u, "More", dotsIcon, nil)
	more.OnTapped = func() { u.voiceMenu(g, n, more) }
	row = append(row, more)
	add("Leave", hangUpLightIcon, func() { go u.voice.Leave() }, true)
	return container.NewCenter(container.NewHBox(row...))
}

// startCamera turns your camera on, saying why if it can't.
func (u *UI) startCamera() {
	go func() {
		err := u.voice.StartCamera()
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
			}
			u.redraw()
		})
	}()
}

// voiceMenu is the ⋯ menu: the soundboard, locking, and the settings.
func (u *UI) voiceMenu(g guildconn.GuildView, n *guildconn.ChannelView, anchor fyne.CanvasObject) {
	var items []*fyne.MenuItem
	if u.soundboardFor(g, n) != nil {
		label := "Show Soundboard"
		if u.showBoard {
			label = "Hide Soundboard"
		}
		items = append(items, fyne.NewMenuItem(label, func() { u.showBoard = !u.showBoard; u.redraw() }))
	}
	if canIn(g, n.ID, proto.PermLockChannel) {
		label, on := "Lock Channel", true
		if n.Locked {
			label, on = "Unlock Channel", false
		}
		items = append(items, fyne.NewMenuItem(label, func() {
			u.voiceDo(g, func(gc *guildconn.Conn) error { return gc.VoiceLock(n.ID, on) })
		}))
	}
	items = append(items, fyne.NewMenuItem("Voice Settings", func() { u.openUserSettings("voice") }))
	menu := widget.NewPopUpMenu(fyne.NewMenu("", items...), u.win.Canvas())
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	menu.ShowAtPosition(pos.Add(fyne.NewPos(0, -menu.MinSize().Height-6)))
}

// soundTile is one soundboard sound: a square picture with a narrow edge;
// its name shows on hover.
type soundTile struct {
	widget.BaseWidget
	u    *UI
	name string
	face fyne.CanvasObject
	tap  func()
	edge *canvas.Rectangle
	pop  *widget.PopUp
}

const soundTileSize = 64

func newSoundTile(u *UI, name string, res fyne.Resource, tap func()) *soundTile {
	var face fyne.CanvasObject
	if res != nil {
		img := canvas.NewImageFromResource(res)
		img.FillMode = canvas.ImageFillContain
		img.CornerRadius = 8
		face = img
	} else { // With no picture, it shows its first letters on a coloured square.
		bg := canvas.NewRectangle(nameHue(name))
		bg.CornerRadius = 8
		face = container.NewStack(bg, container.NewCenter(text(initials(name), 18, colText, true)))
	}
	t := &soundTile{u: u, name: name, face: face, tap: tap}
	t.ExtendBaseWidget(t)
	return t
}

func (t *soundTile) CreateRenderer() fyne.WidgetRenderer {
	t.edge = canvas.NewRectangle(nil)
	t.edge.CornerRadius = 9
	t.edge.StrokeColor = colLineStrong
	t.edge.StrokeWidth = 1
	inner := container.New(layout.NewCustomPaddedLayout(1, 1, 1, 1), t.face)
	return widget.NewSimpleRenderer(container.New(fixedSize{fyne.NewSize(soundTileSize, soundTileSize)}, container.NewStack(inner, t.edge)))
}

func (t *soundTile) Tapped(*fyne.PointEvent) { t.tap() }
func (t *soundTile) Cursor() desktop.Cursor  { return desktop.PointerCursor }
func (t *soundTile) MouseIn(*desktop.MouseEvent) {
	t.edge.StrokeColor = colAccentLink
	t.edge.Refresh()
	t.pop = showTipFor(t.u, t, t.name)
}
func (t *soundTile) MouseMoved(*desktop.MouseEvent) {}
func (t *soundTile) MouseOut() {
	t.edge.StrokeColor = colLineStrong
	t.edge.Refresh()
	if t.pop != nil {
		t.pop.Hide()
		t.pop = nil
	}
}

// initials are up to two first letters of a name.
func initials(s string) string {
	var out []rune
	for _, w := range strings.Fields(s) {
		if r := []rune(w); len(r) > 0 && len(out) < 2 {
			out = append(out, r[0])
		}
	}
	return strings.ToUpper(string(out))
}

// nameHue is a steady colour for a name (dark enough for white letters).
func nameHue(s string) color.Color {
	hues := []color.NRGBA{{0x8b, 0x1a, 0x2b, 255}, {0x5b, 0x21, 0x6b, 255}, {0x1f, 0x4e, 0x79, 255}, {0x1e, 0x6b, 0x4a, 255}, {0x7a, 0x4b, 0x12, 255}, {0x4a, 0x4a, 0x55, 255}}
	h := 0
	for _, r := range s {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return hues[h%len(hues)]
}

// hangUpLightIcon is the hang-up phone in white, for the red Leave button
// (a red phone on red can't be seen).
var hangUpLightIcon = svg("hang-up-light.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#ffffff" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 13.5c4.5-4.2 12.5-4.2 17 0l-2 2.5-3-1.2v-2.3c-2.3-.8-4.7-.8-7 0v2.3l-3 1.2z"/></g></svg>`)
