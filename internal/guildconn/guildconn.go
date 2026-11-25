// Package guildconn keeps the app connected to the guilds the user has
// joined: one live, encrypted connection per guild that signs in,
// follows the guild's events and reconnects by itself. It also pairs the
// user with a broadcasting guild.
package guildconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"nexus/internal/proto"
	"nexus/internal/user"
)

const (
	dialTimeout  = 5 * time.Second
	callTimeout  = 15 * time.Second
	pingInterval = 20 * time.Second
)

// These are the guild connection states shown in the app.
const (
	stConnecting = "connecting"
	stOnline     = "online"
	stOffline    = "offline"
	stElsewhere  = "elsewhere"  // The user is active on another computer.
	stNotMember  = "not_member" // For example, they left from another computer.
	stWrongKey   = "wrong_key"  // The guild's identity changed.
	stPending    = "pending"    // The join request is waiting for the owner's approval.
	stDeclined   = "declined"   // The owner declined the join request.
	stRemoved    = "removed"    // The user was kicked or banned.
	stBanned     = "banned"
)

// Conn is the app's link to one guild: it connects, signs in, keeps
// trying when the guild is unreachable, and holds what the guild sent.
type Conn struct {
	m        *Manager
	id       string
	chat     *chatCache // It is guarded by mu.
	addr     string
	others   []string // These are other addresses the guild gave.
	wake     chan struct{}
	stop     chan struct{}
	stopOnce sync.Once

	mu             sync.Mutex
	status         string
	detail         string
	lastSeen       time.Time
	name           string
	info           *proto.GuildInfo // It holds the About information without the picture bytes.
	isOwner        bool
	transfer       *proto.TransferInfo
	members        []proto.MemberInfo
	requests       int                  // It counts the join requests waiting (owner only).
	roles          []proto.Role         // These are the guild's roles.
	myPerms        map[string][]string  // The key "" means guild-wide; the others are channel IDs.
	pagesRev       int                  // It goes up when Calendar, Threads, Wiki or emoji change.
	emoji          []proto.Emoji        // These are this guild's emoji, and those it borrowed.
	sounds         []proto.Sound        // This is the soundboard.
	settings       proto.MemberSettings // This is what this guild keeps for you.
	settingsLoaded bool
	sess           *proto.Session
	takeOver       bool
}

// Manager holds the links to all the guilds of the unlocked user.
type Manager struct {
	onChange func() // It is called whenever something the window shows has changed.
	u        *user.User
	device   string

	mu    sync.Mutex
	conns map[string]*Conn

	joining    *joining     // It is a join waiting for CAPTCHA answers.
	dms        *dmState     // These are private messages.
	onNotice   func(Notice) // It shows pop-up notifications.
	voiceSink  VoiceSink    // It receives voice that arrives.
	streamSink StreamSink   // It receives shared screens that arrive.
	keyNeeded  func(guildID string)
	// It is called when a moderator moves you ("" means out of voice).
	voiceMoved func(guildID, channel string)
	soundPlay  func(guildID, channel, from, sound string) // It is called when someone plays a soundboard sound.
	// It is called when someone new needs a full camera picture.
	cameraKey func(guildID string)
}

// New starts links to the user's guilds. device names this computer;
// onChange is called whenever something the window shows has changed.
func New(u *user.User, device string, onChange func()) *Manager {
	m := &Manager{onChange: onChange, u: u, device: device, conns: map[string]*Conn{}}
	if list, err := u.Guilds(); err == nil {
		for _, g := range list {
			m.Add(g)
		}
	}
	return m
}

// Add starts a link to a guild (nothing happens if there is one already).
func (m *Manager) Add(g user.Guild) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.conns[g.GuildID]; ok {
		return
	}
	gc := &Conn{m: m, id: g.GuildID, addr: g.GuildAddress, others: g.OtherAddresses, name: g.GuildName, lastSeen: g.LastSeen,
		status: stConnecting, wake: make(chan struct{}, 1), stop: make(chan struct{}), chat: newChatCache()}
	for ch, n := range m.u.Mentions()[g.GuildID] { // These are counts from before a restart.
		gc.chat.mentions[ch] = n
	}
	m.conns[g.GuildID] = gc
	go gc.run()
}

