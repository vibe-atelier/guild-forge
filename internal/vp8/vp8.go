// Package vp8 squeezes shared screens for sending and unpacks them again,
// using VP8 (RFC 6386). The C code in libvpx/ is the reference
// library from the WebM Project (version 1.17.0, BSD licence: see
// libvpx/LICENSE and libvpx/PATENTS). Its settings files were made by
// libvpx's own configure script: VP8 only, real time, no threads.
//
// On 64-bit Windows (libvpx/config_x86_64) it also uses libvpx's fast
// routines for x86 processors, picked at startup by what the processor can
// do: the c_x86_*.c files, and the assembly ones, put together by NASM into
// vpxasm_windows_amd64.syso (build.ps1 makes it again). Elsewhere it is plain
// C (libvpx/config).
package vp8

/*
#cgo CFLAGS: -O2 -I${SRCDIR}/libvpx
#cgo windows,amd64 CFLAGS: -I${SRCDIR}/libvpx/config_x86_64
#cgo !windows CFLAGS: -I${SRCDIR}/libvpx/config
#cgo windows,!amd64 CFLAGS: -I${SRCDIR}/libvpx/config
#cgo CFLAGS: -Wno-unused-function -Wno-unused-variable -Wno-unused-but-set-variable
#cgo LDFLAGS: -lm
#include <stdlib.h>
#include <string.h>
#include "vpx/vpx_encoder.h"
#include "vpx/vpx_decoder.h"
#include "vpx/vp8cx.h"
#include "vpx/vp8dx.h"

static int enc_open(vpx_codec_ctx_t *ctx, int w, int h, int fps, int kbps) {
	vpx_codec_enc_cfg_t cfg;
	if (vpx_codec_enc_config_default(vpx_codec_vp8_cx(), &cfg, 0)) return -1;
	cfg.g_w = w;
	cfg.g_h = h;
	cfg.g_timebase.num = 1;
	cfg.g_timebase.den = fps;
	cfg.rc_target_bitrate = kbps;
	cfg.rc_end_usage = VPX_CBR;
	cfg.g_lag_in_frames = 0;       // send each frame at once
	cfg.g_error_resilient = 1;     // a lost frame spoils less
	cfg.g_threads = 1;
	cfg.kf_mode = VPX_KF_AUTO;
	cfg.kf_max_dist = fps * 5;     // a full picture at least every 5 seconds
	cfg.rc_min_quantizer = 4;
	cfg.rc_max_quantizer = 56;
	cfg.rc_buf_initial_sz = 500;
	cfg.rc_buf_optimal_sz = 600;
	cfg.rc_buf_sz = 1000;
	if (vpx_codec_enc_init(ctx, vpx_codec_vp8_cx(), &cfg, 0)) return -2;
	vpx_codec_control(ctx, VP8E_SET_CPUUSED, 12);           // fast enough for live
	vpx_codec_control(ctx, VP8E_SET_SCREEN_CONTENT_MODE, 1); // screens: text and windows
	vpx_codec_control(ctx, VP8E_SET_STATIC_THRESHOLD, 100);  // skip parts that didn't change
	return 0;
}

// enc_frame encodes one I420 picture; the packet is copied to out.
static int enc_frame(vpx_codec_ctx_t *ctx, unsigned char *yuv, int w, int h, long long pts,
		int force_key, unsigned char *out, int out_max, int *key) {
	vpx_image_t img;
	if (!vpx_img_wrap(&img, VPX_IMG_FMT_I420, w, h, 1, yuv)) return -1;
	if (vpx_codec_encode(ctx, &img, pts, 1, force_key ? VPX_EFLAG_FORCE_KF : 0, VPX_DL_REALTIME)) return -2;
	vpx_codec_iter_t iter = NULL;
	const vpx_codec_cx_pkt_t *pkt;
	int n = 0;
	*key = 0;
	while ((pkt = vpx_codec_get_cx_data(ctx, &iter)) != NULL) {
		if (pkt->kind != VPX_CODEC_CX_FRAME_PKT) continue;
		if ((int)pkt->data.frame.sz + n > out_max) return -3;
		memcpy(out + n, pkt->data.frame.buf, pkt->data.frame.sz);
		n += (int)pkt->data.frame.sz;
		if (pkt->data.frame.flags & VPX_FRAME_IS_KEY) *key = 1;
	}
	return n;
}

static int dec_open(vpx_codec_ctx_t *ctx) {
	return vpx_codec_dec_init(ctx, vpx_codec_vp8_dx(), NULL, 0) ? -1 : 0;
}

// dec_frame decodes a packet; the picture's planes are returned.
static int dec_frame(vpx_codec_ctx_t *ctx, unsigned char *data, int n, vpx_image_t **out) {
	if (vpx_codec_decode(ctx, data, n, NULL, 0)) return -1;
	vpx_codec_iter_t iter = NULL;
	*out = vpx_codec_get_frame(ctx, &iter);
	return *out ? 1 : 0;
}
*/
import "C"

