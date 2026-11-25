//go:build windows

package ui

import (
	"syscall"
	"unsafe"
)

var playSound = syscall.NewLazyDLL("winmm.dll").NewProc("PlaySoundW")

// playFile asks Windows to play a .wav file without waiting.
func playFile(path string) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil || playSound.Find() != nil {
		return
	}
	const sndAsync, sndNoDefault, sndFilename = 0x0001, 0x0002, 0x00020000
	playSound.Call(uintptr(unsafe.Pointer(p)), 0, sndAsync|sndNoDefault|sndFilename)
}
