package guild

// This file has the owner's tools for joining and membership: join settings,
// the CAPTCHA question bank, join requests, kicking and banning.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

var errOwnerOnly = errors.New("you don't have permission to do that")

func (g *Server) isOwnerLocked(user string) bool { return g.owner != nil && g.owner.Key == user }

// ownerAction handles the owner-only requests. ok is false if req.Type is
// not one of them.
func (g *Server) ownerAction(user string, req proto.Request) (resp proto.Response, ok bool) {
	var err error
	resp = proto.Response{Type: req.Type, OK: true}
	switch req.Type {
	case "join_settings":
		resp.JoinSettings, err = g.joinSettings(user)
	case "update_join":
		err = g.updateJoin(user, req.Join)
	case "captcha_get":
		resp.CaptchaFull, resp.Questions, err = g.captchaGet(user)
	case "captcha_settings":
		err = g.captchaSettings(user, req.Captcha)
	case "captcha_put":
		var id string
		id, err = g.captchaPut(user, req)
		resp.Message = id
	case "captcha_delete":
		err = g.captchaDelete(user, req.QuestionID)
	case "captcha_image":
		resp.Image, err = g.captchaImage(user, req.QuestionID)
	case "requests":
		resp.People, err = g.listRequests(user)
	case "bans":
		resp.People, err = g.listBans(user)
	case "approve", "decline":
		err = g.decide(user, req.Target, req.Type == "approve")
	case "kick":
		err = g.removeMember(user, req.Target, false, false)
	case "ban":
		err = g.removeMember(user, req.Target, true, req.BanIP)
	case "unban":
		err = g.unban(user, req.Target)
	case "timeout":
		err = g.timeout(user, req.Target, req.Minutes)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}

func (g *Server) joinSettings(user string) (*proto.JoinSettings, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageJoining); err != nil {
		return nil, err
	}
	j := g.join
	return &proto.JoinSettings{
		ShowJoinButton: j.ShowJoinButton, ShowRulesButton: j.ShowRulesButton, Whitelist: j.Whitelist,
		Allowed: append([]string{}, j.Allowed...), HasPassword: j.PasswordHash != "",
		MinAccountDays: j.MinAccountDays, MinServerCount: j.MinServerCount,
		RulesEnabled: j.RulesEnabled, Rules: j.Rules, RulesPublic: j.RulesPublic, RequireRules: j.RequireRules,
		JoinPlacement: j.JoinPlacement, RequireApproval: j.RequireApproval, PostWaitMinutes: j.PostWaitMinutes,
	}, nil
}

func (g *Server) updateJoin(user string, in *proto.JoinSettings) error {
	if in == nil {
		return errors.New("no settings sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageJoining); err != nil {
		return err
	}
	j := guildstore.JoinSettings{
		ShowJoinButton: in.ShowJoinButton, ShowRulesButton: in.ShowRulesButton, Whitelist: in.Whitelist,
		Allowed: in.Allowed, PasswordHash: g.join.PasswordHash,
		MinAccountDays: in.MinAccountDays, MinServerCount: in.MinServerCount,
		RulesEnabled: in.RulesEnabled, Rules: in.Rules, RulesPublic: in.RulesPublic, RequireRules: in.RequireRules,
		JoinPlacement: in.JoinPlacement, RequireApproval: in.RequireApproval, PostWaitMinutes: in.PostWaitMinutes,
	}
	j.RulesUpdated = g.join.RulesUpdated
	if strings.TrimSpace(in.Rules) != strings.TrimSpace(g.join.Rules) && g.join.Rules != "" {
		j.RulesUpdated = g.nowText() // Members are told.
	}
	if in.Password != nil {
		j.PasswordHash = ""
		if *in.Password != "" {
			if len(*in.Password) < 4 {
				return errors.New("the guild password must be at least 4 characters")
			}
			j.PasswordHash = guildstore.HashPassword(*in.Password)
		}
	}
	if err := j.Check(); err != nil {
		return err
	}
	if err := guildstore.SaveJoinSettings(g.DataDir, j); err != nil {
		return errors.New("the guild could not save its join settings")
	}
	g.join = j
	g.Log.Printf("join settings changed by the owner")
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	return nil
}

func (g *Server) captchaGet(user string) (*proto.CaptchaSettings, []proto.CaptchaQuestion, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageJoining); err != nil {
		return nil, nil, err
	}
	c := g.captcha
	set := &proto.CaptchaSettings{Enabled: c.Enabled, AskCount: c.AskCount, PassPercent: c.PassPercent,
		Welcome: c.Welcome, PassMessage: c.PassMessage, FailMessage: c.FailMessage}
	qs := []proto.CaptchaQuestion{}
	for _, q := range c.Questions {
		qs = append(qs, proto.CaptchaQuestion{ID: q.ID, Type: q.Type, Text: q.Text, HasImage: q.ImageHash != "",
			ImageAlt: q.ImageAlt, Options: q.Options, Correct: q.Correct, Answers: q.Answers, CaseSensitive: q.CaseSensitive,
			Weight: q.Weight, AlwaysAsk: q.AlwaysAsk})
	}
	return set, qs, nil
}

