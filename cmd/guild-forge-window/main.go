//go:build windows

// GuildForge.exe is GuildForge's window on Windows. It works
// on its own: it shows and changes the guilds kept in nexus-data,
// and runs them itself, as background copies of GuildForge.exe. guild-forge
// (cmd/guild-forge-cmd) does the same with commands, and both see the same
// guilds. Linux has no window: there guild-forge does everything.
package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"nexus/internal/assets"
	"nexus/internal/host"
	"nexus/internal/platform"
	"nexus/internal/ui"
)

func main() {
	// The copy set aside by the last update is no longer running.
	host.RemoveOldProgram()
	configDir, err := host.ConfigDir()
	if err != nil {
		showError("GuildForge could not open nexus-data: " + err.Error())
		return
	}

	// These words are for GuildForge itself, not people: "run=ID" runs one
	// guild in the background, and "boot" starts the guilds set to start on
	// boot (Windows' sign-in task runs it).
	if len(os.Args) > 1 {
		word, value, _ := strings.Cut(os.Args[1], "=")
		switch strings.ToLower(word) {
		case "run":
			host.Run(configDir, value)
		case "boot":
			host.StartBootGuilds(configDir, io.Discard)
		}
		return
	}

	err = platform.PrepareSystem(configDir)
	if err == nil {
		err = host.MoveOldGuilds(configDir, io.Discard)
	}
	if err != nil {
		showError("GuildForge could not open nexus-data: " + err.Error())
		return
	}
	var out io.Writer = io.Discard
	if f, err := os.OpenFile(filepath.Join(configDir, "guild-forge.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		out = f
	}
	logger := log.New(out, "", log.LstdFlags)
	// A missing sign-in task is made, and one whose program is gone is
	// pointed here.
	if err := host.CheckBoot(configDir); err != nil {
		logger.Println("Start on Boot:", err)
	}
	// The window's emoji font goes into nexus-data, if it is built in.
	assets.WriteOut()
	ui.RunForge(host.Version, configDir, notices, logger)
}

// showError shows a small Windows message box, since this program has no
// console to print to.
func showError(text string) {
	title, _ := syscall.UTF16PtrFromString("GuildForge")
	body, _ := syscall.UTF16PtrFromString(text)
	const iconError = 0x10
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), iconError)
}
