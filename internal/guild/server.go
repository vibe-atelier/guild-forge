// Package guild is a running guild: it serves members' apps over the
// encrypted Nexus protocol (pairing, signing in, settings, ownership) and
// answers GuildForge commands on a private local control channel.
package guild

// This file is the guild process: what runs in the background for a started guild.
// It listens on the guild's port for members' apps, and on a private local
// "control" port for GuildForge commands.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/portmap"
	"nexus/internal/proto"
)

// These are the broadcast and OTP rules, and connection limits.
const (
	BroadcastDuration = time.Hour
	OTPLifetime       = 5 * time.Minute
	MaxOTPFailures    = 5
	OTPLength         = 9
	otpAlphabet       = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	maxConnections    = 64
	idleTimeout       = 2 * time.Minute  // This applies before signing in.
	sessionTimeout    = 75 * time.Second // Signed-in apps ping every 20 seconds.
)

// Server is a running guild.
type Server struct {
	ID      string
	DataDir string
	Log     *log.Logger
	now     func() time.Time // It can be replaced in tests.

	key ed25519.PrivateKey

	mu       sync.Mutex
	owner    *guildstore.Owner
	profile  guildstore.GuildProfile
	picture  []byte
	members  map[string]guildstore.Member
	sessions map[string]*memberSession // It maps each user key to the one active computer.
	transfer *proto.TransferInfo

	// These are for joining and membership.
	join     guildstore.JoinSettings
	bans     []guildstore.Ban
	requests []guildstore.JoinRequest
	captcha  guildstore.Captcha
	attempts map[string][]time.Time // It counts failed CAPTCHAs by "u:<user>" and "ip:<address>".
	waiting  map[string]*proto.Conn // It holds the people waiting for approval, by user key.

	// These hold channels, the key ledger and messages.
	chat chatState

	// These are for roles.
	roles []proto.Role

	// These are private messages waiting to be collected, by recipient.
	dms map[string][]proto.DMEnvelope

	// These hold the Calendar, Threads, Wiki, pictures and emoji.
	pages    pageState
	currency currencyState          // This is the currency.
	voice    map[string]*voiceState // It records who is in a voice channel, by member.

	// These hold the locked voice channels and the soundboard.
	locked     map[string]bool
	sounds     []proto.Sound
	lastSound  map[string]time.Time
	tg         telegramState // It holds pins, scheduled messages and Slow Mode.
	clockOnce  sync.Once
	public     *portmap.Keeper // It keeps the router and the addresses.
	publicPort int

	bcActive   bool
	bcUntil    time.Time
	failures   int
	otp        string
	otpExpires time.Time

	stopOnce sync.Once
	stopped  chan struct{}
}

// memberSession is a signed-in app (one computer per user).
type memberSession struct {
	user   string
	device string
	conn   *proto.Conn
	status string // It is Online, Idle, DND or Invisible.
}

// NewServer prepares a guild from its folder.
func NewServer(dataDir string, logger *log.Logger) (*Server, error) {
	gd, err := guildstore.LoadGuildData(dataDir)
	if err != nil {
		return nil, err
	}
	key, err := gd.SigningKey()
	if err != nil {
		return nil, err
	}
	owner, err := guildstore.LoadOwner(dataDir)
	if err != nil {
		return nil, err
	}
	profile, picture, err := guildstore.LoadProfile(dataDir)
	if err != nil {
		return nil, err
	}
	list, err := guildstore.LoadMembers(dataDir)
	if err != nil {
		return nil, err
	}
	join, err := guildstore.LoadJoinSettings(dataDir)
	if err != nil {
		return nil, err
	}
	bans, err := guildstore.LoadBans(dataDir)
	if err != nil {
		return nil, err
	}
	requests, err := guildstore.LoadRequests(dataDir)
	if err != nil {
		return nil, err
	}
	captcha, err := guildstore.LoadCaptcha(dataDir)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	g := &Server{
		ID: gd.PublicKey, DataDir: dataDir, Log: logger, now: time.Now,
		key: key, owner: owner, profile: profile, picture: picture,
		members: map[string]guildstore.Member{}, sessions: map[string]*memberSession{},
		join: join, bans: bans, requests: requests, captcha: captcha,
		attempts: map[string][]time.Time{}, waiting: map[string]*proto.Conn{},
		stopped: make(chan struct{}),
	}
	for _, m := range list {
		g.members[m.UserKey] = m
	}
	if err := g.loadChat(); err != nil {
		return nil, err
	}
	if err := g.loadRoles(); err != nil {
		return nil, err
	}
	if err := g.loadDMs(); err != nil {
		return nil, err
	}
	if err := g.loadPages(); err != nil {
		return nil, err
	}
	g.migrateProfiles()
	g.migrateVoiceMod()
	g.loadVoice()
	if err := g.loadTelegram(); err != nil {
		return nil, err
	}
	if err := g.loadSounds(); err != nil {
		return nil, err
	}
	if err := g.loadCurrency(); err != nil {
		return nil, err
	}
	// Guilds paired before member lists existed: the owner is a member.
	if owner != nil {
		if _, ok := g.members[owner.Key]; !ok {
			g.members[owner.Key] = guildstore.Member{UserKey: owner.Key, Joined: owner.PairedAt}
			g.saveMembersLocked()
		}
	}
	return g, nil
}

