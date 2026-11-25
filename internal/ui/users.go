package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nexus/internal/user"
)

// This file has helpers for users: the list of recently opened users, and folder names.

const maxRecent = 10

type recentUser struct {
	Folder string `json:"folder"`
	Name   string `json:"name"`
	UserID string `json:"user_id"`
}

func (u *UI) recentPath() string { return filepath.Join(u.ConfigDir, "recent.json") }

// recent lists users opened on this computer, skipping folders that are gone.
func (u *UI) recent() []recentUser {
	var list []recentUser
	if data, err := os.ReadFile(u.recentPath()); err == nil {
		json.Unmarshal(data, &list)
	}
	kept := []recentUser{}
	for _, r := range list {
		if user.IsUserFolder(r.Folder) {
			kept = append(kept, r)
		}
	}
	return kept
}

func (u *UI) saveRecent(list []recentUser) {
	data, _ := json.MarshalIndent(list, "", "  ")
	os.MkdirAll(u.ConfigDir, 0o700)
	os.WriteFile(u.recentPath(), data, 0o600)
}

func (u *UI) addRecent(r recentUser) {
	list := []recentUser{r}
	for _, old := range u.recent() {
		if !strings.EqualFold(old.Folder, r.Folder) && len(list) < maxRecent {
			list = append(list, old)
		}
	}
	u.saveRecent(list)
}

func (u *UI) forgetRecent(folder string) {
	var list []recentUser
	for _, old := range u.recent() {
		if !strings.EqualFold(old.Folder, folder) {
			list = append(list, old)
		}
	}
	u.saveRecent(list)
}

// cleanFolder tidies a typed folder path: quotes, "~", relative paths.
func cleanFolder(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		return "", errors.New("Please choose a folder.")
	}
	if strings.HasPrefix(s, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[1:])
		}
	}
	return filepath.Abs(s)
}

// uniqueFolder adds " (2)", " (3)", ... if dir already exists.
func uniqueFolder(dir string) string {
	try := dir
	for n := 2; ; n++ {
		if _, err := os.Stat(try); errors.Is(err, os.ErrNotExist) {
			return try
		}
		try = fmt.Sprintf("%s (%d)", dir, n)
	}
}

// defaultUsersFolder is where new users go unless another folder is chosen:
// Documents\Nexus (or ~/Nexus if there is no Documents folder).
func defaultUsersFolder() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "Nexus"
	}
	if info, err := os.Stat(filepath.Join(home, "Documents")); err == nil && info.IsDir() {
		return filepath.Join(home, "Documents", "Nexus")
	}
	return filepath.Join(home, "Nexus")
}

// shortID shows an ID as its first 8 and last 4 characters:
// "ABCDEFGH_WXYZ".
func shortID(id string) string {
	if len(id) <= 13 {
		return id
	}
	return id[:8] + "_" + id[len(id)-4:]
}

// hue gives each user ID its own avatar colour (same as the HTML app).
func hue(id string) int {
	h := 0
	for _, c := range id {
		h = (h*31 + int(c)) % 360
	}
	return h
}
