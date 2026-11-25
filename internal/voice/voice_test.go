package voice

import (
	"bytes"
	"image/jpeg"
	"testing"
	"time"

	"nexus/internal/camera"
)

func TestLoudness(t *testing.T) {
	if loudness(make([]int16, 960)) != silenceDB {
		t.Fatal("silence")
	}
	loud := make([]int16, 960)
	for i := range loud {
		loud[i] = 32767
		if i%2 == 1 {
			loud[i] = -32767
		}
	}
	if db := loudness(loud); db < -0.1 {
		t.Fatalf("full scale is about 0 dB, got %.1f", db)
	}
}

func TestMix(t *testing.T) {
	e := &Engine{speakers: map[string]*speaker{}}
	a, b := &speaker{}, &speaker{}
	for i := 0; i < 2; i++ {
		fa, fb := make([]int16, 960), make([]int16, 960)
		for j := range fa {
			fa[j], fb[j] = 20000, 20000
		}
		a.queue = append(a.queue, fa)
		b.queue = append(b.queue, fb)
	}
	e.speakers["a"], e.speakers["b"] = a, b
	out := make([]int16, 960)
	e.mix(out)
	if out[0] != 32767 {
		t.Fatalf("two loud voices clip at the top, got %d", out[0])
	}
	e.mix(out)
	e.mix(out)
	if out[0] != 0 {
		t.Fatal("nothing left: silence")
	}
}

func TestDefaults(t *testing.T) {
	if (Settings{}).Key() != "`" || (Settings{}).Level() != DefaultSensitivity {
		t.Fatal("defaults")
	}
	if !globalKeyHeld("`") && globalKeyHeld("not a key") {
		t.Fatal("unknown keys are never held")
	}
}

func TestFitSize(t *testing.T) {
	for _, c := range []struct{ w, h, lines, ww, wh int }{
		{1920, 1080, 720, 1280, 720},
		{1920, 1080, 1080, 1920, 1080},
		{1001, 601, 1080, 1000, 600}, // A size smaller than asked is kept, and made even.
		{2560, 1440, 1080, 1920, 1080},
	} {
		if w, h := fitSize(c.w, c.h, c.lines); w != c.ww || h != c.wh {
			t.Errorf("%dx%d at %d: got %dx%d", c.w, c.h, c.lines, w, h)
		}
	}
}

// TestHeavyBlur checks that fine detail (a chessboard of single pixels) is gone; the
// overall colour stays.
func TestHeavyBlur(t *testing.T) {
	const w, h = 640, 360
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			v := byte(0)
			if (x+y)%2 == 0 {
				v = 255
			}
			pix[i], pix[i+1], pix[i+2], pix[i+3] = v, v, v, 255
		}
	}
	out, ow, oh := heavyBlur(pix, w, h)
	if ow != w/4 || oh != h/4 {
		t.Fatalf("size %dx%d", ow, oh)
	}
	for _, p := range [][2]int{{2, 2}, {3, 2}, {80, 45}, {159, 89}} {
		v := out[(p[1]*ow+p[0])*4]
		if v < 110 || v > 145 {
			t.Fatalf("pixel %v is %d: the chessboard should blur to grey", p, v)
		}
	}
}

// TestMixVolumes checks per-person volume, Priority Speaker and
// the soundboard.
func TestMixVolumes(t *testing.T) {
	frame := func(v int16) []int16 {
		f := make([]int16, 960)
		for i := range f {
			f[i] = v
		}
		return f
	}
	e := &Engine{speakers: map[string]*speaker{}, sounds: map[string]*soundTrack{}}
	e.set = e.set.WithVolume("a", 50)
	e.speakers["a"] = &speaker{queue: [][]int16{frame(1000), frame(1000), frame(1000)}}
	out := make([]int16, 960)
	e.mix(out)
	if out[0] != 500 {
		t.Fatalf("a at 50%% should be 500, got %d", out[0])
	}
	// b holds the Priority Speaker key: a drops to a fifth.
	e.speakers["b"] = &speaker{queue: [][]int16{frame(100), frame(100)}, priority: time.Now()}
	e.mix(out)
	if out[0] != 500/5+100 {
		t.Fatalf("with a priority speaker, got %d", out[0])
	}
	// The soundboard, turned down to 40%; it ends by itself.
	e.speakers = map[string]*speaker{}
	e.set.SoundboardCut = 60
	e.sounds["c"] = &soundTrack{pcm: frame(1000)[:480]}
	e.mix(out)
	if out[0] != 400 || out[479] != 400 || out[480] != 0 {
		t.Fatalf("soundboard: %d %d %d", out[0], out[479], out[480])
	}
	if len(e.sounds) != 0 {
		t.Fatal("a finished sound is dropped")
	}
	if (Settings{}).VolumeOf("x") != 100 || (Settings{}).SoundboardVolume() != 100 {
		t.Fatal("full volume unless changed")
	}
}

func TestPreviewJPEG(t *testing.T) {
	pix := make([]byte, 1280*720*4)
	jpg := previewJPEG(pix, 1280, 720)
	img, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil || img.Bounds().Dx() != 320 || img.Bounds().Dy() != 180 {
		t.Fatalf("preview: %v %v", err, img)
	}
	if len(jpg) > 20<<10 {
		t.Fatalf("previews are small, this one is %d bytes", len(jpg))
	}
}

// TestOverlayCamera checks that the camera lands in the chosen corner.
func TestOverlayCamera(t *testing.T) {
	w, h := 400, 300
	cam := camera.Frame{Pix: make([]byte, 40*30*4), W: 40, H: 30}
	for i := range cam.Pix {
		cam.Pix[i] = 200
	}
	at := func(pix []byte, x, y int) byte { return pix[(y*w+x)*4] }
	for _, c := range []struct {
		corner string
		x, y   int
	}{{"top-left", 10, 10}, {"top-right", w - 10, 10}, {"bottom-left", 10, h - 10}, {"", w - 10, h - 10}} {
		pix := make([]byte, w*h*4)
		overlayCamera(pix, w, h, cam, c.corner)
		if at(pix, c.x, c.y) != 200 || at(pix, w/2, h/2) != 0 {
			t.Fatalf("%q: camera not in its corner", c.corner)
		}
	}
}
