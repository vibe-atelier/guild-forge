// shots runs the Nexus app drawn in memory only, to take the guide's
// pictures without a window popping up on the screen. It reads
// the same picture switches as the app (NEXUS_SCREEN, NEXUS_SHOT, …; see
// internal/ui/screens.go). It is a tool for the docs, not part of Nexus.
package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"nexus/internal/host"
	"nexus/internal/ui"
)

// Fyne's test driver runs fyne.Do work straight away on whichever goroutine
// asks, so two updates could collide. Like the real driver, these run one
// at a time, in order, on one goroutine.
type queueDriver struct {
	fyne.Driver
	work chan func()
}

// DoFromGoroutine queues f for the one work goroutine (and waits if asked).
func (d *queueDriver) DoFromGoroutine(f func(), wait bool) {
	if !wait {
		go func() { d.work <- f }()
		return
	}
	done := make(chan struct{})
	d.work <- func() { f(); close(done) }
	<-done
}

type queueApp struct {
	fyne.App
	d *queueDriver
}

// Driver is the queueing driver above.
func (a *queueApp) Driver() fyne.Driver { return a.d }

func main() {
	configDir := os.Getenv("NEXUS_HOME")
	if configDir == "" {
		log.Fatal("set NEXUS_HOME")
	}
	os.MkdirAll(configDir, 0o700)
	var out io.Writer = io.Discard
	if f, err := os.OpenFile(filepath.Join(configDir, "nexus.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		out = f
	}
	base := test.NewApp()
	a := &queueApp{App: base, d: &queueDriver{Driver: base.Driver(), work: make(chan func(), 256)}}
	fyne.SetCurrentApp(a)
	if os.Getenv("FORGE_SCREEN") != "" { // This opens GuildForge's window instead.
		// The Licenses page shows the notices the build script wrote, if any.
		notices, _ := os.ReadFile(filepath.Join("cmd", "guild-forge-window", "NOTICES.txt"))
		a.d.work <- func() {
			ui.RunForgeOffscreen(a, host.Version, os.Getenv("GUILD_FORGE_HOME"), string(notices), log.New(out, "", log.LstdFlags))
		}
	} else {
		// The Licenses pages show the notices the build script wrote, if any.
		if notices, err := os.ReadFile(filepath.Join("cmd", "nexus", "NOTICES.txt")); err == nil {
			ui.Notices = string(notices)
		}
		a.d.work <- func() { ui.RunOffscreen(a, host.Version, configDir, log.New(out, "", log.LstdFlags)) }
	}
	for f := range a.d.work {
		f()
	}
}
