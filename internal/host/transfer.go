package host

// This file has export, import, delete, backup and fall-back.

import (
	"nexus/internal/fsutil"
	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/nexusdata"
	"nexus/internal/platform"

	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"nexus/internal/console"
)

// ---- export -------------------------------------------------------------

// Export runs "guild-forge export=N [FOLDER or FILE]".
func Export(c *console.Console, configDir string, index int, target string) error {
	out := c.Out()
	_, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	pw, err := askPasswordReturning(c, gd, index, e.DataDir)
	if err != nil {
		return err
	}

	name := fmt.Sprintf("GUILD%d-%s.nexus", e.Index, time.Now().Format("2006-01-02"))
	dest := name
	if t := fsutil.CleanPathInput(target); t != "" {
		if info, err := os.Stat(t); err == nil && info.IsDir() {
			dest = filepath.Join(t, name)
		} else {
			dest = t
		}
	}
	dest, err = filepath.Abs(dest)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists; choose another name or folder, e.g. guild-forge export=%d \"OTHER FOLDER\"", dest, index)
	}

	fmt.Fprintln(out, "Packing and encrypting the guild...")
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".tmp-export-*")
	if err != nil {
		return fmt.Errorf("cannot write in %s: %w", filepath.Dir(dest), err)
	}
	defer os.Remove(tmp.Name())
	enc, err := guildstore.NewEncryptWriter(tmp, pw)
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
		return fmt.Errorf("export failed: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return err
	}
	os.Chmod(dest, 0o600)
	platform.GiveToInvokingUser(dest)

	fmt.Fprintf(out, "Exported %s to:\n    %s  (%s)\n", e.Name(""), dest, fileSize(dest))
	fmt.Fprintln(out, "It is encrypted with this guild's password. Keep it safe: it contains the guild's secret key.")
	fmt.Fprintln(out, "To load it on another computer:  guild-forge import=\"PATH TO THE FILE\"")
	return nil
}

// askPasswordReturning asks for the guild's password (3 tries) and returns
// it. An old-style stored password is replaced once the right one is typed.
func askPasswordReturning(c *console.Console, gd *guildstore.GuildData, index int, dir string) (string, error) {
	for try := 1; try <= MaxPasswordTries; try++ {
		pw, err := c.AskPassword(fmt.Sprintf("Password for GUILD#%d: ", index))
		if err != nil {
			return "", err
		}
		if gd.CheckPassword(pw) {
			gd.UpgradePassword(pw, dir)
			return pw, nil
		}
		if left := MaxPasswordTries - try; left > 0 {
			word := "tries"
			if left == 1 {
				word = "try"
			}
			fmt.Fprintf(c.Out(), "Wrong password. %d %s left.\n", left, word)
		}
	}
	return "", ErrWrongPassword
}

// ---- import -------------------------------------------------------------

// Import runs "guild-forge import=PATH". PATH is an export file,
// or an existing guild folder to add back to the list.
func Import(c *console.Console, configDir, arg string) error {
	out := c.Out()
	reg, err := OpenRegistry(configDir, out)
	if err != nil {
		return err
	}
	if _, ok := reg.NextIndex(); !ok {
		return fmt.Errorf("all %d guild slots are in use; delete one first with guild-forge delete=N", MaxGuilds)
	}
	p := fsutil.CleanPathInput(arg)
	if p == "" {
		return errors.New(`type the file after "import=", e.g. guild-forge import="GUILD1-2026-11-15.nexus"`)
	}
	if p, err = filepath.Abs(p); err != nil {
		return err
	}
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("cannot find %s", p)
	}

	if info.IsDir() {
		return importFolder(out, reg, p)
	}

	// An export file. Check the password (3 tries), unpack to a temporary
	// folder, then ask where the guild should live.
	var tmp string
	for try := 1; ; try++ {
		pw, err := c.AskPassword("Password the guild had when it was exported: ")
		if err != nil {
			return err
		}
		tmp, err = unpackExport(p, pw, filepath.Join(configDir, "guilds"))
		if err == nil {
			break
		}
		if !errors.Is(err, guildstore.ErrBadExportPassword) {
			return err
		}
		if try >= MaxPasswordTries {
			return ErrWrongPassword
		}
		fmt.Fprintln(out, "Wrong password (or the file is damaged). Try again.")
	}
	defer os.RemoveAll(tmp)

	gd, err := guildstore.LoadGuildData(tmp)
	if err != nil {
		return err
	}
	if other := reg.GetByID(gd.PublicKey); other != nil {
		return fmt.Errorf("this guild is already on this computer as GUILD#%d", other.Index)
	}
	dir, err := AskGuildFolder(c, reg, defaultGuildDir(configDir, gd.PublicKey))
	if err != nil {
		return err
	}
	if err := guildstore.MoveFolderContents(tmp, dir); err != nil {
		return fmt.Errorf("could not move the guild into %s: %w", dir, err)
	}
	if err := platform.GiveTreeToGuildUser(dir); err != nil {
		return err
	}
	e, err := reg.Add(Entry{GuildID: gd.PublicKey, DataDir: dir})
	if err != nil {
		return err
	}
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nImported as %s.\n", e.Name(guildstore.OwnerKey(dir)))
	fmt.Fprintf(out, "Next step: give it a port on this computer with  guild-forge setup=%d port=50000\n", e.Index)
	return nil
}

