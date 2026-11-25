//go:build !linux && !windows

package platform

import (
	"errors"
	"os"

	"nexus/internal/nexusdata"
)

var errUnsupported = errors.New("this is not supported on this system yet (only Windows and Linux)")

// DefaultConfigDir is GuildForge's part of nexus-data.
func DefaultConfigDir() (string, error) { return nexusdata.Part(nexusdata.ForgePart) }

// On other systems guilds run as the signed-in user, by hand: there is no
// system user to hand files to, and no background service or boot entry.
func RequireAdmin() error                  { return nil }
func PrepareSystem(configDir string) error { return os.MkdirAll(configDir, 0o700) }
func GiveToGuildUser(path string) error    { return nil }
func GiveTreeToGuildUser(dir string) error { return nil }
func ShareWithGuildUser(path string) error { return nil }
func CheckFolderAccess(dir string) error   { return nil }
func StartService(string, string) error    { return errUnsupported }
func StopService(string) (bool, error)     { return false, nil }
func SetBoot(string, string, bool) error   { return errUnsupported }
func ForgetService(string)                 {}
func GiveToInvokingUser(string)            {}

const BootNote = ""

// There is nothing to move from nexus-host here.
func OldConfigDirs() []string    { return nil }
func ForgetOldService(string)    {}
func StopOldService(string) bool { return false }
func RemoveOldFiles()            {}

// SyncBoot has nothing to do here: starting on boot isn't supported yet.
func SyncBoot(anyOn, claim bool) error { return nil }

// ForgetGuildTasks has nothing to do here.
func ForgetGuildTasks(ids []string) {}