// Get returns the link to a guild.
func (m *Manager) Get(id string) (*Conn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	gc := m.conns[id]
	if gc == nil {
		return nil, errors.New("you are not in that guild")
	}
	return gc, nil
}

// Remove closes the link to a guild and forgets it.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	gc := m.conns[id]
	delete(m.conns, id)
	m.mu.Unlock()
	if gc != nil {
		gc.close()
	}
}

// CloseAll closes every link (when the user locks Nexus or it closes).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := m.conns
	m.conns = map[string]*Conn{}
	m.mu.Unlock()
	for _, gc := range list {
		gc.close()
	}
}

func (gc *Conn) close() {
	gc.stopOnce.Do(func() { close(gc.stop) })
	gc.mu.Lock()
	if gc.sess != nil {
		gc.sess.Close()
	}
	gc.mu.Unlock()
}

func (gc *Conn) set(status, detail string) {
	gc.mu.Lock()
	gc.status, gc.detail = status, detail
	gc.mu.Unlock()
	gc.m.onChange()
}

func (gc *Conn) poke() {
	select {
	case gc.wake <- struct{}{}:
	default:
	}
}

// run keeps the guild connected until close is called.
func (gc *Conn) run() {
	backoff := 2 * time.Second
	for {
		wait := gc.connectOnce()
		if wait == 0 { // It was online for a while, so it tries again soon.
			backoff = 2 * time.Second
			wait = backoff
		} else if wait < 0 { // It is offline, so the wait grows, up to 30 seconds.
			wait = backoff
			backoff = min(backoff*2, 30*time.Second)
		}
		select {
		case <-gc.stop:
			return
		case <-gc.wake:
		case <-time.After(wait):
		}
	}
}

// connectOnce connects, signs in and serves events until the connection
// ends. It returns how long to wait before trying again (negative = use the
// growing offline wait, 0 = soon).
func (gc *Conn) connectOnce() time.Duration {
	select {
	case <-gc.stop:
		return time.Hour
	default:
	}
	c, err := proto.Dial(gc.addr, dialTimeout)
	if err != nil {
		// The usual address doesn't answer: try the others the guild gave, such as its
		// internet address when you are away from home.
		if c = gc.dialOthers(); c == nil {
			gc.markOffline()
			return -1
		}
	}
	if KeyText(c.GuildKey) != gc.id {
		c.Close()
		gc.set(stWrongKey, "This address now belongs to a different guild (possible impostor). Nexus will not connect.")
		return time.Minute
	}
	s := proto.NewSession(c)
	defer s.Close()
	gc.mu.Lock()
	gc.sess = s
	take := gc.takeOver
	gc.takeOver = false
	gc.mu.Unlock()
	defer func() {
		gc.mu.Lock()
		gc.sess = nil
		gc.mu.Unlock()
	}()

	if wait, code, ok := gc.signIn(s, take); !ok {
		if code == "pending" {
			return gc.waitForApproval(s)
		}
		return wait
	}
	gc.refreshAbout(s)
	gc.refreshMembers(s)
	gc.refreshRequests(s)
	gc.afterSignIn(s)
	gc.collectDMs(s)
	gc.seen()
	gc.set(stOnline, "")

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-gc.stop:
			return time.Hour
		case <-s.Done():
			gc.markOffline()
			return 0
		case <-ping.C:
			if _, err := s.Call(proto.Request{Type: "ping"}, callTimeout); err != nil {
				gc.markOffline()
				return 0
			}
			gc.seen()
		case v, ok := <-s.Voice:
			if ok && v.Type == "stream" {
				gc.onStream(v.Stream)
			} else if ok {
				gc.onVoice(v.Voice)
			}
		case ev, ok := <-s.Events:
			if !ok {
				gc.markOffline()
				return 0
			}
			switch ev.Event {
			case "taken_over":
				gc.set(stElsewhere, "Active on another computer")
				return 15 * time.Second
			case "guild_updated":
				gc.refreshAbout(s)
				gc.refreshMembers(s)
			case "owner_changed", "transfer_changed":
				if _, _, ok := gc.signIn(s, false); !ok {
					return 15 * time.Second
				}
				gc.refreshAbout(s)
				gc.refreshMembers(s)
				gc.refreshRequests(s)
			case "stream_key":
				go gc.onKeyNeeded()
			case "camera_key":
				go gc.onCameraKey()
			case "voice_moved":
				go gc.onVoiceMoved(ev.Channel)
			case "sound_play":
				go gc.onSoundPlay(ev.Channel, ev.User, ev.SoundID)
			case "sounds_changed":
				gc.refreshSounds(s)
				gc.m.onChange()
			case "voice_changed":
				gc.refreshChannels(s)
				gc.m.onChange()
			case "channels_changed", "roles_changed":
				gc.refreshChannels(s)
				gc.refreshMembers(s)
				gc.refreshRoles(s)
			case "keys_changed":
				gc.refreshKeys(s)
			case "keys_needed":
				go gc.doKeyWork(s, ev.Channel)
			case "message":
				if ev.Chat != nil {
					gc.onMessage(*ev.Chat)
				}
			case "typing":
				gc.onTyping(ev.Channel, ev.User)
			case "presence":
				gc.refreshMembers(s)
			case "dm":
				if ev.DM != nil {
					go gc.m.receive(s, *ev.DM)
				}
			case "requests_changed":
				gc.refreshRequests(s)
			case "emoji_changed":
				gc.refreshEmoji(s)
				gc.mu.Lock()
				gc.pagesRev++
				gc.mu.Unlock()
			default:
				if strings.HasPrefix(ev.Event, "pages_changed") {
					gc.mu.Lock()
					gc.pagesRev++ // The window reloads the page it shows.
					gc.mu.Unlock()
				}
			case "removed":
				gc.set(stRemoved, "You were removed from this guild")
				return time.Hour
			}
			gc.m.onChange()
		}
	}
}

