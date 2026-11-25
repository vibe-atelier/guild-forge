//go:build linux && cgo

package sound

// On Linux, sound uses ALSA, the Linux sound system, through its own library (libasound,
// part of every Linux desktop). Building needs its headers:
//   sudo apt install libasound2-dev

/*
#cgo LDFLAGS: -lasound
#include <alsa/asoundlib.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

func alsaErr(code C.int, what string) error {
	return fmt.Errorf("%s: %s", what, C.GoString(C.snd_strerror(code)))
}

// list reads ALSA's device hints; dir is "Input" or "Output".
func list(dir string) ([]Device, error) {
	iface := C.CString("pcm")
	defer C.free(unsafe.Pointer(iface))
	var hints *unsafe.Pointer
	if r := C.snd_device_name_hint(-1, iface, &hints); r < 0 {
		return nil, alsaErr(r, "listing the sound devices")
	}
	defer C.snd_device_name_free_hint(hints)
	get := func(h unsafe.Pointer, key string) string {
		k := C.CString(key)
		defer C.free(unsafe.Pointer(k))
		v := C.snd_device_name_get_hint(h, k)
		if v == nil {
			return ""
		}
		defer C.free(unsafe.Pointer(v))
		return C.GoString(v)
	}
	out := []Device{{ID: "", Name: "Default", Default: true}}
	for p := hints; *p != nil; p = (*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(p), unsafe.Sizeof(*p))) {
		name, io := get(*p, "NAME"), get(*p, "IOID")
		if name == "" || name == "null" || (io != "" && io != dir) {
			continue
		}
		// Only real cards and the sound servers, not every plug-in.
		if !strings.HasPrefix(name, "default:") && !strings.HasPrefix(name, "sysdefault:") &&
			name != "pulse" && name != "pipewire" {
			continue
		}
		desc := strings.SplitN(get(*p, "DESC"), "\n", 2)[0]
		out = append(out, Device{ID: name, Name: desc})
	}
	return out, nil
}

// Inputs lists the microphones; Outputs lists the speakers.
func Inputs() ([]Device, error)  { return list("Input") }
func Outputs() ([]Device, error) { return list("Output") }

// open opens a device for 48 kHz mono 16-bit sound, about 60 ms of latency.
func open(id string, stream C.snd_pcm_stream_t) (*C.snd_pcm_t, error) {
	if id == "" {
		id = "default"
	}
	name := C.CString(id)
	defer C.free(unsafe.Pointer(name))
	var h *C.snd_pcm_t
	if r := C.snd_pcm_open(&h, name, stream, 0); r < 0 {
		return nil, ErrNoDevice
	}
	if r := C.snd_pcm_set_params(h, C.SND_PCM_FORMAT_S16_LE, C.SND_PCM_ACCESS_RW_INTERLEAVED,
		1, Rate, 1, 60000); r < 0 {
		C.snd_pcm_close(h)
		return nil, alsaErr(r, "setting up the device")
	}
	return h, nil
}

// Capture records from a microphone.
type Capture struct {
	frames chan []int16
	stop   chan struct{}
	once   sync.Once
}

// OpenCapture starts recording from microphone id ("" = the default).
func OpenCapture(id string) (*Capture, error) {
	h, err := open(id, C.SND_PCM_STREAM_CAPTURE)
	if err != nil {
		return nil, err
	}
	c := &Capture{frames: make(chan []int16, 25), stop: make(chan struct{})}
	go func() {
		defer C.snd_pcm_close(h)
		defer close(c.frames)
		for {
			select {
			case <-c.stop:
				return
			default:
			}
			f := make([]int16, Frame)
			n := C.snd_pcm_readi(h, unsafe.Pointer(&f[0]), Frame)
			if n < 0 {
				if C.snd_pcm_recover(h, C.int(n), 1) < 0 {
					return
				}
				continue
			}
			select {
			case c.frames <- f[:int(n)]:
			default:
			}
		}
	}()
	return c, nil
}

// Frames delivers the recorded sound, 20 ms at a time; Close stops it.
func (c *Capture) Frames() <-chan []int16 { return c.frames }
func (c *Capture) Close()                 { c.once.Do(func() { close(c.stop) }) }

// Playback plays to a speaker.
type Playback struct {
	stop chan struct{}
	once sync.Once
}

// OpenPlayback starts playing to speaker id ("" = the default); src is
// asked for 20 ms at a time. ALSA's write waits while the speaker is busy,
// which paces it.
func OpenPlayback(id string, src Source) (*Playback, error) {
	h, err := open(id, C.SND_PCM_STREAM_PLAYBACK)
	if err != nil {
		return nil, err
	}
	p := &Playback{stop: make(chan struct{})}
	go func() {
		defer C.snd_pcm_close(h)
		f := make([]int16, Frame)
		for {
			select {
			case <-p.stop:
				C.snd_pcm_drop(h)
				return
			default:
			}
			src(f)
			if n := C.snd_pcm_writei(h, unsafe.Pointer(&f[0]), Frame); n < 0 {
				if C.snd_pcm_recover(h, C.int(n), 1) < 0 {
					return
				}
			}
		}
	}()
	return p, nil
}

func (p *Playback) Close() { p.once.Do(func() { close(p.stop) }) }

// OpenLoopback fails here: recording what the speakers play is not available yet.
func OpenLoopback(string) (*Capture, error) { return nil, ErrNoDevice }
