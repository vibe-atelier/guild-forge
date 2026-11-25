package host

import (
	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/platform"
	"nexus/internal/portmap"

	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"nexus/internal/console"
)

// These are the port rules: only the never-registered ports are used.
const (
	MinPort = 49152
	MaxPort = 65535
)

// MaxPasswordTries is how many wrong passwords are accepted before giving up.
const MaxPasswordTries = 3

// ErrWrongPassword is returned when the password was wrong too many times.
var ErrWrongPassword = errors.New("wrong password")

// Setup runs "guild-forge setup=N ACTION": no menu, one direct
// command each.
//
//	setup=N                 shows the guild's settings and these commands
//	setup=N password        changes the password
//	setup=N port=50000      sets the port (a running guild restarts on it)
//	setup=N boot=on|off     starts the guild with the computer, or not
//	setup=N router=on|off   asks the router to open the port, or not
//	setup=N backups=daily  automatic backups: daily, weekly, monthly or off
//	setup=N broadcast       broadcasts for pairing, and shows a one-time password
//	setup=N broadcast=off   stops broadcasting
//
// Everything but showing asks for the guild's password first.
func Setup(c *console.Console, configDir string, index int, action string) error {
	out := c.Out()
	reg, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	word, value, _ := strings.Cut(strings.TrimSpace(action), "=")
	word, value = strings.ToLower(word), strings.ToLower(strings.TrimSpace(value))
	if word == "" {
		showSetup(out, configDir, e, index)
		return nil
	}
	switch word {
	case "password", "port", "boot", "broadcast", "router", "backups":
	default:
		return fmt.Errorf("%q is not a setup command; see  guild-forge setup=%d", action, index)
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return fmt.Errorf("cannot read GUILD#%d's folder %s: %w", index, e.DataDir, err)
	}
	if err := askCurrentPassword(c, gd, index, e.DataDir); err != nil {
		return err
	}

	switch word {
	case "password":
		pw, err := askNewPassword(c)
		if err != nil {
			return err
		}
		gd.PasswordHash = guildstore.HashPassword(pw)
		if err := gd.Save(e.DataDir); err != nil {
			return fmt.Errorf("cannot save the new password: %w", err)
		}
		fmt.Fprintln(out, "Password changed.")

	case "port":
		port, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("type the port after \"port=\", e.g. guild-forge setup=%d port=50000", index)
		}
		_, wasRunning := guild.RunningStatus(e.DataDir)
		if err := SetGuildPort(configDir, e.GuildID, port); err != nil {
			return err
		}
		reg, _ = OpenRegistry(configDir, io.Discard)
		e = reg.Get(index)
		fmt.Fprintf(out, "Port set. This guild is now %s.\n", e.Name(guildstore.OwnerKey(e.DataDir)))
		if wasRunning {
			fmt.Fprintln(out, "It was restarted on the new port.")
		}

	case "boot":
		if value != "on" && value != "off" {
			return fmt.Errorf("type on or off, e.g. guild-forge setup=%d boot=on", index)
		}
		if err := SetGuildBoot(configDir, e.GuildID, value == "on"); err != nil {
			return fmt.Errorf("could not change it: %w", err)
		}
		if value == "on" {
			fmt.Fprintln(out, "Start on boot is ON.", platform.BootNote)
			if _, running := guild.RunningStatus(e.DataDir); !running {
				fmt.Fprintf(out, "(It is not running right now; start it with guild-forge start=%d)\n", index)
			}
		} else {
			fmt.Fprintln(out, "Start on boot is OFF.")
		}

	case "backups":
		if err := SetAutoBackups(configDir, e.GuildID, value); err != nil {
			return fmt.Errorf("type daily, weekly, monthly or off, e.g. guild-forge setup=%d backups=daily", index)
		}
		if value == "off" || value == "" {
			fmt.Fprintln(out, "Automatic backups are OFF.")
		} else {
			fmt.Fprintf(out, "Automatic backups: %s, while the guild runs. The last %d are kept.\n", AutoBackupName(value), KeepAutoBackups)
		}

	case "router":
		if value != "on" && value != "off" {
			return fmt.Errorf("type on or off, e.g. guild-forge setup=%d router=on", index)
		}
		if err := SetAutoPort(configDir, e.GuildID, value == "on"); err != nil {
			return fmt.Errorf("could not change it: %w", err)
		}
		if value == "on" {
			fmt.Fprintln(out, "Port forwarding is ON: the router is asked while the guild runs.")
		} else {
			fmt.Fprintln(out, "Port forwarding is OFF. Forward the port by hand for people on the internet.")
		}

	case "broadcast":
		if value == "off" {
			if s, running := guild.RunningStatus(e.DataDir); !running || !s.Broadcasting {
				fmt.Fprintf(out, "%s is not broadcasting.\n", e.Name(""))
				return nil
			}
			if err := Broadcast(configDir, e.GuildID, false); err != nil {
				return err
			}
			fmt.Fprintln(out, "Broadcast stopped.")
			return nil
		}
		if value != "" && value != "on" {
			return fmt.Errorf("type guild-forge setup=%d broadcast, or broadcast=off to stop", index)
		}
		if err := Broadcast(configDir, e.GuildID, true); err != nil {
			return err
		}
		s, _ := guild.RunningStatus(e.DataDir)
		fmt.Fprintf(out, "%s is broadcasting until %s (1 hour).\n\n", e.Name(""), s.BroadcastEnds.Local().Format("15:04"))
		return OTP(out, configDir)
	}
	return nil
}

