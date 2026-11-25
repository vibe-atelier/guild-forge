package host

import (
	"os"
	"testing"
	"time"
)

// TestAutoBackups checks due dates, that the last 10 are kept, and that hand-made backups are left alone.
func TestAutoBackups(t *testing.T) {
	dir := t.TempDir()
	e, err := CreateGuild(dir, "password1", "", SuggestPort(dir))
	if err != nil {
		t.Fatal(err)
	}
	id := e.GuildID
	if err := SetAutoBackups(dir, id, "hourly"); err == nil {
		t.Fatal("only daily, weekly, monthly or off")
	}
	// While they are off, nothing is made.
	if name, err := autoBackupDue(dir, id, nil, time.Now()); err != nil || name != "" {
		t.Fatalf("off made %q, %v", name, err)
	}
	hand, err := MakeBackup(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetAutoBackups(dir, id, "Weekly"); err != nil {
		t.Fatal(err)
	}
	if g, _ := Guilds(dir); g[0].AutoBackups != "weekly" {
		t.Fatalf("saved as %q", g[0].AutoBackups)
	}
	now := time.Now()
	first, err := autoBackupDue(dir, id, nil, now)
	if err != nil || first == "" {
		t.Fatalf("the first is due at once: %q, %v", first, err)
	}
	if name, _ := autoBackupDue(dir, id, nil, now.Add(6*24*time.Hour)); name != "" {
		t.Fatal("weekly: not due again after 6 days")
	}
	if name, _ := autoBackupDue(dir, id, nil, now.Add(8*24*time.Hour)); name == "" {
		t.Fatal("weekly: due after 8 days")
	}
	// Make 13 more, each one dated later than the last.
	for i := 0; i < 13; i++ {
		list, _ := listBackups(dir, id)
		for _, b := range list { // Age the files, as if they were made weeks apart.
			if b.Auto {
				st, _ := os.Stat(b.Path)
				old := st.ModTime().Add(-8 * 24 * time.Hour)
				os.Chtimes(b.Path, old, old)
			}
		}
		if name, err := autoBackupDue(dir, id, nil, time.Now()); err != nil || name == "" {
			t.Fatalf("round %d: %q, %v", i, name, err)
		}
	}
	list, _ := listBackups(dir, id)
	auto, byHand := 0, 0
	for _, b := range list {
		if b.Auto {
			auto++
		} else {
			byHand++
		}
	}
	if auto != KeepAutoBackups || byHand != 1 {
		t.Fatalf("kept %d automatic and %d by hand, want %d and 1", auto, byHand, KeepAutoBackups)
	}
	if err := DeleteBackup(dir, id, hand); err != nil {
		t.Fatal(err)
	}
	if err := DeleteBackup(dir, id, hand); err == nil {
		t.Fatal("a deleted backup is gone")
	}
	if err := SetAutoBackups(dir, id, "off"); err != nil {
		t.Fatal(err)
	}
	if g, _ := Guilds(dir); g[0].AutoBackups != "" {
		t.Fatal("off is saved as empty")
	}
}
