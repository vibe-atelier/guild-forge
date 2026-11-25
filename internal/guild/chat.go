package guild

// This file handles channels and end-to-end encrypted messages. The guild stores and
// relays only encrypted messages and sealed keys; it never knows a channel
// key. It keeps the key ledger and asks members' apps
// to make or hand over keys when needed.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

const (
	maxChannelName   = 50
	maxTopic         = 300
	maxFolderDepth   = 3 // Folders go at most 3 deep.
	maxNodes         = 500
	maxCipher        = 64 * 1024 // A 4,000-character message is far smaller.
	maxBox           = 512
	maxEmoji         = 112 // This fits an emoji, or a custom emoji ":name:hash".
	maxEmojiPerMsg   = 20
	historyPage      = 50
	historyPageLimit = 100
)

// chatState is the guild's channel and message data, guarded by Server.mu.
type chatState struct {
	nodes  []proto.ChannelNode
	keys   map[string]*guildstore.ChannelKeys
	msgs   map[string][]proto.ChatMessage
	lastID int64
}

func (g *Server) loadChat() error {
	nodes, err := guildstore.LoadChannels(g.DataDir)
	if err != nil {
		return err
	}
	g.chat = chatState{nodes: nodes, keys: map[string]*guildstore.ChannelKeys{}, msgs: map[string][]proto.ChatMessage{}}
	for _, n := range nodes {
		if !proto.IsRoom(n.Kind) {
			continue
		}
		k, err := guildstore.LoadKeys(g.DataDir, n.ID)
		if err != nil {
			return err
		}
		g.chat.keys[n.ID] = &k
		msgs, err := guildstore.LoadMessages(g.DataDir, n.ID)
		if err != nil {
			return err
		}
		g.chat.msgs[n.ID] = msgs
		g.purgeOldLocked(n.ID)
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// nextMessageID makes IDs that sort in time order.
func (g *Server) nextMessageID() string {
	n := g.now().UnixNano()
	if n <= g.chat.lastID {
		n = g.chat.lastID + 1
	}
	g.chat.lastID = n
	return fmt.Sprintf("%016x", n)
}

// ---- the channel tree --------------------------------------------------------

func (g *Server) nodeLocked(id string) *proto.ChannelNode {
	for i := range g.chat.nodes {
		if g.chat.nodes[i].ID == id {
			return &g.chat.nodes[i]
		}
	}
	return nil
}

func (g *Server) isChannelLocked(id string) bool {
	n := g.nodeLocked(id)
	return n != nil && n.Kind == proto.KindChannel
}

// isRoomLocked reports whether node id is a channel or a voice channel (both have keys).
func (g *Server) isRoomLocked(id string) bool {
	n := g.nodeLocked(id)
	return n != nil && proto.IsRoom(n.Kind)
}

// depthLocked counts how many folders are above (and including) node id.
func (g *Server) folderDepthLocked(id string) int {
	d := 0
	for n := g.nodeLocked(id); n != nil; n = g.nodeLocked(n.Parent) {
		if n.Kind == proto.KindFolder {
			d++
		}
		if d > maxNodes {
			break
		}
	}
	return d
}

// effectiveLocked works out the settings that apply to node n.
func (g *Server) effectiveLocked(n proto.ChannelNode) (history bool, keepDays int) {
	history = true
	hSet, kSet := false, false
	for cur := &n; cur != nil; cur = g.nodeLocked(cur.Parent) {
		if !hSet && cur.History != nil {
			history, hSet = *cur.History, true
		}
		if !kSet && cur.KeepDays != nil {
			keepDays, kSet = *cur.KeepDays, true
		}
		if cur.Parent == "" {
			break
		}
	}
	return history, keepDays
}

// channelList is the channels user may see; categories and folders
// are always listed.
func (g *Server) channelList(user string) []proto.ChannelNode {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]proto.ChannelNode, 0, len(g.chat.nodes))
	manage := g.hasLocked(user, proto.PermManageChannels)
	for _, n := range g.chat.nodes {
		if proto.IsRoom(n.Kind) && !manage && !g.canSeeLocked(user, n.ID) {
			continue
		}
		if !manage {
			n.Overrides = nil // The rules are only for those who manage channels.
		}
		n.EffHistory, n.EffKeepDays = g.effectiveLocked(n)
		if n.Kind == proto.KindVoice {
			n.Voice = g.voiceMembersLocked(n.ID)
			n.Locked = g.locked[n.ID]
		}
		if proto.IsRoom(n.Kind) {
			n.Pins = g.tg.pins[n.ID]
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

func cleanChannelName(kind, s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if kind == proto.KindChannel {
		s = strings.ToLower(strings.ReplaceAll(s, " ", "-"))
	}
	if s == "" {
		return "", errors.New("give it a name")
	}
	if utf8.RuneCountInString(s) > maxChannelName {
		return "", fmt.Errorf("names can be at most %d characters", maxChannelName)
	}
	return s, nil
}

// channelPut creates (empty ID) or changes a node (Manage Channels).
func (g *Server) channelPut(user string, in *proto.ChannelNode) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageChannels); err != nil {
		return "", err
	}
	n := *in
	n.EffHistory, n.EffKeepDays = false, 0
	var old *proto.ChannelNode
	if n.ID != "" {
		if old = g.nodeLocked(n.ID); old == nil {
			return "", errors.New("there is no such channel")
		}
		n.Kind = old.Kind // A node can't change its kind.
	} else {
		if len(g.chat.nodes) >= maxNodes {
			return "", errors.New("the guild has too many channels")
		}
		n.ID = newID()
	}
	var err error
	if n.Name, err = cleanChannelName(n.Kind, n.Name); err != nil {
		return "", err
	}
	n.Topic = strings.TrimSpace(n.Topic)
	if utf8.RuneCountInString(n.Topic) > maxTopic {
		return "", fmt.Errorf("topics can be at most %d characters", maxTopic)
	}
	if err := checkSlow(n.Slow); err != nil {
		return "", err
	}
	n.Pins = nil // Pins are kept apart.
	if n.KeepDays != nil && (*n.KeepDays < 0 || *n.KeepDays > 36500) {
		return "", errors.New("keep messages for 0 (forever) to 36500 days")
	}
	if err := g.checkOverridesLocked(user, &n); err != nil {
		return "", err
	}
	if err := g.checkPlaceLocked(&n); err != nil {
		return "", err
	}

	before := g.snapshotViewersLocked()
	if old != nil {
		*old = n
	} else {
		g.chat.nodes = append(g.chat.nodes, n)
		if proto.IsRoom(n.Kind) {
			g.chat.keys[n.ID] = &guildstore.ChannelKeys{}
			g.chat.msgs[n.ID] = nil
		}
	}
	if err := guildstore.SaveChannels(g.DataDir, g.chat.nodes); err != nil {
		return "", errors.New("the guild could not save its channels")
	}
	for _, c := range g.chat.nodes {
		if c.Kind == proto.KindChannel {
			g.purgeOldLocked(c.ID) // The keep-for setting may have changed.
		}
	}
	g.notifyLater(g.allConnsLocked(), "channels_changed")
	g.afterAccessChangeLocked(before)
	return n.ID, nil
}

func (g *Server) deepestFolderBelowLocked(id string) int {
	best := 0
	for _, c := range g.chat.nodes {
		if c.Parent == id && c.Kind == proto.KindFolder {
			best = max(best, 1+g.deepestFolderBelowLocked(c.ID))
		}
	}
	return best
}

// channelDelete removes a node and everything inside it, with messages.
func (g *Server) channelDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageChannels); err != nil {
		return err
	}
	if g.nodeLocked(id) == nil {
		return errors.New("there is no such channel")
	}
	gone := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, n := range g.chat.nodes {
			if gone[n.Parent] && !gone[n.ID] {
				gone[n.ID], changed = true, true
			}
		}
	}
	var kept []proto.ChannelNode
	for _, n := range g.chat.nodes {
		if !gone[n.ID] {
			kept = append(kept, n)
			continue
		}
		if proto.IsRoom(n.Kind) {
			delete(g.chat.keys, n.ID)
			delete(g.chat.msgs, n.ID)
			guildstore.DeleteChannelData(g.DataDir, n.ID)
		}
	}
	g.chat.nodes = kept
	if err := guildstore.SaveChannels(g.DataDir, g.chat.nodes); err != nil {
		return errors.New("the guild could not save its channels")
	}
	g.notifyLater(g.allConnsLocked(), "channels_changed")
	return nil
}

