package vp8

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// TestRoundTrip checks that a picture survives being squeezed and unpacked.
func TestRoundTrip(t *testing.T) {
	const w, h = 1280, 720
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{byte(x / 5), byte(y / 3), 128, 255}
			if x > 400 && x < 800 && y > 200 && y < 500 {
				c = color.RGBA{240, 30, 30, 255} // This draws a red box.
			}
			src.Set(x, y, c)
		}
	}
	enc, err := NewEncoder(w, h, 30, 2500)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	start := time.Now()
	total := 0
	var last *image.YCbCr
	for i := 0; i < 30; i++ {
		pkt, key, err := enc.Encode(src, i == 0)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && !key {
			t.Fatal("the first picture is a full one")
		}
		total += len(pkt)
		if len(pkt) == 0 {
			continue
		}
		img, err := dec.Decode(pkt)
		if err != nil {
			t.Fatal(err)
		}
		if img != nil {
			last = img
		}
	}
	took := time.Since(start)
	t.Logf("30 frames of 720p: %d bytes, %v (%.1f ms each)", total, took, float64(took.Milliseconds())/30)
	if last == nil || last.Rect.Dx() != w || last.Rect.Dy() != h {
		t.Fatal("no picture came back")
	}
	r, g, b, _ := last.At(600, 350).RGBA()
	if r>>8 < 200 || g>>8 > 70 || b>>8 > 70 {
		t.Fatalf("the red box came back as %d,%d,%d", r>>8, g>>8, b>>8)
	}
}
