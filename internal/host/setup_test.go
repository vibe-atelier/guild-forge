package host

import (
	"nexus/internal/guildstore"

	"bytes"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"nexus/internal/console"
)

func runSetup(t *testing.T, cfg string, index int, action string, lines ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Setup(console.NewWith(answers(lines...), &out), cfg, index, action)
	return out.String(), err
}

// freePort finds a port nothing is using right now.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestSetupShows checks that setup=N alone shows the settings and the commands, with
// no menu and no password.
func TestSetupShows(t *testing.T) {
	cfg := t.TempDir()
	makeGuild(t, cfg, "goodpassword", 0)
	out, err := runSetup(t, cfg, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[stopped]", "Port:            none yet", "setup=1 port=50000", "setup=1 broadcast"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Press a number") {
		t.Fatal("no menus")
	}
}

func TestSetupPort(t *testing.T) {
	cfg := t.TempDir()
	makeGuild(t, cfg, "goodpassword", 0)
	makeGuild(t, cfg, "otherpassword", 50001)
	for _, bad := range []string{"port=abc", "port=80", "port=70000", "port=50001", "colour=red"} {
		if _, err := runSetup(t, cfg, 1, bad, "goodpassword"); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
	port := freePort(t)
	out, err := runSetup(t, cfg, 1, "port="+strconv.Itoa(port), "goodpassword")
	if err != nil || !strings.Contains(out, "This guild is now GUILD#1@"+strconv.Itoa(port)) {
		t.Fatalf("%v\n%s", err, out)
	}
	reg, _ := LoadRegistry(cfg)
	if reg.Get(1).Port != port {
		t.Fatalf("port not saved: %d", reg.Get(1).Port)
	}
}

func TestSetupWrongPassword(t *testing.T) {
	cfg := t.TempDir()
	makeGuild(t, cfg, "goodpassword", 0)
	out, err := runSetup(t, cfg, 1, "password", "nope1", "nope2", "nope3")
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("want ErrWrongPassword, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "2 tries left") || !strings.Contains(out, "1 try left") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestSetupChangePassword(t *testing.T) {
	cfg := t.TempDir()
	dir := makeGuild(t, cfg, "goodpassword", 50003)
	out, err := runSetup(t, cfg, 1, "password", "goodpassword", "newpassword1", "newpassword1")
	if err != nil {
		t.Fatalf("setup failed: %v\n%s", err, out)
	}
	gd, _ := guildstore.LoadGuildData(dir)
	if !gd.CheckPassword("newpassword1") || gd.CheckPassword("goodpassword") {
		t.Fatal("password was not changed")
	}
}

func TestSetupMissingGuild(t *testing.T) {
	_, err := runSetup(t, t.TempDir(), 4, "")
	if err == nil || !strings.Contains(err.Error(), "no GUILD#4") {
		t.Fatalf("got %v", err)
	}
}

func TestPortBusy(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !PortBusy(l.Addr().(*net.TCPAddr).Port) {
		t.Fatal("a port in use should be reported busy")
	}
	if PortBusy(freePort(t)) {
		t.Fatal("a free port should not be reported busy")
	}
}

func TestParseIndex(t *testing.T) {
	for _, ok := range []string{"1", "8", " 3 "} {
		if _, err := ParseIndex(ok); err != nil {
			t.Errorf("%q should be accepted: %v", ok, err)
		}
	}
	for _, bad := range []string{"0", "9", "", "one", "-1"} {
		if _, err := ParseIndex(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
