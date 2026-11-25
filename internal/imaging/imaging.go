// Package imaging prepares pictures for Nexus: it crops them to a square
// and shrinks them to at most 512×512 using only the standard library.
package imaging

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

const (
	maxUploadBytes = 10 * 1024 * 1024 // An upload may be at most 10 MB.
	pictureSide    = 512
	maxSentPicture = 1800 * 1024 // This stays under the guild's 2 MB limit.
)

// ShrinkPicture turns an uploaded PNG or JPG into a square picture of at
// most 512×512 (cropped around the middle), as PNG, or as JPG if the PNG
// would be too big (photos).
func ShrinkPicture(data []byte) ([]byte, error) {
	if len(data) > maxUploadBytes {
		return nil, errors.New("the picture is larger than 10 MB")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("that file is not a PNG or JPG picture")
	}
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	if side < 1 {
		return nil, errors.New("the picture is empty")
	}
	start := image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2)
	crop := image.Rectangle{Min: start, Max: start.Add(image.Pt(side, side))}
	out := min(side, pictureSide)
	return encode(resizeBox(src, crop, out, out), maxSentPicture)
}

// decodeUpload reads an uploaded PNG or JPG, refusing files over 10 MB,
// files that are not pictures, and empty pictures.
func decodeUpload(data []byte) (image.Image, error) {
	if len(data) > maxUploadBytes {
		return nil, errors.New("the picture is larger than 10 MB")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("that file is not a PNG or JPG picture")
	}
	if b := src.Bounds(); b.Dx() < 1 || b.Dy() < 1 {
		return nil, errors.New("the picture is empty")
	}
	return src, nil
}

// FitPicture shrinks an uploaded PNG or JPG (without cropping) so its longer
// side is at most maxSide, and keeps the file under maxBytes (switching to JPG
// if needed). Used for CAPTCHA question pictures.
func FitPicture(data []byte, maxSide, maxBytes int) ([]byte, error) {
	src, err := decodeUpload(data)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if longer := max(w, h); longer > maxSide {
		w, h = max(w*maxSide/longer, 1), max(h*maxSide/longer, 1)
	}
	return encode(resizeBox(src, b, w, h), maxBytes)
}

func encode(dst *image.NRGBA, maxBytes int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	if buf.Len() <= maxBytes {
		return buf.Bytes(), nil
	}
	for _, q := range []int{88, 75, 60} {
		buf.Reset()
		if err := jpeg.Encode(&buf, flatten(dst), &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		if buf.Len() <= maxBytes {
			break
		}
	}
	return buf.Bytes(), nil
}

// resizeBox scales the area r of src to w×h by averaging the source pixels
// that fall into each target pixel (smooth, no extra libraries).
func resizeBox(src image.Image, r image.Rectangle, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	sx, sy := float64(r.Dx())/float64(w), float64(r.Dy())/float64(h)
	for y := 0; y < h; y++ {
		y0 := r.Min.Y + int(float64(y)*sy)
		y1 := max(r.Min.Y+int(float64(y+1)*sy), y0+1)
		for x := 0; x < w; x++ {
			x0 := r.Min.X + int(float64(x)*sx)
			x1 := max(r.Min.X+int(float64(x+1)*sx), x0+1)
			var rs, gs, bs, as, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					rs += uint64(c.R)
					gs += uint64(c.G)
					bs += uint64(c.B)
					as += uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{uint8(rs / n), uint8(gs / n), uint8(bs / n), uint8(as / n)})
		}
	}
	return dst
}

// flatten puts the picture on black (JPG has no transparency).
func flatten(src *image.NRGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		a := uint32(src.Pix[i+3])
		out.Pix[i] = uint8(uint32(src.Pix[i]) * a / 255)
		out.Pix[i+1] = uint8(uint32(src.Pix[i+1]) * a / 255)
		out.Pix[i+2] = uint8(uint32(src.Pix[i+2]) * a / 255)
		out.Pix[i+3] = 255
	}
	return out
}

// CropPicture crops an uploaded PNG or JPG around the middle to the shape
// w×h, shrinks it to at most w×h, and keeps it under maxBytes. Used for
// profile pictures (256×256) and banners (960×320).
func CropPicture(data []byte, w, h, maxBytes int) ([]byte, error) {
	src, err := decodeUpload(data)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	cw, ch := b.Dx(), b.Dx()*h/w // This is the widest crop of the right shape.
	if ch > b.Dy() {
		cw, ch = b.Dy()*w/h, b.Dy()
	}
	cw, ch = max(cw, 1), max(ch, 1)
	start := image.Pt(b.Min.X+(b.Dx()-cw)/2, b.Min.Y+(b.Dy()-ch)/2)
	crop := image.Rectangle{Min: start, Max: start.Add(image.Pt(cw, ch))}
	ow, oh := min(cw, w), min(ch, h)
	if cw > w {
		oh = max(ch*w/cw, 1)
	}
	return encode(resizeBox(src, crop, ow, oh), maxBytes)
}
