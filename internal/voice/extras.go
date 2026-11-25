package voice

// This file has the voice extras on this computer: being moved or disconnected by a
// moderator, soundboard sounds and screen previews.

import (
	"bytes"
	"image"
	"image/jpeg"

	"nexus/internal/guildconn"
	"nexus/internal/opus"
	"nexus/internal/sound"
)

// moved is called when a moderator moves you to channel, or takes you out of voice ("").
func (e *Engine) moved(guildID, channel string) {
	e.mu.Lock()
	if guildID != e.guild || e.conn == nil {
		e.mu.Unlock()
		return
	}
	if channel == "" {
		e.mu.Unlock()
		e.Leave()
		return
	}
	e.mu.Unlock()
	e.StopSharing()
	e.StopCamera()
	e.Watch("")
	e.mu.Lock()
	e.channel = channel
	e.speakers = map[string]*speaker{}
	e.previews, e.sounds = map[string]image.Image{}, map[string]*soundTrack{}
	e.closePeerCamsLocked()
	e.mu.Unlock()
	e.changed()
}

// prefetchSounds keeps the guild's sounds on this computer, so they play
// at once.
func prefetchSounds(gc *guildconn.Conn) {
	for _, s := range gc.Sounds() {
		gc.SoundPackets(s.ID)
	}
}

// soundPlayed is called when someone plays a soundboard sound in a voice channel.
func (e *Engine) soundPlayed(guildID, channel, from, id string) {
	e.mu.Lock()
	gc := e.conn
	here := guildID == e.guild && channel == e.channel && !e.deafened && gc != nil
	e.mu.Unlock()
	if !here {
		return
	}
	pcm, err := soundPCM(gc, id)
	if err != nil {
		return
	}
	e.mu.Lock()
	if e.sounds != nil && guildID == e.guild {
		e.sounds[from] = &soundTrack{pcm: pcm} // A new sound from the same person replaces their last one.
	}
	e.mu.Unlock()
}

// soundPCM unpacks a guild sound into samples.
func soundPCM(gc *guildconn.Conn, id string) ([]int16, error) {
	packets, err := gc.SoundPackets(id)
	if err != nil {
		return nil, err
	}
	dec, err := opus.NewDecoder()
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	var pcm []int16
	for _, p := range packets {
		f, err := dec.Decode(p)
		if err != nil {
			f = make([]int16, sound.Frame)
		}
		pcm = append(pcm, f...)
	}
	return pcm, nil
}

// PlaySound plays a soundboard sound in your voice channel.
func (e *Engine) PlaySound(id string) error {
	e.mu.Lock()
	gc := e.conn
	e.mu.Unlock()
	if gc == nil {
		return errNotInVoice
	}
	return gc.PlaySound(id)
}

// Preview is the newest snapshot of someone's shared screen (nil when none
// has arrived yet).
func (e *Engine) Preview(user string) image.Image {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.previews[user]
}

// previewWidth and previewEvery: a snapshot 320 pixels wide every 5 seconds.
const (
	previewWidth = 320
	previewEvery = 5 * shareFPS // It counts pictures.
)

// previewJPEG shrinks a BGRA picture to a small JPEG.
func previewJPEG(pix []byte, w, h int) []byte {
	if w <= 0 || h <= 0 {
		return nil
	}
	pw := min(previewWidth, w)
	ph := max(1, h*pw/w)
	img := image.NewRGBA(image.Rect(0, 0, pw, ph))
	for y := 0; y < ph; y++ {
		sy := y * h / ph
		for x := 0; x < pw; x++ {
			i := (sy*w + x*w/pw) * 4
			j := (y*pw + x) * 4
			img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = pix[i+2], pix[i+1], pix[i], 255
		}
	}
	var b bytes.Buffer
	if jpeg.Encode(&b, img, &jpeg.Options{Quality: 60}) != nil {
		return nil
	}
	return b.Bytes()
}

// gotPreview keeps a snapshot that arrived.
func (e *Engine) gotPreview(guildID, from string, data []byte) {
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	e.mu.Lock()
	if e.previews != nil && guildID == e.guild {
		e.previews[from] = img
		e.previewRev++
	}
	e.mu.Unlock()
	e.changed()
}

// PreviewRev goes up each time a preview arrives, so the window knows to
// draw the tiles again.
func (e *Engine) PreviewRev() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.previewRev
}
