//go:build !windows

package ui

import (
	"errors"
	"os/exec"
	"strings"
)

// chooseFolder uses the "zenity" dialog tool (part of most Linux desktops).
// It returns "" if cancelled.
func chooseFolder(title string) (string, error) {
	if _, err := exec.LookPath("zenity"); err != nil {
		return "", errors.New("no folder dialog found: type the folder instead")
	}
	out, err := exec.Command("zenity", "--file-selection", "--directory", "--title="+title).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil // The dialog was cancelled.
		}
		return "", errors.New("could not open the folder dialog")
	}
	return strings.TrimSpace(string(out)), nil
}

// choosePicture uses zenity to pick a PNG or JPG picture.
func choosePicture(title string) (string, error) {
	if _, err := exec.LookPath("zenity"); err != nil {
		return "", errors.New("no file dialog found")
	}
	out, err := exec.Command("zenity", "--file-selection", "--title="+title,
		"--file-filter=Pictures | *.png *.jpg *.jpeg").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil
		}
		return "", errors.New("could not open the file dialog")
	}
	return strings.TrimSpace(string(out)), nil
}

// chooseFile uses zenity to pick any file.
func chooseFile(title string) (string, error) {
	if _, err := exec.LookPath("zenity"); err != nil {
		return "", errors.New("no file dialog found")
	}
	out, err := exec.Command("zenity", "--file-selection", "--title="+title).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil
		}
		return "", errors.New("could not open the file dialog")
	}
	return strings.TrimSpace(string(out)), nil
}

// chooseGuildFile uses zenity to pick a .nexus file.
func chooseGuildFile(title string) (string, error) {
	if _, err := exec.LookPath("zenity"); err != nil {
		return "", errors.New("no file dialog found")
	}
	out, err := exec.Command("zenity", "--file-selection", "--title="+title,
		"--file-filter=Nexus guilds | *.nexus").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil
		}
		return "", errors.New("could not open the file dialog")
	}
	return strings.TrimSpace(string(out)), nil
}

// chooseSound uses zenity to pick a soundboard sound.
func chooseSound(title string) (string, error) {
	if _, err := exec.LookPath("zenity"); err != nil {
		return "", errors.New("no file dialog found")
	}
	out, err := exec.Command("zenity", "--file-selection", "--title="+title,
		"--file-filter=Sounds | *.wav *.ogg *.opus").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil
		}
		return "", errors.New("could not open the file dialog")
	}
	return strings.TrimSpace(string(out)), nil
}