func unpackExport(file, password, workDir string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	salt, err := guildstore.ReadExportHeader(f)
	if err != nil {
		return "", err
	}
	dec, err := guildstore.NewDecryptReader(f, salt, password)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(workDir, ".import-*")
	if err != nil {
		return "", err
	}
	if err := guildstore.ExtractArchive(dec, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func importFolder(out io.Writer, reg *Registry, dir string) error {
	if !guildstore.HasGuildData(dir) {
		return fmt.Errorf("%s is a folder, but not a Nexus guild folder (it has no %s)", dir, guildstore.GuildFileName)
	}
	gd, err := guildstore.LoadGuildData(dir)
	if err != nil {
		return err
	}
	if idx, used := reg.UsesDataDir(dir); used {
		return fmt.Errorf("that folder is already GUILD#%d", idx)
	}
	if err := platform.CheckFolderAccess(dir); err != nil {
		return err
	}
	if err := platform.GiveTreeToGuildUser(dir); err != nil {
		return err
	}
	os.Chmod(dir, 0o700)
	e, err := reg.Add(Entry{GuildID: gd.PublicKey, DataDir: dir})
	if err != nil {
		return err
	}
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Added the guild folder back as %s.\n", e.Name(guildstore.OwnerKey(dir)))
	fmt.Fprintf(out, "Next step: give it a port with  guild-forge setup=%d port=50000\n", e.Index)
	return nil
}

// ---- delete -------------------------------------------------------------

// Delete runs "guild-forge delete=N".
func Delete(c *console.Console, configDir string, index int) error {
	out := c.Out()
	reg, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	if err := askCurrentPassword(c, gd, index, e.DataDir); err != nil {
		return err
	}
	name := e.Name(guildstore.OwnerKey(e.DataDir))
	yes, err := c.Confirm(fmt.Sprintf("Delete %s from this computer?", name), false)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintln(out, "Nothing deleted.")
		return nil
	}
	erase, err := c.Confirm(fmt.Sprintf("Also ERASE its folder %s and everything in it? This cannot be undone.", e.DataDir), false)
	if err != nil {
		return err
	}
	backups, _ := listBackups(configDir, e.GuildID)
	dropBackups := false
	if len(backups) > 0 {
		if dropBackups, err = c.Confirm(fmt.Sprintf("Also delete its %d backup(s)?", len(backups)), false); err != nil {
			return err
		}
	}

	if err := stopEntry(out, e); err != nil {
		return err
	}
	platform.ForgetService(e.GuildID)
	dir, id := e.DataDir, e.GuildID
	reg.Remove(index, out)
	if err := reg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s deleted.\n", name)
	if erase && guildstore.HasGuildData(dir) {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("could not erase %s: %w", dir, err)
		}
		fmt.Fprintf(out, "Erased %s.\n", dir)
	} else {
		fmt.Fprintf(out, "Its folder was kept: %s\n(To add it back later: guild-forge import=\"%s\")\n", dir, dir)
	}
	if dropBackups {
		os.RemoveAll(backupDir(configDir, id))
		fmt.Fprintln(out, "Its backups were deleted.")
	}
	return nil
}

