package guild

// This file handles joining a guild: the join rules in their fixed order,
// the CAPTCHA, approval and the guild-signed
// joining receipt.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	captchaTries   = 3              // This is the number of tries.
	captchaLockout = 24 * time.Hour // This is how long the lockout lasts.
	maxNameLength  = 32
)

// joinState is a join in progress on one connection (between "join" and
// "join_answers").
type joinState struct {
	user, name, ip string
	created        time.Time // These are account facts for staff.
	guilds         int
	asked          []askedQuestion
}

// askedQuestion remembers how a question's choices were shuffled, so the
// numbers the person answers with can be mapped back.
type askedQuestion struct {
	q     guildstore.Question
	order []int // It maps each shown position to the original choice number.
}

func ipOf(from string) string {
	host, _, err := net.SplitHostPort(from)
	if err != nil {
		return from
	}
	return host
}

func cleanName(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxNameLength {
		s = string([]rune(s)[:maxNameLength])
	}
	return s
}

// joinInfoLocked describes what joining needs, for the public About page.
func (g *Server) joinInfoLocked() *proto.JoinInfo {
	j := g.join
	return &proto.JoinInfo{
		ShowJoinButton: j.ShowJoinButton, ShowRulesButton: j.ShowRulesButton,
		RulesEnabled: j.RulesEnabled, RulesPublic: j.RulesPublic, RequireRules: j.RulesEnabled && j.RequireRules,
		JoinPlacement: j.JoinPlacement, NeedsPassword: j.PasswordHash != "", Whitelist: j.Whitelist,
		RequireApproval: j.RequireApproval, Captcha: g.captchaActiveLocked(),
		MinAccountDays: j.MinAccountDays, MinServerCount: j.MinServerCount, RulesUpdated: j.RulesUpdated,
		PostWaitMinutes: j.PostWaitMinutes,
	}
}

func (g *Server) captchaActiveLocked() bool {
	return g.captcha.Enabled && len(g.captcha.Questions) > 0
}

func (g *Server) isBannedLocked(user, ip string) bool {
	if g.isOwnerLocked(user) {
		return false // An address ban must never lock out the owner, who may share the address.
	}
	for _, b := range g.bans {
		if b.UserKey == user || (b.IP != "" && b.IP == ip) {
			return true
		}
	}
	return false
}

func (g *Server) requestIndexLocked(user string) int {
	for i, r := range g.requests {
		if r.UserKey == user {
			return i
		}
	}
	return -1
}

// rulesFor returns the Rules text if user may read it.
func (g *Server) rulesFor(user string) proto.Response {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, member := g.members[user]
	if !g.join.RulesEnabled {
		return proto.Response{Type: "rules", Code: "no_rules", Error: "this guild has no Rules page"}
	}
	if !g.join.RulesPublic && !member {
		return proto.Response{Type: "rules", Code: "members_only", Error: "the rules are only shown to members"}
	}
	return proto.Response{Type: "rules", OK: true, Rules: g.join.Rules}
}

