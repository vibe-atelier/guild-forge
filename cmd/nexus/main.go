// nexus is the Nexus desktop app (the client). Its window is drawn with Fyne;
// everything under the window is standard-library Go.
package main

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"nexus/internal/assets"
	"nexus/internal/host"
	"nexus/internal/nexusdata"
	"nexus/internal/ui"
)

// version is shared by every released program.
const version = host.Version

func main() {
	// The copy set aside by the last update is no longer running.
	host.RemoveOldProgram()
	// The app's settings live in this PC's nexus-data. NEXUS_HOME puts
	// them somewhere else, for tests and the guide's pictures.
	configDir := os.Getenv("NEXUS_HOME")
	moved := false
	if configDir == "" {
		// Settings from before nexus-data move in, once.
		if base, err := os.UserConfigDir(); err == nil {
			moved, _ = nexusdata.MoveIn(filepath.Join(base, "nexus"), nexusdata.AppPart)
		}
		dir, err := nexusdata.Part(nexusdata.AppPart)
		if err != nil {
			log.Fatal(err)
		}
		configDir = dir
	}
	os.MkdirAll(configDir, 0o700)

	var logOut io.Writer = io.Discard
	if f, err := os.OpenFile(filepath.Join(configDir, "nexus.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		defer f.Close()
		logOut = f
	}
	logger := log.New(logOut, "", log.LstdFlags)
	logger.Printf("nexus %s started", version)
	if moved {
		logger.Printf("moved the settings into %s", configDir)
	}
	// The built-in emoji font and noise model go into nexus-data.
	if os.Getenv("NEXUS_HOME") == "" {
		if err := assets.WriteOut(); err != nil {
			logger.Printf("could not write the built-in files: %v", err)
		}
	}
	if hasNotices {
		ui.Notices = notices
	}
	ui.Run(version, configDir, logger)
	logger.Printf("nexus stopped")
}