func (gc *Conn) signIn(s *proto.Session, takeOver bool) (wait time.Duration, code string, ok bool) {
	u := gc.m.u
	sig := ed25519.Sign(u.SigningKey, proto.AuthProof(s.SessionID(), gc.m.device))
	r, err := s.Call(proto.Request{Type: "auth", UserKey: u.SigningKey.Public().(ed25519.PublicKey),
		Sig: sig, DeviceID: gc.m.device, TakeOver: takeOver, UserName: u.Profile.DefaultName,
		EncKey: u.DecryptionKey.PublicKey().Bytes(), TypingText: u.Profile.TypingIndicator,
		Status: u.Profile.OnlineStatus,
		// These are account facts for staff.
		Created: u.Profile.CreationDate.UTC().Format(time.RFC3339), ServerCount: u.MemberGuildCount()}, callTimeout)
	if err != nil {
		gc.markOffline()
		return -1, "", false
	}
	if !r.OK {
		switch r.Code {
		case "elsewhere":
			gc.set(stElsewhere, "Active on another computer")
			return 15 * time.Second, r.Code, false
		case "pending":
			gc.set(stPending, "Waiting for the owner's approval")
			return time.Minute, r.Code, false
		case "banned":
			gc.set(stBanned, "You are banned from this guild")
			return time.Hour, r.Code, false
		case "not_member":
			gc.mu.Lock()
			wasPending := gc.status == stPending || gc.status == stDeclined
			gc.mu.Unlock()
			if wasPending {
				gc.set(stDeclined, "Your join request was declined")
			} else {
				gc.set(stNotMember, "You are not a member of this guild any more")
			}
			return time.Hour, r.Code, false
		}
		gc.set(stOffline, r.Error)
		return 30 * time.Second, r.Code, false
	}
	gc.m.u.UpdateGuild(gc.id, func(g *user.Guild) { g.Membership = "" })
	gc.learnAddresses(r.Addresses)
	gc.mu.Lock()
	gc.isOwner = r.IsOwner
	gc.transfer = r.Transfer
	gc.mu.Unlock()
	return 0, "", true
}

// waitForApproval keeps the connection open while the owner decides.
func (gc *Conn) waitForApproval(s *proto.Session) time.Duration {
	for {
		select {
		case <-gc.stop:
			return time.Hour
		case <-s.Done():
			return 30 * time.Second
		case ev, ok := <-s.Events:
			if !ok {
				return 30 * time.Second
			}
			switch ev.Event {
			case "approved":
				gc.m.u.UpdateGuild(gc.id, func(g *user.Guild) { g.Membership = "" })
				gc.set(stConnecting, "")
				return 1 // Connect again straight away, now as a member.
			case "declined":
				gc.set(stDeclined, "Your join request was declined")
				return time.Hour
			case "removed":
				gc.set(stBanned, "You are banned from this guild")
				return time.Hour
			}
		}
	}
}

