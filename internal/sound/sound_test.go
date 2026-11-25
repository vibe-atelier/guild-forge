package sound

import (
	"os"
	"testing"
	"time"
)

// TestDevices lists the microphones and speakers (nothing is recorded).
func TestDevices(t *testing.T) {
	in, err := Inputs()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Outputs()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range append(in, out...) {
		t.Logf("%q default=%v", d.Name, d.Default)
	}
}

// TestStreams opens the default microphone and speaker for half a second:
// it plays silence and only counts the recorded frames (nothing is kept).
// It runs only with NEXUS_SOUND_TEST=1, since it uses the real devices.
func TestStreams(t *testing.T) {
	if os.Getenv("NEXUS_SOUND_TEST") != "1" {
		t.Skip("set NEXUS_SOUND_TEST=1 to use the real devices")
	}
	asked := 0
	p, err := OpenPlayback("", func(out []int16) {
		for i := range out {
			out[i] = 0
		}
		asked++
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := OpenCapture("")
	if err != nil {
		t.Fatal(err)
	}
	got := 0
	deadline := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case f := <-c.Frames():
			if len(f) != Frame {
				t.Fatalf("frame of %d", len(f))
			}
			got++
		case <-deadline:
			break loop
		}
	}
	c.Close()
	p.Close()
	t.Logf("recorded %d frames, played %d", got, asked)
	if got < 15 || asked < 15 {
		t.Fatalf("expected about 25 frames each way, got %d recorded and %d played", got, asked)
	}
}

// TestLoopback records what the speakers play for half a second (nothing is
// kept). Windows sends nothing while nothing plays, so frames may be few.
func TestLoopback(t *testing.T) {
	if os.Getenv("NEXUS_SOUND_TEST") != "1" {
		t.Skip("set NEXUS_SOUND_TEST=1 to use the real devices")
	}
	c, err := OpenLoopback("")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	c.Close()
}
