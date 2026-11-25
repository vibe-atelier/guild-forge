//go:build linux

package platform

// Linux (Debian) specifics:
//   - guild-forge commands must be run as administrator (sudo).
//   - Guilds run as their own system user, "guild-forge", which owns the guild
//     folders. Other users cannot read them.
//   - Each running guild is a systemd service: guild-forge@<guild ID>.service.
//     systemd starts and stops it, restarts it if it crashes, and starts it
//     at boot when "Start Guild On Boot" is on.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	// GuildUser is the system user that runs guilds and owns their files.
	GuildUser   = "guild-forge"
	systemDir   = "/var/lib/guild-forge"
	unitPath    = "/etc/systemd/system/guild-forge@.service"
	installPath = "/usr/local/bin/guild-forge"
)

// DefaultConfigDir is GuildForge's system folder.
func DefaultConfigDir() (string, error) { return systemDir, nil }

// RequireAdmin makes sure the command runs as root.
func RequireAdmin() error {
	if os.Geteuid() != 0 {
		return errors.New("guild-forge needs administrator rights on Linux. Put sudo in front, e.g.:  sudo guild-forge " + strings.Join(os.Args[1:], " "))
	}
	return nil
}

// guildUserIDs returns the uid and gid of the guild-forge user.
func guildUserIDs() (uid, gid int, err error) {
	u, err := user.Lookup(GuildUser)
	if err != nil {
		return 0, 0, err
	}
	uid, _ = strconv.Atoi(u.Uid)
	gid, _ = strconv.Atoi(u.Gid)
	return uid, gid, nil
}

// PrepareSystem creates the guild-forge user, guild-forge's folders and the
// systemd service template, if they are missing. Safe to run every time.
func PrepareSystem(configDir string) error {
	if os.Geteuid() != 0 {
		return nil // Only root can (and needs to) do this.
	}
	if _, err := user.Lookup(GuildUser); err != nil {
		useradd, lerr := exec.LookPath("useradd")
		if lerr != nil {
			useradd = "/usr/sbin/useradd"
		}
		out, err := exec.Command(useradd, "--system", "--user-group",
			"--home-dir", systemDir, "--no-create-home",
			"--shell", "/usr/sbin/nologin",
			"--comment", "Nexus guild host (GuildForge)", GuildUser).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot create the %s user: %v %s", GuildUser, err, out)
		}
	}
	_, gid, err := guildUserIDs()
	if err != nil {
		return err
	}
	// configDir and guilds/: root owns, guild-forge group may enter and read.
	for _, d := range []string{configDir, filepath.Join(configDir, "guilds")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return err
		}
		os.Chown(d, 0, gid)
		os.Chmod(d, 0o750)
	}
	// Only root may use backups/.
	if err := os.MkdirAll(filepath.Join(configDir, "backups"), 0o700); err != nil {
		return err
	}
	return writeUnit()
}

func writeUnit() error {
	exe := installPath
	if _, err := os.Stat(exe); err != nil {
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	unit := `# Created by guild-forge. One copy runs per started guild.
[Unit]
Description=Nexus guild %i
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=` + GuildUser + `
Group=` + GuildUser + `
ExecStart=` + exe + ` run=%i
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=read-only
PrivateTmp=true

[Install]
WantedBy=multi-user.target
`
	if old, err := os.ReadFile(unitPath); err == nil && string(old) == unit {
		return nil
	}
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", unitPath, err)
	}
	return systemctl("daemon-reload")
}

func unitName(id string) string { return "guild-forge@" + id + ".service" }

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s failed: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// giveToGuildUser makes the guild-forge user the owner of path.
func GiveToGuildUser(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	uid, gid, err := guildUserIDs()
	if err != nil {
		return nil // The user isn't created yet (for example, in tests), so there is nothing to do.
	}
	return os.Lchown(path, uid, gid)
}

// giveTreeToGuildUser makes the guild-forge user the owner of dir and
// everything inside it.
func GiveTreeToGuildUser(dir string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	return filepath.Walk(dir, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return GiveToGuildUser(p)
	})
}

