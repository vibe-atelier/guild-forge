// Package capture takes pictures of a screen or of one window, for screen
// sharing, with Nexus's own code: Windows' drawing system
// (GDI) on Windows, X11 on Linux. Pictures are BGRA: blue, green, red and
// alpha bytes, four per pixel.
package capture

import (
	"errors"
	"fmt"
)

// Target is something that can be shared: a screen or a window.
type Target struct {
	ID     string // It is "screen:N" or "window:HANDLE".
	Name   string // It is the name shown in the list.
	Screen bool
	W, H   int
}

// Picture is one capture.
type Picture struct {
	Pix  []byte // The pixels are BGRA, W*4 bytes a row.
	W, H int
}

// ErrGone means the window was closed (or the screen unplugged).
var ErrGone = errors.New("capture: what was shared is gone")

// ErrUnsupported means this system can't capture yet.
var ErrUnsupported = errors.New("capture: not available on this system yet")

// screenName is how a screen is listed: its size helps tell several apart,
// and is left out when there is only one.
func screenName(i, count, w, h int) string {
	if count <= 1 {
		return "Your Screen"
	}
	return fmt.Sprintf("Screen %d (%d×%d)", i+1, w, h)
}