func (g *Server) saveMembersLocked() {
	list := make([]guildstore.Member, 0, len(g.members))
	for _, m := range g.members {
		list = append(list, m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Joined.Before(list[j].Joined) })
	if err := guildstore.SaveMembers(g.DataDir, list); err != nil {
		g.Log.Printf("could not save the member list: %v", err)
	}
}

// ---- broadcast and OTP ----------------------------------------------------

// broadcasting reports whether a broadcast is on (expiring it if its hour
// is up). Must be called with g.mu held.
func (g *Server) broadcasting() bool {
	if g.bcActive && !g.now().Before(g.bcUntil) {
		g.endBroadcastLocked("the hour is up")
	}
	return g.bcActive
}

func (g *Server) endBroadcastLocked(reason string) {
	if g.bcActive {
		g.Log.Printf("broadcast ended: %s", reason)
	}
	g.bcActive = false
	g.otp = ""
	g.failures = 0
}

// StartBroadcast opens pairing for one hour. Only a guild without an
// owner can broadcast.
func (g *Server) StartBroadcast() (until time.Time, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.owner != nil {
		return time.Time{}, errors.New("this guild already has an owner")
	}
	g.bcActive = true
	g.bcUntil = g.now().Add(BroadcastDuration)
	g.failures = 0
	g.otp = ""
	g.Log.Printf("broadcast started, until %s", g.bcUntil.Format(time.RFC3339))
	return g.bcUntil, nil
}

// EndBroadcast stops a broadcast early.
func (g *Server) EndBroadcast() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.endBroadcastLocked("stopped by the host")
}

// NewOTP makes a new one-time password, replacing any earlier one.
func (g *Server) NewOTP() (otp string, expires time.Time, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.broadcasting() {
		return "", time.Time{}, errors.New("this guild is not broadcasting")
	}
	otp, err = randomOTP()
	if err != nil {
		return "", time.Time{}, err
	}
	g.otp = otp
	g.otpExpires = g.now().Add(OTPLifetime)
	if g.otpExpires.After(g.bcUntil) {
		g.otpExpires = g.bcUntil
	}
	g.Log.Printf("new OTP issued, valid until %s", g.otpExpires.Format(time.RFC3339))
	return otp, g.otpExpires, nil
}

