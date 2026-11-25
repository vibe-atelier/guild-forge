package voice

// This file runs screen sharing on this computer: taking
// pictures of a screen or window 30 times a second, squeezing them with VP8
// and sending them; the computer's own sound with Opus; and, for a screen
// you watch, unpacking the pictures and playing its sound with the call.

import (
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"nexus/internal/capture"
	"nexus/internal/guildconn"
	"nexus/internal/opus"
	"nexus/internal/sound"
	"nexus/internal/vp8"
)

// Quality is a sharing size.
type Quality struct {
	Name string
	H    int // It is the number of lines: 720 or 1080.
	Kbps int
}

var Qualities = []Quality{{"720p", 720, 2500}, {"1080p", 1080, 4500}}

const shareFPS = 30

type sharing struct {
	stop     chan struct{}
	done     sync.WaitGroup
	key      atomic.Bool // Someone new is watching, so send a full picture.
	blurred  atomic.Bool // It is true while Push-To-Blur is held.
	muted    atomic.Bool // It is true while Push-To-Mute is held.
	target   capture.Target
	problem  atomic.Value
	sendSize image.Point
}

type watching struct {
	guild, from string
	dec         *vp8.Decoder
	frame       *image.YCbCr
	got         time.Time
	audio       *opus.Decoder
}

// fitSize is the target scaled to fit quality's lines (never bigger), with
// even sides, as VP8 needs.
func fitSize(w, h, lines int) (int, int) {
	if h > lines {
		w, h = w*lines/h, lines
	}
	return w &^ 1, h &^ 1
}

var errNotInVoice = errors.New("join a voice channel first")

// StartSharing goes live in your voice channel with a screen or window.
func (e *Engine) StartSharing(t capture.Target, q Quality, withSound bool) error {
	e.mu.Lock()
	gc, channel := e.conn, e.channel
	busy := e.share != nil
	e.mu.Unlock()
	if gc == nil {
		return errNotInVoice
	}
	if busy {
		e.StopSharing()
	}
	first, err := capture.Grab(t)
	if err != nil {
		return err
	}
	w, h := fitSize(first.W, first.H, q.H)
	enc, err := vp8.NewEncoder(w, h, shareFPS, q.Kbps)
	if err != nil {
		return err
	}
	if err := gc.StreamStart(); err != nil {
		enc.Close()
		return err
	}
	s := &sharing{stop: make(chan struct{}), target: t, sendSize: image.Pt(w, h)}
	s.key.Store(true)
	e.mu.Lock()
	e.share = s
	e.mu.Unlock()

	s.done.Add(1)
	go func() { // This sends the pictures.
		defer s.done.Done()
		defer enc.Close()
		var seq, previews uint32
		shot := 0 // It counts the pictures since the last preview.
		tick := time.NewTicker(time.Second / shareFPS)
		defer tick.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-tick.C:
			}
			p, err := capture.Grab(t)
			if err != nil {
				s.problem.Store(err.Error())
				go e.StopSharing() // The window closed.
				return
			}
			e.mu.Lock()
			blur := e.heldLocked(e.set.BlurKey)
			e.mu.Unlock()
			if s.blurred.Swap(blur) != blur {
				go e.changed()
			}
			if f := e.ownCamera(); f.Pix != nil { // Draw your camera in its corner.
				e.mu.Lock()
				corner := e.set.CameraCorner
				e.mu.Unlock()
				overlayCamera(p.Pix, p.W, p.H, f, corner)
			}
			if blur { // This is Push-To-Blur.
				p.Pix, p.W, p.H = heavyBlur(p.Pix, p.W, p.H)
			}
			if shot%previewEvery == 0 { // Make a snapshot for the tiles, blurred too.
				if jpg := previewJPEG(p.Pix, p.W, p.H); jpg != nil {
					previews++
					gc.SendStream(channel, "preview", false, previews, jpg)
				}
			}
			shot++
			pkt, key, err := enc.Encode(vp8.NewBGRA(p.Pix, p.W, p.H), s.key.Swap(false))
			if err != nil || len(pkt) == 0 {
				continue
			}
			seq++
			gc.SendStream(channel, "video", key, seq, pkt)
		}
	}()
	if withSound {
		if lb, err := sound.OpenLoopback(""); err == nil {
			s.done.Add(1)
			go func() { // This sends the computer's sound.
				defer s.done.Done()
				defer lb.Close()
				oe, err := opus.NewEncoder(64000)
				if err != nil {
					return
				}
				defer oe.Close()
				var seq uint32
				for {
					select {
					case <-s.stop:
						return
					case f, ok := <-lb.Frames():
						if !ok {
							return
						}
						if len(f) != sound.Frame {
							continue
						}
						e.mu.Lock()
						mute := e.heldLocked(e.set.MuteKey)
						e.mu.Unlock()
						if s.muted.Swap(mute) != mute {
							go e.changed()
						}
						if mute { // Push-To-Mute sends silence, keeping time.
							f = make([]int16, sound.Frame)
						}
						if pkt, err := oe.Encode(f); err == nil {
							seq++
							gc.SendStream(channel, "audio", false, seq, pkt)
						}
					}
				}
			}()
		} else {
			s.problem.Store("The computer's sound can't be shared: " + err.Error())
		}
	}
	e.changed()
	return nil
}

