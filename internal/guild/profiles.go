package guild

// This file has members' pictures, banners, bios and nicknames, and the settings
// a guild keeps for each member.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	profilesDir    = "profiles" // This folder holds members' pictures and banners.
	settingsDir    = "settings" // This folder holds members' synced settings, one file each.
	maxBio         = 190        // This is in characters.
	maxProfilePic  = 256 << 10  // This is in bytes, for a picture or a banner.
	maxSettingsLen = 64 << 10   // This is in bytes, for one member's settings.
	maxSettingKeys = 2000       // This counts channels with a notification choice or a read position.
)

// setProfile saves a member's bio, picture and banner.
func (g *Server) setProfile(user string, p *proto.MemberProfile, picture, banner []byte) error {
	if p == nil {
		return errors.New("missing profile")
	}
	bio := strings.TrimSpace(p.Bio)
	if utf8.RuneCountInString(bio) > maxBio {
		return fmt.Errorf("the bio can be at most %d characters", maxBio)
	}
	for _, b := range [][]byte{picture, banner} {
		if len(b) > 0 && (len(b) > maxProfilePic || !isPicture(b)) {
			return errors.New("profile pictures must be PNG, JPG or GIF, up to 256 KB")
		}
	}
	save := func(b []byte) (string, error) {
		if len(b) == 0 {
			return "", nil
		}
		return guildstore.SaveBlob(g.DataDir, profilesDir, b)
	}
	picHash, err := save(picture)
	if err != nil {
		return err
	}
	bannerHash, err := save(banner)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	m, ok := g.members[user]
	if !ok {
		return errors.New("you are not a member of this guild")
	}
	old := []string{m.PictureHash, m.BannerHash}
	m.Bio = bio
	if name := cleanName(p.Name); name != "" {
		m.Name = name
	}
	if picHash != "" {
		m.PictureHash = picHash
	} else if p.ClearPicture {
		m.PictureHash = ""
	}
	if bannerHash != "" {
		m.BannerHash = bannerHash
	} else if p.ClearBanner {
		m.BannerHash = ""
	}
	g.members[user] = m
	g.saveMembersLocked()
	for _, h := range old {
		g.dropProfileBlobLocked(h)
	}
	g.notifyLater(g.allConnsLocked(), "presence")
	return nil
}

// dropProfileBlobLocked deletes a picture nobody uses any more.
func (g *Server) dropProfileBlobLocked(hash string) {
	if hash == "" {
		return
	}
	for _, m := range g.members {
		if m.PictureHash == hash || m.BannerHash == hash {
			return
		}
	}
	guildstore.DeleteBlob(g.DataDir, profilesDir, hash)
}

// setNick changes a nickname: your own with Change Nickname, or that of
// someone ranked lower with Manage Nicknames.
func (g *Server) setNick(user, target string, nick *string) error {
	if nick == nil {
		return errors.New("missing nickname")
	}
	name := cleanName(*nick)
	g.mu.Lock()
	defer g.mu.Unlock()
	if target == "" {
		target = user
	}
	m, ok := g.members[target]
	if !ok {
		return errors.New("there is no such member")
	}
	if target == user {
		if !g.hasLocked(user, proto.PermChangeNickname) && !g.hasLocked(user, proto.PermManageNicknames) {
			return errors.New("you don't have permission to change your nickname")
		}
	} else {
		if err := g.needLocked(user, proto.PermManageNicknames); err != nil {
			return err
		}
		if !g.outranksLocked(user, target) {
			return errors.New("you can only change the nicknames of people lower in rank than you")
		}
	}
	m.Nick = name
	g.members[target] = m
	g.saveMembersLocked()
	g.notifyLater(g.allConnsLocked(), "presence")
	return nil
}

// ---- synced settings -------------------------------------------------------

func settingsPath(user string) string { return settingsDir + "/" + user + ".json" }

func (g *Server) loadSettingsLocked(user string) proto.MemberSettings {
	var s proto.MemberSettings
	guildstore.LoadPage(g.DataDir, settingsPath(user), &s)
	return s
}

// settingsPut merges a member's settings: notification choices replace the
// ones given ("" removes one), read positions are updated, and the folded
// categories are replaced when given.
func (g *Server) settingsPut(user string, in *proto.MemberSettings) (proto.MemberSettings, error) {
	if in == nil {
		return proto.MemberSettings{}, errors.New("missing settings")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.members[user]; !ok {
		return proto.MemberSettings{}, errors.New("you are not a member of this guild")
	}
	s := g.loadSettingsLocked(user)
	if s.Notify == nil {
		s.Notify = map[string]string{}
	}
	if s.Reads == nil {
		s.Reads = map[string]string{}
	}
	for k, v := range in.Notify {
		switch v {
		case "":
			delete(s.Notify, k)
		case proto.NotifyAll, proto.NotifyMentions, proto.NotifyNothing:
			if len(k) > 64 {
				return s, errors.New("bad channel")
			}
			s.Notify[k] = v
		default:
			return s, errors.New("unknown notification choice")
		}
	}
	for k, v := range in.Reads {
		if len(k) > 64 || len(v) > 64 {
			return s, errors.New("bad read position")
		}
		if v == "" {
			delete(s.Reads, k)
		} else {
			s.Reads[k] = v
		}
	}
	if in.Collapsed != nil {
		s.Collapsed = in.Collapsed
	}
	// Forget channels that no longer exist.
	for _, m := range []map[string]string{s.Notify, s.Reads} {
		for k := range m {
			if k != "guild" && g.nodeLocked(k) == nil {
				delete(m, k)
			}
		}
	}
	data, _ := json.Marshal(s)
	if len(data) > maxSettingsLen || len(s.Notify)+len(s.Reads) > maxSettingKeys {
		return s, errors.New("too many settings")
	}
	return s, guildstore.SavePage(g.DataDir, settingsPath(user), s)
}

// ---- requests -------------------------------------------------------------------------

func (g *Server) profileRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "set_profile":
		err = g.setProfile(user, req.Profile, req.Picture, req.Banner)
	case "set_nick":
		err = g.setNick(user, req.Target, req.Nick)
	case "settings_get":
		g.mu.Lock()
		s := g.loadSettingsLocked(user)
		g.mu.Unlock()
		resp.Settings = &s
	case "settings_put":
		var s proto.MemberSettings
		s, err = g.settingsPut(user, req.Settings)
		resp.Settings = &s
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}

// migrateProfiles makes sure that, in a guild made before profiles existed, Everyone may change their
// own nickname. Done once.
func (g *Server) migrateProfiles() {
	var done bool
	guildstore.LoadPage(g.DataDir, settingsDir+"/.phase8", &done)
	if done {
		return
	}
	if r := g.roleLocked(proto.EveryoneRole); r != nil && !contains(r.Perms, proto.PermChangeNickname) {
		r.Perms = append(r.Perms, proto.PermChangeNickname)
		guildstore.SaveRoles(g.DataDir, g.roles)
	}
	guildstore.SavePage(g.DataDir, settingsDir+"/.phase8", true)
}