func randomOTP() (string, error) {
	b := make([]byte, OTPLength)
	max := big.NewInt(int64(len(otpAlphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = otpAlphabet[n.Int64()]
	}
	return string(b), nil
}

// NormalizeOTP makes typed OTPs comparable: capitals, no dashes or spaces.
func NormalizeOTP(s string) string {
	s = strings.ToUpper(s)
	return strings.NewReplacer("-", "", " ", "").Replace(s)
}

// FormatOTP shows an OTP in groups of three, e.g. K7Q-4MZ-9TB.
func FormatOTP(otp string) string {
	var parts []string
	for i := 0; i < len(otp); i += 3 {
		end := min(i+3, len(otp))
		parts = append(parts, otp[i:end])
	}
	return strings.Join(parts, "-")
}

// tryPair checks a pairing attempt.
func (g *Server) tryPair(otp string, userKey ed25519.PublicKey, from string) proto.Response {
	g.mu.Lock()
	defer g.mu.Unlock()
	fail := func(msg string) proto.Response {
		return proto.Response{Type: "pair_result", Error: msg}
	}
	if g.owner != nil {
		return fail("this guild already has an owner")
	}
	if !g.broadcasting() {
		return fail("this guild is not broadcasting; ask the host to start a broadcast")
	}
	valid := g.otp != "" && g.now().Before(g.otpExpires)
	given := NormalizeOTP(otp)
	if valid && len(given) == len(g.otp) && subtle.ConstantTimeCompare([]byte(given), []byte(g.otp)) == 1 {
		now := g.now().UTC().Truncate(time.Second)
		owner := guildstore.Owner{Key: guildstore.KeyText(userKey), PairedAt: now}
		if err := guildstore.SaveOwner(g.DataDir, owner); err != nil {
			g.Log.Printf("could not save the owner: %v", err)
			return fail("the guild could not save its owner; try again")
		}
		g.owner = &owner
		if _, ok := g.members[owner.Key]; !ok {
			g.members[owner.Key] = guildstore.Member{UserKey: owner.Key, Joined: now}
			g.saveMembersLocked()
		}
		g.Log.Printf("paired with owner %s from %s", owner.Key, from)
		g.endBroadcastLocked("owner paired")
		return proto.Response{Type: "pair_result", OK: true}
	}

	g.failures++
	left := MaxOTPFailures - g.failures
	g.Log.Printf("wrong OTP from %s (%d tries left)", from, left)
	if left <= 0 {
		g.endBroadcastLocked("too many wrong OTPs")
		return fail("wrong OTP; too many wrong tries, so the broadcast has ended")
	}
	msg := "wrong OTP"
	if !valid {
		msg = "no valid OTP right now (it may have expired); ask the host for a new one"
	}
	r := fail(msg)
	r.TriesLeft = left
	return r
}

// ---- status for GuildForge commands ----------------------------------------

// Status describes the guild for GuildForge commands.
func (g *Server) Status() ControlResponse {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := ControlResponse{OK: true, Broadcasting: g.broadcasting()}
	if r.Broadcasting {
		r.BroadcastEnds = g.bcUntil
	}
	if g.owner != nil {
		r.HasOwner = true
		r.OwnerKey = g.owner.Key
	}
	r.Members, r.Online = len(g.members), len(g.sessions)
	if g.public != nil {
		st := g.public.Status()
		r.Public = &st
	}
	return r
}

// SetPublic gives the guild what keeps its port open, so it can report
// its addresses.
func (g *Server) SetPublic(k *portmap.Keeper, port int) {
	g.mu.Lock()
	g.public, g.publicPort = k, port
	g.mu.Unlock()
}

// addressesLocked are the addresses members' apps can try.
func (g *Server) addressesLocked() []string {
	if g.public == nil {
		return nil
	}
	return g.public.Status().Addresses(g.publicPort)
}

// ResetOwner removes the owner. Members and messages stay.
func (g *Server) ResetOwner() error {
	g.mu.Lock()
	if err := guildstore.RemoveOwner(g.DataDir); err != nil {
		g.mu.Unlock()
		return err
	}
	if g.owner != nil {
		g.Log.Printf("owner %s removed by the host (reset_owner)", g.owner.Key)
	}
	g.owner = nil
	g.transfer = nil
	conns := g.allConnsLocked()
	g.mu.Unlock()
	g.notify(conns, "owner_changed")
	return nil
}

// ---- events -----------------------------------------------------------------

func (g *Server) allConnsLocked() []*proto.Conn {
	list := make([]*proto.Conn, 0, len(g.sessions))
	for _, s := range g.sessions {
		list = append(list, s.conn)
	}
	return list
}

func (g *Server) connsOfLocked(users ...string) []*proto.Conn {
	var list []*proto.Conn
	for _, u := range users {
		if s := g.sessions[u]; s != nil {
			list = append(list, s.conn)
		}
	}
	return list
}

// notify sends an event to the given connections (never while holding g.mu).
func (g *Server) notify(conns []*proto.Conn, event string) {
	for _, c := range conns {
		c.Send(proto.Response{Type: "event", OK: true, Event: event})
	}
}

// ---- serving members' apps ------------------------------------------------------

// Serve answers apps on l until Stop is called.
func (g *Server) Serve(l net.Listener) {
	// The clock posts scheduled messages and removes disappearing ones.
	g.clockOnce.Do(func() { go g.clock() })
	slots := make(chan struct{}, maxConnections)
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-g.stopped:
				return
			default:
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			g.Log.Printf("accept failed: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				g.handle(conn)
			}()
		default:
			conn.Close() // The guild is too busy.
		}
	}
}

