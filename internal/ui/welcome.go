package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// welcome is the first screen: Load User or Create User.
func (u *UI) welcome() fyne.CanvasObject {
	load := newChoiceCard(folderIcon, "Load User",
		[]string{"Open a user you already have", "on this computer."}, func() { u.show(u.loadUser()) })
	create := newChoiceCard(userPlusIcon, "Create User",
		[]string{"Make a brand-new identity.", "It lives only on your computer."}, func() { u.show(u.createUser()) })

	page := container.NewVBox(
		container.NewCenter(icon(logoIcon, 76)),
		gap(10),
		centred(text("Nexus", 46, colText, true)),
		gap(2),
		centred(text("Your people. Your servers. Nobody in between.", 16, colDim, false)),
		gap(40),
		container.NewGridWithColumns(2, container.New(layout.NewCustomPaddedLayout(0, 0, 0, 8), load), container.New(layout.NewCustomPaddedLayout(0, 0, 8, 0), create)),
		gap(38),
		centred(text("Version "+u.Version, 12.5, colFaint, false)),
	)
	// The gear at the bottom right holds Licenses and Check Updates.
	return container.NewBorder(nil, u.welcomeGear(), nil, nil, narrow(760, page))
}