func (gc *Conn) refreshRequests(s *proto.Session) {
	gc.mu.Lock()
	owner := gc.isOwner
	gc.mu.Unlock()
	n := 0
	if owner {
		if r, err := s.Call(proto.Request{Type: "requests"}, callTimeout); err == nil && r.OK {
			n = len(r.People)
		}
	}
	gc.mu.Lock()
	gc.requests = n
	gc.mu.Unlock()
}

func (gc *Conn) refreshAbout(s *proto.Session) {
	path := gc.m.u.GuildPicturePath(gc.id)
	gc.mu.Lock()
	have := ""
	if gc.info != nil {
		have = gc.info.PictureHash
	}
	gc.mu.Unlock()
	if have == "" {
		if b, err := os.ReadFile(path); err == nil {
			have = pictureHashOf(b)
		}
	}
	r, err := s.Call(proto.Request{Type: "about", PictureHash: have}, callTimeout)
	if err != nil || r.Guild == nil {
		return
	}
	info := *r.Guild
	if info.PictureHash == "" {
		os.Remove(path)
	} else if len(info.Picture) > 0 {
		os.MkdirAll(filepath.Dir(path), 0o700)
		os.WriteFile(path, info.Picture, 0o600)
	}
	info.Picture = nil
	gc.mu.Lock()
	gc.info = &info
	changed := gc.name != info.Name
	gc.name = info.Name
	gc.mu.Unlock()
	if changed {
		gc.m.u.UpdateGuild(gc.id, func(g *user.Guild) { g.GuildName = info.Name })
	}
}

func (gc *Conn) refreshMembers(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "members"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	gc.mu.Lock()
	gc.members = r.Members
	gc.mu.Unlock()
}

func (gc *Conn) seen() {
	now := time.Now().UTC().Truncate(time.Second)
	gc.mu.Lock()
	gc.lastSeen = now
	gc.mu.Unlock()
	gc.m.u.UpdateGuild(gc.id, func(g *user.Guild) { g.LastSeen = now })
}

func (gc *Conn) markOffline() {
	gc.mu.Lock()
	last := gc.lastSeen
	gc.mu.Unlock()
	detail := "Offline"
	if !last.IsZero() {
		detail = "Offline. Last seen " + ago(last) + "."
	}
	gc.set(stOffline, detail)
}

// Call sends a request on the live connection.
func (gc *Conn) Call(req proto.Request) (proto.Response, error) {
	gc.mu.Lock()
	s, st := gc.sess, gc.status
	gc.mu.Unlock()
	if s == nil || st != stOnline {
		return proto.Response{}, errors.New("the guild is not connected right now")
	}
	r, err := s.Call(req, callTimeout)
	if err != nil {
		return r, err
	}
	if !r.OK {
		return r, errors.New(r.Error)
	}
	return r, nil
}

// UseHere moves this user's session in the guild to this computer.
func (gc *Conn) UseHere() {
	gc.mu.Lock()
	gc.takeOver = true
	gc.mu.Unlock()
	gc.set(stConnecting, "")
	gc.poke()
}

// view is what the window shows about one guild.
type GuildView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Address         string `json:"address"`
	Status          string `json:"status"`
	Detail          string `json:"detail"`
	IsOwner         bool   `json:"is_owner"`
	About           string `json:"about"`
	MemberCount     int    `json:"member_count"`
	ShowMemberCount bool   `json:"show_member_count"`
	// It is the owner's default.
	DefaultNotify string `json:"default_notify"`
	// It is your choice for the whole guild, or "".
	MyNotify string `json:"my_notify"`
	// It is set while the currency is on.
	Currency    *proto.CurrencySettings `json:"currency,omitempty"`
	OwnerKey    string                  `json:"owner_key"`
	PictureHash string                  `json:"picture_hash"`
	Members     []MemberView            `json:"members"`
	Transfer    *TransferView           `json:"transfer,omitempty"`
	// It counts the join requests waiting (owner only).
	Requests int             `json:"requests"`
	Join     *proto.JoinInfo `json:"join,omitempty"`
	Me       string          `json:"me"`
	Channels []ChannelView   `json:"channels"`
	// It maps each channel to lines like "Alice is typing…".
	Typing map[string][]string `json:"typing"`
	// The roles are listed highest rank first.
	Roles []proto.Role `json:"roles"`
	// The key "" means guild-wide; the others are channel IDs.
	MyPerms  map[string][]string `json:"my_perms"`
	Pages    proto.PageSettings  `json:"pages"`     // It says which pages are on.
	PagesRev int                 `json:"pages_rev"` // It changes when page content changes.
}