func (g *Server) handle(raw net.Conn) {
	defer raw.Close()
	from := raw.RemoteAddr().String()
	c, err := proto.ServerHandshake(raw, g.key)
	if err != nil {
		g.Log.Printf("handshake with %s failed: %v", from, err)
		return
	}
	var me *memberSession // It is set once the app has signed in.
	var js *joinState     // It is a join in progress (the CAPTCHA has been asked).
	defer func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if me != nil && g.sessions[me.user] == me {
			delete(g.sessions, me.user)
			g.voiceLeaveLocked(me.user)
			g.notifyLater(g.allConnsLocked(), "presence")
		}
		for u, wc := range g.waiting {
			if wc == c {
				delete(g.waiting, u)
			}
		}
	}()

	for {
		timeout := idleTimeout
		if me != nil {
			timeout = sessionTimeout
		}
		c.SetDeadline(time.Now().Add(timeout))
		var req proto.Request
		if err := c.Recv(&req); err != nil {
			return
		}
		// These come many times a second: they are passed on and never answered.
		if req.Type == "voice" || req.Type == "stream" {
			if me != nil && req.Type == "voice" {
				g.relayVoice(me.user, req.Voice)
			} else if me != nil {
				g.relayStream(me.user, req.Stream)
			}
			continue
		}
		resp, after := g.answer(c, req, from, &me, &js)
		resp.ID = req.ID
		if err := c.Send(resp); err != nil {
			return
		}
		if after != nil && after() {
			return
		}
	}
}

// answer handles one request. after (if not nil) runs once the answer has
// been sent; it returns true to close the connection.
func (g *Server) answer(c *proto.Conn, req proto.Request, from string, me **memberSession, js **joinState) (resp proto.Response, after func() bool) {
	fail := func(code, msg string) proto.Response {
		return proto.Response{Type: req.Type, Code: code, Error: msg}
	}
	switch req.Type {
	case "info":
		s := g.Status()
		return proto.Response{Type: "info", OK: true, HasOwner: s.HasOwner, Broadcasting: s.Broadcasting}, nil

	case "about": // This is public.
		return proto.Response{Type: "about", OK: true, Guild: g.about(req.PictureHash)}, nil

	case "pair":
		if !validUserSig(req.UserKey, proto.PairProof(c.SessionID), req.Sig) {
			return fail("bad_signature", "your key's signature is wrong"), nil
		}
		return g.tryPair(req.OTP, ed25519.PublicKey(req.UserKey), from), nil

	case "auth":
		return g.auth(c, req, from, me)

	case "join":
		return g.joinRequest(c, req, from, js), nil
	case "join_answers":
		return g.joinAnswers(c, req, js), nil
	case "rules":
		user := ""
		if *me != nil {
			user = (*me).user
		}
		return g.rulesFor(user), nil
	}

	// Everything else needs a signed-in member.
	if *me == nil {
		return fail("auth_required", "sign in first"), nil
	}
	user := (*me).user
	switch req.Type {
	case "ping":
		return proto.Response{Type: "ping", OK: true}, nil
	case "members":
		return proto.Response{Type: "members", OK: true, Members: g.memberList(user)}, nil
	case "update_guild":
		if err := g.updateGuild(user, req); err != nil {
			return fail("refused", err.Error()), nil
		}
		return proto.Response{Type: req.Type, OK: true}, nil
	case "leave":
		g.leave(user)
		return proto.Response{Type: "leave", OK: true}, func() bool { return true }
	case "transfer_offer", "transfer_cancel", "transfer_accept", "transfer_decline":
		if err := g.transferStep(user, req); err != nil {
			return fail("refused", err.Error()), nil
		}
		return proto.Response{Type: req.Type, OK: true}, nil
	}
	if r, ok := g.ownerAction(user, req); ok {
		return r, nil
	}
	if r, ok := g.chatRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.rolesRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.dmRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.pagesRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.profileRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.currencyRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.telegramRequest(user, req); ok {
		return r, nil
	}
	if r, ok := g.voiceRequest(user, req); ok {
		return r, nil
	}
	return fail("unknown", "unknown request "+strconv.Quote(req.Type)), nil
}

