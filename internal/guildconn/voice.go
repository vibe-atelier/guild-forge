package guildconn

// This file is voice in the app: joining a voice channel, and
// sending and receiving 20 ms packets of Opus, sealed with the voice
// channel's key so that only the people in it can hear them.

import (
	"errors"
	"os"
	"path/filepath"

	"nexus/internal/e2e"
	"nexus/internal/proto"
)

// VoiceSink receives someone's voice: Opus, already unsealed. It is called
// on the connection's thread, so it must return quickly.
type VoiceSink func(guildID, from string, seq uint32, opus []byte, priority bool)

// SetVoiceSink sets where voice that arrives goes (nil: nowhere).
func (m *Manager) SetVoiceSink(f VoiceSink) {
	m.mu.Lock()
	m.voiceSink = f
	m.mu.Unlock()
}

// voiceScope keeps voice packets apart from messages in the same channel.
func voiceScope(channel string) string { return "voice|" + channel }

func (gc *Conn) call(req proto.Request) error {
	s := gc.session()
	if s == nil {
		return errors.New("the guild is not connected")
	}
	r, err := s.Call(req, callTimeout)
	if err != nil {
		return err
	}
	if !r.OK {
		return errors.New(r.Error)
	}
	return nil
}

// VoiceJoin joins a voice channel (leaving any other in this guild).
func (gc *Conn) VoiceJoin(channel string) error {
	return gc.call(proto.Request{Type: "voice_join", ChannelID: channel})
}

// VoiceLeave leaves voice in this guild.
func (gc *Conn) VoiceLeave() error { return gc.call(proto.Request{Type: "voice_leave"}) }

// VoiceState sets your own mute and deafen.
func (gc *Conn) VoiceState(muted, deafened bool) error {
	return gc.call(proto.Request{Type: "voice_state", Muted: muted, Deafened: deafened})
}

// HasVoiceKey says whether this app can seal voice for a voice channel yet.
func (gc *Conn) HasVoiceKey(channel string) bool {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return len(gc.chat.keys[channel]) > 0
}

// SendVoice seals and sends one packet (never waits for an answer).
// priority: the Priority Speaker key is held.
func (gc *Conn) SendVoice(channel string, seq uint32, opus []byte, priority bool) error {
	gc.mu.Lock()
	var key []byte
	version := 0
	for v, k := range gc.chat.keys[channel] {
		if v > version {
			version, key = v, k
		}
	}
	s := gc.sess
	gc.mu.Unlock()
	if key == nil {
		return errors.New("no key for this voice channel yet")
	}
	if s == nil {
		return errors.New("the guild is not connected")
	}
	nonce, sealed, err := e2e.Encrypt(key, voiceScope(channel), version, opus)
	if err != nil {
		return err
	}
	return s.Post(proto.Request{Type: "voice", Voice: &proto.VoicePacket{Channel: channel, Version: version, Seq: seq, Priority: priority, Nonce: nonce, Cipher: sealed}})
}

// onVoice unseals a packet and hands it to the sink.
func (gc *Conn) onVoice(p *proto.VoicePacket) {
	if p == nil {
		return
	}
	gc.mu.Lock()
	key := gc.chat.keys[p.Channel][p.Version]
	gc.mu.Unlock()
	gc.m.mu.Lock()
	sink := gc.m.voiceSink
	gc.m.mu.Unlock()
	if key == nil || sink == nil {
		return
	}
	opus, err := e2e.Decrypt(key, voiceScope(p.Channel), p.Version, p.Nonce, p.Cipher)
	if err != nil {
		return
	}
	sink(gc.id, p.From, p.Seq, opus, p.Priority)
}

// ---- screen sharing ----------------------------------------------------------

// StreamSink receives part of a shared screen: a VP8 picture or Opus sound,
// already unsealed. It is called on the connection's thread.
type StreamSink func(guildID, from, kind string, key bool, data []byte)

// SetStreamSink sets where shared screens go; keyNeeded is called when
// someone starts watching your screen (send a full picture).
func (m *Manager) SetStreamSink(f StreamSink, keyNeeded func(guildID string)) {
	m.mu.Lock()
	m.streamSink, m.keyNeeded = f, keyNeeded
	m.mu.Unlock()
}

func streamScope(channel string) string { return "stream|" + channel }

// StreamStart and StreamStop go live in your voice channel, or stop.
func (gc *Conn) StreamStart() error { return gc.call(proto.Request{Type: "stream_start"}) }
func (gc *Conn) StreamStop() error  { return gc.call(proto.Request{Type: "stream_stop"}) }

// StreamWatch starts (or stops) watching someone's screen.
func (gc *Conn) StreamWatch(user string, on bool) error {
	t := "stream_unwatch"
	if on {
		t = "stream_watch"
	}
	return gc.call(proto.Request{Type: t, Target: user})
}

// SendStream seals and sends part of your shared screen.
func (gc *Conn) SendStream(channel, kind string, key bool, seq uint32, data []byte) error {
	gc.mu.Lock()
	var k []byte
	version := 0
	for v, kk := range gc.chat.keys[channel] {
		if v > version {
			version, k = v, kk
		}
	}
	s := gc.sess
	gc.mu.Unlock()
	if k == nil || s == nil {
		return errors.New("not ready to share yet")
	}
	nonce, sealed, err := e2e.Encrypt(k, streamScope(channel), version, data)
	if err != nil {
		return err
	}
	return s.Post(proto.Request{Type: "stream", Stream: &proto.StreamPacket{Channel: channel, Kind: kind, Key: key, Seq: seq,
		Version: version, Nonce: nonce, Cipher: sealed}})
}

