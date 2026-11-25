package ui

// The app's Licenses and Check Updates pages. They sit under a gear
// at the bottom right of the welcome screen, as on GuildForge's home screen,
// and again in User Settings under APP once a user is signed in.

import (
	"bytes"
	"os/exec"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/host"
	"nexus/internal/platform"
)

// Notices is the app's third-party notices, shown on its Licenses page. The
// program sets it before Run from the notices built into it.
var Notices = "This copy of Nexus was built without its notices. Build it with build.ps1 to include them."

// RestartArg is the word the app is started with by Restart Nexus. The new
// copy waits for the old one to close before it opens its window, because
// only one copy runs at a time.
const RestartArg = "restart"

// appUpdate is where Check Updates has got to: "" (not checked yet),
// "uptodate", "available" or "done", and the newest version found.
type appUpdate struct {
	state, latest string
}

// welcomeGear is the gear at the bottom right of the welcome screen.
func (u *UI) welcomeGear() fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(0, 14, 0, 16), container.NewHBox(layout.NewSpacer(), newMenuIcon(gearIcon, func() *fyne.Menu {
		return fyne.NewMenu("",
			fyne.NewMenuItem("Licenses", func() { u.show(u.licensesScreen()) }),
			fyne.NewMenuItem("Check Updates", func() { u.show(u.updatesScreen()) }))
	})))
}

// licensesWords is the notices, in a card.
func licensesWords() fyne.CanvasObject {
	words := widget.NewLabel(Notices)
	words.TextStyle = fyne.TextStyle{Monospace: true}
	words.Wrapping = fyne.TextWrapWord
	return card(10, words)
}

const licensesIntro = "Nexus is built with the software below. Each notice is shown as its licence asks."

// licensesScreen is the Licenses page opened from the welcome screen.
func (u *UI) licensesScreen() fyne.CanvasObject {
	head := container.NewBorder(nil, nil, backButton(func() { u.show(u.welcome()) }), nil)
	return forgeFrame(infoIcon, "Licenses", head,
		titled(heading("Licenses"), para(licensesIntro)), licensesWords())
}

// updatesScreen is the Check Updates page opened from the welcome screen.
func (u *UI) updatesScreen() fyne.CanvasObject {
	head := container.NewBorder(nil, nil, backButton(func() { u.show(u.welcome()) }), nil)
	return forgeFrame(infoIcon, "Check Updates", head,
		titled(heading("Check Updates"), para(u.updatesIntro())),
		u.updatesBox(func() { u.show(u.updatesScreen()) }))
}

// licensesPanel is the Licenses page in User Settings.
func (u *UI) licensesPanel() {
	u.openPanel("licenses", 600, panelTitle("Licenses"), para(licensesIntro), licensesWords())
}

// updatesPanel is the Check Updates page in User Settings.
func (u *UI) updatesPanel() {
	u.openPanel("updates", 600, panelTitle("Check Updates"), para(u.updatesIntro()),
		u.updatesBox(func() {
			if u.settings != nil && u.settings.cat == "updates" {
				u.openSettingsCat("updates")
			}
		}))
}

func (u *UI) updatesIntro() string {
	return "This is Nexus " + u.Version + ". Nothing contacts GitHub until you press Check Updates."
}

// updatesBox is the app's part of Check Updates: it installs from GitHub,
// except on Linux, where the app is built from the source code.
func (u *UI) updatesBox(redraw func()) fyne.CanvasObject {
	install, note := func() error { return host.UpdateApp(&bytes.Buffer{}) }, "Your users, settings and messages stay as they are."
	if host.AppFile() == "" {
		install, note = nil, "Build it again from the newest source code to update."
	}
	restart := func() error {
		exe, err := platform.ProgramPath()
		if err != nil {
			return err
		}
		return exec.Command(exe, RestartArg).Start()
	}
	return updatesBox("Nexus", u.Version, &u.upd, note, install, restart, redraw)
}

// updatesBox shows Check Updates for program name, as used by both the app
// and GuildForge. It contacts GitHub only when Check Updates is pressed.
// Install Update is always shown, greyed out until a newer version is found
// and install is not nil; once it has run, the same button restarts the
// program. note follows "… is available." st holds where it has got to, and
// redraw shows the page again after each step.
func updatesBox(name, version string, st *appUpdate, note string, install, restart func() error, redraw func()) fyne.CanvasObject {
	problem := newErrorText()
	// work does a step away from the window, then shows the page again.
	work := func(b *widget.Button, busy, label string, step func() error) {
		b.SetText(busy)
		b.Disable()
		problem.Set(nil)
		go func() {
			err := step()
			fyne.Do(func() {
				b.Enable()
				b.SetText(label)
				if err != nil {
					problem.Set(err)
					return
				}
				redraw()
			})
		}()
	}

	var status fyne.CanvasObject = layout.NewSpacer()
	action := primary("Install Update", nil)
	action.Disable()
	switch st.state {
	case "uptodate":
		status = para(name + " " + version + " is up to date.")
	case "available":
		status = para(name + " " + st.latest + " is available. " + note)
		if install != nil {
			action.Enable()
			action.OnTapped = func() {
				work(action, "Installing...", "Install Update", func() error {
					err := install()
					if err == nil {
						// The page's state changes on the window's own thread.
						fyne.DoAndWait(func() { st.state = "done" })
					}
					return err
				})
			}
		}
	case "done":
		status = para(name + " is updated to " + st.latest + ". Restart " + name + " to use the new version.")
		action.SetText("Restart " + name)
		action.Enable()
		action.OnTapped = func() {
			if err := restart(); err != nil {
				problem.Set(err)
				return
			}
			fyne.CurrentApp().Quit()
		}
	}

	check := widget.NewButton("Check Updates", nil)
	check.OnTapped = func() {
		work(check, "Checking...", "Check Updates", func() error {
			latest, newer, err := host.LatestVersion()
			if err != nil {
				return err
			}
			fyne.DoAndWait(func() {
				*st = appUpdate{state: "uptodate", latest: latest}
				if newer {
					st.state = "available"
				}
			})
			return nil
		})
	}
	if st.state == "done" {
		check.Disable()
	}
	// Both buttons sit at the right, the main one last, as in other forms.
	// The column lines the words up with the text above them.
	return stack(4, status, problem.box, buttonsRow(check, action))
}
