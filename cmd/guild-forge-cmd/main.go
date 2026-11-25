// guild-forge runs Nexus guilds (chat servers) on this computer, with
// commands typed in a terminal, on Windows and Linux alike. On
// Windows, GuildForge.exe (cmd/guild-forge-window) is the same with a window.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"nexus/internal/console"
	"nexus/internal/host"
	"nexus/internal/platform"
)

const usage = `guild-forge ` + host.Version + ` - run Nexus guilds on this computer (up to 8)

On Windows, GuildForge.exe does the same in a window.

Commands:
  guild-forge create              Create a new guild
  guild-forge list                Show all guilds and whether they are running
  guild-forge setup=N             Show guild N's settings
  guild-forge setup=N password    Change its password
  guild-forge setup=N port=50000  Set its port
  guild-forge setup=N boot=on     Start it with the computer (boot=off: don't)
  guild-forge setup=N broadcast   Broadcast to pair an owner, with a one-time
                                 password (broadcast=off: stop)
  guild-forge start=N             Start guild N
  guild-forge stop=N              Stop guild N
  guild-forge OTP                 One-time password for the broadcasting guild
  guild-forge export=N [FOLDER]   Save guild N as one encrypted .nexus file
  guild-forge import=PATH         Add a guild from a .nexus file or a guild folder
  guild-forge delete=N            Delete guild N
  guild-forge backup=N            Save a backup of guild N
  guild-forge fall-back=N         List guild N's backups
  guild-forge fall-back=N NAME    Restore guild N to the backup NAME
  guild-forge reset_owner=N       Remove guild N's owner so it can be paired again
  guild-forge update              Download and install a newer guild-forge, if there
                                 is one (running guilds restart on it)
  guild-forge help                Show this help

N is the guild number shown by "list". Put paths with spaces in double quotes.
On Linux, put sudo in front of every command.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// The copy set aside by the last update is no longer running.
	host.RemoveOldProgram()
	if len(args) == 0 {
		fmt.Print(usage)
		return 0
	}

	// Commands look like "create" or "setup=1": a word, optionally "=value".
	// The word is not case-sensitive.
	word, value, hasValue := strings.Cut(args[0], "=")
	word = strings.ToLower(word)
	extra := args[1:]

	switch word {
	case "help", "-h", "--help", "/?":
		fmt.Print(usage)
		return 0
	case "version", "--version":
		fmt.Println("guild-forge", host.Version)
		return 0
	}

	configDir, err := host.ConfigDir()
	if err != nil {
		return fail(err)
	}

	// This runs the guild process itself (started by "start" or at boot, not by people).
	if word == "run" {
		return fail(host.Run(configDir, value))
	}
	// This starts the guilds set to start on boot; Windows' sign-in task runs it, not people.
	if word == "boot" {
		return fail(host.StartBootGuilds(configDir, os.Stdout))
	}

	if !isCommand(word) {
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\n", args[0])
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err := platform.RequireAdmin(); err != nil {
		return fail(err)
	}
	// Update only swaps the program file, so it runs before the system is
	// set up for guilds and adds nothing else to the computer.
	if word == "update" {
		if hasValue || len(extra) > 0 {
			return fail(errors.New(`"update" takes nothing after it; just type: guild-forge update`))
		}
		return fail(host.Update(os.Stdout, configDir, host.CommandFile(), "guild-forge"))
	}
	if err := platform.PrepareSystem(configDir); err != nil {
		return fail(err)
	}
	if err := host.MoveOldGuilds(configDir, os.Stdout); err != nil {
		return fail(err)
	}
	// A guild-forge moved to another folder points the sign-in task at
	// itself again.
	if err := host.CheckBoot(configDir); err != nil {
		fmt.Fprintln(os.Stderr, "Start on Boot:", err)
	}
	c := console.New()
	var out io.Writer = os.Stdout
	c.SetOut(out)

	// Commands that take nothing.
	noArgs := func() error {
		if hasValue || len(extra) > 0 {
			return fmt.Errorf("%q takes nothing after it; just type: guild-forge %s", word, word)
		}
		return nil
	}
	// Commands that take a guild number: word=N.
	index := func(maxExtra int) (int, error) {
		if !hasValue {
			return 0, fmt.Errorf("type the guild number after %q, e.g. guild-forge %s=1", word+"=", word)
		}
		if len(extra) > maxExtra {
			return 0, fmt.Errorf("too many things after %s=%s (paths with spaces need double quotes)", word, value)
		}
		return host.ParseIndex(value)
	}
	firstExtra := func() string {
		if len(extra) > 0 {
			return extra[0]
		}
		return ""
	}

	switch word {
	case "create":
		if err := noArgs(); err != nil {
			return fail(err)
		}
		return fail(host.Create(c, configDir))
	case "list":
		if err := noArgs(); err != nil {
			return fail(err)
		}
		return fail(host.List(configDir, out))
	case "otp":
		if err := noArgs(); err != nil {
			return fail(err)
		}
		return fail(host.OTP(out, configDir))
	case "import":
		if !hasValue || value == "" {
			return fail(errors.New(`type the file after "import=", e.g. guild-forge import="GUILD1-2026-11-15.nexus"`))
		}
		if len(extra) > 0 {
			return fail(errors.New(`paths with spaces need double quotes, e.g. guild-forge import="C:\My Files\GUILD1.nexus"`))
		}
		return fail(host.Import(c, configDir, value))
	}

	maxExtra := 0
	if word == "export" || word == "fall-back" || word == "setup" {
		maxExtra = 1
	}
	n, err := index(maxExtra)
	if err != nil {
		return fail(err)
	}
	switch word {
	case "setup":
		return fail(host.Setup(c, configDir, n, firstExtra()))
	case "start":
		return fail(host.Start(out, configDir, n))
	case "stop":
		return fail(host.Stop(out, configDir, n))
	case "export":
		return fail(host.Export(c, configDir, n, firstExtra()))
	case "delete":
		return fail(host.Delete(c, configDir, n))
	case "backup":
		return fail(host.Backup(c, configDir, n))
	case "fall-back":
		return fail(host.FallBack(c, configDir, n, firstExtra()))
	case "reset_owner":
		return fail(host.ResetOwner(c, configDir, n))
	}
	return 2
}

func isCommand(word string) bool {
	switch word {
	case "create", "list", "setup", "start", "stop", "otp", "export", "import",
		"delete", "backup", "fall-back", "reset_owner", "update":
		return true
	}
	return false
}

// fail prints err (if any) and returns the exit code: 0 for success, 1 for an error.
func fail(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, host.ErrWrongPassword) {
		fmt.Fprintln(os.Stderr, "Wrong password too many times. Nothing was changed.")
		return 1
	}
	if errors.Is(err, console.ErrNoInput) {
		fmt.Fprintln(os.Stderr, "\nCancelled.")
		return 1
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	return 1
}
