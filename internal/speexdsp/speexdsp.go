// Package speexdsp cleans up the microphone before voice is sent:
// echo cancellation (the call coming back from your speakers is taken out),
// noise suppression and automatic volume, using SpeexDSP 1.2.1 from
// Xiph.org (BSD licence: see libspeexdsp/COPYING). Go builds its C files as
// part of Nexus.
package speexdsp

/*
#cgo CFLAGS: -O2 -I${SRCDIR} -I${SRCDIR}/libspeexdsp/include -I${SRCDIR}/libspeexdsp/libspeexdsp
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable
#cgo LDFLAGS: -lm
#include "config.h"
#include "speex/speex_echo.h"
#include "speex/speex_preprocess.h"

static void pre_int(SpeexPreprocessState *st, int req, int v) { speex_preprocess_ctl(st, req, &v); }
static void pre_echo(SpeexPreprocessState *st, SpeexEchoState *e) { speex_preprocess_ctl(st, SPEEX_PREPROCESS_SET_ECHO_STATE, e); }
static void echo_rate(SpeexEchoState *e, int rate) { speex_echo_ctl(e, SPEEX_ECHO_SET_SAMPLING_RATE, &rate); }
*/
import "C"

import (
	"errors"
	"sync"
	"unsafe"
)

// Options says which clean-ups are on.
type Options struct {
	EchoCancel bool // It takes the call (from the speakers) out of the microphone.
	Denoise    bool // It takes steady background noise out.
	AutoGain   bool // It keeps the voice at an even loudness.
}

// Processor cleans one microphone. Playback and Capture may be called from
// different threads.
type Processor struct {
	mu    sync.Mutex
	frame int
	echo  *C.SpeexEchoState
	pre   *C.SpeexPreprocessState
	out   []int16
}

// New makes a processor for frames of frame samples at rate samples a
// second.
func New(frame, rate int, o Options) (*Processor, error) {
	p := &Processor{frame: frame, out: make([]int16, frame)}
	if o.EchoCancel {
		// An echo up to a quarter of a second late is cancelled.
		p.echo = C.speex_echo_state_init(C.int(frame), C.int(rate/4))
		if p.echo == nil {
			return nil, errors.New("speexdsp: could not start echo cancellation")
		}
		C.echo_rate(p.echo, C.int(rate))
	}
	if o.EchoCancel || o.Denoise || o.AutoGain {
		p.pre = C.speex_preprocess_state_init(C.int(frame), C.int(rate))
		if p.pre == nil {
			p.Close()
			return nil, errors.New("speexdsp: could not start noise suppression")
		}
		den := 0
		if o.Denoise {
			den = 1
		}
		C.pre_int(p.pre, C.SPEEX_PREPROCESS_SET_DENOISE, C.int(den))
		C.pre_int(p.pre, C.SPEEX_PREPROCESS_SET_NOISE_SUPPRESS, -30) // The value is in dB.
		agc := 0
		if o.AutoGain {
			agc = 1
		}
		C.pre_int(p.pre, C.SPEEX_PREPROCESS_SET_AGC, C.int(agc))
		C.pre_int(p.pre, C.SPEEX_PREPROCESS_SET_DEREVERB, 0)
		if p.echo != nil {
			C.pre_echo(p.pre, p.echo) // This also hides what echo is left.
		}
	}
	return p, nil
}

// Playback tells the echo canceller what is going to the speakers.
func (p *Processor) Playback(frame []int16) {
	if p == nil || p.echo == nil || len(frame) != p.frame {
		return
	}
	p.mu.Lock()
	C.speex_echo_playback(p.echo, (*C.spx_int16_t)(unsafe.Pointer(&frame[0])))
	p.mu.Unlock()
}

// Capture cleans one microphone frame in place.
func (p *Processor) Capture(frame []int16) {
	if p == nil || len(frame) != p.frame {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.echo != nil {
		C.speex_echo_capture(p.echo, (*C.spx_int16_t)(unsafe.Pointer(&frame[0])), (*C.spx_int16_t)(unsafe.Pointer(&p.out[0])))
		copy(frame, p.out)
	}
	if p.pre != nil {
		C.speex_preprocess_run(p.pre, (*C.spx_int16_t)(unsafe.Pointer(&frame[0])))
	}
}

// Close frees the processor.
func (p *Processor) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pre != nil {
		C.speex_preprocess_state_destroy(p.pre)
		p.pre = nil
	}
	if p.echo != nil {
		C.speex_echo_state_destroy(p.echo)
		p.echo = nil
	}
}

// CaptureWith is Capture with a step in the middle: after echo cancellation,
// before noise suppression and gain. Nexus runs RNNoise there: echo
// cancellation needs the sound unchanged, and the gain should see the cleaned
// voice.
func (p *Processor) CaptureWith(frame []int16, middle func([]int16)) {
	if p == nil {
		if middle != nil {
			middle(frame)
		}
		return
	}
	if len(frame) != p.frame {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.echo != nil {
		C.speex_echo_capture(p.echo, (*C.spx_int16_t)(unsafe.Pointer(&frame[0])), (*C.spx_int16_t)(unsafe.Pointer(&p.out[0])))
		copy(frame, p.out)
	}
	if middle != nil {
		middle(frame)
	}
	if p.pre != nil {
		C.speex_preprocess_run(p.pre, (*C.spx_int16_t)(unsafe.Pointer(&frame[0])))
	}
}
