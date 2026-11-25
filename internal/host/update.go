package host

// Updating. "guild-forge update" and the Check
// Updates pages of GuildForge and the Nexus app ask the project's GitHub
// releases for a newer version, download this program's own file and put it
// in place of the running program. Each program updates only itself. Guilds
// that were running are stopped first and started again on the new version.
// Downloads are not checked yet: signing comes later.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"nexus/internal/guild"
	"nexus/internal/platform"
)

// ReleaseRepo is the GitHub repository whose releases update reads, as
// "owner/name".
const ReleaseRepo = "vibe-atelier/guild-forge"

// releasesAPI is GitHub's address for a repository's newest release. The
// GUILD_FORGE_RELEASES setting replaces it, so tests can use a pretend
// GitHub on this computer.
func releasesAPI() string {
	if u := os.Getenv("GUILD_FORGE_RELEASES"); u != "" {
		return u
	}
	return "https://api.github.com/repos/" + ReleaseRepo + "/releases/latest"
}

// CommandFile is the release file of guild-forge, the command program, for
// this system. The name only tells people which file to download:
// it is typed as guild-forge, and an update keeps the file's own name.
func CommandFile() string {
	if runtime.GOOS == "windows" {
		return "guild-forge_Windows-cmd.exe"
	}
	return "guild-forge_Debian"
}

// WindowFile is the release file of GuildForge's window, for Windows only.
const WindowFile = "GuildForge-Windows_GUI.exe"

// AppFile is the release file of the Nexus app, which shares
// GuildForge's releases and version number. Only Windows has one: on Linux
// the app is built from the source code, so AppFile is empty there.
func AppFile() string {
	if runtime.GOOS == "windows" {
		return "nexus_chat-client_Windows.exe"
	}
	return ""
}

// UpdateApp brings the running Nexus app up to date from GitHub. Unlike
// Update, it has no guilds to stop.
func UpdateApp(out io.Writer) error {
	if AppFile() == "" {
		return errors.New("Nexus for this system is built from the source code, so it is updated by building it again")
	}
	exe, err := platform.ProgramPath()
	if err != nil {
		return err
	}
	return updateProgram(out, "", exe, AppFile(), "Nexus")
}

// release is the part of GitHub's answer that update needs.
type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// LatestVersion asks GitHub for the newest release's version, and whether
// it is newer than this program.
func LatestVersion() (latest string, newer bool, err error) {
	rel, err := latestRelease()
	if err != nil {
		return "", false, err
	}
	latest = strings.TrimPrefix(rel.Tag, "v")
	return latest, newerVersion(latest, Version), nil
}

// Update brings the running program up to date from GitHub. file is its
// release file (CommandFile or WindowFile), and name is how it is called in
// what Update reports to out.
func Update(out io.Writer, configDir, file, name string) error {
	exe, err := platform.ProgramPath()
	if err != nil {
		return err
	}
	return updateProgram(out, configDir, exe, file, name)
}

// updateProgram does Update's work on the program file exe, which tests
// replace with a stand-in. An empty configDir means the program runs no
// guilds, as for the app.
func updateProgram(out io.Writer, configDir, exe, file, name string) error {
	fmt.Fprintf(out, "Checking GitHub for a newer %s...\n", name)
	rel, err := latestRelease()
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(rel.Tag, "v")
	if !newerVersion(latest, Version) {
		fmt.Fprintf(out, "%s %s is up to date.\n", name, Version)
		return nil
	}
	url := ""
	for _, a := range rel.Assets {
		if a.Name == file {
			url = a.URL
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no %s", rel.Tag, file)
	}

	fmt.Fprintf(out, "Downloading %s %s...\n", name, latest)
	fresh := exe + ".new"
	if err := download(url, fresh); err != nil {
		os.Remove(fresh)
		return err
	}

	// Running guilds use the program file, so they are stopped while it is
	// replaced, and started again afterwards on the new version.
	var running []Entry
	if configDir != "" {
		reg, err := OpenRegistry(configDir, io.Discard)
		if err != nil {
			os.Remove(fresh)
			return err
		}
		for _, e := range reg.Guilds {
			if _, on := guild.RunningStatus(e.DataDir); on {
				running = append(running, e)
			}
		}
	}
	for _, e := range running {
		fmt.Fprintf(out, "Stopping GUILD#%d...\n", e.Index)
		stopEntry(io.Discard, &e)
	}

	err = replaceProgram(exe, fresh)
	if err != nil {
		os.Remove(fresh)
	}
	// Guilds start from the program's file, which now holds the new version
	// (or still the old one, when replacing failed).
	for _, e := range running {
		if serr := startEntry(io.Discard, configDir, &e); serr != nil {
			fmt.Fprintf(out, "GUILD#%d did not start again: %v\n", e.Index, serr)
		} else {
			fmt.Fprintf(out, "GUILD#%d is running again.\n", e.Index)
		}
	}
	if err != nil {
		return fmt.Errorf("could not replace %s: %w", exe, err)
	}
	fmt.Fprintf(out, "%s is updated from %s to %s.\n", name, Version, latest)
	return nil
}

// latestRelease asks GitHub for the repository's newest release.
func latestRelease() (*release, error) {
	req, err := http.NewRequest("GET", releasesAPI(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "guild-forge/"+Version)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("there is no release on GitHub yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("GitHub's answer could not be read: %w", err)
	}
	return &rel, nil
}

// download saves url into path, ready to run.
func download(url, path string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "guild-forge/"+Version)
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("the download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the download failed: %s", resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("the download failed: %w", err)
	}
	return f.Close()
}

// replaceProgram puts fresh in place of exe. Windows won't overwrite a
// running program, but it lets one be renamed, so there the old file is set
// aside as exe.old and removed the next time guild-forge runs.
func replaceProgram(exe, fresh string) error {
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(fresh, exe); err != nil {
			os.Rename(old, exe)
			return err
		}
		return nil
	}
	return os.Rename(fresh, exe)
}

// RemoveOldProgram deletes the copy update set aside on Windows, once the
// new program runs.
func RemoveOldProgram() {
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

// newerVersion reports whether version a is newer than b. Both look like
// 1.2.3; anything after a dash (as in 0.1.0-dev) marks a version made before
// the release of the same number.
func newerVersion(a, b string) bool {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	for i := range an {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	return bpre && !apre
}

// splitVersion turns "1.2.3-dev" into its three numbers and whether it has
// a part after a dash.
func splitVersion(v string) (nums [3]int, pre bool) {
	v, rest, found := strings.Cut(v, "-")
	pre = found && rest != ""
	for i, part := range strings.SplitN(v, ".", 3) {
		nums[i], _ = strconv.Atoi(part)
	}
	return nums, pre
}
