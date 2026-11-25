package host

// Automatic Backups can be Off, Daily, Weekly or Monthly. The running
// guild makes them itself; the last 10 automatic backups are kept, and
// backups made by hand are never removed by this.

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"nexus/internal/guild"
)

// AutoBackupModes are the choices, in order ("" is Off).
var AutoBackupModes = []string{"", "daily", "weekly", "monthly"}

// KeepAutoBackups is how many automatic backups are kept.
const KeepAutoBackups = 10

// autoBackupGap is how long after the last automatic backup the next is due.
func autoBackupGap(mode string) time.Duration {
	switch mode {
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	}
	return 0
}

// AutoBackupName is a mode as shown: Off, Daily, Weekly or Monthly.
func AutoBackupName(mode string) string {
	if mode == "" {
		return "Off"
	}
	return strings.ToUpper(mode[:1]) + mode[1:]
}

// SetAutoBackups chooses a guild's Automatic Backups. A running guild sees
// the change within the hour.
func SetAutoBackups(configDir, id, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "off" {
		mode = ""
	}
	if autoBackupGap(mode) == 0 && mode != "" {
		return fmt.Errorf("%q is not a choice: daily, weekly, monthly or off", mode)
	}
	reg, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	if e.AutoBackups == mode {
		return nil
	}
	e.AutoBackups = mode
	return reg.Save()
}

// lastAutoBackup is when the newest automatic backup was made (zero: none).
func lastAutoBackup(list []backupInfo) time.Time {
	var last time.Time
	for _, b := range list {
		if !b.Auto {
			continue
		}
		when := b.Date
		if st, err := os.Stat(b.Path); err == nil {
			when = st.ModTime()
		}
		if when.After(last) {
			last = when
		}
	}
	return last
}

// pruneAutoBackups removes the oldest automatic backups beyond the last 10.
func pruneAutoBackups(list []backupInfo) error {
	kept := 0
	var errs []error
	for _, b := range list { // The list is newest first.
		if !b.Auto {
			continue
		}
		kept++
		if kept > KeepAutoBackups {
			if err := os.Remove(b.Path); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// autoBackupDue makes an automatic backup if one is due, while the guild
// holds still (so no file is half written). It reports what it made.
func autoBackupDue(configDir, id string, g *guild.Server, now time.Time) (string, error) {
	_, e, err := entryFor(configDir, id) // Read the setting again, because it may have changed.
	if err != nil {
		return "", err
	}
	gap := autoBackupGap(e.AutoBackups)
	if gap == 0 {
		return "", nil
	}
	list, err := listBackups(configDir, e.GuildID)
	if err != nil {
		return "", err
	}
	if last := lastAutoBackup(list); !last.IsZero() && now.Sub(last) < gap {
		return "", nil
	}
	var b backupInfo
	hold := func(f func()) { f() }
	if g != nil {
		hold = g.HoldStill
	}
	hold(func() { b, err = saveBackup(configDir, e, true) })
	if err != nil {
		return "", err
	}
	list, _ = listBackups(configDir, e.GuildID)
	return BackupName(e.Index, b), pruneAutoBackups(list)
}

// runAutoBackups checks once an hour, while the guild runs (the first check
// a minute after it starts).
func runAutoBackups(configDir, id string, g *guild.Server, logger *log.Logger) {
	wait := time.Minute
	for {
		select {
		case <-g.Done():
			return
		case <-time.After(wait):
		}
		wait = time.Hour
		name, err := autoBackupDue(configDir, id, g, time.Now())
		switch {
		case err != nil:
			logger.Printf("automatic backup: %v", err)
		case name != "":
			logger.Printf("automatic backup %s saved", name)
		}
	}
}

// DeleteBackup removes one backup by its name (S<N>_DDMMYYYY_V<X>).
func DeleteBackup(configDir, id, name string) error {
	_, e, err := entryFor(configDir, id)
	if err != nil {
		return err
	}
	list, err := listBackups(configDir, id)
	if err != nil {
		return err
	}
	for _, b := range list {
		if strings.EqualFold(BackupName(e.Index, b), name) {
			return os.Remove(b.Path)
		}
	}
	return fmt.Errorf("there is no backup %s", name)
}