// join runs the join rules for the person on connection c.
func (g *Server) joinRequest(c *proto.Conn, req proto.Request, from string, js **joinState) proto.Response {
	fail := func(code, msg string) proto.Response {
		return proto.Response{Type: "join", Code: code, Error: msg}
	}
	if !validUserSig(req.UserKey, proto.JoinProof(c.SessionID), req.Sig) {
		return fail("bad_signature", "your key's signature is wrong")
	}
	user := guildstore.KeyText(req.UserKey)
	ip := ipOf(from)
	name := cleanName(req.UserName)
	created, _ := time.Parse(time.RFC3339, req.Created)
	facts := &joinState{user: user, name: name, ip: ip, created: created.UTC(), guilds: max(0, req.ServerCount)}

	g.mu.Lock()
	defer g.mu.Unlock()
	j := g.join

	if g.isBannedLocked(user, ip) {
		return fail("banned", "you are banned from this guild")
	}
	if _, ok := g.members[user]; ok {
		return fail("already_member", "you are already a member of this guild")
	}
	if g.requestIndexLocked(user) >= 0 {
		g.waiting[user] = c
		return proto.Response{Type: "join", Code: "pending", Error: "your request is waiting for the owner's approval"}
	}
	if j.Whitelist && !contains(j.Allowed, user) {
		return fail("not_invited", "this guild is invite-only, and your user ID is not on its list")
	}
	if j.MinAccountDays > 0 {
		created, err := time.Parse(time.RFC3339, req.Created)
		if err != nil || created.After(g.now().Add(time.Hour)) || g.now().Sub(created) < time.Duration(j.MinAccountDays)*24*time.Hour {
			return fail("too_new", fmt.Sprintf("this guild requires accounts at least %d days old", j.MinAccountDays))
		}
	}
	if req.ServerCount < j.MinServerCount {
		return fail("too_few_guilds", fmt.Sprintf("this guild requires being in at least %d other guilds", j.MinServerCount))
	}
	if stored := j.PasswordHash; stored != "" {
		// The check is slow on purpose: don't hold the guild up meanwhile.
		g.mu.Unlock()
		ok := guildstore.VerifyPassword(req.JoinPassword, stored)
		var better string
		if ok && guildstore.OldPasswordHash(stored) {
			better = guildstore.HashPassword(req.JoinPassword)
		}
		g.mu.Lock()
		if !ok {
			return fail("wrong_password", "wrong guild password")
		}
		if better != "" && g.join.PasswordHash == stored { // This replaces the old plain hash.
			g.join.PasswordHash = better
			guildstore.SaveJoinSettings(g.DataDir, g.join)
		}
	}
	if j.RulesEnabled && j.RequireRules && !req.AgreedRules {
		return fail("rules_required", "you need to agree to the rules first")
	}

	if g.captchaActiveLocked() {
		if until, locked := g.captchaLockedLocked(user, ip); locked {
			return fail("captcha_locked", "too many failed tries; try again after "+until.Local().Format("2 Jan 15:04"))
		}
		st := facts
		quiz, err := g.makeQuizLocked(st)
		if err != nil {
			return fail("error", err.Error())
		}
		*js = st
		return proto.Response{Type: "join", Code: "captcha", Quiz: quiz}
	}
	return g.finishJoinLocked(c, facts)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// makeQuizLocked picks the questions and shuffles their choices. Questions
// marked Always Ask are always in; random others fill up to the number to
// ask. The order is shuffled too.
func (g *Server) makeQuizLocked(st *joinState) (*proto.Quiz, error) {
	bank := g.captcha.Questions
	var always, others []int
	for i, q := range bank {
		if q.AlwaysAsk {
			always = append(always, i)
		} else {
			others = append(others, i)
		}
	}
	shuffle, err := randomPerm(len(others))
	if err != nil {
		return nil, err
	}
	chosen := always
	for _, k := range shuffle {
		if len(chosen) >= g.captcha.AskCount {
			break
		}
		chosen = append(chosen, others[k])
	}
	order, err := randomPerm(len(chosen))
	if err != nil {
		return nil, err
	}
	var picks []int
	for _, k := range order {
		picks = append(picks, chosen[k])
	}
	quiz := &proto.Quiz{Welcome: g.captcha.Welcome}
	for _, idx := range picks {
		q := bank[idx]
		order, err := randomPerm(len(q.Options))
		if err != nil {
			return nil, err
		}
		qq := proto.QuizQuestion{Type: q.Type, Text: q.Text, ImageAlt: q.ImageAlt}
		for _, o := range order {
			qq.Options = append(qq.Options, q.Options[o])
		}
		if q.ImageHash != "" {
			qq.Image, _ = guildstore.LoadCaptchaImage(g.DataDir, q.ID)
		}
		st.asked = append(st.asked, askedQuestion{q: q, order: order})
		quiz.Questions = append(quiz.Questions, qq)
	}
	return quiz, nil
}

func randomPerm(n int) ([]int, error) {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return nil, err
		}
		p[i], p[j.Int64()] = p[j.Int64()], p[i]
	}
	return p, nil
}

// grade reports whether ans answers the asked question a correctly.
func (a askedQuestion) grade(ans proto.QuizAnswer) bool {
	q := a.q
	if q.Type == guildstore.QText {
		for _, want := range q.Answers { // An answer must match exactly.
			if ans.Text == want || (!q.CaseSensitive && strings.EqualFold(ans.Text, want)) {
				return true
			}
		}
		return false
	}
	chosen := map[int]bool{}
	for _, shown := range ans.Choices {
		if shown < 0 || shown >= len(a.order) {
			return false
		}
		chosen[a.order[shown]] = true
	}
	if len(chosen) != len(q.Correct) {
		return false
	}
	for _, c := range q.Correct {
		if !chosen[c] {
			return false
		}
	}
	return true
}