func (gc *Conn) onStream(p *proto.StreamPacket) {
	if p == nil {
		return
	}
	gc.mu.Lock()
	key := gc.chat.keys[p.Channel][p.Version]
	gc.mu.Unlock()
	gc.m.mu.Lock()
	sink := gc.m.streamSink
	gc.m.mu.Unlock()
	if key == nil || sink == nil {
		return
	}
	data, err := e2e.Decrypt(key, streamScope(p.Channel), p.Version, p.Nonce, p.Cipher)
	if err != nil {
		return
	}
	sink(gc.id, p.From, p.Kind, p.Key, data)
}

func (gc *Conn) onKeyNeeded() {
	gc.m.mu.Lock()
	f := gc.m.keyNeeded
	gc.m.mu.Unlock()
	if f != nil {
		f(gc.id)
	}
}

// ---- voice moderation -----------------------------------------

// VoiceMute sets or lifts a guild mute on someone (Mute Members).
func (gc *Conn) VoiceMute(user string, on bool) error {
	return gc.call(proto.Request{Type: "voice_mute", Target: user, On: on})
}

// VoiceDeafen sets or lifts a guild deafen on someone (Deafen Members).
func (gc *Conn) VoiceDeafen(user string, on bool) error {
	return gc.call(proto.Request{Type: "voice_deafen", Target: user, On: on})
}

// VoiceMove moves someone to another voice channel (Move Members).
func (gc *Conn) VoiceMove(user, channel string) error {
	return gc.call(proto.Request{Type: "voice_move", Target: user, ChannelID: channel})
}

// VoiceDisconnect takes someone out of voice (Disconnect Members).
func (gc *Conn) VoiceDisconnect(user string) error {
	return gc.call(proto.Request{Type: "voice_disconnect", Target: user})
}

// VoiceLock locks or unlocks a voice channel (Lock Channel).
func (gc *Conn) VoiceLock(channel string, on bool) error {
	return gc.call(proto.Request{Type: "voice_lock", ChannelID: channel, On: on})
}

// SetVoiceHooks sets what happens when a moderator moves you (channel ""
// when you were disconnected), and when someone plays a soundboard sound.
func (m *Manager) SetVoiceHooks(moved func(guildID, channel string), sound func(guildID, channel, from, sound string)) {
	m.mu.Lock()
	m.voiceMoved, m.soundPlay = moved, sound
	m.mu.Unlock()
}

func (gc *Conn) onVoiceMoved(channel string) {
	gc.m.mu.Lock()
	f := gc.m.voiceMoved
	gc.m.mu.Unlock()
	if f != nil {
		f(gc.id, channel)
	}
}

func (gc *Conn) onSoundPlay(channel, from, sound string) {
	gc.m.mu.Lock()
	f := gc.m.soundPlay
	gc.m.mu.Unlock()
	if f != nil {
		f(gc.id, channel, from, sound)
	}
}

// ---- the soundboard ---------------------------------------------------------------

func (gc *Conn) refreshSounds(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "sound_list"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	gc.mu.Lock()
	gc.sounds = r.Sounds
	gc.mu.Unlock()
}

// Sounds lists the guild's soundboard.
func (gc *Conn) Sounds() []proto.Sound {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return append([]proto.Sound{}, gc.sounds...)
}

// PutSound adds a sound (Manage Soundboard): data is proto.PackSound's,
// picture its button's picture (nil for none).
func (gc *Conn) PutSound(name string, picture, data []byte) error {
	_, err := gc.Call(proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: name}, SoundData: data, Image: picture})
	if err == nil {
		if s := gc.session(); s != nil {
			gc.refreshSounds(s)
		}
	}
	return err
}

// DeleteSound removes a sound.
func (gc *Conn) DeleteSound(id string) error {
	_, err := gc.Call(proto.Request{Type: "sound_delete", SoundID: id})
	if err == nil {
		if s := gc.session(); s != nil {
			gc.refreshSounds(s)
		}
	}
	return err
}

// PlaySound plays a sound in your voice channel.
func (gc *Conn) PlaySound(id string) error {
	return gc.call(proto.Request{Type: "sound_play", SoundID: id})
}

// SoundPackets returns a sound's Opus packets, kept on this computer once
// fetched.
func (gc *Conn) SoundPackets(id string) ([][]byte, error) {
	var hash string
	for _, s := range gc.Sounds() {
		if s.ID == id {
			hash = s.Hash
		}
	}
	if !hexHash.MatchString(hash) {
		return nil, errors.New("there is no such sound")
	}
	path := filepath.Join(gc.m.u.Dir, "media", "sounds", hash)
	data, err := os.ReadFile(path)
	if err != nil {
		r, err := gc.Call(proto.Request{Type: "sound_get", SoundID: id})
		if err != nil {
			return nil, err
		}
		data = r.Data
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, data, 0o600)
	}
	return proto.UnpackSound(data)
}

// ---- cameras ------------------------------------------------------------

// CameraStart and CameraStop turn your camera on or off in your voice channel.
func (gc *Conn) CameraStart() error { return gc.call(proto.Request{Type: "camera_start"}) }
func (gc *Conn) CameraStop() error  { return gc.call(proto.Request{Type: "camera_stop"}) }

// SetCameraKeyHook sets what happens when someone new in your voice channel
// needs a full camera picture.
func (m *Manager) SetCameraKeyHook(f func(guildID string)) {
	m.mu.Lock()
	m.cameraKey = f
	m.mu.Unlock()
}

func (gc *Conn) onCameraKey() {
	gc.m.mu.Lock()
	f := gc.m.cameraKey
	gc.m.mu.Unlock()
	if f != nil {
		f(gc.id)
	}
}
