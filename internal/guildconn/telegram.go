package guildconn

// This file has the chat features from Telegram in the app.
//
// A poll's question and choices, and a forwarded message's origin, travel
// inside the sealed text as a small labelled packet (Body), so the guild
// can't read them; plain messages stay plain text.

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/e2e"
	"nexus/internal/proto"
)

const bodyMark = `{"nexus-body":`

// Body is a message with more than text in it.
type Body struct {
	V       int        `json:"nexus-body"`
	Text    string     `json:"text,omitempty"`
	Poll    *PollBody  `json:"poll,omitempty"`
	Forward *Forwarded `json:"forward,omitempty"`
}

// PollBody is a poll's question and choices (Answer: the right one in a
// quiz, -1 otherwise).
type PollBody struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
	Answer   int      `json:"answer"`
}

// Forwarded says where a forwarded message came from.
type Forwarded struct {
	Name  string `json:"name"`  // It is who wrote the message.
	Where string `json:"where"` // It is like "Guild / #channel", or "a private chat".
	Time  string `json:"time"`  // It is when the message was first written.
}

// ParseBody reads a message's text: a Body, or plain text.
func ParseBody(text string) Body {
	if strings.HasPrefix(text, bodyMark) {
		var b Body
		if json.Unmarshal([]byte(text), &b) == nil && b.V == 1 {
			return b
		}
	}
	return Body{Text: text}
}

func (b Body) encode() string {
	b.V = 1
	out, _ := json.Marshal(b)
	return string(out)
}

// PollView is a poll as the window shows it.
type PollView struct {
	Question string
	Choices  []PollChoice
	Quiz     bool
	Voted    bool // It is true once you have voted (in a quiz, the right answer then shows).
	Total    int
}

// PollChoice is one choice with its votes.
type PollChoice struct {
	Text  string
	Count int
	Mine  bool
	Right bool // It marks the quiz's answer, once you have voted.
}

// fillExtrasLocked adds polls, forwarding, pins and the rest to a view.
func fillExtras(v *ChatView, m proto.ChatMessage, me string, pinned map[string]bool) {
	v.Expires = m.Expires
	v.EditCount = len(m.Earlier)
	v.Pinned = pinned[m.ID]
	if v.Text == "" {
		return
	}
	b := ParseBody(v.Text)
	v.Text = b.Text
	v.Forward = b.Forward
	if b.Poll != nil && m.Poll != nil && len(b.Poll.Choices) == m.Poll.Choices {
		p := &PollView{Question: b.Poll.Question, Quiz: m.Poll.Quiz}
		for i, c := range b.Poll.Choices {
			pc := PollChoice{Text: c}
			if i < len(m.Votes) {
				pc.Count = len(m.Votes[i])
				for _, u := range m.Votes[i] {
					if u == me {
						pc.Mine, p.Voted = true, true
					}
				}
			}
			p.Total += pc.Count
			p.Choices = append(p.Choices, pc)
		}
		if p.Quiz && p.Voted && b.Poll.Answer >= 0 && b.Poll.Answer < len(p.Choices) {
			p.Choices[b.Poll.Answer].Right = true
		}
		v.Poll = p
	}
}

func (gc *Conn) pinnedLocked(channel string) map[string]bool {
	out := map[string]bool{}
	for _, n := range gc.chat.channels {
		if n.ID == channel {
			for _, id := range n.Pins {
				out[id] = true
			}
		}
	}
	return out
}

// SendOptions are the extras of a new message.
type SendOptions struct {
	Disappear time.Duration // The message deletes itself this long after posting (0 means never).
	At        time.Time     // The message is posted at this time instead of now (zero means now).
}

// sealSigned seals text and signs the message.
func (gc *Conn) sealSigned(channel, text, replyTo string) (*proto.ChatMessage, error) {
	version, nonce, sealed, err := gc.seal(channel, text)
	if err != nil {
		return nil, err
	}
	m := &proto.ChatMessage{Channel: channel, Author: gc.me(), Version: version, Nonce: nonce, Cipher: sealed, ReplyTo: replyTo}
	m.Sig = ed25519.Sign(gc.m.u.SigningKey, m.SignedPart())
	return m, nil
}

// post sends (or schedules) a sealed message.
func (gc *Conn) post(m *proto.ChatMessage, o SendOptions) error {
	if o.Disappear > 0 {
		m.Expires = time.Now().Add(o.Disappear).UTC().Format(time.RFC3339)
	}
	if !o.At.IsZero() {
		_, err := gc.Call(proto.Request{Type: "schedule", Msg: m, At: o.At.UTC().Format(time.RFC3339)})
		return err
	}
	r, err := gc.Call(proto.Request{Type: "send", Msg: m})
	if err == nil && r.Chat != nil {
		gc.onMessage(*r.Chat)
		gc.m.onChange()
	}
	return err
}

// SendWith sends a message with extras: disappearing, or scheduled.
func (gc *Conn) SendWith(channel, text, replyTo string, o SendOptions) error {
	m, err := gc.sealSigned(channel, gc.prepareEmoji(text), replyTo)
	if err != nil {
		return err
	}
	return gc.post(m, o)
}

