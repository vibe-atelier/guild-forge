package host

import (
	"nexus/internal/guildstore"

	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nexus/internal/console"
)

func TestExportImportRoundTrip(t *testing.T) {
	cfg := t.TempDir()
	dir := makeGuild(t, cfg, "goodpassword", 50000)
	guildstore.SaveOwner(dir, guildstore.Owner{Key: "OWNERKEY", PairedAt: time.Now()})
	os.MkdirAll(filepath.Join(dir, "media"), 0o700)
	// This makes more than one chunk.
	os.WriteFile(filepath.Join(dir, "media", "pic.png"), bytes.Repeat([]byte("x"), 200_000), 0o600)
	os.MkdirAll(filepath.Join(dir, guildstore.LogsDir), 0o700)
	os.WriteFile(filepath.Join(dir, guildstore.LogsDir, "guild.log"), []byte("log"), 0o600)
	gd, _ := guildstore.LoadGuildData(dir)

	outDir := t.TempDir()
	var out bytes.Buffer
	if err := Export(console.NewWith(answers("goodpassword"), &out), cfg, 1, outDir); err != nil {
		t.Fatalf("export: %v\n%s", err, out.String())
	}
	files, _ := filepath.Glob(filepath.Join(outDir, "GUILD1-*.nexus"))
	if len(files) != 1 {
		t.Fatalf("export file not found: %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if bytes.Contains(data, []byte("OWNERKEY")) {
		t.Fatal("the export file must be encrypted")
	}

	// Importing onto the same computer is refused (same guild ID).
	out.Reset()
	err := Import(console.NewWith(answers("goodpassword"), &out), cfg, files[0])
	if err == nil || !strings.Contains(err.Error(), "already on this computer") {
		t.Fatalf("duplicate import: %v", err)
	}

	// Onto a "new computer": wrong password twice, then right; default folder.
	cfg2 := t.TempDir()
	makeGuild(t, cfg2, "another1", 0)
	out.Reset()
	if err := Import(console.NewWith(answers("bad", "worse", "goodpassword", ""), &out), cfg2, `"`+files[0]+`"`); err != nil {
		t.Fatalf("import: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Imported as GUILD#2(") && !strings.Contains(out.String(), "Imported as GUILD#2") {
		t.Fatalf("output:\n%s", out.String())
	}
	reg, _ := LoadRegistry(cfg2)
	e := reg.Get(2)
	if e == nil || e.GuildID != gd.PublicKey || e.Port != 0 {
		t.Fatalf("imported entry: %+v", e)
	}
	if guildstore.OwnerKey(e.DataDir) != "OWNERKEY" {
		t.Fatal("owner not kept")
	}
	if b, _ := os.ReadFile(filepath.Join(e.DataDir, "media", "pic.png")); len(b) != 200_000 {
		t.Fatal("media not restored")
	}
	if _, err := os.Stat(filepath.Join(e.DataDir, guildstore.LogsDir)); err == nil {
		t.Fatal("logs should not be exported")
	}

	// Three wrong passwords give up.
	cfg3 := t.TempDir()
	err = Import(console.NewWith(answers("a", "b", "c"), &out), cfg3, files[0])
	if !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("got %v", err)
	}
}

func TestImportFolderAndDelete(t *testing.T) {
	cfg := t.TempDir()
	makeGuild(t, cfg, "password1", 0)
	dir2 := makeGuild(t, cfg, "password2", 0)
	makeGuild(t, cfg, "password3", 0)

	// Delete GUILD#2 but keep its folder: GUILD#3 becomes GUILD#2.
	var out bytes.Buffer
	if err := Delete(console.NewWith(answers("password2", "y", "n"), &out), cfg, 2); err != nil {
		t.Fatalf("delete: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "GUILD#3 is now GUILD#2") || !guildstore.HasGuildData(dir2) {
		t.Fatalf("output:\n%s", out.String())
	}
	// Add the kept folder back: it becomes GUILD#3.
	out.Reset()
	if err := Import(console.NewWith(answers(), &out), cfg, dir2); err != nil {
		t.Fatalf("import folder: %v", err)
	}
	reg, _ := LoadRegistry(cfg)
	if reg.Get(3) == nil || reg.Get(3).DataDir != dir2 {
		t.Fatalf("got %+v", reg.Guilds)
	}
	// Delete it again, erasing the folder this time.
	out.Reset()
	if err := Delete(console.NewWith(answers("password2", "y", "y"), &out), cfg, 3); err != nil {
		t.Fatal(err)
	}
	if guildstore.HasGuildData(dir2) {
		t.Fatal("folder should be erased")
	}
	// Saying no deletes nothing.
	out.Reset()
	Delete(console.NewWith(answers("password1", "n"), &out), cfg, 1)
	if reg, _ := LoadRegistry(cfg); len(reg.Guilds) != 2 {
		t.Fatal("nothing should be deleted after answering no")
	}
}

func TestBackupAndFallBack(t *testing.T) {
	cfg := t.TempDir()
	dir := makeGuild(t, cfg, "goodpassword", 0)
	os.WriteFile(filepath.Join(dir, "chat.txt"), []byte("version 1"), 0o600)

	var out bytes.Buffer
	for i := 0; i < 2; i++ {
		if err := Backup(console.NewWith(answers("goodpassword"), &out), cfg, 1); err != nil {
			t.Fatal(err)
		}
	}
	today := time.Now().Format("02012006")
	if !strings.Contains(out.String(), "S1_"+today+"_V1") || !strings.Contains(out.String(), "S1_"+today+"_V2") {
		t.Fatalf("output:\n%s", out.String())
	}

	// Change the guild, then list and fall back to V1.
	os.WriteFile(filepath.Join(dir, "chat.txt"), []byte("version 2"), 0o600)
	out.Reset()
	if err := FallBack(console.NewWith(answers(), &out), cfg, 1, ""); err != nil {
		t.Fatal(err)
	}
	if i1, i2 := strings.Index(out.String(), "_V2"), strings.Index(out.String(), "_V1"); i1 < 0 || i2 < 0 || i1 > i2 {
		t.Fatalf("list should be newest first:\n%s", out.String())
	}
	out.Reset()
	name := "S1_" + today + "_V1"
	if err := FallBack(console.NewWith(answers("goodpassword", "y"), &out), cfg, 1, name); err != nil {
		t.Fatalf("fall-back: %v\n%s", err, out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "chat.txt")); string(b) != "version 1" {
		t.Fatalf("not restored: %q", b)
	}
	if !strings.Contains(out.String(), "Current state saved as S1_"+today+"_V3") {
		t.Fatalf("safety backup missing:\n%s", out.String())
	}
	// The safety backup holds version 2, so the fall-back can be undone.
	out.Reset()
	if err := FallBack(console.NewWith(answers("goodpassword", "y"), &out), cfg, 1, "s1_"+today+"_v3"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "chat.txt")); string(b) != "version 2" {
		t.Fatalf("undo failed: %q", b)
	}

	// Wrong guild number and unknown names are refused.
	if err := FallBack(console.NewWith(answers(), &out), cfg, 1, "S2_"+today+"_V1"); err == nil {
		t.Fatal("S2 name for GUILD#1 should be refused")
	}
	if err := FallBack(console.NewWith(answers(), &out), cfg, 1, "S1_01011999_V1"); err == nil {
		t.Fatal("unknown backup should be refused")
	}
}

// A backup made with any guild number follows the guild when it is renumbered.
func TestBackupsFollowRenumbering(t *testing.T) {
	cfg := t.TempDir()
	makeGuild(t, cfg, "password1", 0)
	makeGuild(t, cfg, "password2", 0)
	var out bytes.Buffer
	Backup(console.NewWith(answers("password2"), &out), cfg, 2)
	Delete(console.NewWith(answers("password1", "y", "n"), &out), cfg, 1)
	out.Reset()
	FallBack(console.NewWith(answers(), &out), cfg, 1, "")
	if !strings.Contains(out.String(), "S1_") {
		t.Fatalf("the backup should now be shown as S1_...:\n%s", out.String())
	}
}