func validUserSig(key, msg, sig []byte) bool {
	return len(key) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(key), msg, sig)
}

// auth signs a member in on this connection.
func (g *Server) auth(c *proto.Conn, req proto.Request, from string, me **memberSession) (proto.Response, func() bool) {
	fail := func(code, msg string) (proto.Response, func() bool) {
		return proto.Response{Type: "auth", Code: code, Error: msg}, nil
	}
	if req.DeviceID == "" || len(req.DeviceID) > 64 {
		return fail("bad_request", "missing computer ID")
	}
	if !validUserSig(req.UserKey, proto.AuthProof(c.SessionID, req.DeviceID), req.Sig) {
		return fail("bad_signature", "your key's signature is wrong")
	}
	user := guildstore.KeyText(req.UserKey)

	g.mu.Lock()
	ip := ipOf(from)
	if g.isBannedLocked(user, ip) {
		g.mu.Unlock()
		return fail("banned", "you are banned from this guild")
	}
	m, ok := g.members[user]
	if !ok {
		if g.requestIndexLocked(user) >= 0 {
			g.waiting[user] = c // The app is told when the owner decides.
			g.mu.Unlock()
			return fail("pending", "your request is waiting for the owner's approval")
		}
		g.mu.Unlock()
		return fail("not_member", "you are not a member of this guild")
	}
	newEnc := len(req.EncKey) == 32 && string(req.EncKey) != string(m.EncKey)
	typing := cleanTyping(req.TypingText)
	if typing != m.Typing {
		m.Typing = typing
		newEnc = true // This makes sure it is saved below.
	}
	// Account facts for staff: the guild count as the app says now,
	// and the creation date if it wasn't known (members from before).
	if req.ServerCount > 0 && req.ServerCount != m.Guilds {
		m.Guilds = req.ServerCount
		newEnc = true
	}
	if created, err := time.Parse(time.RFC3339, req.Created); err == nil && m.Created.IsZero() {
		m.Created = created.UTC()
		newEnc = true
	}
	if name := cleanName(req.UserName); (name != "" && name != m.Name) || ip != m.LastIP || newEnc {
		if name != "" {
			m.Name = name
		}
		if len(req.EncKey) == 32 {
			m.EncKey = append([]byte{}, req.EncKey...)
		}
		m.LastIP = ip
		g.members[user] = m
		g.saveMembersLocked()
	}
	var kick *proto.Conn
	if old := g.sessions[user]; old != nil && old.conn != c {
		if old.device != req.DeviceID && !req.TakeOver {
			g.mu.Unlock()
			return fail("elsewhere", "this account is active in this guild on another computer")
		}
		kick = old.conn
	}
	s := &memberSession{user: user, device: req.DeviceID, conn: c, status: validStatus(req.Status)}
	g.sessions[user] = s
	g.notifyLater(g.allConnsLocked(), "presence")
	g.requestKeysLocked() // Hand keys to them, or ask them to hand keys on.
	*me = s
	resp := proto.Response{Type: "auth", OK: true, IsOwner: g.owner != nil && g.owner.Key == user, Addresses: g.addressesLocked()}
	if t := g.transfer; t != nil && (t.From == user || t.To == user) {
		cp := *t
		resp.Transfer = &cp
	}
	g.mu.Unlock()

	if kick != nil {
		if req.TakeOver {
			kick.Send(proto.Response{Type: "event", OK: true, Event: "taken_over"})
			g.Log.Printf("%s moved to another computer", user)
		}
		kick.Close()
	}
	return resp, nil
}

