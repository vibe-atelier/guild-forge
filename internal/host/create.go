package host

import (
	"nexus/internal/fsutil"
	"nexus/internal/guildstore"
	"nexus/internal/platform"

	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nexus/internal/console"
)

// MinPasswordLength is the shortest guild password accepted.
const MinPasswordLength = 8

// Create runs "guild-forge create".
func Create(c *console.Console, configDir string) error {
	out := c.Out()
	reg, err := OpenRegistry(configDir, out)
	if err != nil {
		return err
	}
	index, ok := reg.NextIndex()
	if !ok {
		return fmt.Errorf("all %d guild slots are in use; delete one first with guild-forge delete=N", MaxGuilds)
	}
	fmt.Fprintf(out, "Creating GUILD#%d.\n\n", index)

	password, err := askNewPassword(c)
	if err != nil {
		return err
	}
	gd, err := guildstore.NewGuildData(password)
	if err != nil {
		return err
	}
	defaultDir := defaultGuildDir(configDir, gd.PublicKey)
	dataDir, err := AskGuildFolder(c, reg, defaultDir)
	if err != nil {
		return err
	}
	if err := gd.Save(dataDir); err != nil {
		return fmt.Errorf("cannot write to %s: %w", dataDir, err)
	}
	e, err := reg.Add(Entry{GuildID: gd.PublicKey, DataDir: dataDir})
	if err != nil {
		os.Remove(filepath.Join(dataDir, guildstore.GuildFileName))
		return err
	}
	if err := reg.Save(); err != nil {
		// Undo, so the folder is not left as a guild nobody knows about.
		os.Remove(filepath.Join(dataDir, guildstore.GuildFileName))
		return fmt.Errorf("cannot save the guild list: %w", err)
	}

	fmt.Fprintf(out, "\nGUILD#%d created.\n", e.Index)
	fmt.Fprintf(out, "  Folder:   %s\n", dataDir)
	fmt.Fprintf(out, "  Guild ID: %s\n", gd.PublicKey)
	fmt.Fprintf(out, "Next step: give it a port with  guild-forge setup=%d port=50000\n", e.Index)
	return nil
}

func askNewPassword(c *console.Console) (string, error) {
	out := c.Out()
	for {
		pw, err := c.AskPassword(fmt.Sprintf("Choose a password (at least %d characters, hidden while typing): ", MinPasswordLength))
		if err != nil {
			return "", err
		}
		if len([]rune(pw)) < MinPasswordLength {
			fmt.Fprintf(out, "Too short. Please use at least %d characters.\n", MinPasswordLength)
			continue
		}
		again, err := c.AskPassword("Type the password again: ")
		if err != nil {
			return "", err
		}
		if again != pw {
			fmt.Fprintln(out, "The two passwords are different. Please try again.")
			continue
		}
		return pw, nil
	}
}

// AskGuildFolder asks where to keep a guild's folder. The folder must
// be new or empty, because the guild will fill it. Pressing Enter picks
// defaultDir. The returned folder exists and belongs to the guild user.
func AskGuildFolder(c *console.Console, reg *Registry, defaultDir string) (string, error) {
	out := c.Out()
	for {
		fmt.Fprintf(out, "\nWhere should this guild's folder be?\n  Press Enter for: %s\n", defaultDir)
		ans, err := c.Ask("  or type a folder path (in double quotes if it has spaces): ")
		if err != nil {
			return "", err
		}
		dir := fsutil.CleanPathInput(ans)
		if dir == "" {
			dir = defaultDir
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			fmt.Fprintf(out, "That path is not valid: %v\n", err)
			continue
		}
		dir = abs

		if idx, used := reg.UsesDataDir(dir); used {
			fmt.Fprintf(out, "GUILD#%d already uses that folder. Please choose another.\n", idx)
			continue
		}
		if err := platform.CheckFolderAccess(dir); err != nil {
			fmt.Fprintln(out, err)
			continue
		}

		info, err := os.Stat(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if !fsutil.SamePath(dir, defaultDir) {
				yes, err := c.Confirm(fmt.Sprintf("%s does not exist. Create it?", dir), true)
				if err != nil {
					return "", err
				}
				if !yes {
					continue
				}
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				fmt.Fprintf(out, "Could not create the folder: %v\n", err)
				continue
			}
		case err != nil:
			fmt.Fprintf(out, "Cannot use that folder: %v\n", err)
			continue
		case !info.IsDir():
			fmt.Fprintln(out, "That is a file, not a folder. Please choose a folder.")
			continue
		case guildstore.HasGuildData(dir):
			fmt.Fprintln(out, "That folder already contains a Nexus guild. To add an existing guild folder, use  guild-forge import=FOLDER")
			continue
		default:
			if entries, _ := os.ReadDir(dir); len(entries) > 0 {
				fmt.Fprintln(out, "That folder is not empty. A guild needs a folder of its own; please choose a new or empty folder.")
				continue
			}
		}
		os.Chmod(dir, 0o700)
		if err := platform.GiveToGuildUser(dir); err != nil {
			fmt.Fprintf(out, "Could not hand the folder to the guild user: %v\n", err)
			continue
		}
		return dir, nil
	}
}

// defaultGuildDir suggests a folder for a guild that does not exist yet:
// <guild-forge folder>/guilds/guild-XXXXXXXX (the start of its ID), with -2,
// -3, ... added if that name is taken (e.g. by a deleted guild's kept folder).
func defaultGuildDir(configDir, id string) string {
	base := filepath.Join(configDir, "guilds", "guild-"+id[:8])
	dir := base
	for n := 2; ; n++ {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return dir
		}
		dir = fmt.Sprintf("%s-%d", base, n)
	}
}
