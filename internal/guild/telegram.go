package guild

// This file has the chat features from Telegram. Messages stay
// sealed: the guild can't read a poll's question, a spoiler or a forwarded
// message. It keeps what it must to do its part: votes, pins, Slow Mode,
// when a message disappears, and scheduled messages until their time.

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	maxEarlier      = 20 // This much Edit History is kept per message.
	maxPins         = 50 // This is per channel.
	maxPollChoices  = 10
	maxScheduled    = 25 // This is per member.
	maxSlowSeconds  = 6 * 60 * 60
	maxScheduleAway = 365 * 24 * time.Hour
	maxDisappear    = 4 * 7 * 24 * time.Hour
	pinsFile        = "pins.json"
	scheduledFile   = "scheduled.json"
	clockTick       = 10 * time.Second
)

type telegramState struct {
	pins      map[string][]string // It maps each channel to its pinned message IDs, newest first.
	scheduled []proto.Scheduled
	lastSent  map[string]time.Time // It maps "channel|user" to when they last sent, for Slow Mode.
}

func (g *Server) loadTelegram() error {
	g.tg.pins = map[string][]string{}
	if err := guildstore.LoadPage(g.DataDir, pinsFile, &g.tg.pins); err != nil {
		return err
	}
	if g.tg.pins == nil {
		g.tg.pins = map[string][]string{}
	}
	return guildstore.LoadPage(g.DataDir, scheduledFile, &g.tg.scheduled)
}

// checkExtras checks a new message's disappearing time and poll and copies them into m.
func checkExtras(in, m *proto.ChatMessage, now time.Time) error {
	if in.Expires != "" {
		t, err := time.Parse(time.RFC3339, in.Expires)
		if err != nil || !t.After(now) || t.Sub(now) > maxDisappear+time.Hour {
			return errors.New("a message can disappear after at most four weeks")
		}
		m.Expires = t.UTC().Format(time.RFC3339)
	}
	if in.Poll != nil {
		if in.Poll.Choices < 2 || in.Poll.Choices > maxPollChoices {
			return fmt.Errorf("a poll has 2 to %d choices", maxPollChoices)
		}
		p := *in.Poll
		m.Poll = &p
		m.Votes = make([][]string, p.Choices)
	}
	return nil
}

// slowLocked enforces Slow Mode: one message every so many
// seconds, except for those who manage messages or channels.
func (g *Server) slowLocked(user, channel string) error {
	n := g.nodeLocked(channel)
	if n == nil || n.Slow <= 0 || g.canLocked(user, channel, proto.PermManageMessages) || g.hasLocked(user, proto.PermManageChannels) {
		return nil
	}
	if g.tg.lastSent == nil {
		g.tg.lastSent = map[string]time.Time{}
	}
	key := channel + "|" + user
	wait := time.Duration(n.Slow)*time.Second - g.now().Sub(g.tg.lastSent[key])
	if wait > 0 {
		return fmt.Errorf("slow Mode is on here: wait %d seconds", int(wait.Seconds()+0.999))
	}
	g.tg.lastSent[key] = g.now()
	return nil
}

func checkSlow(s int) error {
	if s < 0 || s > maxSlowSeconds {
		return errors.New("slow Mode can be at most six hours")
	}
	return nil
}

// ---- polls -----------------------------------------------------------------------------

// pollVote records user's choice in a poll (choice -1 takes it back; a quiz
// vote can't be changed).
func (g *Server) pollVote(user, channel, id string, choice int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.findMessageLocked(channel, id)
	if m == nil || m.Deleted || m.Poll == nil {
		return errors.New("that poll is gone")
	}
	if !g.canSeeLocked(user, channel) || !g.canLocked(user, channel, proto.PermSendMessages) {
		return errors.New("you can't vote in this channel")
	}
	if choice < -1 || choice >= m.Poll.Choices {
		return errors.New("there is no such choice")
	}
	n := *m
	n.Votes = make([][]string, m.Poll.Choices)
	had := -1
	for i := range n.Votes {
		if i < len(m.Votes) {
			for _, u := range m.Votes[i] {
				if u == user {
					had = i
					continue
				}
				n.Votes[i] = append(n.Votes[i], u)
			}
		}
	}
	if m.Poll.Quiz && had >= 0 {
		return errors.New("a quiz answer can't be changed")
	}
	if choice >= 0 {
		n.Votes[choice] = append(n.Votes[choice], user)
	}
	return g.updateMessageLocked(m, n)
}

// ---- pins ----------------------------------------------------------------------

func (g *Server) pin(user, channel, id string, on bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.findMessageLocked(channel, id)
	switch {
	case m == nil || m.Deleted:
		return errors.New("that message is gone")
	case !g.canSeeLocked(user, channel) || !g.canLocked(user, channel, proto.PermPinMessages):
		return errors.New("you can't pin messages in this channel")
	}
	list := slices.DeleteFunc(slices.Clone(g.tg.pins[channel]), func(x string) bool { return x == id })
	if on {
		if len(list) >= maxPins {
			return errors.New("a channel can have at most 50 pinned messages")
		}
		list = append([]string{id}, list...)
	}
	g.setPinsLocked(channel, list)
	return nil
}

func (g *Server) setPinsLocked(channel string, list []string) {
	if len(list) == 0 {
		delete(g.tg.pins, channel)
	} else {
		g.tg.pins[channel] = list
	}
	if err := guildstore.SavePage(g.DataDir, pinsFile, g.tg.pins); err != nil {
		g.Log.Printf("could not save the pins: %v", err)
	}
	g.notifyLater(g.allConnsLocked(), "channels_changed")
}