// joinAnswers grades the CAPTCHA and, if passed, continues the join.
func (g *Server) joinAnswers(c *proto.Conn, req proto.Request, js **joinState) proto.Response {
	st := *js
	if st == nil {
		return proto.Response{Type: "join", Code: "no_quiz", Error: "start joining first"}
	}
	*js = nil // Only one try is allowed per quiz.
	right, counted, missedMust := 0, 0, false
	for i, a := range st.asked {
		ok := i < len(req.Answers) && a.grade(req.Answers[i])
		switch a.q.Weight {
		case guildstore.WeightNone: // The question is asked but not counted.
			continue
		case guildstore.WeightMust:
			missedMust = missedMust || !ok
		}
		counted++
		if ok {
			right++
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	score := 100
	if counted > 0 {
		score = right * 100 / counted
	}
	if score < g.captcha.PassPercent || missedMust {
		left := g.recordCaptchaFailLocked(st.user, st.ip)
		g.Log.Printf("CAPTCHA failed by %s (%d%%)", st.user, score)
		msg := g.captcha.FailMessage
		if msg == "" {
			msg = "Not quite. You did not pass this time."
		}
		r := proto.Response{Type: "join", Code: "captcha_failed", Message: msg, TriesLeft: left}
		if left == 0 {
			r.Error = "too many failed tries; try again in 24 hours"
		} else {
			r.Error = "you did not pass the questions"
		}
		return r
	}
	r := g.finishJoinLocked(c, st)
	msg := g.captcha.PassMessage
	if msg == "" {
		msg = "Well done!"
	}
	r.Message = msg
	return r
}

func (g *Server) captchaLockedLocked(user, ip string) (time.Time, bool) {
	for _, key := range []string{"u:" + user, "ip:" + ip} {
		var recent []time.Time
		for _, t := range g.attempts[key] {
			if g.now().Sub(t) < captchaLockout {
				recent = append(recent, t)
			}
		}
		g.attempts[key] = recent
		if len(recent) >= captchaTries {
			return recent[0].Add(captchaLockout), true
		}
	}
	return time.Time{}, false
}

// recordCaptchaFailLocked counts a failed CAPTCHA (per user key and per
// address) and returns how many tries are left.
func (g *Server) recordCaptchaFailLocked(user, ip string) int {
	now := g.now()
	left := captchaTries
	for _, key := range []string{"u:" + user, "ip:" + ip} {
		g.attempts[key] = append(g.attempts[key], now)
		n := 0
		for _, t := range g.attempts[key] {
			if now.Sub(t) < captchaLockout {
				n++
			}
		}
		left = min(left, max(captchaTries-n, 0))
	}
	return left
}

// finishJoinLocked either files a join request or adds the member.
func (g *Server) finishJoinLocked(c *proto.Conn, st *joinState) proto.Response {
	user, name, ip := st.user, st.name, st.ip
	if g.join.RequireApproval {
		if g.requestIndexLocked(user) < 0 {
			g.requests = append(g.requests, guildstore.JoinRequest{UserKey: user, Name: name, IP: ip, At: g.now().UTC().Truncate(time.Second), Created: st.created, Guilds: st.guilds})
			if err := guildstore.SaveRequests(g.DataDir, g.requests); err != nil {
				g.Log.Printf("could not save join requests: %v", err)
			}
			g.Log.Printf("join request from %s (%s)", name, user)
			g.notifyLater(g.ownerConnsLocked(), "requests_changed")
		}
		g.waiting[user] = c
		return proto.Response{Type: "join", Code: "pending", Error: "your request is waiting for the owner's approval"}
	}
	receipt := g.addMemberLocked(user, name, ip, st.created, st.guilds)
	return proto.Response{Type: "join", OK: true, Code: "joined", Receipt: receipt}
}

// addMemberLocked makes user a member and returns their signed receipt.
func (g *Server) addMemberLocked(user, name, ip string, created time.Time, guilds int) *proto.JoinReceipt {
	now := g.now().UTC().Truncate(time.Second)
	g.members[user] = guildstore.Member{UserKey: user, Name: name, Joined: now, LastIP: ip,
		AgreedRules: g.join.RulesEnabled && g.join.RequireRules, Created: created, Guilds: guilds}
	g.saveMembersLocked()
	g.Log.Printf("%s (%s) joined", name, user)
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	g.waitEndsLocked(user)
	joined := now.Format(time.RFC3339)
	return &proto.JoinReceipt{GuildID: g.ID, UserKey: user, Joined: joined,
		Sig: ed25519.Sign(g.key, proto.ReceiptMessage(g.ID, user, joined))}
}

func (g *Server) ownerConnsLocked() []*proto.Conn {
	if g.owner == nil {
		return nil
	}
	return g.connsOfLocked(g.owner.Key)
}

// notifyLater sends an event without blocking the caller (who holds g.mu).
func (g *Server) notifyLater(conns []*proto.Conn, event string) {
	if len(conns) > 0 {
		go g.notify(conns, event)
	}
}

// cleanTyping keeps a custom typing text short and on one line.
func cleanTyping(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 60 {
		s = string([]rune(s)[:60])
	}
	return s
}
