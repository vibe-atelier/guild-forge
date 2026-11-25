package guildconn

// This file decides what deserves a pop-up notification.

import (
	"strings"
	"sync"

	"nexus/internal/proto"
)

// Notice is something to tell the user about with a pop-up.
type Notice struct {
	Kind      string // It is "mention", "reply", "message" or "dm".
	Guild     string // It is "" for a private message.
	GuildName string
	Channel   string // It is the channel ID, or the contact's ID for "dm".
	Where     string // It is like "#general", or "".
	From      string // It is the sender's user ID.
	Name      string // It is the sender's name.
	Text      string // It is the message, on one line.
}

var noticeMu sync.Mutex

// OnNotice sets what is called for each notice (from any goroutine).
func (m *Manager) OnNotice(f func(Notice)) {
	noticeMu.Lock()
	m.onNotice = f
	noticeMu.Unlock()
}

func (m *Manager) notice(n Notice) {
	noticeMu.Lock()
	f := m.onNotice
	noticeMu.Unlock()
	if f != nil {
		go f(n)
	}
}

// noticeLocked decides about a new channel message (gc.mu held).
func (gc *Conn) noticeLocked(m proto.ChatMessage, me string) {
	if m.Author == me || m.Deleted || gc.m.IsBlocked(m.Author) {
		return
	}
	text, ok := gc.decryptLocked(m)
	text = previewText(text)
	if !ok {
		return
	}
	kind := "message"
	if strings.Contains(text, "<@"+me+">") {
		kind = "mention"
	} else {
		for _, mem := range gc.members {
			if mem.UserKey != me {
				continue
			}
			for _, r := range mem.Roles {
				if strings.Contains(text, "<@&"+r+">") {
					kind = "mention"
				}
			}
		}
	}
	if kind == "message" && m.ReplyTo != "" {
		for _, r := range gc.chat.msgs[m.Channel] {
			if r.ID == m.ReplyTo && r.Author == me {
				kind = "reply"
			}
		}
	}
	choice, _ := gc.notifyLocked(m.Channel)
	if choice == proto.NotifyNothing || (choice == proto.NotifyMentions && kind == "message") {
		return
	}
	names := gc.memberNamesLocked()
	where := ""
	for _, c := range gc.chat.channels {
		if c.ID == m.Channel {
			where = "#" + c.Name
		}
	}
	if f, ok := ParseFileRef(text); ok {
		text = "📎 " + f.Name
	}
	gc.m.notice(Notice{Kind: kind, Guild: gc.id, GuildName: gc.name, Channel: m.Channel, Where: where,
		From: m.Author, Name: names.name(m.Author), Text: firstLine(names.mentionsToNames(text), 120)})
}
