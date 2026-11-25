package capture

import (
	"testing"
	"time"
)

// TestScreens lists what can be shared and takes one picture of the first
// screen (kept only in memory, to check its size).
func TestScreens(t *testing.T) {
	list, err := Targets()
	if err == ErrUnsupported {
		t.Skip(err)
	}
	if err != nil || len(list) == 0 || !list[0].Screen {
		t.Fatalf("targets: %v %+v", err, list)
	}
	windows := 0
	for _, x := range list {
		if !x.Screen {
			windows++
		}
	}
	t.Logf("%d screens and windows (%d windows)", len(list), windows)
	start := time.Now()
	p, err := Grab(list[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("screen %d×%d in %v", p.W, p.H, time.Since(start))
	if p.W != list[0].W || p.H != list[0].H || len(p.Pix) != p.W*p.H*4 {
		t.Fatalf("picture %d×%d, %d bytes", p.W, p.H, len(p.Pix))
	}
}
