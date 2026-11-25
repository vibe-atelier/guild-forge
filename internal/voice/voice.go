// Package voice runs voice on this computer: it
// records the microphone, decides when you are talking (your voice above
// a level, or a held Push-To-Talk key), squeezes it with Opus and sends it,
// and plays everyone else, mixed together.
package voice

import (
	"image"
	"math"
	"sync"
	"time"

	"nexus/internal/guildconn"
	"nexus/internal/opus"
	"nexus/internal/rnnoise"
	"nexus/internal/sound"
	"nexus/internal/speexdsp"
	"nexus/internal/user"
)

const (
	DefaultSensitivity = user.DefaultSensitivity // It is in dB.
	hangover           = 300 * time.Millisecond
	maxQueue           = 8 // It is the frames waiting per speaker (160 ms).
	startAfter         = 2 // It is the frames to wait for before playing someone (40 ms).
	silenceDB          = -90.0
)

// Settings are the voice settings, as saved with the user.
type Settings = user.Voice

type speaker struct {
	dec      *opus.Decoder
	queue    [][]int16
	playing  bool
	heard    time.Time
	priority time.Time // It is when they were last heard with the Priority Speaker key held.
}

// soundTrack is a soundboard sound being played.
type soundTrack struct {
	pcm []int16
	at  int
}

// Engine is voice on this computer. One voice channel at a time.
type Engine struct {
	m *guildconn.Manager

	mu       sync.Mutex
	set      Settings
	guild    string
	channel  string
	conn     *guildconn.Conn
	muted    bool
	deafened bool
	testing  bool // It is true during the settings page's microphone test.

	capture  *sound.Capture
	playback *sound.Playback
	enc      *opus.Encoder
	seq      uint32
	level    float64 // It is the microphone's level now, in dB.
	loudAt   time.Time
	talking  time.Time // It is when you last sent voice.
	// These are keys held in the window (Push-To-Talk, Push-To-Blur and Push-To-Mute).
	keysHeld   map[string]bool
	speakers   map[string]*speaker
	proc       *speexdsp.Processor    // It does echo cancellation, noise suppression and gain.
	rnn        *rnnoise.Denoiser      // It is Strong noise suppression.
	share      *sharing               // It is your shared screen.
	previews   map[string]image.Image // These are snapshots of others' screens, one every few seconds.
	sounds     map[string]*soundTrack // These are the soundboard sounds playing, by who played them.
	previewRev int
	cam        *cameraState           // It is your camera.
	peerCams   map[string]*peerCamera // These are others' cameras.
	watch      *watching              // It is the screen you watch.
	problem    string
	onChange   func()
}

// New makes the voice engine. onChange is called when what the window shows
// (who is talking, the microphone level) changes.
func New(m *guildconn.Manager, s Settings, onChange func()) *Engine {
	e := &Engine{m: m, set: s, speakers: map[string]*speaker{}, onChange: onChange}
	m.SetVoiceSink(e.receive)
	m.SetVoiceHooks(e.moved, e.soundPlayed)
	m.SetCameraKeyHook(e.cameraKey)
	e.streamHooks(m)
	go e.pttLoop()
	return e
}

// ---- joining and leaving -------------------------------------------------------------

// Join enters a voice channel, starting the microphone and speakers.
func (e *Engine) Join(gc *guildconn.Conn, guildID, channel string) error {
	e.StopCamera() // A new channel starts with the camera off.
	if err := gc.VoiceJoin(channel); err != nil {
		return err
	}
	e.mu.Lock()
	if e.conn != nil && e.conn != gc {
		old := e.conn
		go old.VoiceLeave() // There is one voice channel at a time, across guilds too.
	}
	e.conn, e.guild, e.channel = gc, guildID, channel
	e.speakers = map[string]*speaker{}
	e.previews, e.sounds = map[string]image.Image{}, map[string]*soundTrack{}
	e.closePeerCamsLocked()
	err := e.startLocked()
	muted, deafened := e.muted, e.deafened
	e.mu.Unlock()
	go prefetchSounds(gc)
	if muted || deafened {
		gc.VoiceState(muted, deafened)
	}
	e.changed()
	return err
}

// Leave leaves the voice channel and stops the devices.
func (e *Engine) Leave() {
	e.StopSharing()
	e.StopCamera()
	e.Watch("")
	e.mu.Lock()
	gc := e.conn
	e.conn, e.guild, e.channel = nil, "", ""
	if !e.testing {
		e.stopLocked()
	}
	e.speakers = map[string]*speaker{}
	e.previews, e.sounds = nil, nil
	e.closePeerCamsLocked()
	e.mu.Unlock()
	if gc != nil {
		gc.VoiceLeave()
	}
	e.changed()
}

// Where says which voice channel you are in ("" when none).
func (e *Engine) Where() (guildID, channel string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.guild, e.channel
}

