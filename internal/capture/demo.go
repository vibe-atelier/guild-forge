package capture

import (
	"fmt"
	"strings"
)

// Demo makes Targets and Grab use made-up screens and windows, so the
// guide's pictures never show the real desktop.
var Demo bool

func demoTargets() []Target {
	return []Target{
		{ID: "screen:0", Name: screenName(0, 1, 1920, 1080), Screen: true, W: 1920, H: 1080},
		{ID: "window:1", Name: "Pixel Editor - sprite.png", W: 1280, H: 800},
		{ID: "window:2", Name: "Notes", W: 900, H: 700},
	}
}

func demoGrab(t Target) (*Picture, error) {
	for _, d := range demoTargets() {
		if d.ID != t.ID {
			continue
		}
		p := &Picture{Pix: make([]byte, d.W*d.H*4), W: d.W, H: d.H}
		var seed int
		fmt.Sscanf(strings.TrimLeft(d.ID, "screnwido:"), "%d", &seed)
		for y := 0; y < d.H; y++ {
			for x := 0; x < d.W; x++ {
				i := (y*d.W + x) * 4
				p.Pix[i] = byte(120 + seed*40 + y/8)    // This is the blue byte.
				p.Pix[i+1] = byte(60 + x/10)            // This is the green byte.
				p.Pix[i+2] = byte(140 + seed*30 - y/12) // This is the red byte.
				p.Pix[i+3] = 255
				if (x/40+y/40)%7 == 0 {
					p.Pix[i], p.Pix[i+1], p.Pix[i+2] = 240, 240, 245
				}
			}
		}
		return p, nil
	}
	return nil, ErrGone
}
