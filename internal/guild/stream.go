package guild

// This file handles screen sharing. Someone in a voice channel goes
// live; others in the channel choose to watch. Each picture and each piece
// of the computer's sound is sealed with the voice channel's key, and the
// guild passes it only to those watching, so screens nobody watches cost
// nothing. When someone starts watching, the sharer is asked for a full
// picture to start from.

import (
	"errors"

	"nexus/internal/proto"
)

func (g *Server) streamStart(user string, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	v := g.voice[user]
	if v == nil {
		return errors.New("join a voice channel first")
	}
	if !on {
		g.endStreamLocked(user)
		g.notifyLater(g.allConnsLocked(), "voice_changed")
		return nil
	}
	if !g.canLocked(user, v.channel, proto.PermStream) {
		return errors.New("you can't share your screen in this voice channel")
	}
	v.live = true
	g.notifyLater(g.allConnsLocked(), "voice_changed")
	return nil
}

// endStreamLocked stops someone sharing; their viewers stop watching.
func (g *Server) endStreamLocked(user string) {
	if v := g.voice[user]; v != nil {
		v.live = false
	}
	for _, o := range g.voice {
		if o.watching == user {
			o.watching = ""
		}
	}
}

func (g *Server) streamWatch(user, target string, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	v := g.voice[user]
	if v == nil {
		return errors.New("join the voice channel first")
	}
	if !on {
		v.watching = ""
		return nil
	}
	t := g.voice[target]
	if t == nil || !t.live || t.channel != v.channel {
		return errors.New("they aren't sharing their screen in your voice channel")
	}
	v.watching = target
	// A full picture for the new viewer to start from.
	g.notifyEventLater(g.connsOfLocked(target), proto.Response{Type: "event", OK: true, Event: "stream_key"})
	return nil
}

// relayStream passes part of a shared screen to its viewers. It is never
// answered; bad packets are dropped.
func (g *Server) relayStream(user string, p *proto.StreamPacket) {
	if p == nil || len(p.Cipher) > 4<<20 || len(p.Nonce) > 64 ||
		(p.Kind != proto.StreamVideo && p.Kind != proto.StreamAudio && p.Kind != proto.StreamPreview && p.Kind != proto.StreamCamera) ||
		(p.Kind == proto.StreamPreview && len(p.Cipher) > 96<<10) {
		return
	}
	g.mu.Lock()
	v := g.voice[user]
	on := v != nil && ((p.Kind == proto.StreamCamera && v.camera) || (p.Kind != proto.StreamCamera && v.live))
	if !on || v.channel != p.Channel || !g.canLocked(user, p.Channel, proto.PermStream) {
		g.mu.Unlock()
		return
	}
	var to []*proto.Conn
	for u, o := range g.voice {
		if u == user || o.channel != p.Channel {
			continue
		}
		heard := !(p.Kind == proto.StreamAudio && (o.deafened || g.members[u].VoiceDeafened))
		// Previews and cameras go to everyone in the channel,
		// the rest to viewers.
		if heard && (o.watching == user || p.Kind == proto.StreamPreview || p.Kind == proto.StreamCamera) {
			to = append(to, g.connsOfLocked(u)...)
		}
	}
	g.mu.Unlock()
	out := *p
	out.From = user
	msg := proto.Response{Type: "stream", OK: true, Stream: &out}
	for _, c := range to {
		c.Send(msg)
	}
}

// notifyEventLater sends a ready-made event without blocking the caller.
func (g *Server) notifyEventLater(conns []*proto.Conn, ev proto.Response) {
	if len(conns) == 0 {
		return
	}
	go func() {
		for _, c := range conns {
			c.Send(ev)
		}
	}()
}

// ---- cameras -------------------------------------------------

// cameraStart turns someone's camera on or off. Everyone in the channel
// sees it, with no Watch needed.
func (g *Server) cameraStart(user string, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	v := g.voice[user]
	if v == nil {
		return errors.New("join a voice channel first")
	}
	if on && !g.canLocked(user, v.channel, proto.PermStream) {
		return errors.New("you can't use a camera in this voice channel")
	}
	v.camera = on
	g.notifyLater(g.allConnsLocked(), "voice_changed")
	return nil
}

// cameraKeyLocked asks everyone with a camera on in channel for a full
// picture, for someone who just arrived.
func (g *Server) cameraKeyLocked(channel string) {
	var on []string
	for u, v := range g.voice {
		if v.channel == channel && v.camera {
			on = append(on, u)
		}
	}
	if len(on) > 0 {
		g.notifyEventLater(g.connsOfLocked(on...), proto.Response{Type: "event", OK: true, Event: "camera_key"})
	}
}