// ---- keys: the ledger and sealed boxes ------------

func hasBox(k *guildstore.ChannelKeys, user string, version int) bool {
	for _, b := range k.Boxes {
		if b.To == user && b.Version == version {
			return true
		}
	}
	return false
}

func (g *Server) keyStateLocked(channel string) proto.KeyState {
	k := g.chat.keys[channel]
	st := proto.KeyState{Channel: channel, Current: k.Current, Retired: k.Retired, Missing: []string{}, Viewers: g.viewersLocked(channel)}
	viewer := map[string]bool{}
	for _, u := range st.Viewers {
		viewer[u] = true
	}
	if n := g.nodeLocked(channel); n != nil {
		st.History, _ = g.effectiveLocked(*n)
	}
	seen := map[int]bool{}
	for _, b := range k.Boxes {
		if !seen[b.Version] {
			seen[b.Version] = true
			st.Versions = append(st.Versions, b.Version)
		}
	}
	sort.Ints(st.Versions)
	for user, m := range g.members {
		if k.Current == 0 {
			continue
		}
		if hasBox(k, user, k.Current) {
			st.Holders++
		} else if len(m.EncKey) == 32 && viewer[user] {
			st.Missing = append(st.Missing, user)
		}
	}
	sort.Strings(st.Missing)
	return st
}

