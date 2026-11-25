package opus

import (
	"math"
	"testing"
)

// TestRoundTrip checks that a tone survives being squeezed and unpacked.
func TestRoundTrip(t *testing.T) {
	enc, err := NewEncoder(32000)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	dec, err := NewDecoder()
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()

	var in, out []float64
	total := 0
	for f := 0; f < 50; f++ { // This is one second.
		pcm := make([]int16, FrameSize)
		for i := range pcm {
			s := math.Sin(2 * math.Pi * 440 * float64(f*FrameSize+i) / SampleRate)
			pcm[i] = int16(s * 8000)
			in = append(in, s*8000)
		}
		pkt, err := enc.Encode(pcm)
		if err != nil {
			t.Fatal(err)
		}
		total += len(pkt)
		got, err := dec.Decode(pkt)
		if err != nil || len(got) != FrameSize {
			t.Fatalf("decode: %d %v", len(got), err)
		}
		for _, v := range got {
			out = append(out, float64(v))
		}
	}
	if total > 6000 {
		t.Fatalf("one second took %d bytes; expected about 4000", total)
	}
	// The tone's loudness comes back (Opus delays it a little, so compare power).
	power := func(x []float64) float64 {
		s := 0.0
		for _, v := range x[len(x)/2:] {
			s += v * v
		}
		return math.Sqrt(s / float64(len(x)/2))
	}
	if p, q := power(in), power(out); q < p*0.7 || q > p*1.3 {
		t.Fatalf("loudness in %.0f, out %.0f", p, q)
	}
	// A lost packet is filled in.
	if got, err := dec.Decode(nil); err != nil || len(got) != FrameSize {
		t.Fatalf("lost packet: %d %v", len(got), err)
	}
}
