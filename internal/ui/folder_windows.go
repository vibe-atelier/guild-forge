//go:build windows

package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

// showDialog runs one of Windows' own dialogs through PowerShell, which is
// part of Windows, with no console window. The dialog's title goes in
// through NEXUS_DIALOG_TITLE, so no typed text ever becomes part of the
// script. It returns what the script printed: the path chosen, or "" if
// cancelled.
func showDialog(script, title, what string) (string, error) {
	script = `Add-Type -AssemblyName System.Windows.Forms
$f = New-Object System.Windows.Forms.Form -Property @{TopMost=$true; ShowInTaskbar=$false}
` + script
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.Env = append(os.Environ(), "NEXUS_DIALOG_TITLE="+title)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("could not open the " + what + " dialog")
	}
	return strings.TrimSpace(string(out)), nil
}

// openFile shows Windows' own "open a file" dialog, offering the files that
// filter describes (in the dialog's own "Name (*.x)|*.x" form).
func openFile(title, filter string) (string, error) {
	return showDialog(`$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = $env:NEXUS_DIALOG_TITLE
$d.Filter = '`+filter+`'
if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.FileName) }`, title, "file")
}

// chooseFolder shows Windows' own "choose a folder" dialog. It returns "" if
// cancelled.
func chooseFolder(title string) (string, error) {
	return showDialog(`$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = $env:NEXUS_DIALOG_TITLE
$d.ShowNewFolderButton = $true
if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.SelectedPath) }`, title, "folder")
}

// choosePicture asks for a PNG or JPG picture. It returns "" if cancelled.
func choosePicture(title string) (string, error) {
	return openFile(title, `Pictures (*.png;*.jpg;*.jpeg)|*.png;*.jpg;*.jpeg`)
}

// chooseFile asks for any file, offering pictures first.
func chooseFile(title string) (string, error) {
	return openFile(title, `Pictures (*.png;*.jpg;*.jpeg)|*.png;*.jpg;*.jpeg|All files (*.*)|*.*`)
}

// chooseGuildFile asks for an exported guild (.nexus file).
func chooseGuildFile(title string) (string, error) {
	return openFile(title, `Nexus guilds (*.nexus)|*.nexus|All files (*.*)|*.*`)
}

// chooseSound asks for a soundboard sound. It returns "" if cancelled.
func chooseSound(title string) (string, error) {
	return openFile(title, `Sounds (*.wav;*.ogg;*.opus)|*.wav;*.ogg;*.opus`)
}