// StopSharing stops your screen sharing.
func (e *Engine) StopSharing() {
	e.mu.Lock()
	s, gc := e.share, e.conn
	e.share = nil
	e.mu.Unlock()
	if s == nil {
		return
	}
	close(s.stop)
	s.done.Wait()
	if gc != nil {
		gc.StreamStop()
	}
	e.changed()
}

// Sharing says what you share ("" when nothing).
func (e *Engine) Sharing() (name string, size image.Point) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.share == nil {
		return "", image.Point{}
	}
	return e.share.target.Name, e.share.sendSize
}

// keyNeeded is called when someone starts watching: the next picture is a full one.
func (e *Engine) keyNeeded(guildID string) {
	e.mu.Lock()
	if e.share != nil && guildID == e.guild {
		e.share.key.Store(true)
	}
	e.mu.Unlock()
}

// Watch starts watching someone's screen in your voice channel ("" stops).
func (e *Engine) Watch(from string) error {
	e.mu.Lock()
	gc, guild := e.conn, e.guild
	old := e.watch
	e.watch = nil
	e.mu.Unlock()
	if old != nil {
		old.dec.Close()
		old.audio.Close()
		if gc != nil {
			gc.StreamWatch(old.from, false)
		}
	}
	if from == "" || gc == nil {
		e.changed()
		return nil
	}
	dec, err := vp8.NewDecoder()
	if err != nil {
		return err
	}
	ad, err := opus.NewDecoder()
	if err != nil {
		dec.Close()
		return err
	}
	e.mu.Lock()
	e.watch = &watching{guild: guild, from: from, dec: dec, audio: ad}
	e.mu.Unlock()
	if err := gc.StreamWatch(from, true); err != nil {
		e.Watch("")
		return err
	}
	e.changed()
	return nil
}

// Watching says whose screen you watch, and its newest picture (nil until
// the first full picture arrives).
func (e *Engine) Watching() (from string, frame image.Image) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.watch == nil {
		return "", nil
	}
	if e.watch.frame == nil {
		return e.watch.from, nil
	}
	return e.watch.from, e.watch.frame
}

// receiveStream is where parts of shared screens arrive (from guildconn).
func (e *Engine) receiveStream(guildID, from, kind string, key bool, data []byte) {
	if kind == "camera" { // This is a camera picture.
		e.gotCamera(guildID, from, key, data)
		return
	}
	if kind == "preview" {
		e.gotPreview(guildID, from, data)
		return
	}
	e.mu.Lock()
	w := e.watch
	if w == nil || w.guild != guildID || w.from != from {
		e.mu.Unlock()
		return
	}
	if kind == "audio" {
		pcm, err := w.audio.Decode(data)
		deaf := e.deafened
		if err == nil && len(pcm) == sound.Frame && !deaf {
			s := e.speakers["stream:"+from] // It is mixed in with the call.
			if s == nil {
				s = &speaker{}
				e.speakers["stream:"+from] = s
			}
			s.queue = append(s.queue, pcm)
			if len(s.queue) > maxQueue {
				s.queue = s.queue[len(s.queue)-maxQueue:]
			}
		}
		e.mu.Unlock()
		return
	}
	if w.frame == nil && !key {
		e.mu.Unlock()
		return // Wait for a full picture to start from.
	}
	dec := w.dec
	e.mu.Unlock()
	img, err := dec.Decode(data)
	if err != nil || img == nil {
		return
	}
	e.mu.Lock()
	if e.watch == w {
		w.frame, w.got = img, time.Now()
	}
	e.mu.Unlock()
}

// streamHooks connects the engine to shared screens.
func (e *Engine) streamHooks(m *guildconn.Manager) {
	m.SetStreamSink(e.receiveStream, e.keyNeeded)
}

// SharingHeld says whether Push-To-Blur and Push-To-Mute are held now.
func (e *Engine) SharingHeld() (blurred, muted bool) {
	e.mu.Lock()
	s := e.share
	e.mu.Unlock()
	if s == nil {
		return false, false
	}
	return s.blurred.Load(), s.muted.Load()
}
