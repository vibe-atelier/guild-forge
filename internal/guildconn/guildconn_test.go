package guildconn

import (
	"path/filepath"
	"testing"

	"nexus/internal/user"
)

func TestCheckAddress(t *testing.T) {
	ok := map[string]string{
		" 192.168.1.20:50000 ":   "192.168.1.20:50000",
		"nexus://host.lan:50001": "host.lan:50001",
	}
	for in, want := range ok {
		if got, err := CheckAddress(in); err != nil || got != want {
			t.Errorf("CheckAddress(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"192.168.1.20", ":50000", "host:abc", "host:70000"} {
		if _, err := CheckAddress(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestGuildListAndServerCount(t *testing.T) {
	u, err := user.Create(filepath.Join(t.TempDir(), "u"), "Una", "goodpassword")
	if err != nil {
		t.Fatal(err)
	}
	u.SaveGuild(user.Guild{GuildID: "A", GuildName: "One", GuildAddress: "h:1"})
	u.SaveGuild(user.Guild{GuildID: "B", GuildName: "Two", GuildAddress: "h:2"})
	u.SaveGuild(user.Guild{GuildID: "A", GuildName: "One renamed", GuildAddress: "h:1"})
	list, _ := u.Guilds()
	if len(list) != 2 || list[0].GuildName != "One renamed" || u.Profile.ServerCount != 2 {
		t.Fatalf("got %+v, count %d", list, u.Profile.ServerCount)
	}
	u.RemoveGuild("A")
	if p, _ := user.ReadProfile(u.Dir); p.ServerCount != 1 {
		t.Fatalf("server_count should follow the list, got %d", p.ServerCount)
	}
}
