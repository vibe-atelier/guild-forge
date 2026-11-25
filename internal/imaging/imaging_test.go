package imaging

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestShrinkPicture(t *testing.T) {
	// A wide 1200×600 photo: cropped to the middle square, shrunk to 512.
	src := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1200; x++ {
			src.Set(x, y, color.RGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var in bytes.Buffer
	jpeg.Encode(&in, src, nil)
	out, err := ShrinkPicture(in.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 512 || b.Dy() != 512 {
		t.Fatalf("got %v", b)
	}

	// A small picture is cropped but not enlarged.
	var small bytes.Buffer
	png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 100, 80)))
	out, _ = ShrinkPicture(small.Bytes())
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(out))
	if cfg.Width != 80 || cfg.Height != 80 {
		t.Fatalf("got %dx%d", cfg.Width, cfg.Height)
	}

	if _, err := ShrinkPicture([]byte("hello")); err == nil {
		t.Fatal("a non-picture must be refused")
	}
}
