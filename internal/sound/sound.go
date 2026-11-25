// Package sound records from the microphone and plays to the speakers, with
// Nexus's own code: Windows' sound system (WASAPI) on Windows, ALSA
// on Linux. No sound library is used.
//
// Sound is always 48,000 samples a second, one channel, 16 bits: the same
// as Opus uses, so nothing needs converting (Windows converts for us).
package sound

import "errors"

const (
	Rate  = 48000
	Frame = 960 // This is 20 ms.
)

// Device is a microphone or a speaker.
type Device struct {
	ID      string // The ID "" means the computer's default.
	Name    string
	Default bool
}

// ErrNoDevice means there is no microphone or speaker to use.
var ErrNoDevice = errors.New("no sound device found")

// Source fills a frame of sound to play. It is called from the playback
// thread, every 20 ms; it must not block.
type Source func(out []int16)
