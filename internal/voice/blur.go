package voice

// heavyBlur blurs a BGRA picture so nothing on it can be made out, only
// shapes and colours (Push-To-Blur): it is shrunk to blocks about a
// fortieth of its width, then smoothly stretched back to a quarter of its
// size (the encoder scales it the rest of the way; a blur loses nothing by
// it, and it is 16 times less work).
func heavyBlur(pix []byte, w, h int) (out []byte, ow, oh int) {
	b := max(16, w/40)
	sw, sh := (w+b-1)/b, (h+b-1)/b
	small := make([]float32, sw*sh*3)
	count := make([]float32, sw*sh)
	for y := 0; y < h; y++ {
		row := (y / b) * sw
		for x := 0; x < w; x++ {
			i, j := (y*w+x)*4, row+x/b
			small[j*3] += float32(pix[i])
			small[j*3+1] += float32(pix[i+1])
			small[j*3+2] += float32(pix[i+2])
			count[j]++
		}
	}
	for j := range count {
		small[j*3] /= count[j]
		small[j*3+1] /= count[j]
		small[j*3+2] /= count[j]
	}
	ow, oh = max(2, w/4), max(2, h/4)
	out = make([]byte, ow*oh*4)
	at := func(x, y, c int) float32 {
		x, y = max(0, min(sw-1, x)), max(0, min(sh-1, y))
		return small[(y*sw+x)*3+c]
	}
	for y := 0; y < oh; y++ {
		fy := (float32(y*h/oh)+0.5)/float32(b) - 0.5
		y0 := int(fy)
		if fy < 0 {
			y0 = -1
		}
		ty := fy - float32(y0)
		for x := 0; x < ow; x++ {
			fx := (float32(x*w/ow)+0.5)/float32(b) - 0.5
			x0 := int(fx)
			if fx < 0 {
				x0 = -1
			}
			tx := fx - float32(x0)
			i := (y*ow + x) * 4
			for c := 0; c < 3; c++ {
				top := at(x0, y0, c)*(1-tx) + at(x0+1, y0, c)*tx
				bot := at(x0, y0+1, c)*(1-tx) + at(x0+1, y0+1, c)*tx
				out[i+c] = byte(top*(1-ty) + bot*ty)
			}
			out[i+3] = 255
		}
	}
	return out, ow, oh
}
