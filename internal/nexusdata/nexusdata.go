// Package nexusdata finds nexus-data, the PC's own folder. It holds
// what belongs to the PC rather than to a person: the app's settings (app),
// GuildForge's guild list, backups and default guild folders (forge), and
// the files the programs write out (models, fonts). User folders are not in
// it; they go wherever their users want.
//
// On Windows nexus-data is shared by every account on the PC, in
// C:\ProgramData\nexus-data. Elsewhere it is in the user's data folder
// (~/.local/share/nexus-data); Linux hosting keeps its own system folder.
package nexusdata

import (
	"os"
	"path/filepath"
)

// Name is the folder's name.
const Name = "nexus-data"

// These are the parts of nexus-data.
const (
	AppPart    = "app"    // It holds the app's settings.
	ForgePart  = "forge"  // It holds GuildForge's guild list, backups and guild folders.
	ModelsPart = "models" // It holds files the programs write out, such as the noise model.
	FontsPart  = "fonts"  // It holds the emoji font and its licence.
)

// Dir returns nexus-data, making it if it isn't there yet. The NEXUS_DATA
// setting puts it somewhere else (used by tests and pictures).
func Dir() (string, error) {
	dir := os.Getenv("NEXUS_DATA")
	if dir == "" {
		base, err := baseDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, Name)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		share(dir) // A new folder is opened to every account on the PC.
	}
	return dir, nil
}

// Part returns one part of nexus-data (AppPart, ForgePart and so on),
// making it if needed.
func Part(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	return p, os.MkdirAll(p, 0o755)
}

// Shared makes a folder that was moved into nexus-data take nexus-data's
// sharing, as if it had been made there.
func Shared(dir string) { reshare(dir) }

// KeepPrivate lets only the current account (and the system's own
// administrators) change dir and everything in it. Each guild's folder and
// backups are kept this way inside the shared nexus-data.
func KeepPrivate(dir string) { keepPrivate(dir) }

// MoveIn moves everything in old into a part of nexus-data, once:
// only when old has something in it and the part is still empty. It reports
// whether anything moved. The emptied old folder is removed.
func MoveIn(old, part string) (bool, error) {
	from, err := os.ReadDir(old)
	if err != nil || len(from) == 0 {
		return false, nil // There is nothing to move.
	}
	dst, err := Part(part)
	if err != nil {
		return false, err
	}
	if in, _ := os.ReadDir(dst); len(in) > 0 {
		return false, nil // It was moved before, or this PC already has its own.
	}
	for _, en := range from {
		if err := os.Rename(filepath.Join(old, en.Name()), filepath.Join(dst, en.Name())); err != nil {
			return true, err
		}
	}
	os.Remove(old)
	reshare(dst)
	return true, nil
}

// Path returns where a part of nexus-data is, without making anything. It
// is for looking: a program that only reads never creates nexus-data.
func Path(part string) string {
	dir := os.Getenv("NEXUS_DATA")
	if dir == "" {
		base, err := baseDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(base, Name)
	}
	return filepath.Join(dir, part)
}
