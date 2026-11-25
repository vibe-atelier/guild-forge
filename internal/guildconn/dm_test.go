package guildconn

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexus/internal/guild"
	"nexus/internal/guildstore"
	"nexus/internal/user"
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func contactState(m *Manager, id string) string {
	for _, c := range m.Contacts() {
		if c.UserID == id {
			return c.State
		}
	}
	return ""
}

func hasText(m *Manager, contact, text string) bool {
	msgs, _ := m.Chat(contact)
	for _, d := range msgs {
		if d.Text == text {
			return true
		}
	}
	return false
}

// TestPrivateMessages runs private messages end to end through a real guild:
// request and accept, a message and its read receipt, a message kept while
// offline, the verification code, blocking, and a file.
func TestPrivateMessages(t *testing.T) {
	dir := t.TempDir()
	gd, err := guildstore.NewGuildData("goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	if err := gd.Save(dir); err != nil {
		t.Fatal(err)
	}
	g, err := guild.NewServer(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go g.Serve(l)
	t.Cleanup(func() { g.Stop(); l.Close() })
	addr := l.Addr().String()

	alice, err := user.Create(filepath.Join(t.TempDir(), "Alice"), "Alice", "alice-password")
	if err != nil {
		t.Fatal(err)
	}
	ben, err := user.Create(filepath.Join(t.TempDir(), "Ben"), "Ben", "ben-password")
	if err != nil {
		t.Fatal(err)
	}
	a := New(alice, "pc-a", func() {})
	t.Cleanup(a.CloseAll)
	g.StartBroadcast()
	otp, _, _ := g.NewOTP()
	check, err := a.Check(addr)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := a.Pair(check.Address, check.GuildID, otp); err != nil || !r.OK {
		t.Fatalf("pair: %v %+v", err, r)
	}
	b := New(ben, "pc-b", func() {})
	if r, err := b.JoinStart(check.Address, check.GuildID, "", false); err != nil || r.Code != "joined" {
		t.Fatalf("join: %v %+v", err, r)
	}
	aID, bID := a.myID(), b.myID()
	waitFor(t, "both connected", func() bool {
		ga, _, _, _ := a.route(bID)
		gb, _, _, _ := b.route(aID)
		return ga != nil && gb != nil
	})

	// Request and accept.
	if err := a.SendDM(bID, "too early", ""); err == nil {
		t.Fatal("messages need an accepted contact")
	}
	if err := a.RequestContact(bID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Ben gets the request", func() bool { return contactState(b, aID) == ContactReceived })
	if contactState(a, bID) != ContactSent {
		t.Fatal("Alice's request should show as sent")
	}
	if err := b.AcceptContact(aID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Alice sees the accept", func() bool { return contactState(a, bID) == ContactActive })

	// A message, and its read receipt.
	if err := a.SendDM(bID, "Hello **Ben**", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Ben gets the message", func() bool { return hasText(b, aID, "Hello **Ben**") })
	waitFor(t, "Alice sees it was read", func() bool {
		msgs, _ := a.Chat(bID)
		return len(msgs) == 1 && msgs[0].Mine && msgs[0].Read
	})
	b.LeaveChat()
	a.LeaveChat()

	// While Ben is offline the guild keeps it.
	b.CloseAll()
	if err := a.SendDM(bID, "while you were away", ""); err != nil {
		t.Fatal(err)
	}
	b = New(ben, "pc-b", func() {})
	t.Cleanup(b.CloseAll)
	waitFor(t, "Ben collects it", func() bool { return hasText(b, aID, "while you were away") })

	// The verification code: Ben reads his code to Alice.
	if err := a.Verify(bID, "AAA-BBB-CCC"); err == nil {
		t.Fatal("a wrong code must not verify")
	}
	if err := a.Verify(bID, b.MyCode(aID)); err != nil {
		t.Fatal(err)
	}
	for _, c := range a.Contacts() {
		if c.UserID == bID && c.Verified != "verified" {
			t.Fatalf("verified: %q", c.Verified)
		}
	}

	// Send a file.
	waitFor(t, "Alice reaches Ben again", func() bool { g, _, _, _ := a.route(bID); return g != nil })
	if err := a.SendFile(bID, "sprite.png", []byte("not really a png")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Ben gets the file", func() bool {
		msgs, _ := b.Chat(aID)
		for _, d := range msgs {
			if d.File == "sprite.png" {
				data, err := os.ReadFile(filepath.Join(ben.Dir, d.FilePath))
				return err == nil && string(data) == "not really a png"
			}
		}
		return false
	})

	// Blocking: nothing more arrives, and Alice moves to Blocked.
	b.Block(aID, "Alice")
	a.SendDM(bID, "can you hear me?", "")
	time.Sleep(700 * time.Millisecond)
	if hasText(b, aID, "can you hear me?") {
		t.Fatal("a blocked person's message must be dropped")
	}
	if contactState(b, aID) != ContactBlocked || !b.IsBlocked(aID) {
		t.Fatal("Alice should be on the Blocked list")
	}
}

// TestRetry: receipts are tried again until they get through, but not for
// ever.
func TestRetry(t *testing.T) {
	tries := 0
	ok := retry(func() error {
		tries++
		if tries < 3 {
			return errors.New("busy")
		}
		return nil
	}, []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
	if !ok || tries != 3 {
		t.Fatalf("succeeded=%v after %d tries, want true after 3", ok, tries)
	}
	tries = 0
	ok = retry(func() error { tries++; return errors.New("down") }, []time.Duration{time.Millisecond, time.Millisecond})
	if ok || tries != 3 {
		t.Fatalf("succeeded=%v after %d tries, want false after 3", ok, tries)
	}
}
