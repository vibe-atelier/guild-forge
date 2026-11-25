package rnnoise

import (
	"math"
	"math/rand"
	"os"
	"testing"
)

func power(f []int16) float64 {
	s := 0.0
	for _, v := range f {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(f)))
}

// TestNoise checks that steady noise is turned down hard. It needs the weights
// file, made by tools/rnnoise-weights into models/.
func TestNoise(t *testing.T) {
	data, err := os.ReadFile("../../models/" + WeightsFile)
	if err != nil {
		t.Skip("no weights file in models/")
	}
	m, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	r := rand.New(rand.NewSource(3))
	var before, after float64
	for f := 0; f < 150; f++ { // This makes three seconds of fan-like hiss.
		frame := make([]int16, 960)
		for i := range frame {
			frame[i] = int16(r.NormFloat64() * 800)
		}
		in := power(frame)
		d.Process(frame)
		if f >= 50 {
			before += in
			after += power(frame)
		}
	}
	t.Logf("hiss before %.0f, after %.0f", before/100, after/100)
	if after > before/5 {
		t.Fatalf("hiss should drop to under a fifth (before %.0f, after %.0f)", before/100, after/100)
	}
	if _, err := Load([]byte("not a model")); err == nil {
		t.Fatal("a wrong file is refused")
	}
}
