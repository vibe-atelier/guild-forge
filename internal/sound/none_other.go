//go:build !windows && !(linux && cgo)

package sound

// No sound code for this system (or a Linux build without C): voice is off.

// Inputs and Outputs find no devices here.
func Inputs() ([]Device, error)  { return nil, ErrNoDevice }
func Outputs() ([]Device, error) { return nil, ErrNoDevice }

// Capture would record sound; here it can't be opened.
type Capture struct{}

func OpenCapture(string) (*Capture, error) { return nil, ErrNoDevice }
func (c *Capture) Frames() <-chan []int16  { return nil }
func (c *Capture) Close()                  {}

// Playback would play sound; here it can't be opened.
type Playback struct{}

func OpenPlayback(string, Source) (*Playback, error) { return nil, ErrNoDevice }
func (p *Playback) Close()                           {}

// OpenLoopback fails here: recording what the speakers play is not available yet.
func OpenLoopback(string) (*Capture, error) { return nil, ErrNoDevice }
