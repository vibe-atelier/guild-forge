package guildconn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nexus/internal/e2e"
	"nexus/internal/proto"
)

// This file handles pictures and files in channels, and custom emoji.

// ---- attachments ----------------------------------------------------------------------
//
// A file is encrypted with the channel's current key and kept by the guild
// (it can't read it). The message itself is one line:
//   nexus-file:<hash>/<name>/<size>
// sealed like any other message.

// FileRef is an attachment as found in a message's text.
type FileRef struct {
	Hash string
	Name string
	Size int64
}

var fileLine = regexp.MustCompile(`^nexus-file:([0-9a-f]{64})/([^/\s]+)/([0-9]+)$`)

// ParseFileRef reads a message line that is an attachment.
func ParseFileRef(line string) (FileRef, bool) {
	m := fileLine.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return FileRef{}, false
	}
	name, err := url.PathUnescape(m[2])
	if err != nil {
		return FileRef{}, false
	}
	var size int64
	fmt.Sscan(m[3], &size)
	return FileRef{Hash: m[1], Name: filepath.Base(name), Size: size}, true
}

const maxChannelFile = 2900 << 10 // Once encrypted, the file must fit the guild's 3 MB.

func fileAAD(channel string) string { return channel + "|file" }

// SendFile encrypts a file with the channel's key, gives it to the guild,
// and sends a message that points to it.
func (gc *Conn) SendFile(channel, name string, data []byte, replyTo string) error {
	if len(data) == 0 || len(data) > maxChannelFile {
		return errors.New("files can be at most 2.8 MB")
	}
	gc.mu.Lock()
	version, key := 0, []byte(nil)
	for v, k := range gc.chat.keys[channel] {
		if v > version {
			version, key = v, k
		}
	}
	gc.mu.Unlock()
	if key == nil {
		return errors.New("your app doesn't have this channel's key yet")
	}
	nonce, sealed, err := e2e.Encrypt(key, fileAAD(channel), version, data)
	if err != nil {
		return err
	}
	blob := binary.BigEndian.AppendUint32(nil, uint32(version))
	blob = append(blob, byte(len(nonce)))
	blob = append(append(blob, nonce...), sealed...)
	r, err := gc.Call(proto.Request{Type: "attach_put", ChannelID: channel, Image: blob})
	if err != nil {
		return err
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	ref := fmt.Sprintf("nexus-file:%s/%s/%d", r.Message, url.PathEscape(name), len(data))
	return gc.Send(channel, ref, replyTo)
}

// OpenFile returns a received file, decrypted and kept on this computer.
func (gc *Conn) OpenFile(channel string, f FileRef) (string, error) {
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, f.Name)
	path := filepath.Join(gc.m.u.Dir, "media", "guilds", gc.id+"-files", f.Hash[:16]+"-"+safe)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	r, err := gc.Call(proto.Request{Type: "attach_get", ChannelID: channel, Hash: f.Hash})
	if err != nil {
		return "", err
	}
	b := r.Data
	if len(b) < 6 || len(b) < 5+int(b[4]) {
		return "", errors.New("the file is damaged")
	}
	version := int(binary.BigEndian.Uint32(b[:4]))
	n := int(b[4])
	nonce, sealed := b[5:5+n], b[5+n:]
	gc.mu.Lock()
	key := gc.chat.keys[channel][version]
	gc.mu.Unlock()
	if key == nil {
		return "", errors.New("you don't have the key this file was sent with")
	}
	plain, err := e2e.Decrypt(key, fileAAD(channel), version, nonce, sealed)
	if err != nil {
		return "", err
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	return path, os.WriteFile(path, plain, 0o600)
}

// ---- custom emoji -----------------------------------------------------------------
//
// A custom emoji in a message or reaction is written ":name:hash". Its
// picture is in the guild it came from, or borrowed into the guild where it
// is used.

var emojiToken = regexp.MustCompile(`:([a-z0-9_]{2,32}):([0-9a-f]{64})`)
var emojiShort = regexp.MustCompile(`:([a-z0-9_]{2,32}):`)
var hexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// EmojiChoice is an emoji you may use.
type EmojiChoice struct {
	Name      string
	Hash      string
	Guild     string
	GuildName string
}

// Token is how the emoji is written in a message or reaction.
func (e EmojiChoice) Token() string { return ":" + e.Name + ":" + e.Hash }

// ParseEmoji reads a custom emoji token (":name:hash").
func ParseEmoji(s string) (name, hash string, ok bool) {
	m := emojiToken.FindStringSubmatch(s)
	if m == nil || m[0] != s {
		return "", "", false
	}
	return m[1], m[2], true
}

// EmojiTokens finds the custom emoji in a text.
func EmojiTokens(s string) [][]string { return emojiToken.FindAllStringSubmatch(s, -1) }

func (gc *Conn) refreshEmoji(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "emoji_list"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	gc.mu.Lock()
	gc.emoji = r.Emojis
	gc.mu.Unlock()
}

// UsableEmoji lists the emoji of every guild you're in, the given
// guild's first.
func (m *Manager) UsableEmoji(first string) []EmojiChoice {
	var out, rest []EmojiChoice
	for _, g := range m.Views() {
		gc, err := m.Get(g.ID)
		if err != nil {
			continue
		}
		gc.mu.Lock()
		for _, e := range gc.emoji {
			if e.Borrowed {
				continue
			}
			c := EmojiChoice{Name: e.Name, Hash: e.Hash, Guild: g.ID, GuildName: g.Name}
			if g.ID == first {
				out = append(out, c)
			} else {
				rest = append(rest, c)
			}
		}
		gc.mu.Unlock()
	}
	return append(out, rest...)
}

// EmojiFile returns an emoji's picture kept on this computer, fetching it
// from a guild that has it.
func (m *Manager) EmojiFile(hash string) (string, error) {
	if !hexHash.MatchString(hash) {
		return "", errors.New("bad emoji")
	}
	path := filepath.Join(m.u.Dir, "media", "emoji", hash)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, gc := range m.conns {
		conns = append(conns, gc)
	}
	m.mu.Unlock()
	for _, gc := range conns {
		gc.mu.Lock()
		has := false
		for _, e := range gc.emoji {
			has = has || e.Hash == hash
		}
		gc.mu.Unlock()
		if !has {
			continue
		}
		if r, err := gc.Call(proto.Request{Type: "image_get", Hash: hash}); err == nil && len(r.Data) > 0 {
			os.MkdirAll(filepath.Dir(path), 0o700)
			if err := os.WriteFile(path, r.Data, 0o600); err == nil {
				return path, nil
			}
		}
	}
	return "", errors.New("the emoji's picture isn't available")
}

// prepareEmoji turns ":name:" into custom emoji tokens (this guild's first,
// then your other guilds'), and makes sure this guild can show any emoji
// from another guild by borrowing a copy.
func (gc *Conn) prepareEmoji(text string) string {
	choices := gc.m.UsableEmoji(gc.id)
	if len(choices) == 0 {
		return text
	}
	byName := map[string]EmojiChoice{}
	for i := len(choices) - 1; i >= 0; i-- { // The earlier one (this guild's) wins.
		byName[choices[i].Name] = choices[i]
	}
	parts := emojiToken.Split(text, -1)
	tokens := emojiToken.FindAllString(text, -1)
	var b strings.Builder
	for i, p := range parts {
		b.WriteString(emojiShort.ReplaceAllStringFunc(p, func(s string) string {
			if c, ok := byName[s[1:len(s)-1]]; ok {
				return c.Token()
			}
			return s
		}))
		if i < len(tokens) {
			b.WriteString(tokens[i])
		}
	}
	out := b.String()
	for _, t := range emojiToken.FindAllStringSubmatch(out, -1) {
		gc.ensureEmoji(t[1], t[2])
	}
	return out
}

// ensureEmoji borrows an emoji into this guild if it doesn't have it.
func (gc *Conn) ensureEmoji(name, hash string) {
	gc.mu.Lock()
	for _, e := range gc.emoji {
		if e.Hash == hash {
			gc.mu.Unlock()
			return
		}
	}
	gc.mu.Unlock()
	path, err := gc.m.EmojiFile(hash)
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	origin := ""
	for _, c := range gc.m.UsableEmoji("") {
		if c.Hash == hash {
			origin = c.Guild
		}
	}
	if _, err := gc.Call(proto.Request{Type: "emoji_borrow", EmojiName: name, ItemID: origin, Image: data}); err == nil {
		gc.mu.Lock()
		gc.emoji = append(gc.emoji, proto.Emoji{Name: name, Hash: hash, Borrowed: true, From: origin})
		gc.mu.Unlock()
	}
}

// PutEmoji uploads a guild emoji (Manage Guild).
func (gc *Conn) PutEmoji(name string, data []byte) error {
	_, err := gc.Call(proto.Request{Type: "emoji_put", EmojiName: name, Image: data})
	if err == nil {
		if s := gc.session(); s != nil {
			gc.refreshEmoji(s)
		}
	}
	return err
}

// DeleteEmoji removes a guild emoji.
func (gc *Conn) DeleteEmoji(name string) error {
	_, err := gc.Call(proto.Request{Type: "emoji_delete", EmojiName: name})
	if err == nil {
		if s := gc.session(); s != nil {
			gc.refreshEmoji(s)
		}
	}
	return err
}

// GuildEmoji lists this guild's own emoji.
func (gc *Conn) GuildEmoji() []proto.Emoji {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	var out []proto.Emoji
	for _, e := range gc.emoji {
		if !e.Borrowed {
			out = append(out, e)
		}
	}
	return out
}

func (gc *Conn) session() *proto.Session {
	gc.mu.Lock()
	defer gc.mu.Unlock()
	return gc.sess
}
