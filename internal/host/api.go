package host

// This file does the same work as the commands, without any typing in a terminal, for
// GuildForge's window. Every function takes what the
// window asked for, and returns an error worded for a person, not a command.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"nexus/internal/fsutil"
	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/platform"
	"nexus/internal/portmap"
)

// GuildInfo is one guild as the window shows it.
type GuildInfo struct {
	Entry
	Owner         string // It is the owner's key, or "" when there is none.
	Running       bool
	Broadcasting  bool
	BroadcastEnds time.Time
	Members       int             // It is -1 when the guild is not running.
	Online        int             // It counts the members connected now.
	Public        *portmap.Status // It holds the router and the addresses while the guild runs.
	GuildName     string          // It is the guild's name, as members see it.
}

// Guilds lists the guilds on this computer, with their state.
func Guilds(configDir string) ([]GuildInfo, error) {
	reg, err := OpenRegistry(configDir, io.Discard)
	if err != nil {
		return nil, err
	}
	var out []GuildInfo
	for _, e := range reg.Guilds {
		g := GuildInfo{Entry: e, Owner: guildstore.OwnerKey(e.DataDir), Members: -1}
		if p, _, err := guildstore.LoadProfile(e.DataDir); err == nil {
			g.GuildName = p.Name
		}
		if s, running := guild.RunningStatus(e.DataDir); running {
			g.Running, g.Broadcasting, g.BroadcastEnds = true, s.Broadcasting, s.BroadcastEnds
			g.Members, g.Online = s.Members, s.Online
			g.Public = s.Public
		}
		out = append(out, g)
	}
	return out, nil
}

// CheckNewPassword checks a new guild password.
func CheckNewPassword(pw, again string) error {
	switch {
	case len([]rune(pw)) < MinPasswordLength:
		return fmt.Errorf("the password must be at least %d characters", MinPasswordLength)
	case pw != again:
		return errors.New("the two passwords are different")
	}
	return nil
}

// prepareFolder checks a folder for a new guild and makes it: it
// must be new or empty. dir "" means the default.
func prepareFolder(reg *Registry, dir, defaultDir string) (string, error) {
	dir = fsutil.CleanPathInput(dir)
	if dir == "" {
		dir = defaultDir
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("that folder path is not valid: %w", err)
	}
	dir = abs
	if idx, used := reg.UsesDataDir(dir); used {
		return "", fmt.Errorf("GUILD#%d already uses that folder", idx)
	}
	if err := platform.CheckFolderAccess(dir); err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("could not make the folder: %w", err)
		}
	case err != nil:
		return "", fmt.Errorf("cannot use that folder: %w", err)
	case !info.IsDir():
		return "", errors.New("that is a file, not a folder")
	case guildstore.HasGuildData(dir):
		return "", errors.New("that folder already holds a guild. To add it, use Import Guild")
	default:
		if entries, _ := os.ReadDir(dir); len(entries) > 0 {
			return "", errors.New("that folder is not empty. A guild needs a new or empty folder")
		}
	}
	os.Chmod(dir, 0o700)
	if err := platform.GiveToGuildUser(dir); err != nil {
		return "", fmt.Errorf("could not hand the folder to the guild: %w", err)
	}
	return dir, nil
}

