package camera

import (
	"os"
	"testing"
	"time"
)

// TestDemo checks that the made-up camera gives pictures and stops.
func TestDemo(t *testing.T) {
	Demo = true
	defer func() { Demo = false }()
	c, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-c.Frames():
		if f.W != WantW || f.H != WantH || len(f.Pix) != f.W*f.H*4 {
			t.Fatalf("picture %d×%d, %d bytes", f.W, f.H, len(f.Pix))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no picture")
	}
	c.Close()
}

// TestDevices only lists cameras (no camera light comes on).
func TestDevices(t *testing.T) {
	list, err := Devices()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d cameras", len(list))
}

// TestRealCamera turns the real camera on for a moment; only when asked
// (NEXUS_CAMERA_TEST=1), since it lights the camera.
func TestRealCamera(t *testing.T) {
	if os.Getenv("NEXUS_CAMERA_TEST") != "1" {
		t.Skip("set NEXUS_CAMERA_TEST=1 to try the real camera")
	}
	c, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case f := <-c.Frames():
		t.Logf("picture %d×%d", f.W, f.H)
	case <-time.After(5 * time.Second):
		t.Fatal("no picture from the camera")
	}
}
