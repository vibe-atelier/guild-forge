package host

import (
	"nexus/internal/guildstore"

	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nexus/internal/console"
)

// makeGuild creates a guild directly (without the create questions) and
// returns its folder.
func makeGuild(t *testing.T, cfg, password string, port int) string {
	t.Helper()
	dir := t.TempDir()
	gd, err := guildstore.NewGuildData(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := gd.Save(dir); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Add(Entry{GuildID: gd.PublicKey, DataDir: dir, Port: port}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func answers(lines ...string) *strings.Reader {
	return strings.NewReader(strings.Join(lines, "\n") + "\n")
}

func TestDisplayName(t *testing.T) {
	cases := []struct {
		index, port int
		owner, want string
	}{
		{1, 0, "", "GUILD#1"},
		{1, 50000, "", "GUILD#1@50000"},
		{8, 50000, "ABC", "GUILD#8@50000(ADMIN:ABC)"},
	}
	for _, c := range cases {
		if got := DisplayName(c.index, c.port, c.owner); got != c.want {
			t.Errorf("DisplayName(%d,%d,%q) = %q, want %q", c.index, c.port, c.owner, got, c.want)
		}
	}
}

func TestRegistryFullAndIndexes(t *testing.T) {
	r := &Registry{}
	for i := 1; i <= MaxGuilds; i++ {
		e, err := r.Add(Entry{GuildID: string(rune('A' + i)), DataDir: "x"})
		if err != nil || e.Index != i {
			t.Fatalf("add %d: %v %v", i, e, err)
		}
	}
	if _, err := r.Add(Entry{GuildID: "Z"}); err == nil {
		t.Fatal("a 9th guild should be refused")
	}
	r.Remove(8, nil)
	if _, err := r.Add(Entry{GuildID: string(rune('A' + 1))}); err == nil {
		t.Fatal("the same guild twice should be refused")
	}
}

// Removing guild 2 of 3 turns guild 3 into guild 2.
func TestRemoveRenumbers(t *testing.T) {
	r := &Registry{}
	r.Add(Entry{GuildID: "A"})
	r.Add(Entry{GuildID: "B"})
	r.Add(Entry{GuildID: "C"})
	var out bytes.Buffer
	r.Remove(2, &out)
	if len(r.Guilds) != 2 || r.Get(2).GuildID != "C" {
		t.Fatalf("got %+v", r.Guilds)
	}
	if !strings.Contains(out.String(), "GUILD#3 is now GUILD#2") {
		t.Fatalf("got %q", out.String())
	}
}

// A guild whose folder was deleted disappears from the list.
func TestOpenRegistryForgetsMissingFolders(t *testing.T) {
	cfg := t.TempDir()
	makeGuild(t, cfg, "password1", 0)
	gone := makeGuild(t, cfg, "password2", 0)
	third := makeGuild(t, cfg, "password3", 0)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	reg, err := OpenRegistry(cfg, &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Guilds) != 2 || reg.Get(2).DataDir != third {
		t.Fatalf("got %+v", reg.Guilds)
	}
	if !strings.Contains(out.String(), "GUILD#2's folder is gone") || !strings.Contains(out.String(), "GUILD#3 is now GUILD#2") {
		t.Fatalf("got %q", out.String())
	}
	// Saved, so the next run sees the same thing silently.
	out.Reset()
	if reg, _ = OpenRegistry(cfg, &out); len(reg.Guilds) != 2 || out.Len() != 0 {
		t.Fatalf("not saved: %+v %q", reg.Guilds, out.String())
	}
}

func TestCreateAndList(t *testing.T) {
	cfg := t.TempDir()
	data1 := filepath.Join(t.TempDir(), "guild one") // The folder does not exist yet, and its name has a space.

	var out bytes.Buffer
	in := answers(
		"short",        // This password is too short.
		"goodpassword", // This password is fine.
		"different",    // This one doesn't match.
		"goodpassword", // The password is typed again.
		"goodpassword", // It is confirmed.
		`"`+data1+`"`,  // This is a quoted path with a space.
		"",             // Enter means yes: create the folder.
	)
	if err := Create(console.NewWith(in, &out), cfg); err != nil {
		t.Fatalf("create failed: %v\noutput:\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "GUILD#1 created") {
		t.Fatalf("unexpected output:\n%s", out.String())
	}
	gd, err := guildstore.LoadGuildData(data1)
	if err != nil {
		t.Fatal(err)
	}
	if !gd.CheckPassword("goodpassword") || gd.CheckPassword("wrong") {
		t.Fatal("password check is wrong")
	}
	if len(gd.PublicKey) != 52 {
		t.Fatalf("public key should be 52 characters, got %d", len(gd.PublicKey))
	}

	// Second guild: the used folder and a non-empty folder are refused, then
	// Enter picks the default folder.
	full := t.TempDir()
	os.WriteFile(filepath.Join(full, "something.txt"), []byte("x"), 0o600)
	out.Reset()
	in = answers("goodpassword", "goodpassword", data1, full, "")
	if err := Create(console.NewWith(in, &out), cfg); err != nil {
		t.Fatalf("second create failed: %v\n%s", err, out.String())
	}
	for _, want := range []string{"GUILD#1 already uses that folder", "That folder is not empty", "GUILD#2 created"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output is missing %q:\n%s", want, out.String())
		}
	}
	reg, _ := LoadRegistry(cfg)
	if !strings.HasPrefix(reg.Get(2).DataDir, filepath.Join(cfg, "guilds", "guild-")) {
		t.Fatalf("default folder not used: %s", reg.Get(2).DataDir)
	}

	out.Reset()
	if err := List(cfg, &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "GUILD#1  [stopped]\nGUILD#2  [stopped]\n"; got != want {
		t.Fatalf("list = %q, want %q", got, want)
	}
}

func TestCreateCancelled(t *testing.T) {
	cfg := t.TempDir()
	var out bytes.Buffer
	err := Create(console.NewWith(strings.NewReader(""), &out), cfg)
	if err != console.ErrNoInput {
		t.Fatalf("want ErrNoInput, got %v", err)
	}
	r, _ := LoadRegistry(cfg)
	if len(r.Guilds) != 0 {
		t.Fatal("a cancelled create must not add a guild")
	}
}

func TestListEmpty(t *testing.T) {
	var out bytes.Buffer
	if err := List(t.TempDir(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No guilds yet") {
		t.Fatalf("got %q", out.String())
	}
}