func (g *Server) captchaSettings(user string, in *proto.CaptchaSettings) error {
	if in == nil {
		return errors.New("no settings sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageJoining); err != nil {
		return err
	}
	c := g.captcha
	c.Enabled, c.AskCount, c.PassPercent = in.Enabled, in.AskCount, in.PassPercent
	c.Welcome, c.PassMessage, c.FailMessage = in.Welcome, in.PassMessage, in.FailMessage
	if err := c.CheckSettings(); err != nil {
		return err
	}
	if c.Enabled && len(c.Questions) == 0 {
		return errors.New("add at least one question before turning the CAPTCHA on")
	}
	if err := guildstore.SaveCaptcha(g.DataDir, c); err != nil {
		return errors.New("the guild could not save the CAPTCHA")
	}
	g.captcha = c
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	return nil
}

// captchaPut adds a question (empty ID) or replaces one; returns its ID.
func (g *Server) captchaPut(user string, req proto.Request) (string, error) {
	in := req.Question
	if in == nil {
		return "", errors.New("no question sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageJoining); err != nil {
		return "", err
	}
	q := guildstore.Question{ID: in.ID, Type: in.Type, Text: in.Text, ImageAlt: in.ImageAlt,
		Options: in.Options, Correct: in.Correct, Answers: in.Answers, CaseSensitive: in.CaseSensitive,
		Weight: in.Weight, AlwaysAsk: in.AlwaysAsk}
	idx := -1
	for i, old := range g.captcha.Questions {
		if old.ID == q.ID && q.ID != "" {
			idx = i
			q.ImageHash = old.ImageHash
		}
	}
	if idx < 0 {
		if len(g.captcha.Questions) >= guildstore.MaxQuestions {
			return "", errors.New("the question bank is full (16 questions)")
		}
		b := make([]byte, 6)
		rand.Read(b)
		q.ID = hex.EncodeToString(b)
	}
	if req.ClearImage {
		q.ImageHash = ""
	} else if len(req.Image) > 0 {
		if err := guildstore.CheckCaptchaImage(req.Image); err != nil {
			return "", err
		}
		q.ImageHash = guildstore.PictureHash(req.Image)
	}
	if err := q.Check(); err != nil {
		return "", err
	}
	if req.ClearImage {
		guildstore.SaveCaptchaImage(g.DataDir, q.ID, nil)
	} else if len(req.Image) > 0 {
		if err := guildstore.SaveCaptchaImage(g.DataDir, q.ID, req.Image); err != nil {
			return "", errors.New("the guild could not save the picture")
		}
	}
	c := g.captcha
	c.Questions = append([]guildstore.Question{}, c.Questions...)
	if idx < 0 {
		c.Questions = append(c.Questions, q)
	} else {
		c.Questions[idx] = q
	}
	if err := guildstore.SaveCaptcha(g.DataDir, c); err != nil {
		return "", errors.New("the guild could not save the question")
	}
	g.captcha = c
	return q.ID, nil
}

func (g *Server) captchaDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageJoining); err != nil {
		return err
	}
	c := g.captcha
	var kept []guildstore.Question
	for _, q := range c.Questions {
		if q.ID != id {
			kept = append(kept, q)
		}
	}
	if len(kept) == len(c.Questions) {
		return errors.New("there is no such question")
	}
	c.Questions = kept
	if len(kept) == 0 {
		c.Enabled = false // Nothing is left to ask, so the CAPTCHA is turned off.
	}
	if err := guildstore.SaveCaptcha(g.DataDir, c); err != nil {
		return errors.New("the guild could not save the CAPTCHA")
	}
	guildstore.SaveCaptchaImage(g.DataDir, id, nil)
	g.captcha = c
	return nil
}

func (g *Server) captchaImage(user, id string) ([]byte, error) {
	g.mu.Lock()
	allowed := g.hasLocked(user, proto.PermManageJoining)
	g.mu.Unlock()
	if !allowed {
		return nil, errOwnerOnly
	}
	return guildstore.LoadCaptchaImage(g.DataDir, id)
}

