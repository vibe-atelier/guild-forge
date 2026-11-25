package ui

// GuildForge's gear at the bottom right of its home screen, and the two
// pages it opens: Licenses and Check Updates.

import (
	"bytes"
	"os/exec"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/host"
	"nexus/internal/platform"
)

// menuIcon is a small icon that opens a menu when clicked.
type menuIcon struct {
	widget.BaseWidget
	res  fyne.Resource
	menu func() *fyne.Menu
}

func newMenuIcon(res fyne.Resource, menu func() *fyne.Menu) *menuIcon {
	m := &menuIcon{res: res, menu: menu}
	m.ExtendBaseWidget(m)
	return m
}

func (m *menuIcon) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.New(layout.NewCustomPaddedLayout(6, 6, 6, 6), icon(m.res, 22)))
}

func (m *menuIcon) Cursor() desktop.Cursor { return desktop.PointerCursor }

// Tapped opens the menu above and to the left of the icon, so it stays in
// the window at the bottom right.
func (m *menuIcon) Tapped(*fyne.PointEvent) {
	c := fyne.CurrentApp().Driver().CanvasForObject(m)
	if c == nil {
		return
	}
	pop := widget.NewPopUpMenu(m.menu(), c)
	at := fyne.CurrentApp().Driver().AbsolutePositionForObject(m)
	size := pop.MinSize()
	pop.ShowAtPosition(fyne.NewPos(at.X+m.Size().Width-size.Width, at.Y-size.Height-4))
}

// licensesPage shows the notices of the software GuildForge is built with.
func (f *forge) licensesPage() fyne.CanvasObject {
	words := widget.NewLabel(f.notices)
	words.TextStyle = fyne.TextStyle{Monospace: true}
	words.Wrapping = fyne.TextWrapWord
	head := container.NewBorder(nil, nil, backButton(func() { f.goTo("home") }), nil)
	return forgeFrame(infoIcon, "Licenses", head,
		titled(heading("Licenses"), para("GuildForge is built with the software below. Each notice is shown as its licence asks.")),
		card(10, words))
}

// updatesPage checks GitHub for a newer GuildForge only when asked, and
// installs it. Running guilds are stopped for a moment and started again.
func (f *forge) updatesPage() fyne.CanvasObject {
	head := container.NewBorder(nil, nil, backButton(func() { f.upd = appUpdate{}; f.goTo("home") }), nil)
	install := func() error {
		return host.Update(&bytes.Buffer{}, f.dir, host.WindowFile, "GuildForge")
	}
	restart := func() error {
		exe, err := platform.ProgramPath()
		if err != nil {
			return err
		}
		return exec.Command(exe).Start()
	}
	redraw := func() { f.sig, f.catSig = "", ""; f.load(); f.draw() }
	return forgeFrame(infoIcon, "Check Updates", head,
		titled(heading("Check Updates"), para("This is GuildForge "+f.version+". Nothing contacts GitHub until you press Check Updates.")),
		updatesBox("GuildForge", f.version, &f.upd, "Installing it stops running guilds for a moment and starts them again on the new version.", install, restart, redraw))
}