// CreateGuild makes a new guild with a password and a port. folder "" puts
// its files in GuildForge's own folder.
func CreateGuild(configDir, password, folder string, port int) (*Entry, error) {
	reg, err := OpenRegistry(configDir, io.Discard)
	if err != nil {
		return nil, err
	}
	if _, ok := reg.NextIndex(); !ok {
		return nil, fmt.Errorf("all %d guild slots are in use. Delete one first", MaxGuilds)
	}
	if err := checkPort(reg, 0, port); err != nil {
		return nil, err
	}
	gd, err := guildstore.NewGuildData(password)
	if err != nil {
		return nil, err
	}
	if folder != "" {
		folder = filepath.Join(fsutil.CleanPathInput(folder), "guild-"+gd.PublicKey[:8])
	}
	dir, err := prepareFolder(reg, folder, defaultGuildDir(configDir, gd.PublicKey))
	if err != nil {
		return nil, err
	}
	if err := gd.Save(dir); err != nil {
		return nil, fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	e, err := reg.Add(Entry{GuildID: gd.PublicKey, DataDir: dir, Port: port})
	if err == nil {
		err = reg.Save()
	}
	if err != nil {
		os.Remove(filepath.Join(dir, guildstore.GuildFileName))
		return nil, err
	}
	return e, nil
}

// checkPort checks a port for guild index (0 = a new guild).
func checkPort(reg *Registry, index, port int) error {
	if port < MinPort || port > MaxPort {
		return fmt.Errorf("the port must be a number from %d to %d", MinPort, MaxPort)
	}
	if other, used := reg.PortUsedBy(port); used && other != index {
		return fmt.Errorf("GUILD#%d already uses port %d", other, port)
	}
	if cur := reg.Get(index); cur != nil && cur.Port == port {
		return nil
	}
	if PortBusy(port) {
		return fmt.Errorf("another program on this computer is using port %d", port)
	}
	return nil
}

// SuggestPort is a free port for a new guild, from 50000 up.
func SuggestPort(configDir string) int {
	reg, _ := OpenRegistry(configDir, io.Discard)
	for p := 50000; p < MaxPort; p++ {
		if reg != nil {
			if _, used := reg.PortUsedBy(p); used {
				continue
			}
		}
		if !PortBusy(p) {
			return p
		}
	}
	return 50000
}

// entryFor opens the registry and finds a guild by its ID.
func entryFor(configDir, id string) (*Registry, *Entry, error) {
	reg, err := OpenRegistry(configDir, io.Discard)
	if err != nil {
		return nil, nil, err
	}
	e := reg.GetByID(id)
	if e == nil {
		return nil, nil, errors.New("that guild is no longer on this computer")
	}
	return reg, e, nil
}

// CheckGuildPassword checks a guild's password.
func CheckGuildPassword(configDir, id, password string) error {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	if !gd.CheckPassword(password) {
		return errors.New("wrong password")
	}
	return gd.UpgradePassword(password, e.DataDir)
}

// ChangeGuildPassword sets a new password, after checking the current one.
func ChangeGuildPassword(configDir, id, current, next string) error {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	if !gd.CheckPassword(current) {
		return errors.New("the current password is wrong")
	}
	gd.PasswordHash = guildstore.HashPassword(next)
	return gd.Save(e.DataDir)
}

// StartGuild starts a guild in the background (it needs a port).
func StartGuild(configDir, id string) error {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if e.Port == 0 {
		return errors.New("this guild has no port yet. Set one under Settings")
	}
	return startEntry(io.Discard, configDir, e)
}

// StopGuild stops a running guild.
func StopGuild(configDir, id string) error {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	return stopEntry(io.Discard, e)
}

// SetGuildPort changes a guild's port, restarting it when it is running.
func SetGuildPort(configDir, id string, port int) error {
	reg, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if port == e.Port {
		return nil
	}
	if err := checkPort(reg, e.Index, port); err != nil {
		return err
	}
	s, running := guild.RunningStatus(e.DataDir)
	if running && s.Broadcasting {
		return errors.New("the port can't change while broadcasting. Stop the broadcast first")
	}
	e.Port = port
	if err := reg.Save(); err != nil {
		return err
	}
	if running {
		if err := stopEntry(io.Discard, e); err != nil {
			return err
		}
		return startEntry(io.Discard, configDir, e)
	}
	return nil
}

// SetGuildBoot turns starting on boot (on Windows, on sign-in) on or off.
func SetGuildBoot(configDir, id string, on bool) error {
	reg, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if err := platform.SetBoot(configDir, e.GuildID, on); err != nil {
		return err
	}
	e.StartOnBoot = on
	return reg.Save()
}

// Broadcast starts broadcasting a guild that has no owner (starting the
// guild first if needed), or stops it.
func Broadcast(configDir, id string, on bool) error {
	reg, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if !on {
		_, err := guild.Call(e.DataDir, "end-broadcast")
		return err
	}
	if guildstore.OwnerKey(e.DataDir) != "" {
		return errors.New("this guild already has an owner. Broadcasting is only for pairing a guild with no owner")
	}
	for i := range reg.Guilds {
		other := &reg.Guilds[i]
		if other.GuildID == e.GuildID {
			continue
		}
		if s, running := guild.RunningStatus(other.DataDir); running && s.Broadcasting {
			return fmt.Errorf("GUILD#%d is already broadcasting. Only one guild can broadcast at a time", other.Index)
		}
	}
	if _, running := guild.RunningStatus(e.DataDir); !running {
		if e.Port == 0 {
			return errors.New("this guild has no port yet. Set one under Settings")
		}
		if err := startEntry(io.Discard, configDir, e); err != nil {
			return err
		}
	}
	_, err = guild.Call(e.DataDir, "broadcast")
	return err
}

// NewOTP makes a one-time password for a broadcasting guild.
func NewOTP(configDir, id string) (otp string, expires time.Time, err error) {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if s, running := guild.RunningStatus(e.DataDir); !running || !s.Broadcasting {
		return "", time.Time{}, errors.New("start broadcasting first")
	}
	r, err := guild.Call(e.DataDir, "otp")
	if err != nil {
		return "", time.Time{}, err
	}
	return guild.FormatOTP(r.OTP), r.OTPExpires, nil
}

// RemoveOwner takes a guild's owner away, after its password.
func RemoveOwner(configDir, id, password string) error {
	if err := CheckGuildPassword(configDir, id, password); err != nil {
		return err
	}
	_, e, _ := entryFor(configDir, id)
	if _, running := guild.RunningStatus(e.DataDir); running {
		_, err := guild.Call(e.DataDir, "reset-owner")
		return err
	}
	return guildstore.RemoveOwner(e.DataDir)
}

// ExportGuild saves a guild as one encrypted .nexus file in folder, after
// its password. It returns the file's path.
func ExportGuild(configDir, id, password, folder string) (string, error) {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return "", err
	}
	if err := CheckGuildPassword(configDir, id, password); err != nil {
		return "", err
	}
	folder = fsutil.CleanPathInput(folder)
	if info, err := os.Stat(folder); err != nil || !info.IsDir() {
		return "", errors.New("choose a folder to save the file in")
	}
	base := fmt.Sprintf("GUILD%d-%s", e.Index, time.Now().Format("2006-01-02"))
	dest := filepath.Join(folder, base+".nexus")
	for n := 2; ; n++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = filepath.Join(folder, fmt.Sprintf("%s-%d.nexus", base, n))
	}
	tmp, err := os.CreateTemp(folder, ".tmp-export-*")
	if err != nil {
		return "", fmt.Errorf("cannot write in %s: %w", folder, err)
	}
	defer os.Remove(tmp.Name())
	enc, err := guildstore.NewEncryptWriter(tmp, password)
	if err == nil {
		err = guildstore.WriteArchive(enc, e.DataDir)
	}
	if err == nil {
		err = enc.Close()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("the export failed: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", err
	}
	os.Chmod(dest, 0o600)
	platform.GiveToInvokingUser(dest)
	return dest, nil
}