// MemberView is one member of a guild as the window shows it.
type MemberView struct {
	UserKey      string   `json:"user_key"`
	Name         string   `json:"name"`
	Joined       string   `json:"joined"`
	IsOwner      bool     `json:"is_owner"`
	Online       bool     `json:"online"`
	IsMe         bool     `json:"is_me"`
	Roles        []string `json:"roles"`
	Status       string   `json:"status"`       // It is Online, Idle or DND while they are online.
	AgreedRules  bool     `json:"agreed_rules"` // It is true when they joined by ticking "I agree".
	Nick         string   `json:"nick"`         // It is their name in this guild.
	Bio          string   `json:"bio"`          // It is their bio.
	PictureHash  string   `json:"picture_hash"` // Get the picture with MemberPicture.
	BannerHash   string   `json:"banner_hash"`
	TimeoutUntil string   `json:"timeout_until"` // It is in RFC 3339, while they are timed out.
	// It is when the account was created; only staff get it.
	Created string `json:"created"`
	Guilds  int    `json:"guilds"` // It is how many guilds they are in; only staff get it.
}

// TransferView is an ownership transfer waiting to be accepted.
type TransferView struct {
	From   string `json:"from"`
	To     string `json:"to"`
	FromMe bool   `json:"from_me"`
	ToMe   bool   `json:"to_me"`
}

// Views returns every guild as the window shows it, in the user's order.
func (m *Manager) Views() []GuildView {
	me := KeyText(m.u.SigningKey.Public().(ed25519.PublicKey))
	list, _ := m.u.Guilds()
	reads := m.u.Reads()
	var out []GuildView
	for _, g := range list {
		gc, err := m.Get(g.GuildID)
		if err != nil {
			continue
		}
		gc.mu.Lock()
		v := GuildView{ID: gc.id, Name: gc.name, Address: gc.addr, Status: gc.status, Detail: gc.detail, IsOwner: gc.isOwner, Requests: gc.requests, Me: me}
		if gc.info != nil {
			v.Join = gc.info.Join
		}
		if v.Name == "" {
			v.Name = "Guild"
		}
		if gc.info != nil {
			v.About, v.MemberCount, v.ShowMemberCount = gc.info.About, gc.info.MemberCount, gc.info.ShowMemberCount
			v.DefaultNotify = gc.info.DefaultNotify
			v.Currency = gc.info.Currency
			v.OwnerKey, v.PictureHash = gc.info.OwnerKey, gc.info.PictureHash
		} else if b, err := os.ReadFile(m.u.GuildPicturePath(gc.id)); err == nil {
			v.PictureHash = pictureHashOf(b)
		}
		for _, mem := range gc.members {
			v.Members = append(v.Members, MemberView{UserKey: mem.UserKey, Name: mem.Name, Joined: mem.Joined,
				IsOwner: mem.IsOwner, Online: mem.Online, IsMe: mem.UserKey == me, Roles: mem.Roles, Status: mem.Status, AgreedRules: mem.AgreedRules,
				Nick: mem.Nick, Bio: mem.Bio, PictureHash: mem.PictureHash, BannerHash: mem.BannerHash, TimeoutUntil: mem.TimeoutUntil, Created: mem.Created, Guilds: mem.Guilds})
		}
		if t := gc.transfer; t != nil {
			v.Transfer = &TransferView{t.From, t.To, t.From == me, t.To == me}
		}
		v.Channels = gc.channelViewsLocked(reads[gc.id])
		v.Roles, v.MyPerms = gc.roles, gc.myPerms
		v.PagesRev = gc.pagesRev
		v.MyNotify = gc.settings.Notify["guild"]
		if gc.info != nil && gc.info.Pages != nil {
			v.Pages = *gc.info.Pages
		}
		v.Typing = gc.typingLocked()
		if v.Status == stOffline && v.Detail == "" {
			v.Detail = "Offline"
		}
		gc.mu.Unlock()
		out = append(out, v)
	}
	return out
}

