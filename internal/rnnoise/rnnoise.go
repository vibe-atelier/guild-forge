// Package rnnoise is RNNoise 0.2 from Xiph.org (BSD licence: see
// csrc/COPYING), a noise suppressor trained on recordings of real noise,
// stronger than SpeexDSP's. Go builds its C files as part of Nexus.
//
// Its trained model is read at run time from a weights file (made from
// Xiph's model by tools/rnnoise-weights), so the program doesn't carry it.
package rnnoise

/*
#cgo CFLAGS: -O2 -DHAVE_CONFIG_H -I${SRCDIR} -I${SRCDIR}/csrc -I${SRCDIR}/csrc/include -w
#cgo LDFLAGS: -lm
#include <stdlib.h>
#include <string.h>
#include "rnnoise.h"
*/
import "C"

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"nexus/internal/nexusdata"
)

// FrameSize is RNNoise's step: 10 ms at 48,000 samples a second.
const FrameSize = 480

// WeightsFile is the model's name next to the programs (in models/).
const WeightsFile = "rnnoise.bin"

// Model is a loaded model, shared by every Denoiser.
type Model struct {
	m   *C.RNNModel
	buf unsafe.Pointer // It must stay while the model is used.
}

var (
	loadOnce sync.Once
	loaded   *Model
)

// Default finds and loads the model once: in this PC's nexus-data (where the
// app writes its built-in copy), then models/rnnoise.bin next to the
// program, then in the folder it was started from. It is nil when the model
// isn't there.
func Default() *Model {
	loadOnce.Do(func() {
		places := []string{filepath.Join(nexusdata.Path(nexusdata.ModelsPart), WeightsFile)}
		if exe, err := os.Executable(); err == nil {
			places = append(places, filepath.Join(filepath.Dir(exe), "models", WeightsFile))
		}
		places = append(places, filepath.Join("models", WeightsFile))
		for _, p := range places {
			if data, err := os.ReadFile(p); err == nil {
				if m, err := Load(data); err == nil {
					loaded = m
					return
				}
			}
		}
	})
	return loaded
}

// Load reads a weights file.
func Load(data []byte) (*Model, error) {
	if len(data) == 0 {
		return nil, errors.New("rnnoise: empty weights file")
	}
	buf := C.malloc(C.size_t(len(data)))
	C.memcpy(buf, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	m := C.rnnoise_model_from_buffer(buf, C.int(len(data)))
	if m == nil {
		C.free(buf)
		return nil, errors.New("rnnoise: not a usable weights file")
	}
	// RNNoise reads the numbers only when a denoiser starts: try one now,
	// so a wrong file is refused here.
	trial := C.malloc(C.size_t(C.rnnoise_get_size()))
	ok := C.rnnoise_init((*C.DenoiseState)(trial), m) == 0
	C.free(trial)
	if !ok {
		// This isn't rnnoise_model_free: for a model from a buffer, that closes a file it never set.
		C.free(unsafe.Pointer(m))
		C.free(buf)
		return nil, errors.New("rnnoise: not a usable weights file")
	}
	return &Model{m: m, buf: buf}, nil
}

// Denoiser cleans one stream of sound.
type Denoiser struct {
	mu      sync.Mutex
	st      *C.DenoiseState
	in, out [FrameSize]C.float
}

// New starts a denoiser with a model.
func New(m *Model) (*Denoiser, error) {
	if m == nil {
		return nil, errors.New("rnnoise: no model")
	}
	st := C.rnnoise_create(m.m)
	if st == nil {
		return nil, errors.New("rnnoise: could not start")
	}
	return &Denoiser{st: st}, nil
}

// Process cleans samples in place, 480 at a time (a 20 ms frame of 960 is
// two steps). It returns how likely the last step held a voice (0 to 1).
func (d *Denoiser) Process(pcm []int16) float32 {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.st == nil {
		return 0
	}
	var voice float32
	for start := 0; start+FrameSize <= len(pcm); start += FrameSize {
		part := pcm[start : start+FrameSize]
		for i, v := range part { // RNNoise works on 16-bit-sized numbers.
			d.in[i] = C.float(v)
		}
		voice = float32(C.rnnoise_process_frame(d.st, &d.out[0], &d.in[0]))
		for i := range part {
			v := float32(d.out[i])
			part[i] = int16(max(-32768, min(32767, v)))
		}
	}
	return voice
}

// Close frees the denoiser.
func (d *Denoiser) Close() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.st != nil {
		C.rnnoise_destroy(d.st)
		d.st = nil
	}
}
