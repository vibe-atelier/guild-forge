package guildconn

// Channels and end-to-end encrypted messages in the app: opening sealed channel
// keys, making and handing over keys when the guild asks, encrypting and
// decrypting messages, typing and unread.

import (
	"crypto/ed25519"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/e2e"
	"nexus/internal/proto"
)

const (
	maxMessage = 4000 // A message has at most 4,000 characters.
	typingFor  = 6 * time.Second
)

// chatCache is what the app knows about a guild's channels.
type chatCache struct {
	channels []proto.ChannelNode
	keys     map[string]map[int][]byte // It maps each channel to its keys by version.
	msgs     map[string][]proto.ChatMessage
	more     map[string]bool   // It records which channels have older messages.
	latest   map[string]string // It holds the newest message ID per channel.
	mentions map[string]int    // It counts unread mentions per channel.
	typing   map[string]map[string]time.Time
	viewing  string // It is the channel open in the window.
}

func newChatCache() *chatCache {
	return &chatCache{keys: map[string]map[int][]byte{}, msgs: map[string][]proto.ChatMessage{},
		more: map[string]bool{}, latest: map[string]string{}, mentions: map[string]int{},
		typing: map[string]map[string]time.Time{}}
}

func (gc *Conn) me() string { return KeyText(gc.m.u.SigningKey.Public().(ed25519.PublicKey)) }

// afterSignIn loads the channel list, keys and newest message IDs.
func (gc *Conn) afterSignIn(s *proto.Session) {
	gc.syncLook(s)
	gc.refreshSettings(s)
	gc.refreshRoles(s)
	gc.refreshEmoji(s)
	gc.refreshSounds(s)
	gc.refreshChannels(s)
	gc.refreshKeys(s)
	gc.refreshLatest(s)
}

func (gc *Conn) refreshChannels(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "channels"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	gc.mu.Lock()
	gc.chat.channels = r.Channels
	gc.mu.Unlock()
}

// refreshKeys opens every sealed key addressed to this user.
func (gc *Conn) refreshKeys(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "my_keys"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	keys := map[string]map[int][]byte{}
	for _, b := range r.Boxes {
		k, err := e2e.OpenKey(gc.m.u.DecryptionKey, b.Box)
		if err != nil {
			continue
		}
		if keys[b.Channel] == nil {
			keys[b.Channel] = map[int][]byte{}
		}
		keys[b.Channel][b.Version] = k
	}
	gc.mu.Lock()
	gc.chat.keys = keys
	gc.mu.Unlock()
}

// refreshLatest learns the newest message in each channel (for unread dots).
func (gc *Conn) refreshLatest(s *proto.Session) {
	gc.mu.Lock()
	var ids []string
	for _, c := range gc.chat.channels {
		if c.Kind == proto.KindChannel {
			ids = append(ids, c.ID)
		}
	}
	gc.mu.Unlock()
	for _, id := range ids {
		r, err := s.Call(proto.Request{Type: "history", ChannelID: id, Limit: 1}, callTimeout)
		if err == nil && r.OK && len(r.Messages) > 0 {
			gc.mu.Lock()
			gc.chat.latest[id] = r.Messages[len(r.Messages)-1].ID
			gc.mu.Unlock()
		}
	}
}

