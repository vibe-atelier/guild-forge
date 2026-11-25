// Package camera reads a webcam: pictures about 640×480,
// 30 a second, as BGRA. Windows uses Media Foundation, which is part of
// Windows, reached with plain system calls; other systems come later.
package camera

import "errors"

// Device is a camera.
type Device struct {
	ID   string // It is Windows' link to the camera.
	Name string
}

// Frame is one picture: BGRA, top row first.
type Frame struct {
	Pix  []byte
	W, H int
}

// Want is the size asked of the camera (480p).
const WantW, WantH = 640, 480

// Demo makes the package use a made-up camera, so the guide's pictures
// and tests never turn a real camera on.
var Demo bool

var errNoCamera = errors.New("no camera was found")

// Camera is an open camera. Frames are dropped when nobody takes them.
type Camera struct {
	frames chan Frame
	stop   chan struct{}
	done   chan struct{}
}

// Frames gives the pictures as they come; it is closed when the camera stops.
func (c *Camera) Frames() <-chan Frame { return c.frames }

// Close turns the camera off.
func (c *Camera) Close() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	<-c.done
}

// Devices lists the cameras.
func Devices() ([]Device, error) {
	if Demo {
		return []Device{{ID: "demo", Name: "Demo Camera"}}, nil
	}
	return devices()
}

// Open turns a camera on ("" for the first one).
func Open(id string) (*Camera, error) {
	if Demo {
		return openDemo(), nil
	}
	return open(id)
}

func newCamera() *Camera {
	return &Camera{frames: make(chan Frame, 2), stop: make(chan struct{}), done: make(chan struct{})}
}

// send hands a picture on, dropping it if the last one wasn't taken yet.
func (c *Camera) send(f Frame) {
	select {
	case c.frames <- f:
	default:
	}
}