func (g *Server) about(clientHash string) *proto.GuildInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	info := &proto.GuildInfo{
		ID: g.ID, Name: g.profile.Name, About: g.profile.About,
		MemberCount: len(g.members), ShowMemberCount: g.profile.ShowMemberCount,
		PictureHash: g.profile.PictureHash, Join: g.joinInfoLocked(), Pages: &g.pages.settings, DefaultNotify: g.profile.DefaultNotify, Currency: g.publicCurrencyLocked(),
	}
	if !info.ShowMemberCount {
		info.MemberCount = -1
	}
	if g.owner != nil {
		info.OwnerKey = g.owner.Key
	}
	if g.profile.PictureHash != "" && clientHash != g.profile.PictureHash {
		info.Picture = g.picture
	}
	return info
}

// memberList is the member list as viewer sees it: staff also see each
// account's creation date and guild count.
func (g *Server) memberList(viewer string) []proto.MemberInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	staff := g.isStaffLocked(viewer)
	list := make([]proto.MemberInfo, 0, len(g.members))
	for _, m := range g.members {
		list = append(list, proto.MemberInfo{
			UserKey: m.UserKey, Name: m.Name, Joined: m.Joined.UTC().Format(time.RFC3339), EncKey: m.EncKey, Typing: m.Typing, Roles: m.Roles, AgreedRules: m.AgreedRules,
			Nick: m.Nick, Bio: m.Bio, PictureHash: m.PictureHash, BannerHash: m.BannerHash,
			TimeoutUntil: g.timeoutTextLocked(m),
			IsOwner:      g.owner != nil && g.owner.Key == m.UserKey,
			Online:       g.sessions[m.UserKey] != nil && g.sessions[m.UserKey].status != "Invisible",
		})
		if s := g.sessions[m.UserKey]; s != nil && s.status != "Invisible" {
			list[len(list)-1].Status = s.status
		}
		if staff {
			list[len(list)-1].Created, list[len(list)-1].Guilds = timeText(m.Created), m.Guilds
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Joined < list[j].Joined })
	return list
}

// updateGuild changes the About information (Manage Guild).
func (g *Server) updateGuild(user string, req proto.Request) error {
	g.mu.Lock()
	if err := g.needLocked(user, proto.PermManageGuild); err != nil {
		g.mu.Unlock()
		return err
	}
	p := g.profile
	pic := g.picture
	if req.Name != nil {
		name, err := guildstore.CheckGuildName(*req.Name)
		if err != nil {
			g.mu.Unlock()
			return err
		}
		p.Name = name
	}
	if req.About != nil {
		about := strings.TrimRight(*req.About, " \r\n\t")
		if utf8.RuneCountInString(about) > guildstore.MaxAbout {
			g.mu.Unlock()
			return fmt.Errorf("the About text can be at most %d characters", guildstore.MaxAbout)
		}
		p.About = about
	}
	if req.ShowMemberCount != nil {
		p.ShowMemberCount = *req.ShowMemberCount
	}
	if req.DefaultNotify != nil {
		switch *req.DefaultNotify {
		case "", proto.NotifyAll, proto.NotifyMentions:
			p.DefaultNotify = *req.DefaultNotify
		default:
			g.mu.Unlock()
			return errors.New("the default must be All Messages or Mentions Only")
		}
	}
	if req.ClearPicture {
		pic = nil
	} else if len(req.Picture) > 0 {
		if err := guildstore.CheckPicture(req.Picture); err != nil {
			g.mu.Unlock()
			return err
		}
		pic = req.Picture
	}
	p.PictureHash = guildstore.PictureHash(pic)
	if p.PictureHash != g.profile.PictureHash {
		if err := guildstore.SavePicture(g.DataDir, pic); err != nil {
			g.mu.Unlock()
			return errors.New("the guild could not save the picture")
		}
	}
	if err := guildstore.SaveProfile(g.DataDir, p); err != nil {
		g.mu.Unlock()
		return errors.New("the guild could not save its settings")
	}
	g.profile, g.picture = p, pic
	conns := g.allConnsLocked()
	g.mu.Unlock()
	g.Log.Printf("guild settings changed by the owner")
	g.notify(conns, "guild_updated")
	return nil
}