// SendPoll sends a poll (answer: the right choice of a quiz, -1 for none).
func (gc *Conn) SendPoll(channel, question string, choices []string, answer int, o SendOptions) error {
	question = strings.TrimSpace(question)
	var clean []string
	for _, c := range choices {
		if c = strings.TrimSpace(c); c != "" {
			clean = append(clean, c)
		}
	}
	switch {
	case question == "":
		return errors.New("write the question")
	case len(clean) < 2:
		return errors.New("a poll needs at least two choices")
	case len(clean) > 10:
		return errors.New("a poll can have at most ten choices")
	case answer >= len(clean):
		return errors.New("choose the right answer")
	}
	m, err := gc.sealSigned(channel, Body{Poll: &PollBody{Question: question, Choices: clean, Answer: answer}}.encode(), "")
	if err != nil {
		return err
	}
	m.Poll = &proto.PollInfo{Choices: len(clean), Quiz: answer >= 0}
	return gc.post(m, o)
}

// Vote chooses a poll choice (-1 takes your vote back).
func (gc *Conn) Vote(channel, id string, choice int) error {
	_, err := gc.Call(proto.Request{Type: "poll_vote", ChannelID: channel, MessageID: id, Choice: choice})
	return err
}

// Pin pins or unpins a message (Pin Messages).
func (gc *Conn) Pin(channel, id string, on bool) error {
	_, err := gc.Call(proto.Request{Type: "pin", ChannelID: channel, MessageID: id, On: on})
	return err
}

// Pinned lists a channel's pinned messages, newest pin first.
func (gc *Conn) Pinned(channel string) ([]ChatView, error) {
	r, err := gc.Call(proto.Request{Type: "pinned", ChannelID: channel})
	if err != nil {
		return nil, err
	}
	gc.mu.Lock()
	defer gc.mu.Unlock()
	names, me := gc.memberNamesLocked(), gc.me()
	pinned := gc.pinnedLocked(channel)
	var out []ChatView
	for _, m := range r.Messages {
		v := gc.viewLocked(m, names, me, nil)
		fillExtras(&v, m, me, pinned)
		v.Text = names.mentionsToNames(v.Text)
		out = append(out, v)
	}
	return out, nil
}

// ScheduledView is one of your scheduled messages.
type ScheduledView struct {
	ID      string
	At      time.Time
	Channel string
	Text    string // It is the message's first line.
}

// Scheduled lists your scheduled messages in this guild, soonest first.
func (gc *Conn) Scheduled() ([]ScheduledView, error) {
	r, err := gc.Call(proto.Request{Type: "scheduled"})
	if err != nil {
		return nil, err
	}
	gc.mu.Lock()
	defer gc.mu.Unlock()
	names := gc.memberNamesLocked()
	var out []ScheduledView
	for _, s := range r.Scheduled {
		at, _ := time.Parse(time.RFC3339, s.At)
		text := "(can't be read)"
		if t, ok := gc.decryptLocked(s.Msg); ok {
			b := ParseBody(t)
			text = b.Text
			if b.Poll != nil {
				text = "Poll: " + b.Poll.Question
			}
			text = firstLine(names.mentionsToNames(text), 90)
		}
		out = append(out, ScheduledView{ID: s.ID, At: at.Local(), Channel: s.Msg.Channel, Text: text})
	}
	return out, nil
}

// CancelScheduled cancels one of your scheduled messages.
func (gc *Conn) CancelScheduled(id string) error {
	_, err := gc.Call(proto.Request{Type: "schedule_cancel", ItemID: id})
	return err
}

// EarlierView is a message's text before an edit.
type EarlierView struct {
	Text string
	Time string
}

// EditHistory returns a message's earlier texts, oldest first, then its
// text now.
func (gc *Conn) EditHistory(channel, id string) ([]EarlierView, error) {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	names := gc.memberNamesLocked()
	for _, m := range gc.chat.msgs[channel] {
		if m.ID != id {
			continue
		}
		key := func(v int) []byte { return gc.chat.keys[channel][v] }
		var out []EarlierView
		for _, e := range m.Earlier {
			text := "(can't be read)"
			if k := key(e.Version); k != nil {
				if p, err := e2e.Decrypt(k, channel, e.Version, e.Nonce, e.Cipher); err == nil {
					text = names.mentionsToNames(ParseBody(string(p)).Text)
				}
			}
			out = append(out, EarlierView{Text: text, Time: e.Time})
		}
		if t, ok := gc.decryptLocked(m); ok {
			out = append(out, EarlierView{Text: names.mentionsToNames(ParseBody(t).Text), Time: m.Edited})
		}
		return out, nil
	}
	return nil, errors.New("that message isn't loaded")
}

// ForwardText is a message ready to forward: its text with names in place
// of mentions, and where it came from.
func ForwardText(text, name, where, when string) string {
	b := Body{Text: text, Forward: &Forwarded{Name: name, Where: where, Time: when}}
	out := b.encode()
	if utf8.RuneCountInString(out) > maxMessage {
		return ""
	}
	return out
}

// Forward sends a forwarded message to a channel of this guild.
func (gc *Conn) Forward(channel, body string) error {
	if body == "" {
		return errors.New("that message is too long to forward")
	}
	m, err := gc.sealSigned(channel, body, "")
	if err != nil {
		return err
	}
	return gc.post(m, SendOptions{})
}

// previewText is a message's text for a reply line or a pop-up: a poll
// shows its question.
func previewText(t string) string {
	b := ParseBody(t)
	if b.Poll != nil {
		return "Poll: " + b.Poll.Question
	}
	return b.Text
}
