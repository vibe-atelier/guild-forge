package host

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", false},
		{"1.0.0", "0.12.0", true},
		{"0.10.0", "0.9.0", true},
		{"0.1.0", "0.1.0-dev", true},
		{"0.1.0-dev", "0.1.0", false},
		{"0.1.1", "0.1.0", true},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// pretendGitHub answers like GitHub's releases address, offering tag with
// this system's command file, whose content is body.
func pretendGitHub(t *testing.T, tag, body string) {
	t.Helper()
	pretendGitHubFile(t, tag, CommandFile(), body)
}

// pretendGitHubFile is pretendGitHub for the release file named file.
func pretendGitHubFile(t *testing.T, tag, file, body string) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file" {
			io.WriteString(w, body)
			return
		}
		fmt.Fprintf(w, `{"tag_name": %q, "assets": [{"name": %q, "browser_download_url": %q}]}`,
			tag, file, srv.URL+"/file")
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GUILD_FORGE_RELEASES", srv.URL+"/latest")
}

func TestUpdateReplacesProgram(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, CommandFile())
	os.WriteFile(exe, []byte("old program"), 0o755)
	pretendGitHub(t, "v99.0.0", "new program")

	var out strings.Builder
	if err := updateProgram(&out, t.TempDir(), exe, CommandFile(), "guild-forge"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new program" {
		t.Fatalf("the program holds %q after the update", got)
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Error("the downloaded file was left behind")
	}
	if !strings.Contains(out.String(), "updated from "+Version+" to 99.0.0") {
		t.Errorf("the update said: %s", out.String())
	}
}

func TestUpdateUpToDate(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, CommandFile())
	os.WriteFile(exe, []byte("old program"), 0o755)
	pretendGitHub(t, "v"+Version, "new program")

	var out strings.Builder
	if err := updateProgram(&out, t.TempDir(), exe, CommandFile(), "guild-forge"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old program" {
		t.Fatal("an up-to-date program was replaced")
	}
	if !strings.Contains(out.String(), "is up to date") {
		t.Errorf("the update said: %s", out.String())
	}
}

// TestUpdateAppNeedsNoGuilds checks that the app's update (an empty
// configDir) replaces the program without looking for guilds.
func TestUpdateAppNeedsNoGuilds(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "nexus_chat-client_Windows.exe")
	os.WriteFile(exe, []byte("old app"), 0o755)
	pretendGitHubFile(t, "v99.0.0", "nexus_chat-client_Windows.exe", "new app")

	var out strings.Builder
	if err := updateProgram(&out, "", exe, "nexus_chat-client_Windows.exe", "Nexus"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new app" {
		t.Fatalf("the app holds %q after the update", got)
	}
	if strings.Contains(out.String(), "GUILD") {
		t.Errorf("the app's update mentioned guilds: %s", out.String())
	}
}