// leave removes a member. If the owner leaves, the guild has no owner until
// it is broadcast and paired again.
func (g *Server) leave(user string) {
	g.mu.Lock()
	delete(g.members, user)
	g.voiceLeaveLocked(user)
	g.saveMembersLocked()
	delete(g.sessions, user)
	event := "guild_updated"
	if g.owner != nil && g.owner.Key == user {
		guildstore.RemoveOwner(g.DataDir)
		g.owner = nil
		event = "owner_changed"
		g.Log.Printf("the owner left; the guild has no owner")
	} else {
		g.Log.Printf("member %s left", user)
	}
	g.retireKeysLocked() // The channel keys are replaced.
	if g.transfer != nil && (g.transfer.From == user || g.transfer.To == user) {
		g.transfer = nil
	}
	conns := g.allConnsLocked()
	g.mu.Unlock()
	g.notify(conns, event)
}

// transferStep handles the steps of an ownership transfer.
func (g *Server) transferStep(user string, req proto.Request) error {
	g.mu.Lock()
	isOwner := g.owner != nil && g.owner.Key == user
	t := g.transfer
	var event = "transfer_changed"
	var tell []string
	switch req.Type {
	case "transfer_offer":
		if !isOwner {
			g.mu.Unlock()
			return errors.New("only the owner can transfer ownership")
		}
		if _, ok := g.members[req.Target]; !ok || req.Target == user {
			g.mu.Unlock()
			return errors.New("choose another member of this guild")
		}
		if t != nil {
			tell = append(tell, t.To)
		}
		g.transfer = &proto.TransferInfo{From: user, To: req.Target}
		tell = append(tell, user, req.Target)
		g.Log.Printf("ownership offered to %s", req.Target)
	case "transfer_cancel":
		if !isOwner || t == nil {
			g.mu.Unlock()
			return errors.New("there is no transfer to cancel")
		}
		g.transfer = nil
		tell = []string{t.From, t.To}
	case "transfer_decline":
		if t == nil || t.To != user {
			g.mu.Unlock()
			return errors.New("there is no offer to decline")
		}
		g.transfer = nil
		tell = []string{t.From, t.To}
	case "transfer_accept":
		if t == nil || t.To != user || g.owner == nil || g.owner.Key != t.From {
			g.mu.Unlock()
			return errors.New("there is no offer to accept")
		}
		owner := guildstore.Owner{Key: user, PairedAt: g.now().UTC().Truncate(time.Second)}
		if err := guildstore.SaveOwner(g.DataDir, owner); err != nil {
			g.mu.Unlock()
			return errors.New("the guild could not save the new owner")
		}
		g.owner = &owner
		g.transfer = nil
		event = "owner_changed"
		g.Log.Printf("ownership transferred to %s", user)
	}
	var conns []*proto.Conn
	if event == "owner_changed" {
		conns = g.allConnsLocked()
	} else {
		conns = g.connsOfLocked(tell...)
	}
	g.mu.Unlock()
	g.notify(conns, event)
	return nil
}

// Stop makes Serve and the control loop return.
func (g *Server) Stop() {
	g.stopOnce.Do(func() { close(g.stopped) })
}

// Done is closed once Stop has been called.
func (g *Server) Done() <-chan struct{} { return g.stopped }

// HoldStill runs f while the guild changes nothing, so its files can be
// copied whole (Automatic Backups).
func (g *Server) HoldStill(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f()
}

// isStaffLocked reports whether user is someone who decides who may join or stay (approves joins,
// kicks, bans or times out), so they see account facts.
func (g *Server) isStaffLocked(user string) bool {
	for _, p := range []string{proto.PermApproveJoins, proto.PermKick, proto.PermBan, proto.PermTimeout} {
		if g.needLocked(user, p) == nil {
			return true
		}
	}
	return false
}

// timeText is a time as RFC 3339, or "" when unknown.
func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
