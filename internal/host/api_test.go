package host

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowFunctions checks what GuildForge's window does, without the window.
func TestWindowFunctions(t *testing.T) {
	dir := t.TempDir()
	if err := CheckNewPassword("short", "short"); err == nil {
		t.Fatal("short passwords are refused")
	}
	if err := CheckNewPassword("password1", "password2"); err == nil {
		t.Fatal("different passwords are refused")
	}
	port := SuggestPort(dir)
	for _, low := range []int{80, 8080, 49151} {
		if _, err := CreateGuild(dir, "password1", "", low); err == nil {
			t.Fatalf("port %d is refused: only 49152 to 65535", low)
		}
	}
	e, err := CreateGuild(dir, "password1", "", port)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateGuild(dir, "password1", "", port); err == nil || !strings.Contains(err.Error(), "already uses port") {
		t.Fatalf("a port used twice: %v", err)
	}
	list, _ := Guilds(dir)
	if len(list) != 1 || list[0].Port != port || list[0].Running {
		t.Fatalf("guilds: %+v", list)
	}

	if err := ChangeGuildPassword(dir, e.GuildID, "wrong-pass", "password2"); err == nil {
		t.Fatal("the current password is checked")
	}
	if err := ChangeGuildPassword(dir, e.GuildID, "password1", "password2"); err != nil {
		t.Fatal(err)
	}
	if err := CheckGuildPassword(dir, e.GuildID, "password2"); err != nil {
		t.Fatal(err)
	}
	if err := SetGuildPort(dir, e.GuildID, port+1); err != nil {
		t.Fatal(err)
	}

	name, err := MakeBackup(dir, e.GuildID)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := Backups(dir, e.GuildID); len(b) != 1 || b[0].Name != name {
		t.Fatalf("backups: %+v", b)
	}
	if err := RestoreBackup(dir, e.GuildID, "password2", name); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	file, err := ExportGuild(dir, e.GuildID, "password2", out)
	if err != nil || filepath.Ext(file) != ".nexus" {
		t.Fatalf("export: %s %v", file, err)
	}
	if err := DeleteGuild(dir, e.GuildID, "wrong-pass", true, true); err == nil {
		t.Fatal("deleting needs the password")
	}
	if err := DeleteGuild(dir, e.GuildID, "password2", true, true); err != nil {
		t.Fatal(err)
	}
	if list, _ := Guilds(dir); len(list) != 0 {
		t.Fatal("the guild should be gone")
	}

	if _, err := ImportGuild(dir, file, "wrong-pass"); err == nil {
		t.Fatal("importing needs the export's password")
	}
	back, err := ImportGuild(dir, file, "password2")
	if err != nil || back.GuildID != e.GuildID {
		t.Fatalf("import: %+v %v", back, err)
	}
}
