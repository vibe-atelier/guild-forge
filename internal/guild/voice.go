package guild

// This file handles voice channels. A member joins one voice channel
// at a time. Their app sends 20 ms of Opus every 20 ms, sealed with the voice
// channel's key; the guild passes each packet on to the others in the
// channel and can't hear it. Who is in each voice channel goes with the
// channel list.
//
// Moderators can mute, deafen, move and
// disconnect people ranked below them, and lock a voice channel; the
// Priority Speaker mark is passed on only for those allowed it.

import (
	"errors"
	"slices"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const voiceFile = "voice.json" // This file lists the locked voice channels.

type voiceState struct {
	channel  string
	muted    bool
	deafened bool
	live     bool   // It is true while they share their screen.
	camera   bool   // It is true while their camera is on.
	watching string // It is whose screen they watch.
}

type voiceSaved struct {
	Locked []string `json:"locked,omitempty"`
}

func (g *Server) loadVoice() {
	var v voiceSaved
	guildstore.LoadPage(g.DataDir, voiceFile, &v)
	g.locked = map[string]bool{}
	for _, c := range v.Locked {
		g.locked[c] = true
	}
}

func (g *Server) saveVoiceLocked() {
	var v voiceSaved
	for c := range g.locked {
		v.Locked = append(v.Locked, c)
	}
	slices.Sort(v.Locked)
	if err := guildstore.SavePage(g.DataDir, voiceFile, v); err != nil {
		g.Log.Printf("could not save the locked voice channels: %v", err)
	}
}

// migrateVoiceMod gives roles the five permissions that replaced "Moderate
// Voice", wherever the old one was given or taken away.
func (g *Server) migrateVoiceMod() {
	const old = "moderate_voice"
	swap := func(list []string) ([]string, bool) {
		if !slices.Contains(list, old) {
			return list, false
		}
		list = slices.DeleteFunc(list, func(p string) bool { return p == old })
		for _, p := range proto.VoiceModPerms {
			if !slices.Contains(list, p) {
				list = append(list, p)
			}
		}
		return list, true
	}
	rolesChanged, nodesChanged := false, false
	for i := range g.roles {
		var ch bool
		g.roles[i].Perms, ch = swap(g.roles[i].Perms)
		rolesChanged = rolesChanged || ch
	}
	for i := range g.chat.nodes {
		for j := range g.chat.nodes[i].Overrides {
			o := &g.chat.nodes[i].Overrides[j]
			var a, d bool
			o.Allow, a = swap(o.Allow)
			o.Deny, d = swap(o.Deny)
			nodesChanged = nodesChanged || a || d
		}
	}
	if rolesChanged {
		guildstore.SaveRoles(g.DataDir, g.roles)
	}
	if nodesChanged {
		guildstore.SaveChannels(g.DataDir, g.chat.nodes)
	}
}

// voiceMembersLocked lists who is in a voice channel.
func (g *Server) voiceMembersLocked(channel string) []proto.VoiceMember {
	var out []proto.VoiceMember
	for u, v := range g.voice {
		if v.channel == channel {
			m := g.members[u]
			out = append(out, proto.VoiceMember{User: u, Muted: v.muted, Deafened: v.deafened, Live: v.live, Camera: v.camera,
				GuildMuted: m.VoiceMuted, GuildDeafened: m.VoiceDeafened})
		}
	}
	return out
}

// voiceJoin puts a member in a voice channel (leaving any other).
func (g *Server) voiceJoin(user, channel string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nodeLocked(channel)
	switch {
	case n == nil || n.Kind != proto.KindVoice:
		return errors.New("there is no such voice channel")
	case !g.canSeeLocked(user, channel):
		return errors.New("you can't see this voice channel")
	case !g.canLocked(user, channel, proto.PermConnect):
		return errors.New("you can't join this voice channel")
	case g.locked[channel] && !g.canLocked(user, channel, proto.PermLockChannel) &&
		(g.voice[user] == nil || g.voice[user].channel != channel):
		return errors.New("this voice channel is locked")
	}
	g.putInVoiceLocked(user, channel)
	return nil
}

// putInVoiceLocked moves user into channel, keeping their own mute and
// deafen; moving to another channel ends sharing.
func (g *Server) putInVoiceLocked(user, channel string) {
	if g.voice == nil {
		g.voice = map[string]*voiceState{}
	}
	old := g.voice[user]
	v := &voiceState{channel: channel}
	if old != nil && old.channel != channel {
		g.endStreamLocked(user) // The new state watches nobody.
	}
	if old != nil {
		v.muted, v.deafened = old.muted, old.deafened
		if old.channel == channel {
			v.live, v.watching, v.camera = old.live, old.watching, old.camera
		}
	}
	g.voice[user] = v
	if old == nil || old.channel != channel { // Cameras there send a full picture for the newcomer.
		g.cameraKeyLocked(channel)
	}
	g.notifyLater(g.allConnsLocked(), "voice_changed")
}

// voiceLeaveLocked takes a member out of voice (when they leave the channel,
// or go offline).
func (g *Server) voiceLeaveLocked(user string) {
	if _, in := g.voice[user]; in {
		g.endStreamLocked(user)
		delete(g.voice, user)
		g.notifyLater(g.allConnsLocked(), "voice_changed")
	}
}

func (g *Server) voiceSetState(user string, muted, deafened bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	v := g.voice[user]
	if v == nil {
		return errors.New("you are not in a voice channel")
	}
	v.muted, v.deafened = muted || deafened, deafened // Being deafened means being muted too.
	g.notifyLater(g.allConnsLocked(), "voice_changed")
	return nil
}

// ---- moderation -----------------------------------------------------

// voiceTargetLocked checks that user may use perm on target, who is in a
// voice channel; it returns target's voice state.
func (g *Server) voiceTargetLocked(user, target, perm string) (*voiceState, error) {
	t := g.voice[target]
	switch {
	case t == nil:
		return nil, errors.New("they aren't in a voice channel")
	case target == user:
		return nil, errors.New("you can't do that to yourself")
	case !g.canLocked(user, t.channel, perm):
		return nil, errors.New("you don't have permission to do that")
	case !g.outranksLocked(user, target):
		return nil, errors.New("you can only do that to people ranked below you")
	}
	return t, nil
}

// voiceModerate sets or lifts a guild mute (deafen false) or deafen.
func (g *Server) voiceModerate(user, target string, deafen, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	perm := proto.PermMuteMembers
	if deafen {
		perm = proto.PermDeafenMembers
	}
	m, member := g.members[target]
	if !member {
		return errors.New("that person is not a member")
	}
	if g.voice[target] == nil { // Lifting works when they are away too.
		if on {
			return errors.New("they aren't in a voice channel")
		}
		if !g.hasLocked(user, perm) || !g.outranksLocked(user, target) {
			return errors.New("you don't have permission to do that")
		}
	} else if _, err := g.voiceTargetLocked(user, target, perm); err != nil {
		return err
	}
	if deafen {
		m.VoiceDeafened = on
	} else {
		m.VoiceMuted = on
	}
	g.members[target] = m
	g.saveMembersLocked()
	g.notifyLater(g.allConnsLocked(), "voice_changed")
	return nil
}

func (g *Server) voiceMove(user, target, channel string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	t, err := g.voiceTargetLocked(user, target, proto.PermMoveMembers)
	if err != nil {
		return err
	}
	n := g.nodeLocked(channel)
	switch {
	case n == nil || n.Kind != proto.KindVoice:
		return errors.New("there is no such voice channel")
	case channel == t.channel:
		return errors.New("they are already there")
	case !g.canLocked(user, channel, proto.PermMoveMembers):
		return errors.New("you can't move people into that voice channel")
	case !g.canSeeLocked(target, channel):
		return errors.New("they can't see that voice channel")
	}
	g.putInVoiceLocked(target, channel)
	g.notifyEventLater(g.connsOfLocked(target), proto.Response{Type: "event", OK: true, Event: "voice_moved", Channel: channel})
	return nil
}

func (g *Server) voiceDisconnect(user, target string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, err := g.voiceTargetLocked(user, target, proto.PermDisconnectMembers); err != nil {
		return err
	}
	g.voiceLeaveLocked(target)
	g.notifyEventLater(g.connsOfLocked(target), proto.Response{Type: "event", OK: true, Event: "voice_moved"})
	return nil
}

// voiceLock locks or unlocks a voice channel.
func (g *Server) voiceLock(user, channel string, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := g.nodeLocked(channel)
	switch {
	case n == nil || n.Kind != proto.KindVoice:
		return errors.New("there is no such voice channel")
	case !g.canLocked(user, channel, proto.PermLockChannel):
		return errors.New("you don't have permission to do that")
	}
	if g.locked == nil {
		g.locked = map[string]bool{}
	}
	if on == g.locked[channel] {
		return nil
	}
	if on {
		g.locked[channel] = true
	} else {
		delete(g.locked, channel)
	}
	g.saveVoiceLocked()
	g.notifyLater(g.allConnsLocked(), "voice_changed")
	return nil
}

// relayVoice passes a packet on to the others in the speaker's channel. It
// is never answered, and bad packets are simply dropped.
func (g *Server) relayVoice(user string, p *proto.VoicePacket) {
	if p == nil || len(p.Cipher) > 2000 || len(p.Nonce) > 64 {
		return
	}
	g.mu.Lock()
	v := g.voice[user]
	if v == nil || v.channel != p.Channel || v.muted || g.members[user].VoiceMuted || !g.canLocked(user, p.Channel, proto.PermSpeak) {
		g.mu.Unlock()
		return
	}
	priority := p.Priority && g.canLocked(user, p.Channel, proto.PermPrioritySpeaker)
	var to []*proto.Conn
	for u, o := range g.voice {
		if u != user && o.channel == p.Channel && !o.deafened && !g.members[u].VoiceDeafened {
			to = append(to, g.connsOfLocked(u)...)
		}
	}
	g.mu.Unlock()
	out := *p
	out.From, out.Priority = user, priority
	msg := proto.Response{Type: "voice", OK: true, Voice: &out}
	for _, c := range to {
		c.Send(msg)
	}
}

// voiceRequest handles the voice requests. ok is false for other requests.
func (g *Server) voiceRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "voice_join":
		err = g.voiceJoin(user, req.ChannelID)
	case "voice_leave":
		g.mu.Lock()
		g.voiceLeaveLocked(user)
		g.mu.Unlock()
	case "voice_state":
		err = g.voiceSetState(user, req.Muted, req.Deafened)
	case "voice_mute", "voice_deafen":
		err = g.voiceModerate(user, req.Target, req.Type == "voice_deafen", req.On)
	case "voice_move":
		err = g.voiceMove(user, req.Target, req.ChannelID)
	case "voice_disconnect":
		err = g.voiceDisconnect(user, req.Target)
	case "voice_lock":
		err = g.voiceLock(user, req.ChannelID, req.On)
	case "stream_start", "stream_stop":
		err = g.streamStart(user, req.Type == "stream_start")
	case "camera_start", "camera_stop":
		err = g.cameraStart(user, req.Type == "camera_start")
	case "stream_watch", "stream_unwatch":
		err = g.streamWatch(user, req.Target, req.Type == "stream_watch")
	case "sound_list":
		resp.Sounds = g.soundList()
	case "sound_put":
		err = g.soundPut(user, req.Sound, req.SoundData, req.Image)
	case "sound_delete":
		err = g.soundDelete(user, req.SoundID)
	case "sound_get":
		resp.Data, err = g.soundGet(user, req.SoundID)
	case "sound_play":
		err = g.soundPlay(user, req.SoundID)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}