// SetMute and SetDeafen are your own buttons. Deafening also mutes.
func (e *Engine) SetMute(on bool) {
	e.mu.Lock()
	e.muted = on
	if !on {
		e.deafened = false
	}
	gc, m, d := e.conn, e.muted, e.deafened
	e.mu.Unlock()
	if gc != nil {
		go gc.VoiceState(m, d)
	}
	e.changed()
}

func (e *Engine) SetDeafen(on bool) {
	e.mu.Lock()
	e.deafened = on
	e.muted = on || e.muted
	gc, m, d := e.conn, e.muted, e.deafened
	e.mu.Unlock()
	if gc != nil {
		go gc.VoiceState(m, d)
	}
	e.changed()
}

func (e *Engine) Muted() (muted, deafened bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.muted, e.deafened
}

// ---- settings and the microphone test --------------------------------------------------

// Settings are the voice settings in use.
func (e *Engine) Settings() Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.set
}

// Apply uses new settings; the devices restart if they changed.
func (e *Engine) Apply(s Settings) {
	e.mu.Lock()
	restart := (s.Input != e.set.Input || s.Output != e.set.Output ||
		s.NoEchoCancel != e.set.NoEchoCancel || s.NoDenoise != e.set.NoDenoise || s.Denoise != e.set.Denoise ||
		s.NoAutoGain != e.set.NoAutoGain) && e.capture != nil
	e.set = s
	if restart {
		e.stopLocked()
		e.startLocked()
	}
	e.mu.Unlock()
	e.changed()
}

// Test turns the microphone test on or off: the microphone runs, so its
// level can be shown, without joining anything.
func (e *Engine) Test(on bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.testing = on
	if on {
		return e.startLocked()
	}
	if e.conn == nil {
		e.stopLocked()
	}
	return nil
}

// Level is the microphone's loudness now (dB, -90 to 0), and whether that
// counts as talking.
func (e *Engine) Level() (db float64, talking bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.level, time.Since(e.talking) < 150*time.Millisecond || (e.testing && e.level > e.set.Level())
}

// SetKeyHeld tells the engine a key is held (or let go) in the window.
func (e *Engine) SetKeyHeld(name string, held bool) {
	e.mu.Lock()
	if e.keysHeld == nil {
		e.keysHeld = map[string]bool{}
	}
	e.keysHeld[name] = held
	e.mu.Unlock()
}

// heldLocked says whether a key is held, in the window or (on Windows)
// anywhere. An empty name is never held.
func (e *Engine) heldLocked(name string) bool {
	return name != "" && (e.keysHeld[name] || globalKeyHeld(name))
}

// Problem is the last trouble with the devices ("" when none).
func (e *Engine) Problem() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.problem
}

// Speaking says who was heard in the last moment (by user ID).
func (e *Engine) Speaking() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]bool{}
	for u, s := range e.speakers {
		if time.Since(s.heard) < 250*time.Millisecond {
			out[u] = true
		}
	}
	return out
}

// ---- devices ------------------------------------------------------------------------------

func (e *Engine) startLocked() error {
	if e.capture != nil {
		return nil
	}
	var err error
	if e.enc == nil {
		if e.enc, err = opus.NewEncoder(32000); err != nil {
			return e.fail(err)
		}
	}
	c, err := sound.OpenCapture(e.set.Input)
	if err != nil {
		return e.fail(err)
	}
	p, err := sound.OpenPlayback(e.set.Output, e.mix)
	if err != nil {
		c.Close()
		return e.fail(err)
	}
	e.capture, e.playback, e.problem = c, p, ""
	// Echo cancellation, noise suppression and automatic gain;
	// Strong noise suppression is RNNoise.
	level := e.set.NoiseLevel(rnnoise.Default() != nil)
	if proc, err := speexdsp.New(sound.Frame, sound.Rate, speexdsp.Options{
		EchoCancel: !e.set.NoEchoCancel, Denoise: level == "standard", AutoGain: !e.set.NoAutoGain}); err == nil {
		e.proc = proc
	}
	if level == "strong" {
		if d, err := rnnoise.New(rnnoise.Default()); err == nil {
			e.rnn = d
		}
	}
	go e.captureLoop(c)
	return nil
}

func (e *Engine) fail(err error) error {
	e.problem = err.Error()
	return err
}

func (e *Engine) stopLocked() {
	if e.capture != nil {
		e.capture.Close()
		e.capture = nil
	}
	if e.playback != nil {
		e.playback.Close()
		e.playback = nil
	}
	if e.proc != nil {
		p := e.proc
		e.proc = nil
		go func() { time.Sleep(200 * time.Millisecond); p.Close() }() // Close it after the device threads let go.
	}
	if e.rnn != nil {
		d := e.rnn
		e.rnn = nil
		go func() { time.Sleep(200 * time.Millisecond); d.Close() }()
	}
	e.level = silenceDB
}

