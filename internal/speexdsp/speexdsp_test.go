package speexdsp

import (
	"math"
	"math/rand"
	"testing"
)

const frame, rate = 960, 48000

func power(f []int16) float64 {
	s := 0.0
	for _, v := range f {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(f)))
}

// TestEchoCancel checks that what the speakers play, heard again by the microphone a
// little later, is mostly taken out.
func TestEchoCancel(t *testing.T) {
	p, err := New(frame, rate, Options{EchoCancel: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := rand.New(rand.NewSource(1))
	// Real sound hardware takes at least 40 ms from speaker to microphone;
	// SpeexDSP expects that. 50 ms here.
	const delay = 2*frame + 480
	var played []int16
	var before, after float64
	for f := 0; f < 300; f++ { // This is six seconds.
		out := make([]int16, frame)
		for i := range out {
			out[i] = int16(r.NormFloat64() * 3000) // This is speech-like noise from the call.
		}
		played = append(played, out...)
		p.Playback(out)
		mic := make([]int16, frame)
		for i := range mic { // The room adds a quieter, later copy.
			if j := f*frame + i - delay; j >= 0 {
				mic[i] = int16(float64(played[j]) * 0.5)
			}
		}
		in := power(mic)
		p.Capture(mic)
		if f >= 200 { // This measures after it has learned the room.
			before += in
			after += power(mic)
		}
	}
	t.Logf("echo before %.0f, after %.0f", before/100, after/100)
	if after > before/4 {
		t.Fatalf("the echo should drop to under a quarter (before %.0f, after %.0f)", before/100, after/100)
	}
}

// TestDenoise checks that steady hiss is turned down.
func TestDenoise(t *testing.T) {
	p, err := New(frame, rate, Options{Denoise: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := rand.New(rand.NewSource(2))
	var before, after float64
	for f := 0; f < 150; f++ {
		mic := make([]int16, frame)
		for i := range mic {
			mic[i] = int16(r.NormFloat64() * 500)
		}
		in := power(mic)
		p.Capture(mic)
		if f >= 100 {
			before += in
			after += power(mic)
		}
	}
	t.Logf("hiss before %.0f, after %.0f", before/50, after/50)
	if after > before/2 {
		t.Fatalf("steady hiss should be turned down (before %.0f, after %.0f)", before/50, after/50)
	}
}