// keyStates returns the ledger of every channel, for a member.
func (g *Server) keyStates() []proto.KeyState {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []proto.KeyState
	for _, n := range g.chat.nodes {
		if proto.IsRoom(n.Kind) {
			out = append(out, g.keyStateLocked(n.ID))
		}
	}
	return out
}

// myBoxes returns every sealed key addressed to user.
func (g *Server) myBoxes(user string) []proto.KeyBox {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := []proto.KeyBox{}
	for _, k := range g.chat.keys {
		for _, b := range k.Boxes {
			if b.To == user {
				out = append(out, b)
			}
		}
	}
	return out
}

// putKeys stores sealed boxes made by user's app: a brand-new key version
// (when the channel has none or it was retired), or existing keys handed to
// members who lack them.
func (g *Server) putKeys(user string, boxes []proto.KeyBox) error {
	if len(boxes) == 0 {
		return errors.New("no keys sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	type group struct {
		channel string
		version int
	}
	groups := map[group][]proto.KeyBox{}
	for _, b := range boxes {
		if !g.isRoomLocked(b.Channel) {
			return errors.New("there is no such channel")
		}
		if _, ok := g.members[b.To]; !ok {
			return errors.New("a key is addressed to someone who is not a member")
		}
		if !g.canSeeLocked(user, b.Channel) {
			return errors.New("you can't read this channel")
		}
		if !g.canSeeLocked(b.To, b.Channel) {
			continue // They may not read it, so their box is dropped.
		}
		if len(b.Box) == 0 || len(b.Box) > maxBox || b.Version < 1 {
			return errors.New("a key box is damaged")
		}
		b.From = user
		k := group{b.Channel, b.Version}
		groups[k] = append(groups[k], b)
	}
	tell := map[string]bool{}
	for gk, list := range groups {
		k := g.chat.keys[gk.channel]
		switch {
		case gk.version == k.Current+1 && (k.Current == 0 || k.Retired):
			// A new key: it must include the maker themself.
			mine := false
			for _, b := range list {
				mine = mine || b.To == user
			}
			if !mine {
				return errors.New("a new key must include a box for its maker")
			}
			k.Current, k.Retired = gk.version, false
			k.Boxes = append(k.Boxes, list...)
			for u := range g.members {
				tell[u] = true
			}
			g.Log.Printf("new key %d for channel %s made by %s", gk.version, gk.channel, user)
		case gk.version >= 1 && gk.version <= k.Current:
			if !hasBox(k, user, gk.version) {
				return errors.New("you can only hand over keys you have")
			}
			history, _ := g.effectiveLocked(*g.nodeLocked(gk.channel))
			if gk.version < k.Current && !history {
				return errors.New("this channel doesn't give older messages to newcomers")
			}
			for _, b := range list {
				if !hasBox(k, b.To, gk.version) {
					k.Boxes = append(k.Boxes, b)
					tell[b.To] = true
				}
			}
		default:
			return errors.New("that key version is out of date")
		}
		if err := guildstore.SaveKeys(g.DataDir, gk.channel, *k); err != nil {
			return errors.New("the guild could not save the keys")
		}
	}
	var users []string
	for u := range tell {
		users = append(users, u)
	}
	g.notifyLater(g.connsOfLocked(users...), "keys_changed")
	g.requestKeysLocked()
	return nil
}

// retireKeysLocked marks every channel key as needing replacement, after a
// member left or was removed.
func (g *Server) retireKeysLocked() {
	for id, k := range g.chat.keys {
		if k.Current > 0 && !k.Retired {
			k.Retired = true
			guildstore.SaveKeys(g.DataDir, id, *k)
		}
	}
	g.requestKeysLocked()
}

// requestKeysLocked asks online members' apps for the key work that's
// needed: a new key where there is none or it was retired (asked of the
// first online member who can receive keys), and handing the current key to
// members who lack it (asked of an online member who holds it).
func (g *Server) requestKeysLocked() {
	var online []string
	for u := range g.sessions {
		if m, ok := g.members[u]; ok && len(m.EncKey) == 32 {
			online = append(online, u)
		}
	}
	sort.Strings(online)
	if g.owner != nil { // The owner is asked first, if online.
		for i, u := range online {
			if u == g.owner.Key {
				online[0], online[i] = online[i], online[0]
			}
		}
	}
	for _, n := range g.chat.nodes {
		if !proto.IsRoom(n.Kind) {
			continue
		}
		k := g.chat.keys[n.ID]
		var helper string
		if k.Current == 0 || k.Retired {
			for _, u := range online { // This finds the first online member who may read it.
				if g.canSeeLocked(u, n.ID) {
					helper = u
					break
				}
			}
		} else if st := g.keyStateLocked(n.ID); len(st.Missing) > 0 {
			for _, u := range online {
				if hasBox(k, u, k.Current) {
					helper = u
					break
				}
			}
		}
		if helper == "" {
			continue
		}
		if s := g.sessions[helper]; s != nil {
			go s.conn.Send(proto.Response{Type: "event", OK: true, Event: "keys_needed", Channel: n.ID})
		}
	}
}

// ---- messages ------------------------------------------------------------------

func (g *Server) userPublicKey(user string) ed25519.PublicKey {
	b, err := guildstore.KeyFromText(user)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}

func (g *Server) findMessageLocked(channel, id string) *proto.ChatMessage {
	list := g.chat.msgs[channel]
	i := sort.Search(len(list), func(i int) bool { return list[i].ID >= id })
	if i < len(list) && list[i].ID == id {
		return &list[i]
	}
	return nil
}

func (g *Server) checkCipherLocked(user string, m *proto.ChatMessage) error {
	k := g.chat.keys[m.Channel]
	if m.Version < 1 || m.Version > k.Current || !hasBox(k, user, m.Version) {
		return errors.New("your app doesn't have this channel's key yet; wait a moment")
	}
	if len(m.Cipher) == 0 || len(m.Cipher) > maxCipher || len(m.Nonce) != 12 {
		return errors.New("the message is damaged")
	}
	pub := g.userPublicKey(user)
	if pub == nil || !ed25519.Verify(pub, m.SignedPart(), m.Sig) {
		return errors.New("the message's signature is wrong")
	}
	return nil
}

// send stores a new encrypted message and relays it.
func (g *Server) send(user string, in *proto.ChatMessage) (*proto.ChatMessage, error) {
	if in == nil {
		return nil, errors.New("nothing sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	m, err := g.checkSendLocked(user, in)
	if err != nil {
		return nil, err
	}
	if err := g.slowLocked(user, m.Channel); err != nil {
		return nil, err
	}
	return g.postLocked(m)
}

// checkSendLocked checks a message someone may send (now, or scheduled),
// and returns it as the guild keeps it.
func (g *Server) checkSendLocked(user string, in *proto.ChatMessage) (proto.ChatMessage, error) {
	if !g.isChannelLocked(in.Channel) {
		return proto.ChatMessage{}, errors.New("there is no such channel")
	}
	if !g.canLocked(user, in.Channel, proto.PermSendMessages) || !g.canSeeLocked(user, in.Channel) {
		return proto.ChatMessage{}, errors.New("you can't send messages in this channel")
	}
	m := proto.ChatMessage{Channel: in.Channel, Author: user, Version: in.Version, Nonce: in.Nonce,
		Cipher: in.Cipher, Sig: in.Sig, ReplyTo: in.ReplyTo}
	if err := g.checkCipherLocked(user, &m); err != nil {
		return proto.ChatMessage{}, err
	}
	if err := checkExtras(in, &m, g.now()); err != nil {
		return proto.ChatMessage{}, err
	}
	return m, nil
}

// postLocked stores a checked message and relays it.
func (g *Server) postLocked(m proto.ChatMessage) (*proto.ChatMessage, error) {
	if m.ReplyTo != "" && g.findMessageLocked(m.Channel, m.ReplyTo) == nil {
		m.ReplyTo = "" // The message it replied to is gone.
		return nil, errors.New("the message you replied to was deleted")
	}
	m.ID = g.nextMessageID()
	m.Time = g.now().UTC().Format(time.RFC3339Nano)
	if err := guildstore.AppendMessage(g.DataDir, m.Channel, m); err != nil {
		return nil, errors.New("the guild could not save the message")
	}
	g.chat.msgs[m.Channel] = append(g.chat.msgs[m.Channel], m)
	g.relayLocked(m)
	return &m, nil
}

// edit replaces the author's own message text.
func (g *Server) edit(user string, in *proto.ChatMessage) error {
	if in == nil {
		return errors.New("nothing sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.findMessageLocked(in.Channel, in.ID)
	if m == nil || m.Deleted {
		return errors.New("that message is gone")
	}
	if m.Author != user {
		return errors.New("you can only edit your own messages")
	}
	if !g.canSeeLocked(user, in.Channel) || !g.canLocked(user, in.Channel, proto.PermSendMessages) {
		return errors.New("you can't send messages in this channel")
	}
	n := *m
	n.Version, n.Nonce, n.Cipher, n.Sig = in.Version, in.Nonce, in.Cipher, in.Sig
	if err := g.checkCipherLocked(user, &n); err != nil {
		return err
	}
	// Edit History: keep the text it had, still sealed.
	was := m.Edited
	if was == "" {
		was = m.Time
	}
	n.Earlier = append(append([]proto.Earlier{}, m.Earlier...), proto.Earlier{Version: m.Version, Nonce: m.Nonce, Cipher: m.Cipher, Time: was})
	if len(n.Earlier) > maxEarlier {
		n.Earlier = n.Earlier[len(n.Earlier)-maxEarlier:]
	}
	n.Edited = g.now().UTC().Format(time.RFC3339Nano)
	return g.updateMessageLocked(m, n)
}

// deleteMessage removes a message: your own, or anyone's with Manage Messages.
func (g *Server) deleteMessage(user, channel, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.findMessageLocked(channel, id)
	if m == nil || m.Deleted {
		return errors.New("that message is gone")
	}
	if m.Author != user && !g.canLocked(user, channel, proto.PermManageMessages) {
		return errors.New("you can only delete your own messages")
	}
	n := proto.ChatMessage{ID: m.ID, Channel: m.Channel, Author: m.Author, Time: m.Time, ReplyTo: m.ReplyTo, Deleted: true}
	g.unpinGoneLocked(channel, id)
	return g.updateMessageLocked(m, n)
}

// react adds or removes user's emoji reaction.
func (g *Server) react(user, channel, id, emoji string, on bool) error {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" || len(emoji) > maxEmoji || strings.ContainsAny(emoji, " <>\"'") {
		return errors.New("that is not an emoji")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.findMessageLocked(channel, id)
	if m == nil || m.Deleted {
		return errors.New("that message is gone")
	}
	if !g.canSeeLocked(user, channel) || !g.canLocked(user, channel, proto.PermAddReactions) {
		return errors.New("you can't add reactions in this channel")
	}
	n := *m
	r := map[string][]string{}
	for e, us := range m.Reactions {
		r[e] = append([]string{}, us...)
	}
	users := r[emoji]
	idx := -1
	for i, u := range users {
		if u == user {
			idx = i
		}
	}
	switch {
	case on && idx < 0:
		if _, exists := r[emoji]; !exists && len(r) >= maxEmojiPerMsg {
			return errors.New("this message has too many different reactions")
		}
		r[emoji] = append(users, user)
	case !on && idx >= 0:
		r[emoji] = append(users[:idx:idx], users[idx+1:]...)
		if len(r[emoji]) == 0 {
			delete(r, emoji)
		}
	default:
		return nil
	}
	n.Reactions = r
	return g.updateMessageLocked(m, n)
}

func (g *Server) updateMessageLocked(at *proto.ChatMessage, n proto.ChatMessage) error {
	if err := guildstore.AppendMessage(g.DataDir, n.Channel, n); err != nil {
		return errors.New("the guild could not save the change")
	}
	*at = n
	g.relayLocked(n)
	return nil
}

// relayLocked sends a new or changed message to everyone online (live).
func (g *Server) relayLocked(m proto.ChatMessage) {
	ev := proto.Response{Type: "event", OK: true, Event: "message", Chat: &m}
	for u, s := range g.sessions {
		if g.canSeeLocked(u, m.Channel) {
			go s.conn.Send(ev)
		}
	}
}

// history returns up to limit messages before the given ID (newest last).
func (g *Server) history(user, channel, before string, limit int) ([]proto.ChatMessage, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.isChannelLocked(channel) {
		return nil, false, errors.New("there is no such channel")
	}
	if !g.canSeeLocked(user, channel) {
		return nil, false, errors.New("you can't read this channel")
	}
	if limit <= 0 || limit > historyPageLimit {
		limit = historyPage
	}
	list := g.chat.msgs[channel]
	end := len(list)
	if before != "" {
		end = sort.Search(len(list), func(i int) bool { return list[i].ID >= before })
	}
	start := max(end-limit, 0)
	out := append([]proto.ChatMessage{}, list[start:end]...)
	return out, start > 0, nil
}

// typing tells the others that user is typing in channel.
func (g *Server) typing(user, channel string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.isChannelLocked(channel) || !g.canLocked(user, channel, proto.PermSendMessages) {
		return
	}
	ev := proto.Response{Type: "event", OK: true, Event: "typing", Channel: channel, User: user}
	for u, s := range g.sessions {
		if u != user && g.canSeeLocked(u, channel) {
			go s.conn.Send(ev)
		}
	}
}

// purgeOldLocked drops messages older than the channel's limit.
func (g *Server) purgeOldLocked(channel string) {
	n := g.nodeLocked(channel)
	if n == nil {
		return
	}
	_, days := g.effectiveLocked(*n)
	if days <= 0 {
		return
	}
	cut := g.now().Add(-time.Duration(days) * 24 * time.Hour)
	list := g.chat.msgs[channel]
	i := 0
	for i < len(list) {
		t, err := time.Parse(time.RFC3339Nano, list[i].Time)
		if err != nil || !t.Before(cut) {
			break
		}
		i++
	}
	if i == 0 {
		return
	}
	g.chat.msgs[channel] = append([]proto.ChatMessage{}, list[i:]...)
	if err := guildstore.RewriteMessages(g.DataDir, channel, g.chat.msgs[channel]); err != nil {
		g.Log.Printf("could not remove old messages: %v", err)
	}
	g.Log.Printf("removed %d messages older than %d days from %s", i, days, channel)
}

// chatRequest handles channel and message requests from signed-in members. ok is
// false if req.Type is not one of them.
func (g *Server) chatRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "channels":
		resp.Channels = g.channelList(user)
	case "channel_put":
		resp.Message, err = g.channelPut(user, req.Node)
	case "channel_delete":
		err = g.channelDelete(user, req.ChannelID)
	case "channel_move":
		err = g.channelMove(user, req.ChannelID, req.Under, req.Place)
	case "key_states":
		resp.Keys = g.keyStates()
	case "my_keys":
		resp.Boxes = g.myBoxes(user)
	case "put_keys":
		err = g.putKeys(user, req.Boxes)
	case "send":
		resp.Chat, err = g.send(user, req.Msg)
	case "edit":
		err = g.edit(user, req.Msg)
	case "delete_message":
		err = g.deleteMessage(user, req.ChannelID, req.MessageID)
	case "react":
		err = g.react(user, req.ChannelID, req.MessageID, req.Emoji, req.On)
	case "history":
		resp.Messages, resp.More, err = g.history(user, req.ChannelID, req.Before, req.Limit)
	case "typing":
		g.typing(user, req.ChannelID)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}

// retireChannelKeyLocked marks one channel's key as needing replacement
// (someone lost access to it).
func (g *Server) retireChannelKeyLocked(id string) {
	if k := g.chat.keys[id]; k != nil && k.Current > 0 && !k.Retired {
		k.Retired = true
		guildstore.SaveKeys(g.DataDir, id, *k)
	}
}

// checkOverridesLocked checks a node's permission rules.
// Changing them needs Manage Roles, and only for roles ranked below you.
func (g *Server) checkOverridesLocked(user string, n *proto.ChannelNode) error {
	var oldO []proto.Override
	oldInherit := false
	if old := g.nodeLocked(n.ID); old != nil {
		oldO, oldInherit = old.Overrides, old.InheritPerms
	}
	if fmt.Sprint(oldO) == fmt.Sprint(n.Overrides) && oldInherit == n.InheritPerms {
		return nil
	}
	if err := g.needLocked(user, proto.PermManageRoles); err != nil {
		return errors.New("changing permission rules needs Manage Roles")
	}
	top, admin, mine := g.topRankLocked(user), g.isAdminLocked(user), g.guildPermsLocked(user)
	seen := map[string]bool{}
	var out []proto.Override
	for _, o := range n.Overrides {
		r := g.roleLocked(o.Role)
		if r == nil {
			return errors.New("a rule names a role that doesn't exist")
		}
		if seen[o.Role] {
			return errors.New("a role can only have one rule here")
		}
		seen[o.Role] = true
		if r.ID != proto.EveryoneRole && r.Rank >= top {
			changed := true
			for _, x := range oldO {
				if x.Role == o.Role && fmt.Sprint(x) == fmt.Sprint(o) {
					changed = false
				}
			}
			if changed {
				return errors.New("you can only set rules for roles ranked below you")
			}
		}
		for _, list := range [][]string{o.Allow, o.Deny} {
			for _, p := range list {
				ok := false
				for _, c := range proto.ChannelPerms {
					ok = ok || c == p
				}
				if !ok {
					return fmt.Errorf("%q can't be set per channel", p)
				}
				if !admin && !mine[p] {
					return fmt.Errorf("you can't allow or deny a permission you don't have (%s)", p)
				}
			}
		}
		o.Allow, _ = cleanPerms(o.Allow)
		o.Deny, _ = cleanPerms(o.Deny)
		if len(o.Allow)+len(o.Deny) > 0 {
			out = append(out, o)
		}
	}
	n.Overrides = out
	return nil
}

// checkPlaceLocked checks that a node may go where its Parent says: categories
// at the top, folders in a category or folder, channels anywhere but inside
// a channel; no loops, and folders at most 3 deep.
func (g *Server) checkPlaceLocked(n *proto.ChannelNode) error {
	parent := g.nodeLocked(n.Parent)
	switch n.Kind {
	case proto.KindCategory:
		if n.Parent != "" {
			return errors.New("categories are always at the top")
		}
	case proto.KindFolder:
		if parent == nil || proto.IsRoom(parent.Kind) {
			return errors.New("a folder goes inside a category or another folder")
		}
	case proto.KindChannel, proto.KindVoice:
		if n.Parent != "" && (parent == nil || proto.IsRoom(parent.Kind)) {
			return errors.New("a channel goes inside a category or folder, or at the top")
		}
	default:
		return errors.New("unknown kind")
	}
	for p := parent; p != nil; p = g.nodeLocked(p.Parent) {
		if p.ID == n.ID {
			return errors.New("a folder can't go inside itself")
		}
	}
	depth := 0
	if parent != nil {
		depth = g.folderDepthLocked(parent.ID)
	}
	if n.Kind == proto.KindFolder {
		depth++
		depth += g.deepestFolderBelowLocked(n.ID)
	}
	if depth > maxFolderDepth {
		return fmt.Errorf("folders can be at most %d levels deep", maxFolderDepth)
	}
	return nil
}

// channelMove puts a node at place (0 = first) among the nodes in parent,
// numbering them again in order (dragging in the side bar).
func (g *Server) channelMove(user, id, parent string, place int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageChannels); err != nil {
		return err
	}
	node := g.nodeLocked(id)
	if node == nil {
		return errors.New("there is no such channel")
	}
	moved := *node
	moved.Parent = parent
	if err := g.checkPlaceLocked(&moved); err != nil {
		return err
	}
	var kids []*proto.ChannelNode
	for i := range g.chat.nodes {
		if n := &g.chat.nodes[i]; n.Parent == parent && n.ID != id {
			kids = append(kids, n)
		}
	}
	sort.SliceStable(kids, func(i, j int) bool { return kids[i].Order < kids[j].Order })
	place = max(0, min(place, len(kids)))
	before := g.snapshotViewersLocked()
	node.Parent = parent
	kids = append(kids[:place], append([]*proto.ChannelNode{node}, kids[place:]...)...)
	for i, n := range kids {
		n.Order = i
	}
	if err := guildstore.SaveChannels(g.DataDir, g.chat.nodes); err != nil {
		return errors.New("the guild could not save its channels")
	}
	g.notifyLater(g.allConnsLocked(), "channels_changed")
	g.afterAccessChangeLocked(before)
	return nil
}
