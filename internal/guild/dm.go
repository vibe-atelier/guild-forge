package guild

import (
	"crypto/ed25519"
	"errors"
	"sort"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// This file handles private messages and status.
//
// The guild passes sealed envelopes between two members. It can't read them:
// they are encrypted for the two people and signed by the sender. It keeps
// each envelope until the recipient's app says it has saved it, for at most
// 30 days. "Live" envelopes (typing) are only passed to someone
// online, and never kept.

const (
	dmKeepFor     = 30 * 24 * time.Hour
	dmMaxPerUser  = 2000
	dmMaxCipher   = 3 << 20 // This leaves room for a picture inside.
	dmMaxLiveSize = 512
)

func (g *Server) loadDMs() error {
	all, err := guildstore.LoadDMs(g.DataDir)
	g.dms = all
	if g.dms == nil {
		g.dms = map[string][]proto.DMEnvelope{}
	}
	g.purgeDMsLocked()
	return err
}

// purgeDMsLocked drops envelopes older than 30 days, and people's queues
// once they are no longer members.
func (g *Server) purgeDMsLocked() {
	cut := g.now().Add(-dmKeepFor).UTC().Format(time.RFC3339)
	for to, list := range g.dms {
		if _, member := g.members[to]; !member {
			delete(g.dms, to)
			guildstore.SaveDMs(g.DataDir, to, nil)
			continue
		}
		kept := list[:0]
		for _, d := range list {
			if d.Time >= cut {
				kept = append(kept, d)
			}
		}
		if len(kept) != len(list) {
			g.dms[to] = kept
			guildstore.SaveDMs(g.DataDir, to, kept)
		}
	}
}

// dmSend passes a sealed envelope on, and keeps it until it is collected.
func (g *Server) dmSend(user string, in *proto.DMEnvelope) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	d := *in
	if d.From != user {
		return "", errors.New("you can only send as yourself")
	}
	if len(d.Cipher) == 0 || len(d.Cipher) > dmMaxCipher || len(d.Nonce) == 0 || len(d.Nonce) > 32 {
		return "", errors.New("the message is damaged or too big")
	}
	if d.Live && len(d.Cipher) > dmMaxLiveSize {
		return "", errors.New("that is too big to send live")
	}
	pub := g.userPublicKey(user)
	if pub == nil || !ed25519.Verify(pub, d.SignedPart(), d.Sig) {
		return "", errors.New("the message's signature is wrong")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.members[d.To]; !ok || d.To == user {
		return "", errors.New("they are not a member of this guild")
	}
	d.ID = newID()
	d.Time = g.now().UTC().Format(time.RFC3339)
	ev := proto.Response{Type: "event", OK: true, Event: "dm", DM: &d}
	if s := g.sessions[d.To]; s != nil {
		go s.conn.Send(ev)
	}
	if d.Live {
		return d.ID, nil
	}
	list := append(g.dms[d.To], d)
	if len(list) > dmMaxPerUser {
		list = list[len(list)-dmMaxPerUser:]
	}
	g.dms[d.To] = list
	if err := guildstore.SaveDMs(g.DataDir, d.To, list); err != nil {
		return "", errors.New("the guild could not keep the message")
	}
	return d.ID, nil
}

// dmInbox returns the envelopes waiting for user, oldest first.
func (g *Server) dmInbox(user string) []proto.DMEnvelope {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.purgeDMsLocked()
	out := append([]proto.DMEnvelope{}, g.dms[user]...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// dmAck forgets envelopes the recipient's app has saved.
func (g *Server) dmAck(user string, ids []string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	list := g.dms[user]
	kept := list[:0]
	for _, d := range list {
		if !drop[d.ID] {
			kept = append(kept, d)
		}
	}
	if len(kept) == len(list) {
		return nil
	}
	g.dms[user] = kept
	return guildstore.SaveDMs(g.DataDir, user, kept)
}

// ---- status ----------------------------------------------------------

func validStatus(s string) string {
	for _, x := range proto.Statuses {
		if x == s {
			return s
		}
	}
	return "Online"
}

// setStatus changes how an online member shows: Online, Idle, Do Not
// Disturb, or Invisible (shown as offline).
func (g *Server) setStatus(user, status string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s := g.sessions[user]; s != nil {
		s.status = validStatus(status)
	}
	g.notifyLater(g.allConnsLocked(), "presence")
}

// dmRequest handles private message and status requests. ok is false for other requests.
func (g *Server) dmRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "dm_send":
		resp.Message, err = g.dmSend(user, req.DM)
	case "dm_inbox":
		resp.DMs = g.dmInbox(user)
	case "dm_ack":
		err = g.dmAck(user, req.DMIDs)
	case "set_status":
		g.setStatus(user, req.Status)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}