import (
	"errors"
	"image"
	"unsafe"
)

// Encoder turns pictures of the screen into VP8 packets.
type Encoder struct {
	ctx   C.vpx_codec_ctx_t
	w, h  int
	pts   int64
	yuv   []byte
	out   []byte
	ready bool
}

// NewEncoder makes an encoder for w×h pictures (even numbers), fps frames
// a second, at about kbps kilobits a second.
func NewEncoder(w, h, fps, kbps int) (*Encoder, error) {
	if w <= 0 || h <= 0 || w%2 != 0 || h%2 != 0 {
		return nil, errors.New("vp8: the size must be even")
	}
	e := &Encoder{w: w, h: h, yuv: make([]byte, w*h*3/2), out: make([]byte, w*h*2+100000)}
	if C.enc_open(&e.ctx, C.int(w), C.int(h), C.int(fps), C.int(kbps)) != 0 {
		return nil, errors.New("vp8: could not start the encoder")
	}
	e.ready = true
	return e, nil
}

// Encode squeezes one picture (any size: it is scaled to the encoder's).
// key forces a full picture (for someone who just started watching). It
// returns the packet, and whether it is a full picture.
func (e *Encoder) Encode(src image.Image, key bool) ([]byte, bool, error) {
	if !e.ready {
		return nil, false, errors.New("vp8: closed")
	}
	toI420(src, e.w, e.h, e.yuv)
	var isKey C.int
	k := 0
	if key {
		k = 1
	}
	n := C.enc_frame(&e.ctx, (*C.uchar)(unsafe.Pointer(&e.yuv[0])), C.int(e.w), C.int(e.h), C.longlong(e.pts),
		C.int(k), (*C.uchar)(unsafe.Pointer(&e.out[0])), C.int(len(e.out)), &isKey)
	e.pts++
	if n < 0 {
		return nil, false, errors.New("vp8: encoding failed")
	}
	return append([]byte(nil), e.out[:n]...), isKey != 0, nil
}

// Close frees the encoder.
func (e *Encoder) Close() {
	if e.ready {
		C.vpx_codec_destroy(&e.ctx)
		e.ready = false
	}
}

// Decoder turns VP8 packets back into pictures.
type Decoder struct {
	ctx   C.vpx_codec_ctx_t
	ready bool
}

// NewDecoder makes a decoder for one shared screen or camera.
func NewDecoder() (*Decoder, error) {
	d := &Decoder{}
	if C.dec_open(&d.ctx) != 0 {
		return nil, errors.New("vp8: could not start the decoder")
	}
	d.ready = true
	return d, nil
}

// Decode unpacks one packet. It returns nil (and no error) when the packet
// held no picture.
func (d *Decoder) Decode(pkt []byte) (*image.YCbCr, error) {
	if !d.ready || len(pkt) == 0 {
		return nil, errors.New("vp8: nothing to decode")
	}
	var img *C.vpx_image_t
	r := C.dec_frame(&d.ctx, (*C.uchar)(unsafe.Pointer(&pkt[0])), C.int(len(pkt)), &img)
	if r < 0 {
		return nil, errors.New("vp8: a damaged picture")
	}
	if r == 0 || img == nil {
		return nil, nil
	}
	w, h := int(img.d_w), int(img.d_h)
	out := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	plane := func(i int, dst []byte, stride, rows, cols int) {
		src := unsafe.Pointer(img.planes[i])
		sstride := int(img.stride[i])
		for y := 0; y < rows; y++ {
			copy(dst[y*stride:y*stride+cols], unsafe.Slice((*byte)(unsafe.Add(src, y*sstride)), cols))
		}
	}
	plane(0, out.Y, out.YStride, h, w)
	plane(1, out.Cb, out.CStride, (h+1)/2, (w+1)/2)
	plane(2, out.Cr, out.CStride, (h+1)/2, (w+1)/2)
	return out, nil
}

func (d *Decoder) Close() {
	if d.ready {
		C.vpx_codec_destroy(&d.ctx)
		d.ready = false
	}
}
