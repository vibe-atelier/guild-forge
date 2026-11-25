package host

// This file moves guilds from an older folder, once. The first time GuildForge runs
// with a new folder, the guilds kept in the old one move over: nexus-host's
// folder, or GuildForge's own folder in %APPDATA% from before
// nexus-data. The folder is renamed, guild folders inside it are
// pointed at their new place, start-on-boot is set up again, and guilds that
// were running are started again.

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexus/internal/fsutil"
	"nexus/internal/guild"
	"nexus/internal/nexusdata"
	"nexus/internal/platform"
)

// MoveOldGuilds moves the guilds of the first older folder that has any into
// configDir, once. It says what it did on out. Nothing happens when there is
// nothing to move, or when configDir already has guilds of its own.
func MoveOldGuilds(configDir string, out io.Writer) error {
	if os.Getenv("GUILD_FORGE_HOME") != "" {
		return nil // Tests and pictures use their own folders.
	}
	if _, err := os.Stat(filepath.Join(configDir, registryFileName)); err == nil {
		return nil // The guilds were already moved, or this PC has its own.
	}
	for _, old := range platform.OldConfigDirs() {
		if fsutil.SamePath(old, configDir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(old, registryFileName)); err != nil {
			continue // This older folder has no guilds.
		}
		return moveGuildsFrom(old, configDir, out)
	}
	return nil
}

// moveGuildsFrom moves the folder old, with its guilds, to configDir.
func moveGuildsFrom(old, configDir string, out io.Writer) error {
	oldReg, err := LoadRegistry(old)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Moving %d guild(s) from %s to %s...\n", len(oldReg.Guilds), old, configDir)

	// Stop the guilds that are running there, remembering which they were.
	var wasRunning []string
	for _, e := range oldReg.Guilds {
		if _, running := guild.RunningStatus(e.DataDir); !running {
			continue
		}
		wasRunning = append(wasRunning, e.GuildID)
		if !platform.StopOldService(e.GuildID) {
			guild.Call(e.DataDir, "stop")
		}
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if _, running := guild.RunningStatus(e.DataDir); !running {
				break
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(configDir), 0o755); err != nil {
		return err
	}
	// GuildForge makes its empty folders when it starts; they are in the way
	// of the move, so they go first. A folder with any file in it stays.
	if onlyEmptyFolders(configDir) {
		os.RemoveAll(configDir)
	}
	if err := os.Rename(old, configDir); err != nil {
		return fmt.Errorf("could not move %s to %s: %w. Stop every guild and try again", old, configDir, err)
	}
	nexusdata.Shared(configDir) // The moved folder is shared like nexus-data.
	if err := platform.PrepareSystem(configDir); err != nil {
		return err
	}
	reg, err := LoadRegistry(configDir)
	if err != nil {
		return err
	}
	for i := range reg.Guilds {
		e := &reg.Guilds[i]
		if rel, err := filepath.Rel(old, e.DataDir); err == nil && !strings.HasPrefix(rel, "..") {
			e.DataDir = filepath.Join(configDir, rel)
		}
		platform.GiveTreeToGuildUser(e.DataDir)
		keepGuildPrivate(configDir, e)
		platform.ForgetOldService(e.GuildID)
		if e.StartOnBoot {
			if err := platform.SetBoot(configDir, e.GuildID, true); err != nil {
				fmt.Fprintf(out, "Could not set GUILD#%d to start on boot again: %v\n", e.Index, err)
			}
		}
	}
	if err := reg.Save(); err != nil {
		return err
	}
	platform.RemoveOldFiles()
	for _, id := range wasRunning {
		if e := reg.GetByID(id); e != nil {
			if err := startEntry(out, configDir, e); err != nil {
				fmt.Fprintf(out, "Could not start GUILD#%d again: %v\n", e.Index, err)
			}
		}
	}
	fmt.Fprintf(out, "Done. GuildForge's folder is %s.\n", configDir)
	return nil
}

// onlyEmptyFolders reports whether dir holds nothing but empty folders.
func onlyEmptyFolders(dir string) bool {
	empty := true
	filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			empty = false
			return fs.SkipAll
		}
		return nil
	})
	return empty
}

// keepGuildPrivate lets only the account that runs a guild change its
// folder and backups, inside the shared nexus-data.
func keepGuildPrivate(configDir string, e *Entry) {
	nexusdata.KeepPrivate(e.DataDir)
	if dir := backupDir(configDir, e.GuildID); fileExists(dir) {
		nexusdata.KeepPrivate(dir)
	}
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