// shareWithGuildUser lets the guild-forge group read path (owned by root).
func ShareWithGuildUser(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	_, gid, err := guildUserIDs()
	if err != nil {
		return nil
	}
	if err := os.Lchown(path, 0, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0o640)
}

// CheckFolderAccess makes sure the guild-forge user will be able to reach dir:
// every folder above it must let that user pass through.
func CheckFolderAccess(dir string) error {
	for _, private := range []string{"/home", "/root", "/run/user"} {
		if dir == private || strings.HasPrefix(dir, private+"/") {
			return fmt.Errorf("guild folders cannot be inside %s (personal folders are protected from the %s user). Choose a folder elsewhere, e.g. under %s/guilds", private, GuildUser, systemDir)
		}
	}
	uid, gid, err := guildUserIDs()
	if err != nil {
		return nil
	}
	for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		if err == nil {
			st := info.Sys().(*syscall.Stat_t)
			mode := info.Mode().Perm()
			ok := mode&0o001 != 0 ||
				(int(st.Uid) == uid && mode&0o100 != 0) ||
				(int(st.Gid) == gid && mode&0o010 != 0)
			if !ok {
				return fmt.Errorf("the %s user cannot open %s (it is private to its owner), so it cannot reach a guild inside it. Choose a folder elsewhere, e.g. under %s/guilds", GuildUser, p, systemDir)
			}
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}

// startService starts the guild as a systemd service.
func StartService(configDir, guildID string) error {
	return systemctl("start", unitName(guildID))
}

// stopService stops the guild's systemd service.
func StopService(id string) (handled bool, err error) {
	return true, systemctl("stop", unitName(id))
}

// setBoot turns "start at boot" on or off for the guild.
func SetBoot(configDir, id string, on bool) error {
	if on {
		return systemctl("enable", unitName(id))
	}
	return systemctl("disable", unitName(id))
}

// forgetService stops the guild and removes it from boot. Errors are ignored.
func ForgetService(id string) {
	if os.Geteuid() == 0 {
		exec.Command("systemctl", "disable", "--now", unitName(id)).Run()
	}
}

// BootNote explains what "on boot" means on this system.
const BootNote = "The guild will start whenever this computer starts."

// giveToInvokingUser hands a file made under sudo (e.g. an export) to the
// person who typed sudo, so they can move or copy it without sudo.
func GiveToInvokingUser(path string) {
	if os.Geteuid() != 0 {
		return
	}
	uid, err1 := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, err2 := strconv.Atoi(os.Getenv("SUDO_GID"))
	if err1 == nil && err2 == nil {
		os.Lchown(path, uid, gid)
	}
}

// OldConfigDirs are where guilds were kept before GuildForge: nexus-host's
// folder. Linux hosting keeps its system folder.
func OldConfigDirs() []string { return []string{"/var/lib/nexus-host"} }

// ForgetOldService removes nexus-host's service for a guild (and, once no
// guild uses it, the old service file and program).
func ForgetOldService(id string) {
	exec.Command("systemctl", "disable", "--now", "nexus-host@"+id+".service").Run()
}

// StopOldService stops a guild run by nexus-host's service.
func StopOldService(id string) bool {
	return exec.Command("systemctl", "stop", "nexus-host@"+id+".service").Run() == nil
}

// RemoveOldFiles removes nexus-host's service file and program.
func RemoveOldFiles() {
	os.Remove("/etc/systemd/system/nexus-host@.service")
	os.Remove("/usr/local/bin/nexus-host")
	exec.Command("systemctl", "daemon-reload").Run()
}

// SyncBoot has nothing to do on Linux: each guild has its own systemd
// service, turned on or off by SetBoot.
func SyncBoot(anyOn, claim bool) error { return nil }

// ForgetGuildTasks has nothing to do on Linux, which has no sign-in tasks.
func ForgetGuildTasks(ids []string) {}
