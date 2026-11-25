//go:build windows

package platform

import "testing"

func TestTaskProgram(t *testing.T) {
	cases := map[string]string{
		`<Arguments>--headless "C:\Tools\GuildForge.exe" boot</Arguments>`:              `C:\Tools\GuildForge.exe`,
		`<Arguments>--headless &quot;C:\A B\guild-forge-cmd.exe&quot; boot</Arguments>`: `C:\A B\guild-forge-cmd.exe`,
		`<Command>something else</Command>`:                                             "",
	}
	for xml, want := range cases {
		if got := taskProgram(xml); got != want {
			t.Errorf("taskProgram(%q) = %q, want %q", xml, got, want)
		}
	}
}
