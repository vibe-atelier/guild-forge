//go:build windows

package sound

import (
	"nexus/internal/wincom"
	"os"
	"testing"
	"unsafe"
)

// TestLoopbackWithoutMe checks that Windows lets us record every program but Nexus.
func TestLoopbackWithoutMe(t *testing.T) {
	if os.Getenv("NEXUS_SOUND_TEST") != "1" {
		t.Skip("set NEXUS_SOUND_TEST=1 to use the real devices")
	}
	var err error
	onCOMThread(func() {
		var c unsafe.Pointer
		if c, err = loopbackWithoutMe(); c != nil {
			wincom.Release(c)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