// ---- backup and fall-back -------------------------------------------------

type backupInfo struct {
	Date     time.Time
	DateText string // It is the date as DDMMYYYY.
	V        int
	Path     string
	Auto     bool // It is true for a backup made by Automatic Backups, not by hand.
}

var backupFileRE = regexp.MustCompile(`^(\d{8})_V(\d+)(\.auto)?\.tar\.gz$`)
var backupNameRE = regexp.MustCompile(`^[Ss](\d+)_(\d{8})_[Vv](\d+)$`)

// Backups live in guild-forge's folder, named by guild ID so they follow the
// guild when its number changes.
func backupDir(configDir, id string) string { return filepath.Join(configDir, "backups", id) }

// BackupName is how a backup is shown and typed: S<N>_DDMMYYYY_V<X>.
func BackupName(index int, b backupInfo) string {
	return fmt.Sprintf("S%d_%s_V%d", index, b.DateText, b.V)
}

func listBackups(configDir, id string) ([]backupInfo, error) {
	entries, err := os.ReadDir(backupDir(configDir, id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []backupInfo
	for _, en := range entries {
		m := backupFileRE.FindStringSubmatch(en.Name())
		if m == nil {
			continue
		}
		date, err := time.Parse("02012006", m[1])
		if err != nil {
			continue
		}
		v, _ := strconv.Atoi(m[2])
		list = append(list, backupInfo{Date: date, DateText: m[1], V: v, Auto: m[3] != "", Path: filepath.Join(backupDir(configDir, id), en.Name())})
	}
	sort.Slice(list, func(i, j int) bool { // The list is newest first.
		if !list[i].Date.Equal(list[j].Date) {
			return list[i].Date.After(list[j].Date)
		}
		return list[i].V > list[j].V
	})
	return list, nil
}

// makeBackup saves the guild folder as today's next backup.
func makeBackup(configDir string, e *Entry) (backupInfo, error) {
	return saveBackup(configDir, e, false)
}

// saveBackup saves the guild folder as today's next backup; auto marks it as
// made by Automatic Backups.
func saveBackup(configDir string, e *Entry, auto bool) (backupInfo, error) {
	dir := backupDir(configDir, e.GuildID)
	isNew := !fileExists(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return backupInfo{}, err
	}
	if isNew {
		// A guild's backups hold its secret key, so only the account that
		// runs the guild may reach them inside the shared nexus-data.
		nexusdata.KeepPrivate(dir)
	}
	existing, err := listBackups(configDir, e.GuildID)
	if err != nil {
		return backupInfo{}, err
	}
	now := time.Now()
	today := now.Format("02012006")
	v := 1
	for _, b := range existing {
		if b.DateText == today && b.V >= v {
			v = b.V + 1
		}
	}
	mark := ""
	if auto {
		mark = ".auto"
	}
	b := backupInfo{DateText: today, V: v, Auto: auto, Path: filepath.Join(dir, fmt.Sprintf("%s_V%d%s.tar.gz", today, v, mark))}
	b.Date, _ = time.Parse("02012006", today)

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return backupInfo{}, err
	}
	defer os.Remove(tmp.Name())
	err = guildstore.WriteArchive(tmp, e.DataDir)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return backupInfo{}, err
	}
	os.Chmod(tmp.Name(), 0o600)
	if err := os.Rename(tmp.Name(), b.Path); err != nil {
		return backupInfo{}, err
	}
	return b, nil
}

// Backup runs "guild-forge backup=N".
func Backup(c *console.Console, configDir string, index int) error {
	out := c.Out()
	_, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	if err := askCurrentPassword(c, gd, index, e.DataDir); err != nil {
		return err
	}
	b, err := makeBackup(configDir, e)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	fmt.Fprintf(out, "Saved backup %s (%s).\n", BackupName(index, b), fileSize(b.Path))
	fmt.Fprintf(out, "To see all backups: guild-forge fall-back=%d\n", index)
	return nil
}