// ImportGuild adds a guild from a .nexus file (with the password it had
// when exported) or from a guild folder (no password needed).
func ImportGuild(configDir, path, password string) (*Entry, error) {
	reg, err := OpenRegistry(configDir, io.Discard)
	if err != nil {
		return nil, err
	}
	if _, ok := reg.NextIndex(); !ok {
		return nil, fmt.Errorf("all %d guild slots are in use. Delete one first", MaxGuilds)
	}
	p := fsutil.CleanPathInput(path)
	if p == "" {
		return nil, errors.New("choose a .nexus file or a guild folder")
	}
	if p, err = filepath.Abs(p); err != nil {
		return nil, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("cannot find %s", p)
	}
	if info.IsDir() {
		if err := importFolder(io.Discard, reg, p); err != nil {
			return nil, err
		}
		return reg.GetByID(mustGuildID(p)), nil
	}
	tmp, err := unpackExport(p, password, filepath.Join(configDir, "guilds"))
	if errors.Is(err, guildstore.ErrBadExportPassword) {
		return nil, errors.New("wrong password, or the file is damaged")
	}
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	gd, err := guildstore.LoadGuildData(tmp)
	if err != nil {
		return nil, err
	}
	if other := reg.GetByID(gd.PublicKey); other != nil {
		return nil, fmt.Errorf("this guild is already on this computer as GUILD#%d", other.Index)
	}
	dir, err := prepareFolder(reg, "", defaultGuildDir(configDir, gd.PublicKey))
	if err != nil {
		return nil, err
	}
	if err := guildstore.MoveFolderContents(tmp, dir); err != nil {
		return nil, fmt.Errorf("could not move the guild into %s: %w", dir, err)
	}
	if err := platform.GiveTreeToGuildUser(dir); err != nil {
		return nil, err
	}
	e, err := reg.Add(Entry{GuildID: gd.PublicKey, DataDir: dir})
	if err == nil {
		err = reg.Save()
	}
	return e, err
}