// ---- pairing from the app (replaces the test client) ----------------------

// PairCheck is what a guild says about itself before pairing or joining.
type PairCheck struct {
	GuildID      string `json:"guild_id"`
	Name         string `json:"name"`
	HasOwner     bool   `json:"has_owner"`
	Broadcasting bool   `json:"broadcasting"`
	Address      string `json:"address"`
	Known        bool   `json:"known"`

	// For joining: the public About page and what joining needs.
	About       string          `json:"about"`
	MemberCount int             `json:"member_count"`
	Picture     []byte          `json:"picture,omitempty"`
	Join        *proto.JoinInfo `json:"join,omitempty"`
	Rules       string          `json:"rules,omitempty"` // The Rules are included when they are public.
}

// CheckAddress tidies a typed guild address ("host:port").
func CheckAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "nexus://"), "http://")
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return "", errors.New("type the port in the Port box, or after a colon, like 192.168.1.20:50000")
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "", errors.New("the port must be a number from 1 to 65535")
	}
	return net.JoinHostPort(host, port), nil
}

// Check asks the guild at addr who it is (before pairing or joining).
func (m *Manager) Check(addr string) (*PairCheck, error) {
	addr, err := CheckAddress(addr)
	if err != nil {
		return nil, err
	}
	c, err := proto.Dial(addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s. Is the guild running, and is the address right?", addr)
	}
	s := proto.NewSession(c)
	defer s.Close()
	info, err := s.Call(proto.Request{Type: "info"}, callTimeout)
	if err != nil {
		return nil, err
	}
	about, err := s.Call(proto.Request{Type: "about", PictureHash: "-"}, callTimeout)
	if err != nil || about.Guild == nil {
		return nil, errors.New("the guild did not answer")
	}
	id := KeyText(s.GuildKey())
	_, known := m.Get(id)
	g := about.Guild
	pc := &PairCheck{GuildID: id, Name: g.Name, HasOwner: info.HasOwner,
		Broadcasting: info.Broadcasting, Address: addr, Known: known == nil,
		About: g.About, MemberCount: g.MemberCount, Picture: g.Picture, Join: g.Join}
	if g.Join != nil && g.Join.RulesEnabled && g.Join.RulesPublic {
		if r, err := s.Call(proto.Request{Type: "rules"}, callTimeout); err == nil && r.OK {
			pc.Rules = r.Rules
		}
	}
	return pc, nil
}

// PairResult is the answer to a pairing attempt.
type PairResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	TriesLeft int    `json:"tries_left,omitempty"`
}

// Pair makes the user the owner of the guild at addr with its one-time
// password; expectID is the guild ID Check returned, so a different guild
// answering at that address is refused.
func (m *Manager) Pair(addr, expectID, otp string) (*PairResult, error) {
	addr, err := CheckAddress(addr)
	if err != nil {
		return nil, err
	}
	c, err := proto.Dial(addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s", addr)
	}
	s := proto.NewSession(c)
	defer s.Close()
	id := KeyText(s.GuildKey())
	if expectID != "" && id != expectID {
		return nil, errors.New("the guild at this address changed since you checked it; check again")
	}
	u := m.u
	r, err := s.Call(proto.Request{Type: "pair", OTP: otp,
		UserKey: u.SigningKey.Public().(ed25519.PublicKey),
		Sig:     ed25519.Sign(u.SigningKey, proto.PairProof(s.SessionID()))}, callTimeout)
	if err != nil {
		return nil, err
	}
	if !r.OK {
		return &PairResult{Error: r.Error, TriesLeft: r.TriesLeft}, nil
	}
	name := DefaultGuildName
	if a, err := s.Call(proto.Request{Type: "about", PictureHash: "-"}, callTimeout); err == nil && a.Guild != nil {
		name = a.Guild.Name
	}
	g := user.Guild{GuildID: id, GuildName: name, SignatureVerificationKey: id,
		JoinDate: time.Now().UTC().Truncate(time.Second), GuildAddress: addr}
	if err := u.SaveGuild(g); err != nil {
		return nil, err
	}
	m.Add(g)
	m.onChange()
	return &PairResult{OK: true}, nil
}