// FallBack runs "guild-forge fall-back=N" (list) and
// "guild-forge fall-back=N NAME" (restore).
func FallBack(c *console.Console, configDir string, index int, name string) error {
	out := c.Out()
	_, e, err := openGuild(configDir, out, index)
	if err != nil {
		return err
	}
	backups, err := listBackups(configDir, e.GuildID)
	if err != nil {
		return err
	}

	if name == "" {
		if len(backups) == 0 {
			fmt.Fprintf(out, "GUILD#%d has no backups yet. Make one with: guild-forge backup=%d\n", index, index)
			return nil
		}
		fmt.Fprintf(out, "Backups of GUILD#%d (newest first):\n", index)
		for _, b := range backups {
			fmt.Fprintf(out, "  %-20s %s\n", BackupName(index, b), fileSize(b.Path))
		}
		fmt.Fprintf(out, "To go back to one: guild-forge fall-back=%d NAME\n", index)
		return nil
	}

	m := backupNameRE.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return fmt.Errorf("%q is not a backup name; they look like S%d_15112026_V1 (see guild-forge fall-back=%d)", name, index, index)
	}
	if n, _ := strconv.Atoi(m[1]); n != index {
		return fmt.Errorf("%s belongs to GUILD#%d, not GUILD#%d", name, n, index)
	}
	v, _ := strconv.Atoi(m[3])
	var chosen *backupInfo
	for i := range backups {
		if backups[i].DateText == m[2] && backups[i].V == v {
			chosen = &backups[i]
		}
	}
	if chosen == nil {
		return fmt.Errorf("there is no backup %s; see the list with guild-forge fall-back=%d", name, index)
	}

	gd, err := guildstore.LoadGuildData(e.DataDir)
	if err != nil {
		return err
	}
	if err := askCurrentPassword(c, gd, index, e.DataDir); err != nil {
		return err
	}
	label := BackupName(index, *chosen)
	yes, err := c.Confirm(fmt.Sprintf("Restore GUILD#%d to %s? (The current state is saved as a new backup first.)", index, label), false)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintln(out, "Nothing changed.")
		return nil
	}

	_, wasRunning := guild.RunningStatus(e.DataDir)
	if wasRunning {
		if err := stopEntry(out, e); err != nil {
			return err
		}
	}
	safety, err := makeBackup(configDir, e)
	if err != nil {
		return fmt.Errorf("could not save the current state first, so nothing was changed: %w", err)
	}
	fmt.Fprintf(out, "Current state saved as %s.\n", BackupName(index, safety))

	if err := restoreBackup(chosen.Path, e); err != nil {
		return fmt.Errorf("restore failed (the guild was left as it was): %w", err)
	}
	fmt.Fprintf(out, "GUILD#%d is back to %s.\n", index, label)
	if wasRunning {
		return startEntry(out, configDir, e)
	}
	return nil
}

// restoreBackup replaces the guild folder's contents with the backup. It
// unpacks next to the folder first and only then swaps, so a failure leaves
// the guild untouched.
func restoreBackup(file string, e *Entry) error {
	dir := e.DataDir
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	tmp := dir + ".restore-" + stamp
	if err := os.Mkdir(tmp, 0o700); err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	err = guildstore.ExtractArchive(f, tmp)
	f.Close()
	if err == nil {
		var gd *guildstore.GuildData
		if gd, err = guildstore.LoadGuildData(tmp); err == nil && gd.PublicKey != e.GuildID {
			err = errors.New("that backup belongs to a different guild")
		}
	}
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	old := dir + ".old-" + stamp
	if err := os.Rename(dir, old); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dir); err != nil {
		os.Rename(old, dir)
		os.RemoveAll(tmp)
		return err
	}
	// Keep the log history.
	os.Rename(filepath.Join(old, guildstore.LogsDir), filepath.Join(dir, guildstore.LogsDir))
	os.RemoveAll(old)
	os.Chmod(dir, 0o700)
	return platform.GiveTreeToGuildUser(dir)
}

// fileSize returns a file's size in a readable form.
func fileSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	n := float64(info.Size())
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", n/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", n/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", n/(1<<10))
	}
	return fmt.Sprintf("%d bytes", info.Size())
}
