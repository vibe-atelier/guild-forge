package vp8

import "image"

// toI420 scales src to w×h and turns it into I420 (VP8's own picture
// form: brightness for every pixel, colour for every 2×2 block) in dst.
// Screens arrive as BGRA from Windows, so *image.RGBA (with the bytes in
// BGRA order, see BGRA) and *image.NRGBA are read directly.
func toI420(src image.Image, w, h int, dst []byte) {
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	yp := dst[:w*h]
	up := dst[w*h : w*h+w*h/4]
	vp := dst[w*h+w*h/4:]

	var pix []byte
	stride, bgr := 0, false
	switch s := src.(type) {
	case *BGRA:
		pix, stride, bgr = s.Pix, s.Stride, true
	case *image.RGBA:
		pix, stride = s.Pix, s.Stride
	case *image.NRGBA:
		pix, stride = s.Pix, s.Stride
	}
	px := func(x, y int) (r, g, b int32) {
		sx, sy := x*sw/w, y*sh/h // It takes the nearest pixel, which is quick and sharp for text.
		if pix != nil {
			i := sy*stride + sx*4
			if bgr {
				return int32(pix[i+2]), int32(pix[i+1]), int32(pix[i])
			}
			return int32(pix[i]), int32(pix[i+1]), int32(pix[i+2])
		}
		cr, cg, cb, _ := src.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
		return int32(cr >> 8), int32(cg >> 8), int32(cb >> 8)
	}
	clamp := func(v int32) byte {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return byte(v)
	}
	for y := 0; y < h; y += 2 {
		for x := 0; x < w; x += 2 {
			var sr, sg, sb int32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					r, g, b := px(x+dx, y+dy)
					// This uses BT.601, as VP8 expects.
					yp[(y+dy)*w+x+dx] = clamp((66*r+129*g+25*b+128)>>8 + 16)
					sr, sg, sb = sr+r, sg+g, sb+b
				}
			}
			r, g, b := sr/4, sg/4, sb/4
			ci := (y/2)*(w/2) + x/2
			up[ci] = clamp((-38*r-74*g+112*b+128)>>8 + 128)
			vp[ci] = clamp((112*r-94*g-18*b+128)>>8 + 128)
		}
	}
}

// BGRA is a picture whose bytes are in Windows' order (blue, green, red,
// alpha). It is what screen capture gives; only Pix, Stride and Rect are
// used by the encoder.
type BGRA struct {
	image.RGBA
}

// NewBGRA wraps captured bytes.
func NewBGRA(pix []byte, w, h int) *BGRA {
	return &BGRA{image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}}
}
