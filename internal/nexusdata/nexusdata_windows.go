package nexusdata

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"syscall"
)

// baseDir is C:\ProgramData (or wherever Windows keeps data for all users).
func baseDir() (string, error) {
	if d := os.Getenv("ProgramData"); d != "" {
		return d, nil
	}
	return "", errors.New("cannot find the ProgramData folder")
}

// These are the well-known Windows accounts, by their fixed IDs, so the names work in
// every language of Windows.
const (
	usersGroup     = "*S-1-5-32-545" // It stands for every account on the PC.
	administrators = "*S-1-5-32-544" // It stands for the PC's administrators.
	system         = "*S-1-5-18"     // It stands for Windows itself.
)

// icacls runs Windows' own permission tool quietly, without a window, and
// reports whether it succeeded. A failure is not fatal: nexus-data still
// works for the account that made it.
func icacls(args ...string) bool {
	cmd := exec.Command("icacls", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run() == nil
}

// share lets every account on the PC read and change dir and what is in it.
func share(dir string) {
	icacls(dir, "/grant", usersGroup+":(OI)(CI)M", "/Q")
}

// reshare makes a moved folder take its permissions from its new parent.
func reshare(dir string) {
	icacls(dir, "/reset", "/T", "/C", "/Q")
}

// keepPrivate lets only this account, Windows and the administrators reach
// dir and everything in it. It works on folders only: the files inside a
// private folder inherit its permissions, so a file is left alone.
func keepPrivate(dir string) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return
	}
	me, err := user.Current()
	if err != nil {
		return
	}
	// First the three accounts are given full control, and only when that
	// worked are the inherited permissions removed, so a failure can never
	// leave the folder with nobody allowed in.
	grants := []string{"*" + me.Uid + ":(OI)(CI)F", system + ":(OI)(CI)F", administrators + ":(OI)(CI)F"}
	if !icacls(append([]string{dir, "/grant"}, append(grants, "/Q")...)...) {
		return
	}
	// Windows passes the folder's permissions on to everything inside it.
	icacls(dir, "/inheritance:r", "/Q")
}