func (g *Server) listRequests(user string) ([]proto.PersonInfo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermApproveJoins); err != nil {
		return nil, err
	}
	out := []proto.PersonInfo{}
	for _, r := range g.requests {
		out = append(out, proto.PersonInfo{UserKey: r.UserKey, Name: r.Name, At: r.At.Format(time.RFC3339), Created: timeText(r.Created), Guilds: r.Guilds})
	}
	return out, nil
}

func (g *Server) listBans(user string) ([]proto.PersonInfo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermBan); err != nil {
		return nil, err
	}
	out := []proto.PersonInfo{}
	for _, b := range g.bans {
		out = append(out, proto.PersonInfo{UserKey: b.UserKey, Name: b.Name, At: b.At.Format(time.RFC3339), IP: b.IP})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out, nil
}

// decide approves or declines a join request.
func (g *Server) decide(user, target string, approve bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermApproveJoins); err != nil {
		return err
	}
	i := g.requestIndexLocked(target)
	if i < 0 {
		return errors.New("there is no such join request")
	}
	r := g.requests[i]
	g.requests = append(g.requests[:i:i], g.requests[i+1:]...)
	if err := guildstore.SaveRequests(g.DataDir, g.requests); err != nil {
		g.Log.Printf("could not save join requests: %v", err)
	}
	event := "declined"
	if approve {
		g.addMemberLocked(r.UserKey, r.Name, r.IP, r.Created, r.Guilds)
		event = "approved"
	} else {
		g.Log.Printf("join request from %s declined", r.UserKey)
	}
	if c := g.waiting[target]; c != nil {
		g.notifyLater([]*proto.Conn{c}, event)
	}
	g.notifyLater(g.ownerConnsLocked(), "requests_changed")
	return nil
}

// removeMember kicks or bans target. A ban also works on a
// person who is only waiting for approval.
func (g *Server) removeMember(user, target string, ban, banIP bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	need := proto.PermKick
	if ban {
		need = proto.PermBan
	}
	if err := g.needLocked(user, need); err != nil {
		return err
	}
	if _, member := g.members[target]; member && !g.outranksLocked(user, target) {
		return errors.New("you can only remove people ranked below you")
	}
	if target == user {
		return errors.New("you can't remove yourself; use Leave guild")
	}
	m, isMember := g.members[target]
	ri := g.requestIndexLocked(target)
	if !isMember && (!ban || ri < 0) {
		return errors.New("that person is not a member")
	}
	name, ip := m.Name, m.LastIP
	if ri >= 0 {
		r := g.requests[ri]
		name, ip = r.Name, r.IP
		g.requests = append(g.requests[:ri:ri], g.requests[ri+1:]...)
		guildstore.SaveRequests(g.DataDir, g.requests)
	}
	if isMember {
		delete(g.members, target)
		g.voiceLeaveLocked(target)
		g.saveMembersLocked()
	}
	if g.transfer != nil && (g.transfer.To == target || g.transfer.From == target) {
		g.transfer = nil
	}
	if isMember {
		g.retireKeysLocked() // The channel keys are replaced.
	}
	if ban {
		b := guildstore.Ban{UserKey: target, Name: name, At: g.now().UTC().Truncate(time.Second)}
		if banIP {
			b.IP = ip
		}
		g.bans = append(g.bans, b)
		if err := guildstore.SaveBans(g.DataDir, g.bans); err != nil {
			g.Log.Printf("could not save bans: %v", err)
		}
		g.Log.Printf("%s banned (address blocked: %v)", target, banIP && ip != "")
	} else {
		g.Log.Printf("%s kicked", target)
	}
	// Tell them, and end their session.
	var conns []*proto.Conn
	if s := g.sessions[target]; s != nil {
		conns = append(conns, s.conn)
		delete(g.sessions, target)
	}
	if c := g.waiting[target]; c != nil {
		conns = append(conns, c)
		delete(g.waiting, target)
	}
	go func() {
		g.notify(conns, "removed")
		for _, c := range conns {
			c.Close()
		}
	}()
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	return nil
}

func (g *Server) unban(user, target string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermBan); err != nil {
		return err
	}
	var kept []guildstore.Ban
	for _, b := range g.bans {
		if b.UserKey != target {
			kept = append(kept, b)
		}
	}
	if len(kept) == len(g.bans) {
		return errors.New("that person is not banned")
	}
	g.bans = kept
	if err := guildstore.SaveBans(g.DataDir, g.bans); err != nil {
		return errors.New("the guild could not save the bans")
	}
	g.Log.Printf("%s unbanned", target)
	return nil
}
