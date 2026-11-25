// Package opus squeezes voice for sending and unpacks it again, using Opus,
// the IETF's voice format (RFC 6716). The C code in libopus/ is the
// reference library from opus-codec.org (version 1.6.1, BSD licence: see
// libopus/COPYING); Go builds it as part of Nexus, so nothing is installed.
//
// Voice is 48,000 samples a second, one channel (mono), in frames of 20 ms
// (960 samples).
package opus

/*
#cgo CFLAGS: -O2 -DOPUS_BUILD -DUSE_ALLOCA -DHAVE_LRINTF -DHAVE_LRINT
#cgo CFLAGS: -I${SRCDIR}/libopus/include -I${SRCDIR}/libopus/celt -I${SRCDIR}/libopus/silk -I${SRCDIR}/libopus/silk/float -I${SRCDIR}/libopus/src
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable
#cgo LDFLAGS: -lm
#include <stdlib.h>
#include "opus.h"

static int enc_ctl_int(OpusEncoder *e, int req, int v) { return opus_encoder_ctl(e, req, v); }
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const (
	SampleRate = 48000
	FrameSize  = 960  // These are the samples in one 20 ms frame.
	MaxPacket  = 1275 // This is the largest Opus packet.
)

func opusErr(code C.int) error {
	return fmt.Errorf("opus: %s", C.GoString(C.opus_strerror(code)))
}

// Encoder turns 20 ms of voice into one small packet.
type Encoder struct{ e *C.OpusEncoder }

// NewEncoder makes an encoder for speech at bitrate bits a second
// (0 = Opus chooses).
func NewEncoder(bitrate int) (*Encoder, error) {
	var code C.int
	e := C.opus_encoder_create(SampleRate, 1, C.OPUS_APPLICATION_VOIP, &code)
	if code != C.OPUS_OK {
		return nil, opusErr(code)
	}
	if bitrate > 0 {
		C.enc_ctl_int(e, C.OPUS_SET_BITRATE_REQUEST, C.int(bitrate))
	}
	C.enc_ctl_int(e, C.OPUS_SET_INBAND_FEC_REQUEST, 1)       // This lets the other side recover lost packets.
	C.enc_ctl_int(e, C.OPUS_SET_PACKET_LOSS_PERC_REQUEST, 5) // Expect a little loss.
	return &Encoder{e}, nil
}

// Encode squeezes one frame (FrameSize samples) into a packet.
func (enc *Encoder) Encode(pcm []int16) ([]byte, error) {
	if len(pcm) != FrameSize {
		return nil, errors.New("opus: a frame is 960 samples")
	}
	out := make([]byte, MaxPacket)
	n := C.opus_encode(enc.e, (*C.opus_int16)(unsafe.Pointer(&pcm[0])), FrameSize,
		(*C.uchar)(unsafe.Pointer(&out[0])), MaxPacket)
	if n < 0 {
		return nil, opusErr(n)
	}
	return out[:n], nil
}

// Close frees the encoder.
func (enc *Encoder) Close() {
	if enc.e != nil {
		C.opus_encoder_destroy(enc.e)
		enc.e = nil
	}
}

// Decoder turns packets back into voice.
type Decoder struct{ d *C.OpusDecoder }

// NewDecoder makes a decoder for one person's voice (48,000 samples a
// second, one channel).
func NewDecoder() (*Decoder, error) {
	var code C.int
	d := C.opus_decoder_create(SampleRate, 1, &code)
	if code != C.OPUS_OK {
		return nil, opusErr(code)
	}
	return &Decoder{d}, nil
}

// Decode unpacks one packet into a frame. A nil packet means it was lost:
// Opus fills the gap with a good guess.
func (dec *Decoder) Decode(packet []byte) ([]int16, error) {
	pcm := make([]int16, FrameSize)
	var data *C.uchar
	if len(packet) > 0 {
		data = (*C.uchar)(unsafe.Pointer(&packet[0]))
	}
	n := C.opus_decode(dec.d, data, C.opus_int32(len(packet)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])), FrameSize, 0)
	if n < 0 {
		return nil, opusErr(n)
	}
	return pcm[:n], nil
}

// DecodeAny unpacks a packet of any length Opus allows (up to 120 ms), as
// found in Ogg Opus files.
func (dec *Decoder) DecodeAny(packet []byte) ([]int16, error) {
	if len(packet) == 0 {
		return nil, errors.New("opus: empty packet")
	}
	const most = 5760 // This is 120 ms.
	pcm := make([]int16, most)
	n := C.opus_decode(dec.d, (*C.uchar)(unsafe.Pointer(&packet[0])), C.opus_int32(len(packet)),
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])), most, 0)
	if n < 0 {
		return nil, opusErr(n)
	}
	return pcm[:n], nil
}

func (dec *Decoder) Close() {
	if dec.d != nil {
		C.opus_decoder_destroy(dec.d)
		dec.d = nil
	}
}