// unpinGoneLocked drops a deleted or disappeared message from the pins.
func (g *Server) unpinGoneLocked(channel, id string) {
	if list := g.tg.pins[channel]; slices.Contains(list, id) {
		g.setPinsLocked(channel, slices.DeleteFunc(slices.Clone(list), func(x string) bool { return x == id }))
	}
}

// ---- scheduled messages ----------------------------------------------------------

func (g *Server) schedule(user string, in *proto.ChatMessage, at string) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return "", errors.New("choose a date and time")
	}
	now := g.now()
	switch {
	case !t.After(now.Add(30 * time.Second)):
		return "", errors.New("choose a time in the future")
	case t.Sub(now) > maxScheduleAway:
		return "", errors.New("a message can be scheduled at most a year ahead")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	m, err := g.checkSendLocked(user, in)
	if err != nil {
		return "", err
	}
	if m.Expires != "" { // A disappearing message goes that long after it is posted.
		e, _ := time.Parse(time.RFC3339, m.Expires)
		m.Expires = t.Add(e.Sub(now)).UTC().Format(time.RFC3339)
	}
	mine := 0
	for _, s := range g.tg.scheduled {
		if s.Msg.Author == user {
			mine++
		}
	}
	if mine >= maxScheduled {
		return "", errors.New("you can have at most 25 scheduled messages")
	}
	s := proto.Scheduled{ID: newID(), At: t.UTC().Format(time.RFC3339), Msg: m}
	g.tg.scheduled = append(g.tg.scheduled, s)
	g.saveScheduledLocked()
	return s.ID, nil
}

func (g *Server) saveScheduledLocked() {
	if err := guildstore.SavePage(g.DataDir, scheduledFile, g.tg.scheduled); err != nil {
		g.Log.Printf("could not save the scheduled messages: %v", err)
	}
}

// scheduledOf lists user's scheduled messages, soonest first.
func (g *Server) scheduledOf(user string) []proto.Scheduled {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []proto.Scheduled
	for _, s := range g.tg.scheduled {
		if s.Msg.Author == user {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b proto.Scheduled) int { return cmpStr(a.At, b.At) })
	return out
}

func cmpStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// cancelScheduled removes one of user's scheduled messages.
func (g *Server) cancelScheduled(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := len(g.tg.scheduled)
	g.tg.scheduled = slices.DeleteFunc(g.tg.scheduled, func(s proto.Scheduled) bool { return s.ID == id && s.Msg.Author == user })
	if len(g.tg.scheduled) == n {
		return errors.New("that message was already sent or cancelled")
	}
	g.saveScheduledLocked()
	return nil
}

// ---- the clock: posting scheduled messages, removing disappeared ones ----------------------

// clock runs until the guild stops.
func (g *Server) clock() {
	t := time.NewTicker(clockTick)
	defer t.Stop()
	for {
		select {
		case <-g.stopped:
			return
		case <-t.C:
			g.tick()
		}
	}
}

// tick does what is due now.
func (g *Server) tick() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	// Scheduled messages whose time has come. A writer who lost the right
	// to write there (or left) gets nothing posted.
	var keep []proto.Scheduled
	changed := false
	for _, s := range g.tg.scheduled {
		t, _ := time.Parse(time.RFC3339, s.At)
		if t.After(now) {
			keep = append(keep, s)
			continue
		}
		changed = true
		m := s.Msg
		if _, member := g.members[m.Author]; !member || !g.isChannelLocked(m.Channel) ||
			!g.canLocked(m.Author, m.Channel, proto.PermSendMessages) || !g.canSeeLocked(m.Author, m.Channel) {
			continue
		}
		if m.ReplyTo != "" && g.findMessageLocked(m.Channel, m.ReplyTo) == nil {
			m.ReplyTo = ""
		}
		if _, err := g.postLocked(m); err != nil {
			g.Log.Printf("could not post a scheduled message: %v", err)
		}
	}
	if changed {
		g.tg.scheduled = keep
		g.saveScheduledLocked()
	}
	g.endAuctionsLocked() // Auctions whose time is up are ended.
	// Disappearing messages whose time is up.
	for channel, list := range g.chat.msgs {
		for i := range list {
			m := &list[i]
			if m.Expires == "" || m.Gone {
				continue
			}
			if t, err := time.Parse(time.RFC3339, m.Expires); err == nil && !t.After(now) {
				n := proto.ChatMessage{ID: m.ID, Channel: m.Channel, Author: m.Author, Time: m.Time, Deleted: true, Gone: true}
				if err := g.updateMessageLocked(m, n); err != nil {
					g.Log.Printf("could not remove a disappearing message: %v", err)
				}
				g.unpinGoneLocked(channel, m.ID)
			}
		}
	}
}

// telegramRequest handles polls, pins, forwards and the other message extras. ok is false for other requests.
func (g *Server) telegramRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "poll_vote":
		err = g.pollVote(user, req.ChannelID, req.MessageID, req.Choice)
	case "pin":
		err = g.pin(user, req.ChannelID, req.MessageID, req.On)
	case "schedule":
		resp.Message, err = g.schedule(user, req.Msg, req.At)
	case "pinned":
		resp.Messages, err = g.pinnedMessages(user, req.ChannelID)
	case "scheduled":
		resp.Scheduled = g.scheduledOf(user)
	case "schedule_cancel":
		err = g.cancelScheduled(user, req.ItemID)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}

// pinnedMessages returns a channel's pinned messages, newest pin first.
func (g *Server) pinnedMessages(user, channel string) ([]proto.ChatMessage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.canSeeLocked(user, channel) {
		return nil, errors.New("you can't read this channel")
	}
	var out []proto.ChatMessage
	for _, id := range g.tg.pins[channel] {
		if m := g.findMessageLocked(channel, id); m != nil && !m.Deleted {
			out = append(out, *m)
		}
	}
	return out, nil
}
