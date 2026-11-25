package voice

// This file runs cameras in voice channels: your camera at 480p,
// 30 pictures a second, squeezed with VP8 and sent to everyone in the
// channel; others' cameras unpacked for their tiles; and, while you share
// your screen, your camera drawn into the corner you choose.

import (
	"image"
	"sync"
	"sync/atomic"

	"nexus/internal/camera"
	"nexus/internal/vp8"
)

const (
	cameraLines = 480 // Cameras send 480 lines.
	cameraKbps  = 800
)

// These are the corners for the camera while sharing, as saved in the settings.
var Corners = []string{"top-left", "top-right", "bottom-left", "bottom-right"}

type cameraState struct {
	cam  *camera.Camera
	done sync.WaitGroup
	key  atomic.Bool // Someone new arrived, so send a full picture.

	mu     sync.Mutex
	latest camera.Frame // It is for your own tile and the corner.
}

type peerCamera struct {
	dec *vp8.Decoder
	img *image.YCbCr
}

// StartCamera turns your camera on in your voice channel.
func (e *Engine) StartCamera() error {
	e.mu.Lock()
	gc, channel, busy, device := e.conn, e.channel, e.cam != nil, e.set.Camera
	e.mu.Unlock()
	if gc == nil {
		return errNotInVoice
	}
	if busy {
		return nil
	}
	cam, err := camera.Open(device)
	if err != nil {
		return err
	}
	if err := gc.CameraStart(); err != nil {
		cam.Close()
		return err
	}
	cs := &cameraState{cam: cam}
	cs.key.Store(true)
	e.mu.Lock()
	e.cam = cs
	e.mu.Unlock()
	cs.done.Add(1)
	go func() {
		defer cs.done.Done()
		var enc *vp8.Encoder
		defer func() {
			if enc != nil {
				enc.Close()
			}
		}()
		var seq uint32
		for f := range cam.Frames() {
			cs.mu.Lock()
			cs.latest = f
			cs.mu.Unlock()
			if enc == nil {
				w, h := fitSize(f.W, f.H, cameraLines)
				if enc, err = vp8.NewEncoder(w, h, 30, cameraKbps); err != nil {
					return
				}
			}
			pkt, key, err := enc.Encode(vp8.NewBGRA(f.Pix, f.W, f.H), cs.key.Swap(false))
			if err != nil || len(pkt) == 0 {
				continue
			}
			seq++
			gc.SendStream(channel, "camera", key, seq, pkt)
		}
	}()
	e.changed()
	return nil
}

// StopCamera turns your camera off.
func (e *Engine) StopCamera() {
	e.mu.Lock()
	cs, gc := e.cam, e.conn
	e.cam = nil
	e.mu.Unlock()
	if cs == nil {
		return
	}
	cs.cam.Close()
	cs.done.Wait()
	if gc != nil {
		gc.CameraStop()
	}
	e.changed()
}

// CameraOn says whether your camera is on.
func (e *Engine) CameraOn() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cam != nil
}

// cameraKey is called when someone new arrives: the next picture is a full one.
func (e *Engine) cameraKey(guildID string) {
	e.mu.Lock()
	if e.cam != nil && guildID == e.guild {
		e.cam.key.Store(true)
	}
	e.mu.Unlock()
}

// ownCamera is your camera's newest picture (empty before the first).
func (e *Engine) ownCamera() camera.Frame {
	e.mu.Lock()
	cs := e.cam
	e.mu.Unlock()
	if cs == nil {
		return camera.Frame{}
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.latest
}

// CameraImage is someone's newest camera picture (nil when none): your own
// for your own user ID.
func (e *Engine) CameraImage(user, me string) image.Image {
	if user == me {
		f := e.ownCamera()
		if f.Pix == nil {
			return nil
		}
		img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
		for i := 0; i+3 < len(f.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = f.Pix[i+2], f.Pix[i+1], f.Pix[i], 255
		}
		return img
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if p := e.peerCams[user]; p != nil && p.img != nil {
		return p.img
	}
	return nil
}

// gotCamera unpacks someone's camera picture.
func (e *Engine) gotCamera(guildID, from string, key bool, data []byte) {
	e.mu.Lock()
	if e.peerCams == nil || guildID != e.guild {
		e.mu.Unlock()
		return
	}
	p := e.peerCams[from]
	if p == nil {
		if !key {
			e.mu.Unlock()
			return // Wait for a full picture to start from.
		}
		dec, err := vp8.NewDecoder()
		if err != nil {
			e.mu.Unlock()
			return
		}
		p = &peerCamera{dec: dec}
		e.peerCams[from] = p
	}
	e.mu.Unlock()
	img, err := p.dec.Decode(data)
	if err != nil || img == nil {
		return
	}
	e.mu.Lock()
	p.img = img
	e.mu.Unlock()
}

// closePeerCams frees the decoders (on leaving or moving).
func (e *Engine) closePeerCamsLocked() {
	for _, p := range e.peerCams {
		p.dec.Close()
	}
	e.peerCams = map[string]*peerCamera{}
}

// overlayCamera draws your camera into a corner of a shared picture:
// a quarter of its width, a little in from the edges.
func overlayCamera(pix []byte, w, h int, f camera.Frame, corner string) {
	if f.Pix == nil || w < 64 || h < 64 {
		return
	}
	cw := w / 4
	ch := cw * f.H / f.W
	margin := w / 60
	x0, y0 := w-cw-margin, h-ch-margin // The corner is bottom-right unless another is chosen.
	switch corner {
	case "top-left":
		x0, y0 = margin, margin
	case "top-right":
		y0 = margin
	case "bottom-left":
		x0 = margin
	}
	if x0 < 0 || y0 < 0 {
		return
	}
	for y := 0; y < ch; y++ {
		sy := y * f.H / ch
		for x := 0; x < cw; x++ {
			sx := x * f.W / cw
			i := ((y0+y)*w + x0 + x) * 4
			j := (sy*f.W + sx) * 4
			copy(pix[i:i+4], f.Pix[j:j+4])
		}
	}
}
