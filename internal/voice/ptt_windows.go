//go:build windows

package voice

// Push-To-Talk on Windows works even when Nexus isn't the window in front,
// like other voice apps: the key is read from Windows directly.

import (
	"strings"
	"syscall"
)

var getAsyncKeyState = syscall.NewLazyDLL("user32.dll").NewProc("GetAsyncKeyState")

// keyCodes are Windows' codes for the keys Push-To-Talk can use, by Fyne's
// key names.
var keyCodes = map[string]uintptr{
	"`": 0xC0, "Space": 0x20, "Tab": 0x09, "Insert": 0x2D, "Delete": 0x2E,
	"Home": 0x24, "End": 0x23, "Prior": 0x21, "Next": 0x22,
	"LeftShift": 0xA0, "RightShift": 0xA1, "LeftControl": 0xA2, "RightControl": 0xA3,
	"LeftAlt": 0xA4, "RightAlt": 0xA5, "-": 0xBD, "=": 0xBB, "[": 0xDB, "]": 0xDD,
	"\\": 0xDC, ";": 0xBA, "'": 0xDE, ",": 0xBC, ".": 0xBE, "/": 0xBF,
}

func init() {
	for c := 'A'; c <= 'Z'; c++ {
		keyCodes[string(c)] = uintptr(c)
	}
	for c := '0'; c <= '9'; c++ {
		keyCodes[string(c)] = uintptr(c)
	}
	for i := 1; i <= 24; i++ {
		keyCodes["F"+itoa(i)] = uintptr(0x6F + i)
	}
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// globalKeyHeld says whether the named key is held down anywhere.
func globalKeyHeld(name string) bool {
	code, ok := keyCodes[strings.TrimSpace(name)]
	if !ok {
		return false
	}
	r, _, _ := getAsyncKeyState.Call(code)
	return r&0x8000 != 0
}
