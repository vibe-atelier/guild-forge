//go:build windows

package platform

// These are the Windows specifics:
//   - Guilds run as the signed-in user (no separate user yet), in the
//     background without a window.
//   - "Start Guild On Boot" adds a Task Scheduler task that starts the guild
//     when this user signs in.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"nexus/internal/nexusdata"
)

// DefaultConfigDir is GuildForge's part of this PC's nexus-data.
func DefaultConfigDir() (string, error) {
	dir, err := nexusdata.Part(nexusdata.ForgePart)
	if err != nil {
		return "", fmt.Errorf("cannot make nexus-data: %w", err)
	}
	return dir, nil
}

// RequireAdmin isn't needed on Windows, because guilds belong to the signed-in user.
func RequireAdmin() error { return nil }

// PrepareSystem creates GuildForge's folders.
func PrepareSystem(configDir string) error {
	for _, d := range []string{configDir, filepath.Join(configDir, "guilds"), filepath.Join(configDir, "backups")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// On Windows guilds run as the signed-in user, so a guild's folder is handed
// to that account by making it private to it: other accounts on the PC can
// see nexus-data, but not change a guild they didn't make. Linux has
// a separate guild user instead.
func GiveToGuildUser(path string) error    { nexusdata.KeepPrivate(path); return nil }
func GiveTreeToGuildUser(dir string) error { nexusdata.KeepPrivate(dir); return nil }

// ShareWithGuildUser has nothing to do on Windows: the guild runs as you.
func ShareWithGuildUser(path string) error { return nil }

// CheckFolderAccess has nothing to check: the guild runs as the same user, so any folder works.
func CheckFolderAccess(dir string) error { return nil }

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// StartService starts the guild as a background process of this program
// (run=ID): GuildForge.exe or guild-forge-cmd.exe, whichever asked.
func StartService(configDir, guildID string) error {
	exe, err := ProgramPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "run="+guildID)
	cmd.Env = append(os.Environ(), "GUILD_FORGE_HOME="+configDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup | createNoWindow,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// StopService does nothing here: on Windows the guild is stopped through its control channel.
func StopService(id string) (handled bool, err error) { return false, nil }

// oldTaskName is the per-guild sign-in task of earlier versions, before
// Start on Boot moved into nexus-data.
func oldTaskName(id string) string { return `Nexus\guild-` + id }

// bootTask is GuildForge's one sign-in task.
const bootTask = `Nexus\GuildForge`

// SetBoot records nothing on Windows: whether a guild starts on boot is kept in
// nexus-data, and SyncBoot keeps GuildForge's one sign-in task in step. It only
// removes the guild's task from earlier versions.
func SetBoot(configDir, id string, on bool) error {
	if on && os.Getenv("GUILD_FORGE_HOME") != "" {
		return errors.New("start on sign-in is not available while GUILD_FORGE_HOME is set (test mode)")
	}
	ForgetService(id)
	return nil
}

// SyncBoot makes GuildForge's sign-in task match nexus-data: when any guild
// starts on boot, the task runs "<program> boot"; when none does, there is no
// task. GuildForge.exe and guild-forge-cmd.exe each work on their own, so the
// task names whichever one last changed a setting (claim is true then). At
// start-up claim is false: the task is only pointed at this program when it is
// missing, or its program file is gone (moved or deleted).
func SyncBoot(anyOn, claim bool) error {
	if os.Getenv("GUILD_FORGE_HOME") != "" {
		return nil // Tests and pictures never touch Windows' tasks.
	}
	if !anyOn {
		exec.Command("schtasks", "/Delete", "/TN", bootTask, "/F").Run()
		return nil
	}
	exe, err := ProgramPath()
	if err != nil {
		return err
	}
	if xml, err := exec.Command("schtasks", "/Query", "/TN", bootTask, "/XML").Output(); err == nil {
		current := taskProgram(string(xml))
		if strings.EqualFold(current, exe) {
			return nil // The task already runs this program.
		}
		if !claim && current != "" {
			if _, err := os.Stat(current); err == nil {
				return nil // The other GuildForge program has it, and is still there.
			}
		}
	}
	// conhost --headless runs the program without showing a window.
	action := fmt.Sprintf(`conhost.exe --headless "%s" boot`, exe)
	out, err := exec.Command("schtasks", "/Create", "/TN", bootTask, "/TR", action, "/SC", "ONLOGON", "/F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not set up the sign-in task: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// taskProgram finds the program file in the sign-in task's description: the
// part in quotes after "--headless". It is "" when there is none.
func taskProgram(xml string) string {
	xml = strings.ReplaceAll(xml, "&quot;", `"`)
	_, rest, ok := strings.Cut(xml, `--headless "`)
	if !ok {
		return ""
	}
	path, _, _ := strings.Cut(rest, `"`)
	return path
}

// ForgetService removes a guild's sign-in task from earlier versions, if
// there is one. Errors are ignored.
func ForgetService(id string) {
	exec.Command("schtasks", "/Delete", "/TN", oldTaskName(id), "/F").Run()
}

// BootNote explains what "on boot" means on this system.
const BootNote = "On Windows the guild will start whenever you sign in."

// GiveToInvokingUser has nothing to do on Windows (files already belong to you).
func GiveToInvokingUser(path string) {}

// OldConfigDirs are where guilds were kept before, oldest first: nexus-host's
// folder, then GuildForge's own folder in %APPDATA% from before
// nexus-data.
func OldConfigDirs() []string {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(base, "nexus-host"), filepath.Join(base, "GuildForge")}
}

// ForgetOldService removes nexus-host's startup task for a guild.
func ForgetOldService(id string) { ForgetService(id) }

// StopOldService does nothing on Windows: old guilds are stopped through their control channel.
func StopOldService(id string) bool { return false }

// RemoveOldFiles has nothing to remove here.
func RemoveOldFiles() {}

// ForgetGuildTasks removes the per-guild sign-in tasks of earlier versions, now
// that one task starts every guild. It asks Windows for its task list once, and
// deletes only the tasks that are there.
func ForgetGuildTasks(ids []string) {
	if os.Getenv("GUILD_FORGE_HOME") != "" || len(ids) == 0 {
		return
	}
	out, err := exec.Command("schtasks", "/Query", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return
	}
	list := strings.ToLower(string(out))
	for _, id := range ids {
		if strings.Contains(list, strings.ToLower(`\`+oldTaskName(id)+`"`)) {
			ForgetService(id)
		}
	}
}