// loudness is a frame's level in dB (0 is the loudest possible).
func loudness(f []int16) float64 {
	var sum float64
	for _, s := range f {
		v := float64(s) / 32768
		sum += v * v
	}
	rms := math.Sqrt(sum / float64(len(f)))
	if rms < 1e-5 {
		return silenceDB
	}
	return math.Max(silenceDB, 20*math.Log10(rms))
}

func (e *Engine) captureLoop(c *sound.Capture) {
	lastShown := time.Now()
	for f := range c.Frames() {
		if len(f) != sound.Frame {
			continue
		}
		e.mu.Lock()
		proc, rnn := e.proc, e.rnn
		e.mu.Unlock()
		if rnn != nil { // Clean up echo, then use RNNoise, then even out loudness.
			proc.CaptureWith(f, func(x []int16) { rnn.Process(x) })
		} else {
			proc.Capture(f) // Clean up echo, noise and loudness.
		}
		db := loudness(f)
		e.mu.Lock()
		e.level = db
		send := false
		if e.conn != nil && !e.muted && !e.deafened {
			if e.set.PushToTalk {
				send = e.heldLocked(e.set.Key())
			} else {
				if db > e.set.Level() {
					e.loudAt = time.Now()
				}
				send = time.Since(e.loudAt) < hangover
			}
		}
		gc, channel, enc := e.conn, e.channel, e.enc
		priority := send && e.heldLocked(e.set.PriorityKey) // This is Priority Speaker.
		var seq uint32
		if send {
			e.seq++
			seq = e.seq
			e.talking = time.Now()
		}
		testing := e.testing
		e.mu.Unlock()
		if send {
			if pkt, err := enc.Encode(f); err == nil {
				gc.SendVoice(channel, seq, pkt, priority)
			}
		}
		// The window shows levels and rings; keep it fresh, not busy.
		if (testing || send) && time.Since(lastShown) > 100*time.Millisecond {
			lastShown = time.Now()
			e.changed()
		}
	}
}

// receive is where others' voice arrives (from guildconn).
func (e *Engine) receive(guildID, from string, seq uint32, data []byte, priority bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if guildID != e.guild || e.deafened {
		return
	}
	s := e.speakers[from]
	if s == nil {
		dec, err := opus.NewDecoder()
		if err != nil {
			return
		}
		s = &speaker{dec: dec}
		e.speakers[from] = s
	}
	pcm, err := s.dec.Decode(data)
	if err != nil || len(pcm) != sound.Frame {
		return
	}
	wasQuiet := time.Since(s.heard) > 250*time.Millisecond
	s.heard = time.Now()
	if priority {
		s.priority = s.heard
	}
	s.queue = append(s.queue, pcm)
	if len(s.queue) > maxQueue { // It has fallen behind, so drop the oldest.
		s.queue = s.queue[len(s.queue)-maxQueue:]
	}
	if wasQuiet {
		go e.changed()
	}
}

// mix fills 20 ms for the speakers with everyone's voice added together.
// Each person is played at their own volume. While a Priority
// Speaker talks, everyone else (and the soundboard) is a fifth as loud.
func (e *Engine) mix(out []int16) {
	sum := make([]float32, len(out))
	e.mu.Lock()
	now := time.Now()
	priority := ""
	for u, s := range e.speakers {
		if now.Sub(s.priority) < priorityHold {
			priority = u
		}
	}
	dim := func(who string) float32 {
		if priority != "" && who != priority {
			return priorityDim
		}
		return 1
	}
	for u, s := range e.speakers {
		if !s.playing && len(s.queue) >= startAfter {
			s.playing = true
		}
		if !s.playing {
			continue
		}
		if len(s.queue) == 0 {
			s.playing = false // Wait for a little to build up again.
			continue
		}
		f := s.queue[0]
		s.queue = s.queue[1:]
		person := u
		if len(u) > 7 && u[:7] == "stream:" { // This is a shared screen's sound.
			person = u[7:]
		}
		gain := float32(e.set.VolumeOf(person)) / 100 * dim(u)
		for i, v := range f {
			sum[i] += float32(v) * gain
		}
	}
	board := float32(e.set.SoundboardVolume()) / 100 * dim("")
	for who, t := range e.sounds {
		for i := range sum {
			if t.at >= len(t.pcm) {
				break
			}
			sum[i] += float32(t.pcm[t.at]) * board
			t.at++
		}
		if t.at >= len(t.pcm) {
			delete(e.sounds, who)
		}
	}
	proc := e.proc
	e.mu.Unlock()
	for i, v := range sum {
		out[i] = int16(max(-32768, min(32767, v)))
	}
	proc.Playback(out) // The echo canceller learns what the speakers play.
}

const (
	priorityHold = 300 * time.Millisecond // It is how long a Priority Speaker keeps others down after a packet.
	priorityDim  = 0.2
)

func (e *Engine) changed() {
	if e.onChange != nil {
		e.onChange()
	}
}

// pttLoop is here for systems that need watching for the key; see ptt_*.go.
func (e *Engine) pttLoop() {}