// showSetup prints a guild's settings and the setup commands.
func showSetup(out io.Writer, configDir string, e *Entry, index int) {
	state := "stopped"
	s, running := guild.RunningStatus(e.DataDir)
	if running {
		state = "running"
		if s.Broadcasting {
			state = "broadcasting until " + s.BroadcastEnds.Local().Format("15:04")
		}
	}
	port, boot, router := "none yet", "off", "on"
	if e.Port != 0 {
		port = strconv.Itoa(e.Port)
	}
	if e.StartOnBoot {
		boot = "on"
	}
	if e.NoAutoPort {
		router = "off"
	}
	fmt.Fprintf(out, "%s  [%s]\n", e.Name(guildstore.OwnerKey(e.DataDir)), state)
	fmt.Fprintf(out, "  Port:            %s\n  Start on boot:   %s\n  Port forwarding: %s (the router is asked)\n  Backups:         %s (automatic)\n  Folder:          %s\n\n", port, boot, router, AutoBackupName(e.AutoBackups), e.DataDir)
	if running && s.Public != nil && e.Port != 0 {
		showAddresses(out, *s.Public, e.Port)
	}
	fmt.Fprintf(out, "To change it:\n")
	fmt.Fprintf(out, "  guild-forge setup=%d password        change the password\n", index)
	fmt.Fprintf(out, "  guild-forge setup=%d port=50000      set the port\n", index)
	fmt.Fprintf(out, "  guild-forge setup=%d boot=on         start it with the computer (boot=off to stop)\n", index)
	fmt.Fprintf(out, "  guild-forge setup=%d router=on       ask the router to open the port (router=off to stop)\n", index)
	fmt.Fprintf(out, "  guild-forge setup=%d backups=daily   automatic backups: daily, weekly, monthly or off\n", index)
	fmt.Fprintf(out, "  guild-forge setup=%d broadcast       broadcast for pairing an owner (broadcast=off to stop)\n", index)
}

func askCurrentPassword(c *console.Console, gd *guildstore.GuildData, index int, dir string) error {
	_, err := askPasswordReturning(c, gd, index, dir)
	return err
}

// PortBusy reports whether some program on this computer already uses port.
// It only checks this computer (127.0.0.1), so it never opens anything to the
// network and never triggers a firewall question.
func PortBusy(port int) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
		conn.Close()
		return true
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	l.Close()
	return false
}

// showAddresses prints where people can reach a running guild.
func showAddresses(out io.Writer, st portmap.Status, port int) {
	hp := func(a string) string { return net.JoinHostPort(a, strconv.Itoa(port)) }
	fmt.Fprintln(out, "Addresses:")
	for _, a := range st.Local {
		fmt.Fprintf(out, "  Home network:    %s\n", hp(a))
	}
	switch {
	case st.External != "" && st.CGNAT:
		fmt.Fprintln(out, "  Internet (IPv4): none; your provider shares one address among many homes (CGNAT)")
	case st.External != "" && st.Open:
		fmt.Fprintf(out, "  Internet (IPv4): %s (the router opened the port)\n", hp(st.External))
	case st.External != "":
		fmt.Fprintf(out, "  Internet (IPv4): %s (forward the port by hand for it to work)\n", hp(st.External))
	default:
		fmt.Fprintln(out, "  Internet (IPv4): unknown; the router didn't answer (forward the port by hand)")
	}
	if len(st.IPv6) == 0 {
		fmt.Fprintln(out, "  Internet (IPv6): none on this computer")
	}
	for _, a := range st.IPv6 {
		fmt.Fprintf(out, "  Internet (IPv6): %s\n", hp(a))
	}
	fmt.Fprintln(out)
}