func mustGuildID(dir string) string {
	gd, err := guildstore.LoadGuildData(dir)
	if err != nil {
		return ""
	}
	return gd.PublicKey
}

// DeleteGuild takes a guild off this computer, after its password. erase
// also removes its folder; dropBackups removes its backups.
func DeleteGuild(configDir, id, password string, erase, dropBackups bool) error {
	if err := CheckGuildPassword(configDir, id, password); err != nil {
		return err
	}
	reg, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if err := stopEntry(io.Discard, e); err != nil {
		return err
	}
	platform.ForgetService(e.GuildID)
	dir := e.DataDir
	reg.Remove(e.Index, io.Discard)
	if err := reg.Save(); err != nil {
		return err
	}
	if erase && guildstore.HasGuildData(dir) {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("the guild was removed, but its folder could not be erased: %w", err)
		}
	}
	if dropBackups {
		os.RemoveAll(backupDir(configDir, id))
	}
	return nil
}

// BackupView is one backup as the window shows it.
type BackupView struct {
	Name string // It looks like S<N>_DDMMYYYY_V<X>.
	Date time.Time
	Size string
	Auto bool // It is true for a backup made by Automatic Backups.
}

// Backups lists a guild's backups, newest first.
func Backups(configDir, id string) ([]BackupView, error) {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return nil, err
	}
	list, err := listBackups(configDir, id)
	if err != nil {
		return nil, err
	}
	var out []BackupView
	for _, b := range list {
		when := b.Date
		if st, err := os.Stat(b.Path); err == nil && st.ModTime().Format("02012006") == b.DateText {
			when = st.ModTime() // This adds the time of day too.
		}
		out = append(out, BackupView{Name: BackupName(e.Index, b), Date: when, Size: fileSize(b.Path), Auto: b.Auto})
	}
	return out, nil
}

// MakeBackup saves a backup of a guild now.
func MakeBackup(configDir, id string) (string, error) {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return "", err
	}
	b, err := makeBackup(configDir, e)
	if err != nil {
		return "", fmt.Errorf("the backup failed: %w", err)
	}
	return BackupName(e.Index, b), nil
}

// RestoreBackup puts a guild back to a backup, after its password. The
// current state is saved as a new backup first.
func RestoreBackup(configDir, id, password, name string) error {
	if err := CheckGuildPassword(configDir, id, password); err != nil {
		return err
	}
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	list, err := listBackups(configDir, id)
	if err != nil {
		return err
	}
	var chosen *backupInfo
	for i := range list {
		if BackupName(e.Index, list[i]) == name {
			chosen = &list[i]
		}
	}
	if chosen == nil {
		return errors.New("that backup is gone")
	}
	_, wasRunning := guild.RunningStatus(e.DataDir)
	if wasRunning {
		if err := stopEntry(io.Discard, e); err != nil {
			return err
		}
	}
	if _, err := makeBackup(configDir, e); err != nil {
		return fmt.Errorf("could not save the current state first, so nothing was changed: %w", err)
	}
	if err := restoreBackup(chosen.Path, e); err != nil {
		return fmt.Errorf("the restore failed, and the guild was left as it was: %w", err)
	}
	if wasRunning {
		return startEntry(io.Discard, configDir, e)
	}
	return nil
}

// SetAutoPort turns Port Forwarding on or off for a guild.
// A running guild restarts, so the router is asked (or told to forget).
func SetAutoPort(configDir, id string, on bool) error {
	reg, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if e.NoAutoPort == !on {
		return nil
	}
	e.NoAutoPort = !on
	if err := reg.Save(); err != nil {
		return err
	}
	if _, running := guild.RunningStatus(e.DataDir); running {
		if err := stopEntry(io.Discard, e); err != nil {
			return err
		}
		return startEntry(io.Discard, configDir, e)
	}
	return nil
}
