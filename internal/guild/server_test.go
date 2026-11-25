package guild

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// startTestGuild runs a guild in this test process on a free local port.
func startTestGuild(t *testing.T) (*Server, string) {
	t.Helper()
	dir := newGuildDir(t, "goodpassword")
	g, err := NewServer(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(l)
	t.Cleanup(func() { g.Stop(); l.Close() })
	return g, l.Addr().String()
}

type testUser struct{ key ed25519.PrivateKey }

func newTestUser() testUser {
	_, k, _ := ed25519.GenerateKey(rand.Reader)
	return testUser{k}
}

func (u testUser) pair(t *testing.T, c *proto.Conn, otp string) proto.Response {
	t.Helper()
	err := c.Send(proto.Request{
		Type: "pair", OTP: otp,
		UserKey: u.key.Public().(ed25519.PublicKey),
		Sig:     ed25519.Sign(u.key, proto.PairProof(c.SessionID)),
	})
	if err != nil {
		t.Fatal(err)
	}
	var r proto.Response
	if err := c.Recv(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPairingFlow(t *testing.T) {
	g, addr := startTestGuild(t)
	user := newTestUser()

	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if guildstore.KeyText(c.GuildKey) != g.ID {
		t.Fatal("client sees the wrong guild key")
	}

	// The guild isn't broadcasting yet, so pairing is refused.
	if r := user.pair(t, c, "AAAAAAAAA"); r.OK || !strings.Contains(r.Error, "not broadcasting") {
		t.Fatalf("got %+v", r)
	}
	if _, _, err := g.NewOTP(); err == nil {
		t.Fatal("no OTP should be made without a broadcast")
	}

	if _, err := g.StartBroadcast(); err != nil {
		t.Fatal(err)
	}
	otp, _, err := g.NewOTP()
	if err != nil {
		t.Fatal(err)
	}
	if len(otp) != OTPLength {
		t.Fatalf("OTP %q should be %d characters", otp, OTPLength)
	}

	// One wrong try, then the right one typed in lower case with dashes.
	if r := user.pair(t, c, "WRONGWRNG"); r.OK || r.TriesLeft != MaxOTPFailures-1 {
		t.Fatalf("got %+v", r)
	}
	typed := strings.ToLower(FormatOTP(otp))
	if r := user.pair(t, c, typed); !r.OK {
		t.Fatalf("pairing failed: %+v", r)
	}

	s := g.Status()
	if !s.HasOwner || s.OwnerKey != guildstore.KeyText(user.key.Public().(ed25519.PublicKey)) || s.Broadcasting {
		t.Fatalf("after pairing: %+v", s)
	}
	if guildstore.OwnerKey(g.DataDir) != s.OwnerKey {
		t.Fatal("the owner was not saved to owner.json")
	}
	// A second person cannot take over, and a new broadcast is refused.
	if r := newTestUser().pair(t, c, typed); r.OK {
		t.Fatal("a second owner must be refused")
	}
	if _, err := g.StartBroadcast(); err == nil {
		t.Fatal("a guild with an owner must not broadcast")
	}
}

func TestFiveWrongOTPsEndBroadcast(t *testing.T) {
	g, addr := startTestGuild(t)
	user := newTestUser()
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g.StartBroadcast()
	otp, _, _ := g.NewOTP()
	for i := 1; i <= MaxOTPFailures; i++ {
		r := user.pair(t, c, "ZZZZZZZZZ")
		if r.OK {
			t.Fatal("wrong OTP accepted")
		}
		if i == MaxOTPFailures && !strings.Contains(r.Error, "broadcast has ended") {
			t.Fatalf("5th wrong try: %+v", r)
		}
	}
	if g.Status().Broadcasting {
		t.Fatal("broadcast should have ended")
	}
	if r := user.pair(t, c, otp); r.OK {
		t.Fatal("the old OTP must not work after the broadcast ended")
	}
}

func TestOTPAndBroadcastExpire(t *testing.T) {
	g, addr := startTestGuild(t)
	now := time.Now()
	g.now = func() time.Time { return now }
	user := newTestUser()
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	g.StartBroadcast()
	otp, _, _ := g.NewOTP()
	now = now.Add(OTPLifetime + time.Second) // The OTP has expired, while the broadcast is still on.
	if r := user.pair(t, c, otp); r.OK || !strings.Contains(r.Error, "expired") {
		t.Fatalf("expired OTP: %+v", r)
	}
	// A new OTP replaces the old one.
	otp1, _, _ := g.NewOTP()
	otp2, _, _ := g.NewOTP()
	if otp1 == otp2 {
		t.Fatal("two OTPs in a row should differ")
	}
	if r := user.pair(t, c, otp1); r.OK {
		t.Fatal("a replaced OTP must not work")
	}
	now = now.Add(BroadcastDuration) // The hour is up.
	if g.Status().Broadcasting {
		t.Fatal("broadcast should end after an hour")
	}
}

func TestInfoRequest(t *testing.T) {
	g, addr := startTestGuild(t)
	g.StartBroadcast()
	c, err := proto.Dial(addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Send(proto.Request{Type: "info"})
	var r proto.Response
	if err := c.Recv(&r); err != nil || !r.Broadcasting || r.HasOwner {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestControlChannel(t *testing.T) {
	g, _ := startTestGuild(t)
	l, err := StartControl(g, g.DataDir, 50000)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	s, running := RunningStatus(g.DataDir)
	if !running || s.Port != 50000 || s.Broadcasting {
		t.Fatalf("status: %+v %v", s, running)
	}
	if _, err := Call(g.DataDir, "otp"); err == nil {
		t.Fatal("otp without broadcast should fail")
	}
	if _, err := Call(g.DataDir, "broadcast"); err != nil {
		t.Fatal(err)
	}
	r, err := Call(g.DataDir, "otp")
	if err != nil || len(r.OTP) != OTPLength {
		t.Fatalf("otp: %+v %v", r, err)
	}
	if _, err := Call(g.DataDir, "stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not stop the guild")
	}
}

func TestFormatOTP(t *testing.T) {
	if got := FormatOTP("K7Q4MZ9TB"); got != "K7Q-4MZ-9TB" {
		t.Fatalf("got %s", got)
	}
	if got := NormalizeOTP(" k7q-4mz-9tb "); got != "K7Q4MZ9TB" {
		t.Fatalf("got %s", got)
	}
}
