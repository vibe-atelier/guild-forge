package guild

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// joinAs connects as u and sends a join request with the given extras.
func joinAs(t *testing.T, addr string, u testUser, extra proto.Request) (*proto.Session, proto.Response) {
	t.Helper()
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s := proto.NewSession(c)
	t.Cleanup(func() { s.Close() })
	req := extra
	req.Type = "join"
	req.UserKey = u.key.Public().(ed25519.PublicKey)
	req.Sig = ed25519.Sign(u.key, proto.JoinProof(s.SessionID()))
	if req.UserName == "" {
		req.UserName = "Tester"
	}
	if req.Created == "" {
		req.Created = time.Now().Add(-400 * 24 * time.Hour).UTC().Format(time.RFC3339)
	}
	r, err := s.Call(req, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}

func setJoin(t *testing.T, so *proto.Session, j proto.JoinSettings) {
	t.Helper()
	if j.JoinPlacement == "" {
		j.JoinPlacement = guildstore.JoinOnAbout
	}
	r, _ := so.Call(proto.Request{Type: "update_join", Join: &j}, time.Second)
	if !r.OK {
		t.Fatalf("update_join: %+v", r)
	}
}

func TestOpenJoinAndReceipt(t *testing.T) {
	g, addr, _ := pairedGuild(t)
	alice := newTestUser()
	_, r := joinAs(t, addr, alice, proto.Request{UserName: "  Alice   Smith "})
	if !r.OK || r.Code != "joined" || r.Receipt == nil {
		t.Fatalf("join: %+v", r)
	}
	key := guildstore.KeyText(alice.key.Public().(ed25519.PublicKey))
	rc := r.Receipt
	gk := g.key.Public().(ed25519.PublicKey)
	if rc.UserKey != key || rc.GuildID != g.ID || !ed25519.Verify(gk, proto.ReceiptMessage(rc.GuildID, rc.UserKey, rc.Joined), rc.Sig) {
		t.Fatal("the joining receipt must be signed by the guild")
	}
	if g.members[key].Name != "Alice Smith" {
		t.Fatalf("name: %q", g.members[key].Name)
	}
	// Now a member, they can sign in, and joining again says so.
	if _, r := signedIn(t, addr, alice, "pc-a", false); !r.OK || r.IsOwner {
		t.Fatalf("auth after joining: %+v", r)
	}
	if _, r := joinAs(t, addr, alice, proto.Request{}); r.Code != "already_member" {
		t.Fatalf("second join: %+v", r)
	}
	// The member list shows names.
	s, _ := signedIn(t, addr, alice, "pc-a", false)
	m, _ := s.Call(proto.Request{Type: "members"}, time.Second)
	if len(m.Members) != 2 {
		t.Fatalf("members: %+v", m.Members)
	}
}

func TestJoinRulesInOrder(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	so, _ := signedIn(t, addr, owner, "pc-o", false)
	u := newTestUser()
	ukey := guildstore.KeyText(u.key.Public().(ed25519.PublicKey))

	pw := "secret-pass"
	setJoin(t, so, proto.JoinSettings{ShowJoinButton: true, Whitelist: true, Allowed: []string{strings.ToLower(ukey)},
		MinAccountDays: 30, MinServerCount: 1, Password: &pw,
		RulesEnabled: true, Rules: "Be kind.", RequireRules: true, RulesPublic: true, JoinPlacement: guildstore.JoinOnRules})

	stranger := newTestUser()
	if _, r := joinAs(t, addr, stranger, proto.Request{}); r.Code != "not_invited" {
		t.Fatalf("whitelist: %+v", r)
	}
	young := time.Now().Add(-2 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if _, r := joinAs(t, addr, u, proto.Request{Created: young}); r.Code != "too_new" || !strings.Contains(r.Error, "30 days") {
		t.Fatalf("account age: %+v", r)
	}
	if _, r := joinAs(t, addr, u, proto.Request{ServerCount: 0}); r.Code != "too_few_guilds" {
		t.Fatalf("server count: %+v", r)
	}
	if _, r := joinAs(t, addr, u, proto.Request{ServerCount: 1, JoinPassword: "nope"}); r.Code != "wrong_password" {
		t.Fatalf("password: %+v", r)
	}
	if _, r := joinAs(t, addr, u, proto.Request{ServerCount: 1, JoinPassword: pw}); r.Code != "rules_required" {
		t.Fatalf("rules: %+v", r)
	}
	if _, r := joinAs(t, addr, u, proto.Request{ServerCount: 1, JoinPassword: pw, AgreedRules: true}); !r.OK {
		t.Fatalf("all rules met: %+v", r)
	}

	// Rules are readable before joining when public.
	c, _ := proto.Dial(addr, time.Second)
	s := proto.NewSession(c)
	defer s.Close()
	if r, _ := s.Call(proto.Request{Type: "rules"}, time.Second); r.Rules != "Be kind." {
		t.Fatalf("public rules: %+v", r)
	}
	if r, _ := s.Call(proto.Request{Type: "about"}, time.Second); r.Guild.Join == nil || !r.Guild.Join.NeedsPassword || r.Guild.Join.JoinPlacement != guildstore.JoinOnRules {
		t.Fatalf("about join info: %+v", r.Guild.Join)
	}
	// A member cannot change join settings.
	su, _ := signedIn(t, addr, u, "pc-u", false)
	j := proto.JoinSettings{JoinPlacement: guildstore.JoinOnAbout}
	if r, _ := su.Call(proto.Request{Type: "update_join", Join: &j}, time.Second); r.OK {
		t.Fatal("only the owner may change join settings")
	}
}

// solve answers a quiz correctly, using the bank built in TestCaptcha.
func solve(q proto.Quiz) []proto.QuizAnswer {
	var out []proto.QuizAnswer
	for _, qq := range q.Questions {
		switch qq.Type {
		case guildstore.QText:
			out = append(out, proto.QuizAnswer{Text: "Grey"})
		default:
			var a proto.QuizAnswer
			for i, o := range qq.Options {
				if strings.HasPrefix(o, "yes") {
					a.Choices = append(a.Choices, i)
				}
			}
			out = append(out, a)
		}
	}
	return out
}

func TestCaptcha(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	so, _ := signedIn(t, addr, owner, "pc-o", false)
	put := func(q proto.CaptchaQuestion, img []byte) {
		t.Helper()
		r, _ := so.Call(proto.Request{Type: "captcha_put", Question: &q, Image: img}, time.Second)
		if !r.OK {
			t.Fatalf("captcha_put: %+v", r)
		}
	}
	// Turning it on with no questions is refused.
	if r, _ := so.Call(proto.Request{Type: "captcha_settings", Captcha: &proto.CaptchaSettings{Enabled: true, AskCount: 3, PassPercent: 100}}, time.Second); r.OK {
		t.Fatal("an empty CAPTCHA must not be enabled")
	}
	put(proto.CaptchaQuestion{Type: guildstore.QText, Text: "What colour is grey?", Answers: []string{"grey", "gray"}}, tinyPNG(t, 40, 40))
	put(proto.CaptchaQuestion{Type: guildstore.QRadio, Text: "Pick yes", Options: []string{"no", "yes!", "nope"}, Correct: []int{1}}, nil)
	put(proto.CaptchaQuestion{Type: guildstore.QCheckbox, Text: "Pick all yes", Options: []string{"yes 1", "no", "yes 2", "nah"}, Correct: []int{0, 2}}, nil)
	put(proto.CaptchaQuestion{Type: guildstore.QDropdown, Text: "Pick yes", Options: []string{"maybe", "yes"}, Correct: []int{1}}, nil)
	bad := proto.CaptchaQuestion{Type: guildstore.QRadio, Text: "x", Options: []string{"a", "b"}, Correct: []int{0, 1}}
	if r, _ := so.Call(proto.Request{Type: "captcha_put", Question: &bad}, time.Second); r.OK {
		t.Fatal("a radio question with two correct choices must be refused")
	}
	set := proto.CaptchaSettings{Enabled: true, AskCount: 4, PassPercent: 75, Welcome: "Hi!", PassMessage: "Welcome in", FailMessage: "Nope"}
	if r, _ := so.Call(proto.Request{Type: "captcha_settings", Captcha: &set}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}

	// The text question is case-sensitive? No: "Grey" must still match "grey".
	u := newTestUser()
	s, r := joinAs(t, addr, u, proto.Request{})
	if r.Code != "captcha" || r.Quiz == nil || len(r.Quiz.Questions) != 4 || r.Quiz.Welcome != "Hi!" {
		t.Fatalf("quiz: %+v", r)
	}
	for _, q := range r.Quiz.Questions {
		if q.Type == guildstore.QText && len(q.Image) == 0 {
			t.Fatal("the question's picture should be sent")
		}
	}
	ans, _ := s.Call(proto.Request{Type: "join_answers", Answers: solve(*r.Quiz)}, 2*time.Second)
	if !ans.OK || ans.Message != "Welcome in" || ans.Receipt == nil {
		t.Fatalf("passing the quiz: %+v", ans)
	}

	// Failing three times locks the person out for a day.
	v := newTestUser()
	for i := 1; i <= 3; i++ {
		s, r := joinAs(t, addr, v, proto.Request{})
		if r.Code != "captcha" {
			t.Fatalf("try %d: %+v", i, r)
		}
		ans, _ := s.Call(proto.Request{Type: "join_answers", Answers: []proto.QuizAnswer{{Text: "red"}}}, time.Second)
		if ans.OK || ans.Code != "captcha_failed" || ans.Message != "Nope" || ans.TriesLeft != 3-i {
			t.Fatalf("failed try %d: %+v", i, ans)
		}
	}
	if _, r := joinAs(t, addr, v, proto.Request{}); r.Code != "captcha_locked" {
		t.Fatalf("after 3 fails: %+v", r)
	}
	// Answers can't be sent without a quiz.
	s2, _ := joinAs(t, addr, newTestUser(), proto.Request{})
	s2.Call(proto.Request{Type: "join_answers", Answers: solve(*r.Quiz)}, time.Second)
	if r, _ := s2.Call(proto.Request{Type: "join_answers"}, time.Second); r.Code != "no_quiz" {
		t.Fatalf("second answer on the same quiz: %+v", r)
	}
}

func TestApprovalKickAndBan(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	so, _ := signedIn(t, addr, owner, "pc-o", false)
	setJoin(t, so, proto.JoinSettings{ShowJoinButton: true, RequireApproval: true})

	u := newTestUser()
	ukey := guildstore.KeyText(u.key.Public().(ed25519.PublicKey))
	su, r := joinAs(t, addr, u, proto.Request{UserName: "Una"})
	if r.Code != "pending" {
		t.Fatalf("join with approval: %+v", r)
	}
	waitEvent(t, so, "requests_changed")
	if _, r := signedIn(t, addr, u, "pc-u", false); r.Code != "pending" {
		t.Fatalf("auth while waiting: %+v", r)
	}
	req, _ := so.Call(proto.Request{Type: "requests"}, time.Second)
	if len(req.People) != 1 || req.People[0].Name != "Una" {
		t.Fatalf("requests: %+v", req.People)
	}
	if r, _ := so.Call(proto.Request{Type: "approve", Target: ukey}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	_ = su
	if _, ok := g.members[ukey]; !ok {
		t.Fatal("approved person should be a member")
	}

	// A kicked member is removed, and told.
	sm, _ := signedIn(t, addr, u, "pc-u", false)
	if r, _ := so.Call(proto.Request{Type: "kick", Target: ukey}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	waitEvent(t, sm, "removed")
	if _, r := signedIn(t, addr, u, "pc-u", false); r.Code != "not_member" {
		t.Fatalf("after kick: %+v", r)
	}
	// A kicked person may join again (approval still needed); decline this time.
	joinAs(t, addr, u, proto.Request{})
	if r, _ := so.Call(proto.Request{Type: "decline", Target: ukey}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}

	// Ban with the address: another user from the same address is refused too.
	setJoin(t, so, proto.JoinSettings{ShowJoinButton: true})
	joinAs(t, addr, u, proto.Request{})
	if r, _ := so.Call(proto.Request{Type: "ban", Target: ukey, BanIP: true}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	if _, r := joinAs(t, addr, u, proto.Request{}); r.Code != "banned" {
		t.Fatalf("banned user: %+v", r)
	}
	if _, r := joinAs(t, addr, newTestUser(), proto.Request{}); r.Code != "banned" {
		t.Fatalf("same address should be blocked too: %+v", r)
	}
	bans, _ := so.Call(proto.Request{Type: "bans"}, time.Second)
	if len(bans.People) != 1 || bans.People[0].IP == "" {
		t.Fatalf("bans: %+v", bans.People)
	}
	if r, _ := so.Call(proto.Request{Type: "unban", Target: ukey}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	if _, r := joinAs(t, addr, u, proto.Request{}); !r.OK {
		t.Fatalf("after unban: %+v", r)
	}
	// The owner can't kick themself.
	okey := guildstore.KeyText(owner.key.Public().(ed25519.PublicKey))
	if r, _ := so.Call(proto.Request{Type: "kick", Target: okey}, time.Second); r.OK {
		t.Fatal("the owner must not kick themself")
	}
}

func TestAddressBanNeverBlocksOwner(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	so, _ := signedIn(t, addr, owner, "pc-o", false)
	u := newTestUser()
	joinAs(t, addr, u, proto.Request{})
	ukey := guildstore.KeyText(u.key.Public().(ed25519.PublicKey))
	if r, _ := so.Call(proto.Request{Type: "ban", Target: ukey, BanIP: true}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	// Everyone here connects from 127.0.0.1, like a shared home network.
	if _, r := signedIn(t, addr, owner, "pc-o2", true); !r.OK {
		t.Fatalf("the owner must not be blocked by an address ban: %+v", r)
	}
}

// TestCaptchaWeights checks that Always Ask questions are always in the quiz, a Must
// question has to be right, and a None question doesn't count.
func TestCaptchaWeights(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	so, _ := signedIn(t, addr, owner, "pc-o", false)
	put := func(q proto.CaptchaQuestion) {
		t.Helper()
		if r, _ := so.Call(proto.Request{Type: "captcha_put", Question: &q}, time.Second); !r.OK {
			t.Fatalf("captcha_put: %+v", r)
		}
	}
	radio := func(text, weight string, always bool) proto.CaptchaQuestion {
		return proto.CaptchaQuestion{Type: guildstore.QRadio, Text: text, Options: []string{"no", "yes"}, Correct: []int{1}, Weight: weight, AlwaysAsk: always}
	}
	bad := radio("Odd", "heavy", false)
	if r, _ := so.Call(proto.Request{Type: "captcha_put", Question: &bad}, time.Second); r.OK {
		t.Fatal("an unknown weight must be refused")
	}
	put(radio("Must", guildstore.WeightMust, true))
	put(radio("Regular", "", false))
	put(radio("Ignored", guildstore.WeightNone, false))
	for _, x := range []string{"Extra 1", "Extra 2", "Extra 3"} {
		put(radio(x, "", false))
	}
	settings := func(ask, pass int) {
		t.Helper()
		set := proto.CaptchaSettings{Enabled: true, AskCount: ask, PassPercent: pass}
		if r, _ := so.Call(proto.Request{Type: "captcha_settings", Captcha: &set}, time.Second); !r.OK {
			t.Fatal(r.Error)
		}
	}
	// answer answers correctly only the questions named in right.
	answer := func(q proto.Quiz, right ...string) []proto.QuizAnswer {
		var out []proto.QuizAnswer
		for _, qq := range q.Questions {
			want := "no"
			for _, r := range right {
				if qq.Text == r {
					want = "yes"
				}
			}
			for i, o := range qq.Options {
				if o == want {
					out = append(out, proto.QuizAnswer{Choices: []int{i}})
				}
			}
		}
		return out
	}

	// With 2 of 6 asked, the Always Ask one is in every quiz.
	settings(2, 100)
	for i := 0; i < 6; i++ {
		_, r := joinAs(t, addr, newTestUser(), proto.Request{})
		found := false
		for _, q := range r.Quiz.Questions {
			found = found || q.Text == "Must"
		}
		if len(r.Quiz.Questions) != 2 || !found {
			t.Fatalf("quiz %d: %+v", i, r.Quiz.Questions)
		}
	}

	// With all six asked, 50% is needed to pass.
	settings(6, 50)
	try := func(right ...string) proto.Response {
		t.Helper()
		s, r := joinAs(t, addr, newTestUser(), proto.Request{})
		ans, _ := s.Call(proto.Request{Type: "join_answers", Answers: answer(*r.Quiz, right...)}, 2*time.Second)
		return ans
	}
	// 5 counted (Ignored is not); Must + 2 others = 60%: passes.
	if r := try("Must", "Regular", "Extra 1"); !r.OK {
		t.Fatalf("60%% with Must right should pass: %+v", r)
	}
	// 4 of 5 right but Must wrong: fails.
	if r := try("Regular", "Extra 1", "Extra 2", "Extra 3", "Ignored"); r.OK || r.Code != "captcha_failed" {
		t.Fatalf("Must wrong should fail: %+v", r)
	}
	// Must + Ignored right = 1 of 5 counted: fails; Ignored didn't help.
	if r := try("Must", "Ignored"); r.OK {
		t.Fatalf("an Ignored question must not count: %+v", r)
	}
}

// TestAccountFacts checks that the creation date and guild count reach staff, in join
// requests and the member list, and nobody else.
func TestAccountFacts(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	so, _ := signedIn(t, addr, owner, "pc-o", false)
	setJoin(t, so, proto.JoinSettings{ShowJoinButton: true, RequireApproval: true})
	created := "2025-03-04T05:06:07Z"
	u := newTestUser()
	ukey := guildstore.KeyText(u.key.Public().(ed25519.PublicKey))
	if _, r := joinAs(t, addr, u, proto.Request{UserName: "Una", Created: created, ServerCount: 4}); r.Code != "pending" {
		t.Fatalf("join: %+v", r)
	}
	req, _ := so.Call(proto.Request{Type: "requests"}, time.Second)
	if len(req.People) != 1 || req.People[0].Created != created || req.People[0].Guilds != 4 {
		t.Fatalf("requests: %+v", req.People)
	}
	if r, _ := so.Call(proto.Request{Type: "approve", Target: ukey}, time.Second); !r.OK {
		t.Fatal(r.Error)
	}
	if m := g.members[ukey]; m.Guilds != 4 || m.Created.Format(time.RFC3339) != created {
		t.Fatalf("stored: %+v", m)
	}
	find := func(list []proto.MemberInfo) proto.MemberInfo {
		for _, m := range list {
			if m.UserKey == ukey {
				return m
			}
		}
		t.Fatal("not in the list")
		return proto.MemberInfo{}
	}
	m, _ := so.Call(proto.Request{Type: "members"}, time.Second)
	if f := find(m.Members); f.Created != created || f.Guilds != 4 {
		t.Fatalf("the owner sees: %+v", f)
	}
	su, _ := signedIn(t, addr, u, "pc-u", false)
	m, _ = su.Call(proto.Request{Type: "members"}, time.Second)
	if f := find(m.Members); f.Created != "" || f.Guilds != 0 {
		t.Fatalf("a plain member sees: %+v", f)
	}
}
