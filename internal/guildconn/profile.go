package guildconn

// This file has your look in each guild, nicknames, and the settings each guild
// keeps for you.

import (
	"crypto/sha256"
	"encoding/hex"

	"nexus/internal/proto"
	"nexus/internal/user"
)

func hashOf(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// syncLook gives the guild your name, bio, picture and banner if it has
// older ones. The pictures stay in your user folder.
func (gc *Conn) syncLook(s *proto.Session) {
	u := gc.m.u
	pic, banner := u.Picture(), u.BannerPicture()
	me := gc.me()
	gc.mu.Lock()
	var mine *proto.MemberInfo
	for i := range gc.members {
		if gc.members[i].UserKey == me {
			mine = &gc.members[i]
		}
	}
	same := mine != nil && mine.Name == u.Profile.DefaultName && mine.Bio == u.Profile.Bio &&
		mine.PictureHash == hashOf(pic) && mine.BannerHash == hashOf(banner)
	gc.mu.Unlock()
	if mine == nil || same {
		return
	}
	req := proto.Request{Type: "set_profile", Profile: &proto.MemberProfile{Name: u.Profile.DefaultName, Bio: u.Profile.Bio,
		ClearPicture: len(pic) == 0, ClearBanner: len(banner) == 0}}
	if hashOf(pic) != mine.PictureHash {
		req.Picture = pic
	}
	if hashOf(banner) != mine.BannerHash {
		req.Banner = banner
	}
	s.Call(req, callTimeout)
}

// SetLook saves your bio, picture and banner, and gives them to every guild
// that is connected (the others get them when they connect).
func (m *Manager) SetLook(bio string, picture, banner []byte, clearPicture, clearBanner bool) error {
	if err := m.u.SetLook(bio, picture, banner, clearPicture, clearBanner); err != nil {
		return err
	}
	m.syncAll()
	return nil
}

// SetName changes your name everywhere.
func (m *Manager) SetName(name string) error {
	if err := m.u.SetName(name); err != nil {
		return err
	}
	m.syncAll()
	return nil
}

func (m *Manager) syncAll() {
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, gc := range m.conns {
		conns = append(conns, gc)
	}
	m.mu.Unlock()
	for _, gc := range conns {
		if s := gc.session(); s != nil {
			go gc.syncLook(s)
		}
	}
}

// SetNick changes a nickname in this guild: yours (target "") or someone
// else's. An empty nickname removes it.
func (gc *Conn) SetNick(target, nick string) error {
	_, err := gc.Call(proto.Request{Type: "set_nick", Target: target, Nick: &nick})
	if err == nil {
		if s := gc.session(); s != nil {
			gc.refreshMembers(s)
			gc.m.onChange()
		}
	}
	return err
}

// ---- synced settings -------------------------------------------------------

// refreshSettings loads what the guild keeps for you; read positions from
// another computer count here too.
func (gc *Conn) refreshSettings(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "settings_get"}, callTimeout)
	if err != nil || !r.OK || r.Settings == nil {
		return
	}
	gc.mu.Lock()
	gc.settings = *r.Settings
	gc.settingsLoaded = true
	gc.mu.Unlock()
	for ch, id := range r.Settings.Reads {
		gc.m.u.SetRead(gc.id, ch, id)
	}
	// Positions read here while the guild was away go back to it.
	mine := gc.m.u.Reads()[gc.id]
	newer := map[string]string{}
	for ch, id := range mine {
		if id > r.Settings.Reads[ch] {
			newer[ch] = id
		}
	}
	if len(newer) > 0 {
		gc.putSettings(proto.MemberSettings{Reads: newer})
	}
}

// putSettings sends changed settings and keeps the guild's answer.
func (gc *Conn) putSettings(change proto.MemberSettings) error {
	r, err := gc.Call(proto.Request{Type: "settings_put", Settings: &change})
	if err != nil {
		return err
	}
	if r.Settings != nil {
		gc.mu.Lock()
		gc.settings = *r.Settings
		gc.mu.Unlock()
	}
	return nil
}

// readUpTo records a read position on the guild too (called with gc.mu held).
func (gc *Conn) readUpToLocked(channel, id string) {
	if id <= gc.settings.Reads[channel] {
		return
	}
	if gc.settings.Reads == nil {
		gc.settings.Reads = map[string]string{}
	}
	gc.settings.Reads[channel] = id
	go gc.putSettings(proto.MemberSettings{Reads: map[string]string{channel: id}})
}

// Notify returns the notification choice for a channel ("" channel: the
// guild's), and whether it was set by you or is the default.
func (gc *Conn) Notify(channel string) (choice string, chosen bool) {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return gc.notifyLocked(channel)
}

func (gc *Conn) notifyLocked(channel string) (string, bool) {
	if c := gc.settings.Notify[channel]; channel != "" && c != "" {
		return c, true
	}
	if c := gc.settings.Notify["guild"]; c != "" {
		return c, channel == ""
	}
	if gc.info != nil && gc.info.DefaultNotify != "" {
		return gc.info.DefaultNotify, false
	}
	return proto.NotifyMentions, false
}

// SetNotify sets a channel's notification choice ("" channel: the whole
// guild's; "" choice: back to the default).
func (gc *Conn) SetNotify(channel, choice string) error {
	key := channel
	if key == "" {
		key = "guild"
	}
	err := gc.putSettings(proto.MemberSettings{Notify: map[string]string{key: choice}})
	gc.m.onChange()
	return err
}

// Collapsed returns the folded categories, and whether the guild has given
// your settings yet.
func (gc *Conn) Collapsed() ([]string, bool) {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return append([]string{}, gc.settings.Collapsed...), gc.settingsLoaded
}

// SetCollapsed saves the folded categories (sent to the guild in the
// background).
func (gc *Conn) SetCollapsed(ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	gc.mu.Lock()
	gc.settings.Collapsed = ids
	gc.mu.Unlock()
	go gc.putSettings(proto.MemberSettings{Collapsed: ids}) // The window shows it straight away.
	return nil
}

// User is the signed-in user.
func (m *Manager) User() *user.User { return m.u }
