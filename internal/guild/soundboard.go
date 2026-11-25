package guild

// This file is the soundboard: up to 25 sounds a guild, each up to 10
// seconds, with a name and, if wanted, a picture for its button. Manage
// Soundboard adds and removes them; Use Soundboard plays one in your voice
// channel, and everyone there who isn't deafened hears it. Sounds are kept
// as Opus packets (proto.PackSound) in sounds/, named by their hash.

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	soundsFile    = "sounds.json"
	soundsDir     = "sounds"
	soundPicsDir  = "soundpics" // This folder holds the buttons' pictures.
	maxSoundName  = 32
	soundCooldown = time.Second // This is the wait between one person's sounds.
)

func (g *Server) loadSounds() error {
	first := !g.fileExists(soundsFile)
	if err := guildstore.LoadPage(g.DataDir, soundsFile, &g.sounds); err != nil {
		return err
	}
	if first { // In a guild from before the soundboard, Everyone may use it.
		if r := g.roleLocked(proto.EveryoneRole); r != nil && !contains(r.Perms, proto.PermUseSoundboard) {
			r.Perms = append(r.Perms, proto.PermUseSoundboard)
			guildstore.SaveRoles(g.DataDir, g.roles)
		}
		guildstore.SavePage(g.DataDir, soundsFile, []proto.Sound{})
	}
	return nil
}

func (g *Server) soundList() []proto.Sound {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]proto.Sound{}, g.sounds...)
}

func (g *Server) soundLocked(id string) *proto.Sound {
	for i := range g.sounds {
		if g.sounds[i].ID == id {
			return &g.sounds[i]
		}
	}
	return nil
}

func (g *Server) soundPut(user string, in *proto.Sound, data, picture []byte) error {
	if in == nil {
		return errors.New("nothing sent")
	}
	name := strings.Join(strings.Fields(in.Name), " ")
	switch {
	case name == "":
		return errors.New("give the sound a name")
	case utf8.RuneCountInString(name) > maxSoundName:
		return errors.New("sound names can be at most 32 characters")
	case len(picture) > 0 && (len(picture) > maxEmojiSize || !isPicture(picture)):
		return errors.New("the picture must be PNG, JPG or GIF, up to 256 KB")
	case len(data) > proto.MaxSoundBytes:
		return errors.New("that sound is too big")
	}
	packets, err := proto.UnpackSound(data)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageSoundboard); err != nil {
		return err
	}
	if len(g.sounds) >= proto.MaxSounds {
		return errors.New("the soundboard is full (25 sounds)")
	}
	for _, s := range g.sounds {
		if strings.EqualFold(s.Name, name) {
			return errors.New("there is already a sound with that name")
		}
	}
	h, err := guildstore.SaveBlob(g.DataDir, soundsDir, data)
	if err != nil {
		return err
	}
	pic := ""
	if len(picture) > 0 {
		if pic, err = guildstore.SaveBlob(g.DataDir, soundPicsDir, picture); err != nil {
			return err
		}
	}
	g.sounds = append(g.sounds, proto.Sound{ID: newID(), Name: name, Picture: pic, Hash: h, Millis: len(packets) * 20})
	g.notifyLater(g.allConnsLocked(), "sounds_changed")
	return guildstore.SavePage(g.DataDir, soundsFile, g.sounds)
}

func (g *Server) soundDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageSoundboard); err != nil {
		return err
	}
	var kept []proto.Sound
	for _, s := range g.sounds {
		if s.ID == id {
			guildstore.RemoveBlob(g.DataDir, soundsDir, s.Hash)
			if s.Picture != "" {
				guildstore.RemoveBlob(g.DataDir, soundPicsDir, s.Picture)
			}
			continue
		}
		kept = append(kept, s)
	}
	g.sounds = kept
	g.notifyLater(g.allConnsLocked(), "sounds_changed")
	return guildstore.SavePage(g.DataDir, soundsFile, kept)
}

func (g *Server) soundGet(user, id string) ([]byte, error) {
	g.mu.Lock()
	_, member := g.members[user]
	s := g.soundLocked(id)
	g.mu.Unlock()
	if !member || s == nil {
		return nil, errors.New("there is no such sound")
	}
	return guildstore.LoadBlob(g.DataDir, soundsDir, s.Hash)
}

// soundPlay plays a sound to everyone in user's voice channel.
func (g *Server) soundPlay(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	v := g.voice[user]
	switch {
	case v == nil:
		return errors.New("join a voice channel first")
	case g.soundLocked(id) == nil:
		return errors.New("there is no such sound")
	case !g.canLocked(user, v.channel, proto.PermUseSoundboard):
		return errors.New("you can't use the soundboard here")
	case v.muted || g.members[user].VoiceMuted:
		return errors.New("you can't play sounds while muted")
	case g.now().Sub(g.lastSound[user]) < soundCooldown:
		return errors.New("wait a moment between sounds")
	}
	if g.lastSound == nil {
		g.lastSound = map[string]time.Time{}
	}
	g.lastSound[user] = g.now()
	var to []string
	for u, o := range g.voice {
		if o.channel == v.channel && !o.deafened && !g.members[u].VoiceDeafened {
			to = append(to, u)
		}
	}
	g.notifyEventLater(g.connsOfLocked(to...), proto.Response{Type: "event", OK: true, Event: "sound_play",
		Channel: v.channel, User: user, SoundID: id})
	return nil
}