// DefaultGuildName matches the guild's own default.
const DefaultGuildName = "New Guild"

// ---- helpers ------------------------------------------------------------------

// KeyText shows a key the way Nexus does everywhere (base32).
func KeyText(k []byte) string { return user.KeyText(k) }

func keyTextDecode(s string) ([]byte, error) { return user.KeyFromText(s) }

func pictureHashOf(b []byte) string { return user.PictureHash(b) }

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return t.Local().Format("2 Jan 2006")
}

// DeviceID is this computer's ID for guild sessions. It is kept
// with the app's settings, not in the user folder, so the same user folder
// on two computers is seen as two computers.
func DeviceID(configDir string) (string, error) {
	path := filepath.Join(configDir, "device.json")
	var d struct {
		ID string `json:"device_id"`
	}
	if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &d) == nil && len(d.ID) == 32 {
		return d.ID, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	d.ID = hex.EncodeToString(b)
	data, _ := json.Marshal(d)
	os.MkdirAll(configDir, 0o700)
	return d.ID, os.WriteFile(path, data, 0o600)
}

// ID is the guild's ID (its public key).
func (gc *Conn) ID() string { return gc.id }

// refreshRoles loads the guild's roles and what this user may do.
func (gc *Conn) refreshRoles(s *proto.Session) {
	r, err := s.Call(proto.Request{Type: "roles"}, callTimeout)
	if err != nil || !r.OK {
		return
	}
	gc.mu.Lock()
	gc.roles, gc.myPerms = r.Roles, r.MyPerms
	gc.mu.Unlock()
	gc.m.onChange()
}

// SetStatus tells every connected guild your new status.
func (m *Manager) SetStatus(status string) {
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.conns))
	for _, gc := range m.conns {
		conns = append(conns, gc)
	}
	m.mu.Unlock()
	for _, gc := range conns {
		gc.mu.Lock()
		s := gc.sess
		gc.mu.Unlock()
		if s != nil {
			go s.Call(proto.Request{Type: "set_status", Status: status}, callTimeout)
		}
	}
}

// ---- other addresses ---------------------------------------------------

// dialOthers tries the guild's other addresses. One that answers as this
// guild becomes the usual address (the old one is kept as another).
func (gc *Conn) dialOthers() *proto.Conn {
	gc.mu.Lock()
	others, usual := append([]string(nil), gc.others...), gc.addr
	gc.mu.Unlock()
	for _, a := range others {
		if a == usual {
			continue
		}
		c, err := proto.Dial(a, dialTimeout)
		if err != nil {
			continue
		}
		if KeyText(c.GuildKey) != gc.id { // Someone else answers at that address now.
			c.Close()
			continue
		}
		gc.mu.Lock()
		gc.addr = a
		gc.mu.Unlock()
		gc.m.u.UpdateGuild(gc.id, func(g *user.Guild) {
			g.GuildAddress = a
			g.OtherAddresses = mergeAddresses(g.OtherAddresses, []string{usual}, a)
		})
		return c
	}
	return nil
}

// learnAddresses keeps the addresses a guild says it answers on.
func (gc *Conn) learnAddresses(list []string) {
	gc.mu.Lock()
	usual := gc.addr
	gc.mu.Unlock()
	var clean []string
	for _, a := range list {
		if _, err := CheckAddress(a); err == nil {
			clean = append(clean, a)
		}
	}
	if len(clean) == 0 {
		return
	}
	merged := mergeAddresses(nil, clean, usual)
	gc.mu.Lock()
	gc.others = merged
	gc.mu.Unlock()
	gc.m.u.UpdateGuild(gc.id, func(g *user.Guild) { g.OtherAddresses = merged })
}

// mergeAddresses joins two lists, leaving out repeats and skip: newest
// first, at most 8.
func mergeAddresses(old, add []string, skip string) []string {
	var out []string
	seen := map[string]bool{skip: true}
	for _, a := range append(append([]string{}, add...), old...) {
		if !seen[a] && len(out) < 8 {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}
