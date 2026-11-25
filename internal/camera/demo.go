package camera

import (
	"math"
	"time"
)

// openDemo is a made-up camera: a smiling face on a soft background, gently
// moving, so pictures show something like a person without a real camera.
func openDemo() *Camera {
	c := newCamera()
	go func() {
		defer close(c.done)
		defer close(c.frames)
		tick := time.NewTicker(time.Second / 30)
		defer tick.Stop()
		for t := 0; ; t++ {
			select {
			case <-c.stop:
				return
			case <-tick.C:
			}
			c.send(demoFrame(t))
		}
	}()
	return c
}

func demoFrame(t int) Frame {
	w, h := WantW, WantH
	pix := make([]byte, w*h*4)
	sway := 8 * math.Sin(float64(t)/20)
	cx, cy := float64(w)/2+sway, float64(h)/2+20
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			b, g, r := byte(110+y/8), byte(90+y/10), byte(70+x/12) // This paints the room.
			dx, dy := float64(x)-cx, float64(y)-cy
			if dx*dx/(150*150)+(dy-160)*(dy-160)/(120*120) < 1 { // These are the shoulders.
				b, g, r = 140, 90, 60
			}
			if dx*dx/(95*95)+dy*dy/(120*120) < 1 { // This is the face.
				b, g, r = 150, 185, 225
				ex := math.Abs(dx) - 35
				if ex*ex+(dy+25)*(dy+25) < 100 { // These are the eyes.
					b, g, r = 40, 40, 50
				}
				// This is the smile.
				if dy > 30 && dy < 45 && dx*dx+(dy-10)*(dy-10) < 45*45 && dx*dx+(dy-10)*(dy-10) > 33*33 {
					b, g, r = 60, 60, 160
				}
			}
			if dy < -70 && dx*dx/(100*100)+(dy+60)*(dy+60)/(80*80) < 1 { // This is the hair.
				b, g, r = 40, 50, 70
			}
			pix[i], pix[i+1], pix[i+2], pix[i+3] = b, g, r, 255
		}
	}
	return Frame{Pix: pix, W: w, H: h}
}
