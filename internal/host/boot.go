package host

// Start on Boot. Which guilds start on boot is kept in nexus-data,
// in the guild list. On Windows, GuildForge's one sign-in task runs
// "GuildForge.exe boot" or "guild-forge-cmd.exe boot", which starts
// them; on Linux each guild has its own systemd service instead.

import (
	"fmt"
	"io"

	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/platform"
)

// StartBootGuilds runs "guild-forge boot": it starts every guild set to
// start on boot that isn't running yet. Guilds this account can't reach
// (another account's, on a shared PC) are left alone.
func StartBootGuilds(configDir string, out io.Writer) error {
	reg, err := OpenRegistry(configDir, io.Discard)
	if err != nil {
		return err
	}
	for i := range reg.Guilds {
		e := &reg.Guilds[i]
		if !e.StartOnBoot || e.Port == 0 || !guildstore.HasGuildData(e.DataDir) {
			continue
		}
		if _, running := guild.RunningStatus(e.DataDir); running {
			continue
		}
		if err := startEntry(out, configDir, e); err != nil {
			fmt.Fprintf(out, "Could not start GUILD#%d: %v\n", e.Index, err)
		}
	}
	return nil
}

// CheckBoot makes the system's sign-in task match nexus-data when GuildForge
// starts: a missing task is made, and one whose program was moved or deleted
// is pointed at this program.
func CheckBoot(configDir string) error {
	reg, err := LoadRegistry(configDir)
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range reg.Guilds {
		ids = append(ids, e.GuildID)
	}
	platform.ForgetGuildTasks(ids) // Earlier versions had one task per guild.
	return platform.SyncBoot(reg.anyStartsOnBoot(), false)
}
