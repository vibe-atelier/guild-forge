package ui

import (
	"testing"

	"nexus/internal/guildconn"
)

func TestParseTransfer(t *testing.T) {
	g := guildconn.GuildView{Members: []guildconn.MemberView{
		{UserKey: "ME", Name: "Alice", IsMe: true},
		{UserKey: "BEN", Name: "Ben"},
		{UserKey: "BENJ", Name: "Ben Jones", Nick: "BJ"},
	}}
	for _, c := range []struct {
		in   string
		key  string
		n    int64
		fail bool
	}{
		{"@Ben 50", "BEN", 50, false},
		{"@ben jones 1,250", "BENJ", 1250, false},
		{"@BJ 7", "BENJ", 7, false},
		{"@Alice 5", "", 0, true}, // You can't pay yourself.
		{"Ben 5", "", 0, true},    // It has no @.
		{"@Ben -5", "", 0, true},
		{"@Ben lots", "", 0, true},
	} {
		key, n, err := parseTransfer(g, c.in)
		if (err != nil) != c.fail || key != c.key || n != c.n {
			t.Errorf("%q: got %q %d %v", c.in, key, n, err)
		}
	}
	if s := groupDigits(1234567); s != "1,234,567" {
		t.Errorf("groupDigits: %s", s)
	}
}
