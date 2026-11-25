package guildconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nexus/internal/e2e"
	"nexus/internal/proto"
	"nexus/internal/user"
)

// This file handles contacts and private messages.
//
// A private message is a dmBody, encrypted with the key the two people share
// (e2e.PairKey), signed, and sent through a guild both are in. The other
// app saves it (encrypted) in its user folder and tells the guild it has it.

// dmBody is what is inside an envelope.
type dmBody struct {
	// It is request, accept, decline, msg, edit, delete, react, read, typing or file.
	Kind    string `json:"kind"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"` // It is the sender's name (for requests).
	Text    string `json:"text,omitempty"`
	ReplyTo string `json:"reply_to,omitempty"`
	Emoji   string `json:"emoji,omitempty"`
	On      bool   `json:"on,omitempty"`
	File    string `json:"file,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

const (
	maxDMText = 4000
	maxDMFile = 2 << 20 // At 2 MB, a file fits a guild's envelope limit once encrypted.
)

// These are the contact states as shown.
const (
	ContactActive   = "contact"
	ContactSent     = "sent"
	ContactReceived = "received"
	ContactBlocked  = "blocked"
)

// ContactView is a contact as the window shows it.
type ContactView struct {
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
	State     string `json:"state"`    // It is contact, sent, received or blocked.
	Verified  string `json:"verified"` // It is unverified, verified or changed.
	Reachable bool   `json:"reachable"`
	Online    bool   `json:"online"`
	Status    string `json:"status"` // It is Online, Idle or DND while they are online.
	Unread    int    `json:"unread"`
	Typing    bool   `json:"typing"`
	Note      string `json:"note"`
	LastChat  time.Time
}

// DMView is one private message as the window shows it.
type DMView struct {
	ID        string
	Mine      bool
	Time      string
	Text      string
	ReplyTo   string
	ReplyText string
	Edited    bool
	Deleted   bool
	Reactions []ReactionView
	Read      bool // It is true for your message once they have read it.
	Got       bool // It is true for your message once their app has received it.
	File      string
	FilePath  string // It is the file's path inside the user folder.
	FileSize  int64
}

// dmState is the manager's private message state.
type dmState struct {
	mu      sync.Mutex
	unread  map[string]int       // It counts unread messages per contact.
	typing  map[string]time.Time // It records when each contact last typed.
	files   sync.Mutex           // It allows one chat-file change at a time.
	viewing string               // It is the chat on screen.
}

func (m *Manager) dm() *dmState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dms == nil {
		m.dms = &dmState{unread: map[string]int{}, typing: map[string]time.Time{}}
	}
	return m.dms
}

func (m *Manager) myID() string { return KeyText(m.u.SigningKey.Public().(ed25519.PublicKey)) }

func newMsgID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return time.Now().UTC().Format("20060102150405") + hex.EncodeToString(b)
}

// ---- where a contact can be reached ----------------------------------------------

// route finds a connected guild the contact is a member of, preferring one
// where they are online, and their encryption key.
func (m *Manager) route(contact string) (*Conn, *proto.Session, []byte, bool) {
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, gc := range m.conns {
		conns = append(conns, gc)
	}
	m.mu.Unlock()
	sort.Slice(conns, func(i, j int) bool { return conns[i].id < conns[j].id })
	var best *Conn
	var bestS *proto.Session
	var enc []byte
	online := false
	for _, gc := range conns {
		gc.mu.Lock()
		s := gc.sess
		for _, mem := range gc.members {
			if mem.UserKey == contact && len(mem.EncKey) == 32 && s != nil {
				if best == nil || (mem.Online && !online) {
					best, bestS, enc, online = gc, s, mem.EncKey, mem.Online
				}
			}
		}
		gc.mu.Unlock()
	}
	return best, bestS, enc, online
}

// encKeyOf finds someone's encryption key in any guild's member list.
func (m *Manager) encKeyOf(who string) []byte {
	_, _, enc, _ := m.route(who)
	return enc
}

// ---- sending -------------------------------------------------------------------

func (m *Manager) sendBody(to string, b dmBody, live bool) error {
	_, s, enc, _ := m.route(to)
	if s == nil {
		return errors.New("you don't share a connected guild with them right now")
	}
	me := m.myID()
	key, err := e2e.PairKey(m.u.DecryptionKey, enc, me, to)
	if err != nil {
		return err
	}
	plain, _ := json.Marshal(b)
	nonce, sealed, err := e2e.Encrypt(key, e2e.DMChannel(me, to), 0, plain)
	if err != nil {
		return err
	}
	d := proto.DMEnvelope{From: me, To: to, Live: live, Nonce: nonce, Cipher: sealed}
	d.Sig = ed25519.Sign(m.u.SigningKey, d.SignedPart())
	r, err := s.Call(proto.Request{Type: "dm_send", DM: &d}, callTimeout)
	if err != nil {
		return err
	}
	if !r.OK {
		return errors.New(r.Error)
	}
	return nil
}

// ---- receiving -----------------------------------------------------------------

// receive opens an envelope, acts on it, and (unless it was live) tells the
// guild it is saved.
func (m *Manager) receive(s *proto.Session, d proto.DMEnvelope) {
	defer func() {
		if !d.Live {
			s.Call(proto.Request{Type: "dm_ack", DMIDs: []string{d.ID}}, callTimeout)
		}
	}()
	me := m.myID()
	if d.To != me {
		return
	}
	pubBytes, err := user.KeyFromText(d.From)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize || !ed25519.Verify(pubBytes, d.SignedPart(), d.Sig) {
		return // The envelope is not really from them.
	}
	enc := m.encKeyOf(d.From)
	if enc == nil {
		return
	}
	key, err := e2e.PairKey(m.u.DecryptionKey, enc, me, d.From)
	if err != nil {
		return
	}
	plain, err := e2e.Decrypt(key, e2e.DMChannel(me, d.From), 0, d.Nonce, d.Cipher)
	if err != nil {
		return
	}
	var b dmBody
	if json.Unmarshal(plain, &b) != nil {
		return
	}
	if m.apply(d, b, enc) {
		m.onChange()
		if b.Kind == "msg" || b.Kind == "file" { // Tell them it arrived (two ticks).
			m.sendReceipt(d.From, dmBody{Kind: "got", ID: b.ID})
		}
	}
}

// contact returns a copy of a contact, or nil.
func (m *Manager) contact(id string) *user.Contact {
	list, _ := m.u.Contacts()
	for _, c := range list {
		if c.UserID == id {
			c := c
			return &c
		}
	}
	return nil
}

// apply acts on a received message body; it reports whether anything changed.
func (m *Manager) apply(d proto.DMEnvelope, b dmBody, enc []byte) bool {
	st0 := m.dm()
	st0.files.Lock()
	defer st0.files.Unlock()
	from := d.From
	c := m.contact(from)
	if c != nil && c.BlockStatus {
		return false // Messages from blocked people are dropped quietly.
	}
	switch b.Kind {
	case "request":
		if c != nil && c.RequestState == ContactSent { // Both have asked, so they are now contacts.
			m.setContact(from, func(c *user.Contact) { c.RequestState = "" })
			return true
		}
		if c != nil {
			return false
		}
		name := strings.TrimSpace(b.Name)
		if name == "" || utf8.RuneCountInString(name) > 32 {
			name = from[:6]
		}
		m.u.EditContacts(func(list []user.Contact) []user.Contact {
			return append(list, user.Contact{UserID: from, Name: name, SignatureVerificationKey: from,
				PublicEncryptionKey: user.KeyText(enc), VerificationStatus: "unverified",
				RequestState: ContactReceived, LastChat: user.Now()})
		})
		return true
	case "accept":
		if c == nil || c.RequestState != ContactSent {
			return false
		}
		m.setContact(from, func(c *user.Contact) { c.RequestState = "" })
		return true
	case "decline":
		if c == nil || c.RequestState != ContactSent {
			return false
		}
		m.removeContact(from)
		return true
	}
	if c == nil || c.RequestState != "" {
		return false // Only accepted contacts can message you.
	}
	if b.Kind == "typing" {
		st := m.dm()
		st.mu.Lock()
		st.typing[from] = time.Now()
		st.mu.Unlock()
		return true
	}
	chat, _ := m.u.LoadChat(from)
	find := func(id string) *user.ChatEntry {
		for i := range chat {
			if chat[i].ID == id {
				return &chat[i]
			}
		}
		return nil
	}
	switch b.Kind {
	case "msg", "file":
		if b.ID == "" || find(b.ID) != nil || utf8.RuneCountInString(b.Text) > maxDMText {
			return false
		}
		e := user.ChatEntry{ID: b.ID, From: from, Time: d.Time, Text: b.Text, ReplyTo: b.ReplyTo}
		if b.Kind == "file" {
			if len(b.Data) == 0 || len(b.Data) > maxDMFile {
				return false
			}
			path, err := m.u.SaveChatFile(from, b.ID, b.File, b.Data)
			if err != nil {
				return false
			}
			e.File, e.FilePath, e.FileSize = b.File, path, int64(len(b.Data))
		}
		chat = append(chat, e)
		st := m.dm()
		st.mu.Lock()
		delete(st.typing, from)
		if st.viewing != from {
			st.unread[from]++
		}
		viewing := st.viewing == from
		st.mu.Unlock()
		body := firstLine(b.Text, 120)
		if b.Kind == "file" {
			body = "📎 " + b.File
		}
		m.notice(Notice{Kind: "dm", Channel: from, From: from, Name: c.Name, Text: body})
		m.setContact(from, func(c *user.Contact) { c.LastChat = user.Now() })
		if viewing {
			defer m.sendRead(from, b.ID) // It is sent in the background, trying again if needed.
		}
	case "edit":
		e := find(b.ID)
		if e == nil || e.From != from || e.Deleted {
			return false
		}
		e.Text, e.Edited = b.Text, true
	case "delete":
		e := find(b.ID)
		if e == nil || e.From != from {
			return false
		}
		e.Text, e.Deleted, e.Reactions = "", true, nil
	case "react":
		e := find(b.ID)
		if e == nil || b.Emoji == "" || len(b.Emoji) > 32 {
			return false
		}
		react(e, from, b.Emoji, b.On)
	case "got": // Their app has received your messages up to this one.
		for i := range chat {
			if chat[i].From != from && chat[i].GotAt == "" && chat[i].ID <= b.ID {
				chat[i].GotAt = d.Time
			}
		}
	case "read":
		e := find(b.ID)
		if e == nil {
			return false
		}
		for i := range chat { // Everything you sent up to that one has been read.
			if chat[i].From != from && chat[i].ReadAt == "" && chat[i].ID <= b.ID {
				chat[i].ReadAt = d.Time
			}
		}
	default:
		return false
	}
	m.u.SaveChat(from, chat)
	return true
}

func react(e *user.ChatEntry, who, emoji string, on bool) {
	if e.Reactions == nil {
		e.Reactions = map[string][]string{}
	}
	list := e.Reactions[emoji]
	var kept []string
	for _, u := range list {
		if u != who {
			kept = append(kept, u)
		}
	}
	if on {
		kept = append(kept, who)
	}
	if len(kept) == 0 {
		delete(e.Reactions, emoji)
	} else {
		e.Reactions[emoji] = kept
	}
}

func (m *Manager) setContact(id string, change func(*user.Contact)) {
	m.u.EditContacts(func(list []user.Contact) []user.Contact {
		for i := range list {
			if list[i].UserID == id {
				change(&list[i])
			}
		}
		return list
	})
}

func (m *Manager) removeContact(id string) {
	m.u.EditContacts(func(list []user.Contact) []user.Contact {
		var kept []user.Contact
		for _, c := range list {
			if c.UserID != id {
				kept = append(kept, c)
			}
		}
		return kept
	})
}

// collectDMs fetches the private messages a guild kept while you were away.
func (gc *Conn) collectDMs(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "dm_inbox"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	for _, d := range r.DMs {
		gc.m.receive(s, d)
	}
}

// ---- actions from the window -------------------------------------------------------

// RequestContact asks someone to become a contact.
func (m *Manager) RequestContact(id string) error {
	id = strings.TrimSpace(id)
	if id == m.myID() {
		return errors.New("that's you")
	}
	if !safeUserID(id) {
		return errors.New("that is not a user ID (52 letters and digits)")
	}
	if c := m.contact(id); c != nil {
		switch {
		case c.BlockStatus:
			return errors.New("you blocked them; unblock them first")
		case c.RequestState == ContactReceived:
			return m.AcceptContact(id)
		default:
			return errors.New("they are already your friend")
		}
	}
	_, s, enc, _ := m.route(id)
	if s == nil {
		return errors.New("you can only add people you share a guild with")
	}
	if err := m.sendBody(id, dmBody{Kind: "request", Name: m.u.Profile.DefaultName}, false); err != nil {
		return err
	}
	name := id[:6]
	for _, g := range m.Views() {
		for _, mem := range g.Members {
			if mem.UserKey == id && mem.Name != "" {
				name = mem.Name
			}
		}
	}
	return m.u.EditContacts(func(list []user.Contact) []user.Contact {
		return append(list, user.Contact{UserID: id, Name: name, SignatureVerificationKey: id,
			PublicEncryptionKey: user.KeyText(enc), VerificationStatus: "unverified",
			RequestState: ContactSent, LastChat: user.Now()})
	})
}

func safeUserID(s string) bool {
	if len(s) != 52 {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

// AcceptContact accepts someone's request.
func (m *Manager) AcceptContact(id string) error {
	c := m.contact(id)
	if c == nil || c.RequestState != ContactReceived {
		return errors.New("there is no request from them")
	}
	if err := m.sendBody(id, dmBody{Kind: "accept"}, false); err != nil {
		return err
	}
	m.setContact(id, func(c *user.Contact) { c.RequestState = "" })
	m.onChange()
	return nil
}

// DeclineContact turns a request down (and forgets it), or cancels yours.
func (m *Manager) DeclineContact(id string) error {
	c := m.contact(id)
	if c == nil {
		return nil
	}
	if c.RequestState == ContactReceived {
		m.sendBody(id, dmBody{Kind: "decline"}, false) // This is best effort.
	}
	m.removeContact(id)
	m.onChange()
	return nil
}

// RemoveContact removes a contact (your chat with them is kept on disk).
func (m *Manager) RemoveContact(id string) { m.removeContact(id); m.onChange() }

// Block blocks someone: they leave your contacts for the Blocked list, and
// nothing they send reaches you.
func (m *Manager) Block(id, name string) {
	if m.contact(id) == nil {
		m.u.EditContacts(func(list []user.Contact) []user.Contact {
			return append(list, user.Contact{UserID: id, Name: name, SignatureVerificationKey: id,
				VerificationStatus: "unverified", BlockStatus: true})
		})
	} else {
		m.setContact(id, func(c *user.Contact) { c.BlockStatus = true })
	}
	m.onChange()
}

// Unblock lifts a block (they go back to being a contact, if they were).
func (m *Manager) Unblock(id string) {
	c := m.contact(id)
	if c == nil {
		return
	}
	if c.RequestState != "" || c.PublicEncryptionKey == "" {
		m.removeContact(id)
	} else {
		m.setContact(id, func(c *user.Contact) { c.BlockStatus = false })
	}
	m.onChange()
}

// IsBlocked reports whether someone is blocked (their guild messages are
// collapsed).
func (m *Manager) IsBlocked(id string) bool {
	c := m.contact(id)
	return c != nil && c.BlockStatus
}

// SetNote saves a private note about a contact.
func (m *Manager) SetNote(id, note string) {
	m.setContact(id, func(c *user.Contact) { c.PersonalNote = strings.TrimSpace(note) })
	m.onChange()
}

// MyCode is the code to tell a contact who wants to verify you.
func (m *Manager) MyCode(contact string) string { return e2e.VerifyCode(m.myID(), contact) }

// Verify checks the code a contact told you; if right, they get the badge.
func (m *Manager) Verify(contact, code string) error {
	if !e2e.SameCode(code, e2e.VerifyCode(contact, m.myID())) {
		return errors.New("that code doesn't match; check it with them again")
	}
	enc := m.encKeyOf(contact)
	m.setContact(contact, func(c *user.Contact) {
		c.VerificationStatus = "verified"
		if enc != nil {
			c.VerifiedKey = user.KeyText(enc)
		}
	})
	m.onChange()
	return nil
}

// Contacts lists contacts (and requests and blocked people), most recent
// chat first.
func (m *Manager) Contacts() []ContactView {
	list, _ := m.u.Contacts()
	st := m.dm()
	out := make([]ContactView, 0, len(list))
	for _, c := range list {
		v := ContactView{UserID: c.UserID, Name: c.Name, State: ContactActive, Verified: c.VerificationStatus,
			Note: c.PersonalNote, LastChat: c.LastChat}
		switch {
		case c.BlockStatus:
			v.State = ContactBlocked
		case c.RequestState != "":
			v.State = c.RequestState
		}
		if v.Verified == "" {
			v.Verified = "unverified"
		}
		gc, _, enc, online := m.route(c.UserID)
		v.Reachable, v.Online = gc != nil, online
		if gc != nil {
			gc.mu.Lock()
			for _, mem := range gc.members {
				if mem.UserKey == c.UserID {
					v.Status = mem.Status
				}
			}
			gc.mu.Unlock()
		}
		// A verified contact whose key changed is a warning sign.
		if c.VerificationStatus == "verified" && enc != nil && c.VerifiedKey != "" && c.VerifiedKey != user.KeyText(enc) {
			v.Verified = "changed"
		}
		st.mu.Lock()
		v.Unread = st.unread[c.UserID]
		v.Typing = time.Since(st.typing[c.UserID]) < 6*time.Second
		st.mu.Unlock()
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastChat.After(out[j].LastChat) })
	return out
}

// Chat returns the messages with a contact and marks them as read.
func (m *Manager) Chat(contact string) ([]DMView, error) {
	// Read the chat and its unread count together, while no message is
	// being saved: a message counted as unread but not yet saved would
	// otherwise be cleared unseen, and never get its read receipt.
	st := m.dm()
	st.files.Lock()
	chat, err := m.u.LoadChat(contact)
	if err != nil {
		st.files.Unlock()
		return nil, err
	}
	st.mu.Lock()
	st.viewing = contact
	had := st.unread[contact]
	delete(st.unread, contact)
	st.mu.Unlock()
	st.files.Unlock()
	me := m.myID()
	byID := map[string]user.ChatEntry{}
	for _, e := range chat {
		byID[e.ID] = e
	}
	out := make([]DMView, 0, len(chat))
	var lastTheirs string
	for _, e := range chat {
		v := DMView{ID: e.ID, Mine: e.From == me, Time: e.Time, Text: e.Text, ReplyTo: e.ReplyTo,
			Edited: e.Edited, Deleted: e.Deleted, Read: e.ReadAt != "", Got: e.GotAt != "" || e.ReadAt != "", File: e.File, FilePath: e.FilePath, FileSize: e.FileSize}
		if r, ok := byID[e.ReplyTo]; ok {
			v.ReplyText = previewText(r.Text)
		}
		for emoji, who := range e.Reactions {
			rv := ReactionView{Emoji: emoji, Count: len(who)}
			for _, w := range who {
				rv.Mine = rv.Mine || w == me
			}
			v.Reactions = append(v.Reactions, rv)
		}
		sort.Slice(v.Reactions, func(i, j int) bool { return v.Reactions[i].Emoji < v.Reactions[j].Emoji })
		if !v.Mine {
			lastTheirs = e.ID
		}
		out = append(out, v)
	}
	if had > 0 && lastTheirs != "" {
		m.sendRead(contact, lastTheirs)
	}
	return out, nil
}

// LeaveChat records that no chat is on screen any more.
func (m *Manager) LeaveChat() {
	st := m.dm()
	st.mu.Lock()
	st.viewing = ""
	st.mu.Unlock()
}

// sendRead tells a contact you have read their messages up to id,
// unless you turned read receipts off.
func (m *Manager) sendRead(contact, id string) {
	if !m.u.Profile.HideReadReceipts {
		m.sendReceipt(contact, dmBody{Kind: "read", ID: id})
	}
}

// receiptWaits are the pauses before each new try at sending a receipt:
// about 3 minutes in all.
var receiptWaits = []time.Duration{
	time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
	15 * time.Second, 30 * time.Second, time.Minute, time.Minute,
}

// sendReceipt sends a receipt ("got" or "read") in the background, trying
// again when it fails (for example while the guild is busy or reconnecting).
// Receipts are safe to send twice, so trying again does no harm.
func (m *Manager) sendReceipt(to string, b dmBody) {
	go retry(func() error { return m.sendBody(to, b, false) }, receiptWaits)
}

// retry calls send until it succeeds, pausing for each of waits in turn
// between tries. It reports whether send succeeded in the end.
func retry(send func() error, waits []time.Duration) bool {
	for i := 0; ; i++ {
		if send() == nil {
			return true
		}
		if i >= len(waits) {
			return false
		}
		time.Sleep(waits[i])
	}
}

// edit changes your own copy of the chat after sending.
func (m *Manager) editChat(contact string, change func([]user.ChatEntry) []user.ChatEntry) {
	st := m.dm()
	st.files.Lock()
	chat, _ := m.u.LoadChat(contact)
	m.u.SaveChat(contact, change(chat))
	st.files.Unlock()
	m.onChange()
}

func (m *Manager) mustBeContact(id string) error {
	c := m.contact(id)
	if c == nil || c.RequestState != "" || c.BlockStatus {
		return errors.New("you can only message friends")
	}
	return nil
}

// SendDM sends a private message (replyTo may be "").
func (m *Manager) SendDM(contact, text, replyTo string) error {
	if err := m.mustBeContact(contact); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("write something first")
	}
	if utf8.RuneCountInString(text) > maxDMText {
		return errors.New("messages can be at most 4,000 characters")
	}
	id := newMsgID()
	if err := m.sendBody(contact, dmBody{Kind: "msg", ID: id, Text: text, ReplyTo: replyTo}, false); err != nil {
		return err
	}
	m.setContact(contact, func(c *user.Contact) { c.LastChat = user.Now() })
	m.editChat(contact, func(c []user.ChatEntry) []user.ChatEntry {
		return append(c, user.ChatEntry{ID: id, From: m.myID(), Time: user.Now().UTC().Format(time.RFC3339), Text: text, ReplyTo: replyTo})
	})
	return nil
}

// SendFile sends a picture or file (at most 2 MB).
func (m *Manager) SendFile(contact, name string, data []byte) error {
	if err := m.mustBeContact(contact); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maxDMFile {
		return errors.New("files can be at most 2 MB")
	}
	id := newMsgID()
	if err := m.sendBody(contact, dmBody{Kind: "file", ID: id, File: name, Data: data}, false); err != nil {
		return err
	}
	path, err := m.u.SaveChatFile(contact, id, name, data)
	if err != nil {
		return err
	}
	m.setContact(contact, func(c *user.Contact) { c.LastChat = user.Now() })
	m.editChat(contact, func(c []user.ChatEntry) []user.ChatEntry {
		return append(c, user.ChatEntry{ID: id, From: m.myID(), Time: user.Now().UTC().Format(time.RFC3339),
			File: name, FilePath: path, FileSize: int64(len(data))})
	})
	return nil
}

func (m *Manager) changeMine(contact, id string, b dmBody, change func(*user.ChatEntry)) error {
	if err := m.mustBeContact(contact); err != nil {
		return err
	}
	if err := m.sendBody(contact, b, false); err != nil {
		return err
	}
	m.editChat(contact, func(c []user.ChatEntry) []user.ChatEntry {
		for i := range c {
			if c[i].ID == id {
				change(&c[i])
			}
		}
		return c
	})
	return nil
}

// EditDM changes one of your messages.
func (m *Manager) EditDM(contact, id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxDMText {
		return errors.New("write 1 to 4,000 characters")
	}
	return m.changeMine(contact, id, dmBody{Kind: "edit", ID: id, Text: text}, func(e *user.ChatEntry) {
		e.Text, e.Edited = text, true
	})
}

// DeleteDM deletes one of your messages (for both of you).
func (m *Manager) DeleteDM(contact, id string) error {
	return m.changeMine(contact, id, dmBody{Kind: "delete", ID: id}, func(e *user.ChatEntry) {
		e.Text, e.Deleted, e.Reactions = "", true, nil
	})
}

// ReactDM adds or removes your reaction.
func (m *Manager) ReactDM(contact, id, emoji string, on bool) error {
	me := m.myID()
	return m.changeMine(contact, id, dmBody{Kind: "react", ID: id, Emoji: emoji, On: on}, func(e *user.ChatEntry) {
		react(e, me, emoji, on)
	})
}

// TypingDM tells a contact you are typing (only if they are online).
func (m *Manager) TypingDM(contact string) {
	if m.mustBeContact(contact) == nil {
		m.sendBody(contact, dmBody{Kind: "typing"}, true)
	}
}

// SetReadReceipts turns read receipts on or off.
func (m *Manager) SetReadReceipts(on bool) error { return m.u.SetHideReadReceipts(!on) }

// Unverify takes a contact's verification away.
func (m *Manager) Unverify(contact string) {
	m.setContact(contact, func(c *user.Contact) { c.VerificationStatus, c.VerifiedKey = "", "" })
	m.onChange()
}

// ClearChat deletes your copy of the chat with a contact, and with
// eraseMedia its pictures and files too. It waits for any message
// being saved, so nothing is half cleared.
func (m *Manager) ClearChat(contact string, eraseMedia bool) error {
	st := m.dm()
	st.files.Lock()
	err := m.u.ClearChat(contact, eraseMedia)
	st.mu.Lock()
	delete(st.unread, contact)
	st.mu.Unlock()
	st.files.Unlock()
	m.onChange()
	return err
}
