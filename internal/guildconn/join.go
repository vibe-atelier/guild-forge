package guildconn

// This file handles joining a guild from the app. A join may need a CAPTCHA, so the
// connection stays open between "join" and "join_answers".

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"sync"
	"time"

	"nexus/internal/proto"
	"nexus/internal/user"
)

// JoinResult is what the window shows after a join step.
type JoinResult struct {
	OK bool `json:"ok"`
	// It is joined, pending, captcha, captcha_failed, or the rule that failed.
	Code      string      `json:"code"`
	Error     string      `json:"error,omitempty"`
	Message   string      `json:"message,omitempty"` // It is the owner's pass or fail message.
	TriesLeft int         `json:"tries_left,omitempty"`
	Quiz      *proto.Quiz `json:"quiz,omitempty"`
	GuildID   string      `json:"guild_id,omitempty"`
}

type joining struct {
	sess    *proto.Session
	id      string
	addr    string
	name    string
	started time.Time
}

var joinMu sync.Mutex

// JoinStart connects to the guild at addr and asks to join.
func (m *Manager) JoinStart(addr, expectID, password string, agreed bool) (*JoinResult, error) {
	addr, err := CheckAddress(addr)
	if err != nil {
		return nil, err
	}
	m.JoinCancel()
	c, err := proto.Dial(addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s", addr)
	}
	s := proto.NewSession(c)
	id := KeyText(s.GuildKey())
	if expectID != "" && id != expectID {
		s.Close()
		return nil, errors.New("the guild at this address changed since you checked it; check again")
	}
	u := m.u
	r, err := s.Call(proto.Request{
		Type: "join", UserKey: u.SigningKey.Public().(ed25519.PublicKey),
		Sig:      ed25519.Sign(u.SigningKey, proto.JoinProof(s.SessionID())),
		UserName: u.Profile.DefaultName, Created: u.Profile.CreationDate.UTC().Format(time.RFC3339),
		ServerCount: u.MemberGuildCount(), JoinPassword: password, AgreedRules: agreed,
	}, callTimeout)
	if err != nil {
		s.Close()
		return nil, err
	}
	name := DefaultGuildName
	if a, err := s.Call(proto.Request{Type: "about", PictureHash: "-"}, callTimeout); err == nil && a.Guild != nil {
		name = a.Guild.Name
	}
	j := &joining{sess: s, id: id, addr: addr, name: name, started: time.Now()}
	res := m.joinOutcome(j, r)
	if res.Code == "captcha" {
		joinMu.Lock()
		m.joining = j // Keep the connection for the answers.
		joinMu.Unlock()
	} else {
		s.Close()
	}
	return res, nil
}

// JoinAnswers sends the CAPTCHA answers for the join in progress.
func (m *Manager) JoinAnswers(answers []proto.QuizAnswer) (*JoinResult, error) {
	joinMu.Lock()
	j := m.joining
	m.joining = nil
	joinMu.Unlock()
	if j == nil {
		return nil, errors.New("start joining again")
	}
	defer j.sess.Close()
	r, err := j.sess.Call(proto.Request{Type: "join_answers", Answers: answers}, callTimeout)
	if err != nil {
		return nil, errors.New("the guild did not answer; try joining again")
	}
	return m.joinOutcome(j, r), nil
}

// JoinCancel drops a join in progress.
func (m *Manager) JoinCancel() {
	joinMu.Lock()
	j := m.joining
	m.joining = nil
	joinMu.Unlock()
	if j != nil {
		j.sess.Close()
	}
}

// joinOutcome turns the guild's answer into a result, and saves the guild
// to the user's list when they joined or are waiting for approval.
func (m *Manager) joinOutcome(j *joining, r proto.Response) *JoinResult {
	res := &JoinResult{OK: r.OK, Code: r.Code, Error: r.Error, Message: r.Message, TriesLeft: r.TriesLeft, Quiz: r.Quiz, GuildID: j.id}
	switch {
	case r.OK && r.Receipt != nil:
		res.Code = "joined"
		m.saveJoined(j, "", &user.GuildReceipt{Joined: r.Receipt.Joined, Sig: r.Receipt.Sig}, r.Receipt)
	case r.Code == "pending":
		m.saveJoined(j, "pending", nil, nil)
	}
	return res
}

func (m *Manager) saveJoined(j *joining, membership string, receipt *user.GuildReceipt, signed *proto.JoinReceipt) {
	// Keep the receipt only if the guild really signed it.
	if signed != nil && !ed25519.Verify(j.sess.GuildKey(), proto.ReceiptMessage(signed.GuildID, signed.UserKey, signed.Joined), signed.Sig) {
		receipt = nil
	}
	g := user.Guild{GuildID: j.id, GuildName: j.name, SignatureVerificationKey: j.id,
		JoinDate: time.Now().UTC().Truncate(time.Second), GuildAddress: j.addr,
		Membership: membership, JoinReceipt: receipt}
	if err := m.u.SaveGuild(g); err != nil {
		return
	}
	m.Remove(j.id) // Restart its connection with the new membership.
	m.Add(g)
	m.onChange()
}

// Forget removes a guild the user is not (or is no longer) a member of from
// their list, e.g. after a declined request or being removed.
func (m *Manager) Forget(id string) error {
	m.Remove(id)
	return m.u.RemoveGuild(id)
}
