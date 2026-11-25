package host

import (
	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/platform"

	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nexus/internal/console"
)

// DisplayName is how a guild is shown in "guild-forge list":
// GUILD#1, GUILD#1@50000, or GUILD#1@50000(ADMIN:<owner key>).
func DisplayName(index, port int, ownerKey string) string {
	name := fmt.Sprintf("GUILD#%d", index)
	if port == 0 {
		return name
	}
	name += fmt.Sprintf("@%d", port)
	if ownerKey != "" {
		name += "(ADMIN:" + ownerKey + ")"
	}
	return name
}

// ParseIndex turns the text after "=" (as in setup=1) into a guild number.
func ParseIndex(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > MaxGuilds {
		return 0, fmt.Errorf("%q is not a guild number; use 1 to %d", s, MaxGuilds)
	}
	return n, nil
}

// openGuild opens the registry and finds guild index.
func openGuild(configDir string, out io.Writer, index int) (*Registry, *Entry, error) {
	reg, err := OpenRegistry(configDir, out)
	if err != nil {
		return nil, nil, err
	}
	e := reg.Get(index)
	if e == nil {
		if len(reg.Guilds) == 0 {
			return nil, nil, fmt.Errorf("there is no GUILD#%d; there are no guilds yet (create one with guild-forge create)", index)
		}
		return nil, nil, fmt.Errorf("there is no GUILD#%d; the guilds are GUILD#1 to GUILD#%d (see guild-forge list)", index, len(reg.Guilds))
	}
	return reg, e, nil
}

// List runs "guild-forge list".
func List(configDir string, out io.Writer) error {
	reg, err := OpenRegistry(configDir, out)
	if err != nil {
		return err
	}
	if len(reg.Guilds) == 0 {
		fmt.Fprintln(out, "No guilds yet. Create one with:  guild-forge create")
		return nil
	}
	for _, e := range reg.Guilds {
		state := "stopped"
		if s, running := guild.RunningStatus(e.DataDir); running {
			state = "running"
			if s.Broadcasting {
				state = "broadcasting until " + s.BroadcastEnds.Local().Format("15:04")
			}
		}
		if e.StartOnBoot {
			state += ", starts on boot"
		}
		fmt.Fprintf(out, "%s  [%s]\n", e.Name(guildstore.OwnerKey(e.DataDir)), state)
	}
	return nil
}

// Start runs "guild-forge start=N".
func Start(out io.Writer, configDir string, index int) error {
	_, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	return startEntry(out, configDir, e)
}

func startEntry(out io.Writer, configDir string, e *Entry) error {
	name := e.Name(guildstore.OwnerKey(e.DataDir))
	if e.Port == 0 {
		return fmt.Errorf("GUILD#%d has no port yet; give it one with  guild-forge setup=%d port=50000", e.Index, e.Index)
	}
	if _, running := guild.RunningStatus(e.DataDir); running {
		fmt.Fprintf(out, "%s is already running.\n", name)
		return nil
	}
	if PortBusy(e.Port) {
		return fmt.Errorf("port %d is being used by another program; choose another with  guild-forge setup=%d port=NUMBER", e.Port, e.Index)
	}
	if err := platform.StartService(configDir, e.GuildID); err != nil {
		return fmt.Errorf("could not start %s: %w", name, err)
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if _, running := guild.RunningStatus(e.DataDir); running {
			fmt.Fprintf(out, "%s is running.\n", name)
			return nil
		}
	}
	return fmt.Errorf("%s did not start. Last lines of its log:\n%s", name, logTail(e.DataDir, 5))
}

// Stop runs "guild-forge stop=N".
func Stop(out io.Writer, configDir string, index int) error {
	_, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	return stopEntry(out, e)
}

func stopEntry(out io.Writer, e *Entry) error {
	name := e.Name(guildstore.OwnerKey(e.DataDir))
	if _, running := guild.RunningStatus(e.DataDir); !running {
		fmt.Fprintf(out, "%s is not running.\n", name)
		return nil
	}
	handled, err := platform.StopService(e.GuildID)
	if err != nil {
		return err
	}
	if !handled {
		if _, err := guild.Call(e.DataDir, "stop"); err != nil && !errors.Is(err, guild.ErrNotRunning) {
			return err
		}
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if _, running := guild.RunningStatus(e.DataDir); !running {
			fmt.Fprintf(out, "%s stopped.\n", name)
			return nil
		}
	}
	return fmt.Errorf("%s did not stop within 10 seconds", name)
}

// OTP runs "guild-forge OTP": a one-time password for the guild that
// is broadcasting.
func OTP(out io.Writer, configDir string) error {
	reg, err := OpenRegistry(configDir, out)
	if err != nil {
		return err
	}
	for _, e := range reg.Guilds {
		s, running := guild.RunningStatus(e.DataDir)
		if !running || !s.Broadcasting {
			continue
		}
		r, err := guild.Call(e.DataDir, "otp")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "One-time password for %s:\n\n    %s\n\n", e.Name(""), guild.FormatOTP(r.OTP))
		fmt.Fprintf(out, "Valid for %d minutes (until %s). Making a new one cancels this one.\n",
			int(time.Until(r.OTPExpires).Round(time.Minute).Minutes()), r.OTPExpires.Local().Format("15:04"))
		printConnectHint(out, e.Port)
		return nil
	}
	return errors.New("no guild is broadcasting. Start one with  guild-forge setup=N broadcast")
}

func printConnectHint(out io.Writer, port int) {
	addrs := LocalAddresses()
	if len(addrs) == 0 {
		return
	}
	fmt.Fprintln(out, "In the client, enter this computer's address and port, e.g.:")
	for _, a := range addrs {
		fmt.Fprintf(out, "    %s:%d\n", a, port)
	}
}

// logTail returns the last n lines of a guild's log.
func logTail(dataDir string, n int) string {
	f, err := os.Open(filepath.Join(dataDir, guildstore.LogsDir, "guild.log"))
	if err != nil {
		return "  (no log yet)"
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, "  "+sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if len(lines) == 0 {
		return "  (log is empty)"
	}
	return strings.Join(lines, "\n")
}

// ResetOwner runs "guild-forge reset_owner=N": the guild loses its
// owner, so it can be broadcast and paired again. Members and messages stay.
func ResetOwner(c *console.Console, configDir string, index int) error {
	out := c.Out()
	_, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	owner := guildstore.OwnerKey(e.DataDir)
	if owner == "" {
		fmt.Fprintf(out, "%s has no owner. To pair one:  guild-forge setup=%d broadcast\n", e.Name(""), index)
		return nil
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	if err := askCurrentPassword(c, gd, index, e.DataDir); err != nil {
		return err
	}
	fmt.Fprintf(out, "Current owner (ADMIN): %s\n", owner)
	yes, err := c.Confirm("Remove this owner? The guild will have no owner until it is broadcast and paired again.", false)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintln(out, "Nothing changed.")
		return nil
	}
	if _, running := guild.RunningStatus(e.DataDir); running {
		if _, err := guild.Call(e.DataDir, "reset-owner"); err != nil {
			return err
		}
	} else if err := guildstore.RemoveOwner(e.DataDir); err != nil {
		return err
	}
	fmt.Fprintf(out, "Owner removed. %s now has no owner.\n", e.Name(""))
	fmt.Fprintf(out, "To pair a new owner:  guild-forge setup=%d broadcast\n", index)
	return nil
}
