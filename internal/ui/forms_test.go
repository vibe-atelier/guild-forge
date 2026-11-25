package ui

import "testing"

func TestJoinAddress(t *testing.T) {
	for _, c := range []struct{ link, port, want string }{
		{"192.168.1.20", "50000", "192.168.1.20:50000"},
		{" play.example.com ", " 6000 ", "play.example.com:6000"},
		{"192.168.1.20:50001", "50000", "192.168.1.20:50001"}, // A port after a colon wins.
		{"nexus://example.com/", "50000", "example.com:50000"},
		{"192.168.1.20", "", "192.168.1.20"}, // The check then asks for a port.
		{"", "50000", ""},
		// IPv6: the address's own colons are not a port.
		{"2401:4900:1c2a::5", "7000", "[2401:4900:1c2a::5]:7000"},
		{"[2401:4900:1c2a::5]", "7000", "[2401:4900:1c2a::5]:7000"},
		{"[2401:4900:1c2a::5]:7001", "7000", "[2401:4900:1c2a::5]:7001"},
		{"::1", "7000", "[::1]:7000"},
	} {
		if got := joinAddress(c.link, c.port); got != c.want {
			t.Errorf("joinAddress(%q, %q) = %q, want %q", c.link, c.port, got, c.want)
		}
	}
}

func TestShortID(t *testing.T) {
	id := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567ABCDEFGHIJKLMNOPWXYZ"
	if got := shortID(id); got != "ABCDEFGH_WXYZ" {
		t.Errorf("shortID = %q", got)
	}
	if got := shortID("SHORT"); got != "SHORT" {
		t.Errorf("short IDs stay whole: %q", got)
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{"saved": "Saved.", "Done!": "Done!", "Moving…": "Moving…", "": ""} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}
