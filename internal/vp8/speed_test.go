package vp8

import (
	"image"
	"testing"
	"time"
)

// TestSpeed1080p measures how long a 1080p picture takes to squeeze, on a
// screen-like picture with a moving part (30 a second leaves 33 ms each).
func TestSpeed1080p(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	const w, h = 1920, 1080
	enc, err := NewEncoder(w, h, 30, 4500)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ { // This draws a desktop: bands of colour and text-like stripes.
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(40+y/8), byte(60+x/12), byte((x^y)&0x3f+100), 255
		}
	}
	img := &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
	frames := 60
	start := time.Now()
	for f := 0; f < frames; f++ {
		for y := 300; y < 780; y++ { // This draws a moving window.
			for x := 0; x < 640; x++ {
				i := (y*w + (x+f*20)%w) * 4
				pix[i], pix[i+1], pix[i+2] = byte(f*7+x), byte(y), byte(f*3)
			}
		}
		if _, _, err := enc.Encode(img, f == 0); err != nil {
			t.Fatal(err)
		}
	}
	per := time.Since(start) / time.Duration(frames)
	t.Logf("1080p: %v a picture (33 ms is the most for 30 a second)", per.Round(time.Millisecond/10))
}