// doKeyWork is the answer to the guild's "keys_needed":
// make a new key, or hand the current one to members who lack it.
func (gc *Conn) doKeyWork(s *proto.Session, channel string) {
	r, err := s.Call(proto.Request{Type: "key_states"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	var st *proto.KeyState
	for i := range r.Keys {
		if r.Keys[i].Channel == channel {
			st = &r.Keys[i]
		}
	}
	if st == nil {
		return
	}
	mr, err := s.Call(proto.Request{Type: "members"}, callTimeout)
	if err != nil || !mr.OK {
		return
	}
	encOf := map[string][]byte{}
	for _, m := range mr.Members {
		if len(m.EncKey) == 32 {
			encOf[m.UserKey] = m.EncKey
		}
	}
	var boxes []proto.KeyBox
	seal := func(to string, version int, key []byte) {
		if pub := encOf[to]; pub != nil {
			if b, err := e2e.SealKey(pub, key); err == nil {
				boxes = append(boxes, proto.KeyBox{Channel: channel, Version: version, To: to, Box: b})
			}
		}
	}
	if st.Current == 0 || st.Retired {
		key, err := e2e.NewChannelKey()
		if err != nil {
			return
		}
		for u := range encOf {
			if len(st.Viewers) > 0 && !contains(st.Viewers, u) {
				continue // Only those who may read the channel get its key.
			}
			seal(u, st.Current+1, key)
		}
	} else {
		gc.mu.Lock()
		mine := gc.chat.keys[channel]
		gc.mu.Unlock()
		cur := mine[st.Current]
		if cur == nil {
			return // This app doesn't hold the key; someone else will be asked.
		}
		for _, u := range st.Missing {
			seal(u, st.Current, cur)
			if st.History { // Older keys are handed on too.
				for v, k := range mine {
					if v < st.Current {
						seal(u, v, k)
					}
				}
			}
		}
	}
	if len(boxes) == 0 {
		return
	}
	if r, err := s.Call(proto.Request{Type: "put_keys", Boxes: boxes}, callTimeout); err == nil && r.OK {
		gc.refreshKeys(s)
	}
}

// onMessage stores a new or changed message from the guild.
func (gc *Conn) onMessage(m proto.ChatMessage) {
	me := gc.me()
	gc.mu.Lock()
	defer gc.mu.Unlock()
	list := gc.chat.msgs[m.Channel]
	i := sort.Search(len(list), func(i int) bool { return list[i].ID >= m.ID })
	isNew := !(i < len(list) && list[i].ID == m.ID)
	if !isNew {
		list[i] = m
	} else if list != nil || gc.chat.viewing == m.Channel {
		list = append(list, proto.ChatMessage{})
		copy(list[i+1:], list[i:])
		list[i] = m
		gc.chat.msgs[m.Channel] = list
	}
	if m.ID > gc.chat.latest[m.Channel] {
		gc.chat.latest[m.Channel] = m.ID
	}
	if isNew {
		gc.noticeLocked(m, me)
	}
	if isNew && m.Author != me && gc.chat.viewing != m.Channel {
		if text, ok := gc.decryptLocked(m); ok && strings.Contains(text, "<@"+me+">") {
			gc.chat.mentions[m.Channel]++
			// The count is kept over a restart.
			gc.m.u.SetMentions(gc.id, m.Channel, gc.chat.mentions[m.Channel])
		}
	}
	delete(gc.chat.typing[m.Channel], m.Author) // They sent it, so they are no longer typing.
}

func (gc *Conn) onTyping(channel, user string) {
	if user == gc.me() {
		return
	}
	gc.mu.Lock()
	if gc.chat.typing[channel] == nil {
		gc.chat.typing[channel] = map[string]time.Time{}
	}
	gc.chat.typing[channel][user] = time.Now()
	gc.mu.Unlock()
	go func() { // Refresh the window when it runs out.
		time.Sleep(typingFor + 100*time.Millisecond)
		gc.m.onChange()
	}()
}

func (gc *Conn) decryptLocked(m proto.ChatMessage) (string, bool) {
	key := gc.chat.keys[m.Channel][m.Version]
	if key == nil || m.Deleted {
		return "", false
	}
	p, err := e2e.Decrypt(key, m.Channel, m.Version, m.Nonce, m.Cipher)
	if err != nil {
		return "", false
	}
	return string(p), true
}

// ---- what the window sees ------------------------------------------------------

// ChannelView is one node of the channel list.
type ChannelView struct {
	proto.ChannelNode
	Unread   bool `json:"unread"`
	Mentions int  `json:"mentions"`
	NoKey    bool `json:"no_key"` // It is true when this app can't read the channel yet.
}

// ChatView is one message as the window shows it.
type ChatView struct {
	ID        string         `json:"id"`
	Author    string         `json:"author"`
	Name      string         `json:"name"`
	Time      string         `json:"time"`
	Edited    bool           `json:"edited"`
	Deleted   bool           `json:"deleted"`
	Text      string         `json:"text"`
	Locked    bool           `json:"locked"` // It is true when the message can't be decrypted (no key).
	Bad       bool           `json:"bad"`    // It is true when the signature doesn't match the author.
	ReplyTo   string         `json:"reply_to,omitempty"`
	ReplyName string         `json:"reply_name,omitempty"`
	ReplyText string         `json:"reply_text,omitempty"`
	Reactions []ReactionView `json:"reactions"`
	Mine      bool           `json:"mine"`
	Poll      *PollView      `json:"-"`
	Forward   *Forwarded     `json:"-"`
	Expires   string         `json:"expires,omitempty"`
	Pinned    bool           `json:"pinned,omitempty"`
	EditCount int            `json:"edit_count,omitempty"`
}

// ReactionView is one emoji under a message.
type ReactionView struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine"`
}

func (gc *Conn) channelViewsLocked(reads map[string]string) []ChannelView {
	out := make([]ChannelView, 0, len(gc.chat.channels))
	for _, c := range gc.chat.channels {
		v := ChannelView{ChannelNode: c}
		if c.Kind == proto.KindChannel {
			last := gc.chat.latest[c.ID]
			v.Unread = last != "" && last > reads[c.ID] && gc.chat.viewing != c.ID
			v.Mentions = gc.chat.mentions[c.ID]
			v.NoKey = len(gc.chat.keys[c.ID]) == 0
		}
		out = append(out, v)
	}
	return out
}

func (gc *Conn) typingLocked() map[string][]string {
	out := map[string][]string{}
	names := gc.memberNamesLocked()
	for ch, users := range gc.chat.typing {
		for u, at := range users {
			if time.Since(at) < typingFor {
				out[ch] = append(out[ch], names.typingText(u))
			}
		}
	}
	return out
}

type nameBook map[string]proto.MemberInfo

func (gc *Conn) memberNamesLocked() nameBook {
	b := nameBook{}
	for _, m := range gc.members {
		b[m.UserKey] = m
	}
	return b
}

func (b nameBook) name(u string) string {
	if m, ok := b[u]; ok && m.Nick != "" {
		return m.Nick
	}
	if m, ok := b[u]; ok && m.Name != "" {
		return m.Name
	}
	if len(u) > 6 {
		return u[:6]
	}
	return u
}

// typingText is the member's own typing text, with their name.
func (b nameBook) typingText(u string) string {
	t := b[u].Typing
	if t == "" || !strings.Contains(t, "{name}") {
		t = "{name} is typing…"
	}
	return strings.ReplaceAll(t, "{name}", b.name(u))
}

func (gc *Conn) viewLocked(m proto.ChatMessage, names nameBook, me string, all []proto.ChatMessage) ChatView {
	v := ChatView{ID: m.ID, Author: m.Author, Name: names.name(m.Author), Time: m.Time,
		Edited: m.Edited != "", Deleted: m.Deleted, ReplyTo: m.ReplyTo, Mine: m.Author == me, Reactions: []ReactionView{}}
	if !m.Deleted {
		if pub, err := keyFromText(m.Author); err != nil || !ed25519.Verify(pub, m.SignedPart(), m.Sig) {
			v.Bad = true
		} else if text, ok := gc.decryptLocked(m); ok {
			v.Text = text
		} else {
			v.Locked = true
		}
	}
	if m.ReplyTo != "" {
		for _, r := range all {
			if r.ID == m.ReplyTo {
				v.ReplyName = names.name(r.Author)
				if t, ok := gc.decryptLocked(r); ok {
					v.ReplyText = firstLine(names.mentionsToNames(previewText(t)), 90)
				} else if r.Deleted {
					v.ReplyText = "(deleted)"
				}
			}
		}
	}
	var emojis []string
	for e := range m.Reactions {
		emojis = append(emojis, e)
	}
	sort.Strings(emojis)
	for _, e := range emojis {
		us := m.Reactions[e]
		mine := false
		for _, u := range us {
			mine = mine || u == me
		}
		v.Reactions = append(v.Reactions, ReactionView{e, len(us), mine})
	}
	return v
}

func firstLine(s string, n int) string {
	s = strings.SplitN(s, "\n", 2)[0]
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

func keyFromText(s string) (ed25519.PublicKey, error) {
	b, err := keyTextDecode(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("bad key")
	}
	return ed25519.PublicKey(b), nil
}

// ---- actions from the window ------------------------------------------------------

// History returns a channel's messages (loading older ones when before is
// set) and marks it as the one being viewed.
func (gc *Conn) History(channel, before string) ([]ChatView, bool, error) {
	gc.mu.Lock()
	s := gc.sess
	have := gc.chat.msgs[channel]
	gc.chat.viewing = channel
	seen := gc.chat.mentions[channel] > 0
	gc.chat.mentions[channel] = 0
	gc.mu.Unlock()
	if seen {
		gc.m.u.SetMentions(gc.id, channel, 0)
	}
	if s == nil {
		return nil, false, errors.New("the guild is not connected right now")
	}
	if before != "" || have == nil {
		r, err := s.Call(proto.Request{Type: "history", ChannelID: channel, Before: before}, callTimeout)
		if err != nil {
			return nil, false, err
		}
		if !r.OK {
			return nil, false, errors.New(r.Error)
		}
		gc.mu.Lock()
		merged := append(append([]proto.ChatMessage{}, r.Messages...), gc.chat.msgs[channel]...)
		sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
		dedup := merged[:0]
		for i, m := range merged {
			if i == 0 || m.ID != merged[i-1].ID {
				dedup = append(dedup, m)
			} else {
				dedup[len(dedup)-1] = m
			}
		}
		gc.chat.msgs[channel] = dedup
		if before != "" || have == nil {
			gc.chat.more[channel] = r.More
		}
		gc.mu.Unlock()
	}
	gc.mu.Lock()
	defer gc.mu.Unlock()
	names, me, all := gc.memberNamesLocked(), gc.me(), gc.chat.msgs[channel]
	pinned := gc.pinnedLocked(channel)
	out := make([]ChatView, 0, len(all))
	for _, m := range all {
		if m.Gone { // The message has disappeared.
			continue
		}
		v := gc.viewLocked(m, names, me, all)
		fillExtras(&v, m, me, pinned)
		out = append(out, v)
	}
	if n := len(all); n > 0 {
		gc.m.u.SetRead(gc.id, channel, all[n-1].ID)
		gc.readUpToLocked(channel, all[n-1].ID)
	}
	return out, gc.chat.more[channel], nil
}

// mentionTokens turns "@Name" into "<@USERID>" for members of this guild.
func (gc *Conn) mentionTokens(text string) string {
	gc.mu.Lock()
	ms := append([]proto.MemberInfo{}, gc.members...)
	gc.mu.Unlock()
	// The longest names are tried first.
	sort.Slice(ms, func(i, j int) bool { return len(ms[i].Name) > len(ms[j].Name) })
	for _, m := range ms {
		if m.Name != "" {
			text = strings.ReplaceAll(text, "@"+m.Name, "<@"+m.UserKey+">")
		}
	}
	return text
}

func (gc *Conn) seal(channel, text string) (version int, nonce, sealed []byte, err error) {
	if strings.TrimSpace(text) == "" {
		return 0, nil, nil, errors.New("write something first")
	}
	text = gc.mentionTokens(text)
	if utf8.RuneCountInString(text) > maxMessage {
		return 0, nil, nil, errors.New("messages can be at most 4,000 characters")
	}
	gc.mu.Lock()
	var key []byte
	for v, k := range gc.chat.keys[channel] {
		if v > version {
			version, key = v, k
		}
	}
	gc.mu.Unlock()
	if key == nil {
		return 0, nil, nil, errors.New("your app doesn't have this channel's key yet; it arrives when another member is online")
	}
	nonce, sealed, err = e2e.Encrypt(key, channel, version, []byte(text))
	return version, nonce, sealed, err
}

// Send encrypts, signs and sends a message.
func (gc *Conn) Send(channel, text, replyTo string) error {
	text = gc.prepareEmoji(text)
	version, nonce, sealed, err := gc.seal(channel, text)
	if err != nil {
		return err
	}
	m := proto.ChatMessage{Channel: channel, Author: gc.me(), Version: version, Nonce: nonce, Cipher: sealed, ReplyTo: replyTo}
	m.Sig = ed25519.Sign(gc.m.u.SigningKey, m.SignedPart())
	r, err := gc.Call(proto.Request{Type: "send", Msg: &m})
	if err == nil && r.Chat != nil {
		gc.onMessage(*r.Chat)
		gc.m.onChange()
	}
	return err
}

// Edit replaces the text of the user's own message.
func (gc *Conn) Edit(channel, id, text string) error {
	gc.mu.Lock()
	var replyTo string
	for _, m := range gc.chat.msgs[channel] {
		if m.ID == id {
			replyTo = m.ReplyTo
		}
	}
	gc.mu.Unlock()
	text = gc.prepareEmoji(text)
	version, nonce, sealed, err := gc.seal(channel, text)
	if err != nil {
		return err
	}
	m := proto.ChatMessage{ID: id, Channel: channel, Author: gc.me(), Version: version, Nonce: nonce, Cipher: sealed, ReplyTo: replyTo}
	m.Sig = ed25519.Sign(gc.m.u.SigningKey, m.SignedPart())
	_, err = gc.Call(proto.Request{Type: "edit", Msg: &m})
	return err
}

// Delete removes a message.
func (gc *Conn) Delete(channel, id string) error {
	_, err := gc.Call(proto.Request{Type: "delete_message", ChannelID: channel, MessageID: id})
	return err
}

// React adds or removes an emoji reaction.
func (gc *Conn) React(channel, id, emoji string, on bool) error {
	if name, hash, ok := ParseEmoji(emoji); ok && on {
		gc.ensureEmoji(name, hash)
	}
	_, err := gc.Call(proto.Request{Type: "react", ChannelID: channel, MessageID: id, Emoji: emoji, On: on})
	return err
}

// Typing tells the others this user is typing.
func (gc *Conn) Typing(channel string) {
	gc.Call(proto.Request{Type: "typing", ChannelID: channel})
}

// Leave stops treating a channel as open (unread markers work again).
func (gc *Conn) LeaveChannel() {
	gc.mu.Lock()
	gc.chat.viewing = ""
	gc.mu.Unlock()
}

var mentionToken = regexp.MustCompile(`<@([A-Z2-7]{52})>`)

// mentionsToNames shows "<@USERID>" as "@Name" (for short previews).
func (b nameBook) mentionsToNames(s string) string {
	return mentionToken.ReplaceAllStringFunc(s, func(t string) string {
		return "@" + b.name(t[2:len(t)-1])
	})
}

func contains(list []string, x string) bool {
	for _, y := range list {
		if y == x {
			return true
		}
	}
	return false
}
