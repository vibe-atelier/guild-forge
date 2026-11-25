// Command notices writes the third-party notices for Nexus's programs: the
// licence of every outside library built into them, as each licence asks. Go
// itself lists what is built in, so the file always matches the build. The
// build script uses it for GuildForge's Licenses page, and the release script
// for THIRD-PARTY-NOTICES.txt.
//
//	go run ./tools/notices -o FILE [-goos windows] [-cgo 1] [-tags T] -title NAME PACKAGE...
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// cLibraries are the C libraries copied into Nexus, by the
// package that builds each one.
var cLibraries = []struct {
	pkg, title string
	files      []string
}{
	{"nexus/internal/opus", "Opus 1.6.1 (Voice)", []string{"internal/opus/libopus/COPYING"}},
	{"nexus/internal/vp8", "libvpx 1.17.0 (VP8 Video)", []string{"internal/vp8/libvpx/LICENSE", "internal/vp8/libvpx/PATENTS"}},
	{"nexus/internal/speexdsp", "SpeexDSP 1.2.1 (Echo Cancellation)", []string{"internal/speexdsp/libspeexdsp/COPYING"}},
	{"nexus/internal/rnnoise", "RNNoise 0.2 (Noise Suppression and Its Model)", []string{"internal/rnnoise/csrc/COPYING"}},
}

var licenceName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|patents)`)

func main() {
	out := flag.String("o", "", "the file to write")
	goos := flag.String("goos", "", "the system the programs are built for")
	cgo := flag.String("cgo", "", "1 when the programs are built with C")
	tags := flag.String("tags", "", "the build tags the programs are built with")
	title := flag.String("title", "Nexus", "the name at the top of the file")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fail("usage: notices -o FILE [-goos G] [-cgo 1] [-tags T] -title NAME PACKAGE...")
	}

	env := os.Environ()
	if *goos != "" {
		env = append(env, "GOOS="+*goos)
	}
	if *cgo != "" {
		env = append(env, "CGO_ENABLED="+*cgo)
	}
	args := []string{"list", "-deps", "-f", "{{.ImportPath}}|{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}"}
	if *tags != "" {
		args = append(args, "-tags", *tags)
	}
	list, err := goCommand(env, append(args, flag.Args()...)...)
	if err != nil {
		fail("go list: %v", err)
	}

	packages := map[string]bool{}
	modules := map[string][3]string{}
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		packages[parts[0]] = true
		if len(parts) == 4 && parts[1] != "nexus" {
			modules[parts[1]] = [3]string{parts[1], parts[2], parts[3]}
		}
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, "%s: Third-Party Notices\n\n", *title)
	// Two names together ("A and B") take "are".
	verb := "is"
	if strings.Contains(*title, " and ") {
		verb = "are"
	}
	fmt.Fprintf(&b, "%s %s built with the software below. Each notice is\nreproduced as its licence asks.\n\n", *title, verb)

	goRoot, _ := goCommand(env, "env", "GOROOT")
	goVersion, _ := goCommand(env, "env", "GOVERSION")
	section(&b, fmt.Sprintf("The Go Programming Language (%s)", strings.TrimSpace(goVersion)),
		filepath.Join(strings.TrimSpace(goRoot), "LICENSE"), filepath.Join(strings.TrimSpace(goRoot), "PATENTS"))
	for _, c := range cLibraries {
		if packages[c.pkg] {
			section(&b, c.title, c.files...)
		}
	}
	if strings.Contains(*tags, "assetfont") && packages["nexus/internal/assets"] {
		section(&b, "Noto Color Emoji (Emoji Font)", "fonts/OFL.txt")
	}

	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	for _, name := range names {
		m := modules[name]
		found := licenceFiles(m[2])
		if len(found) == 0 {
			fail("no licence file found for %s", name)
		}
		section(&b, m[0]+" "+m[1], found...)
		// GLFW's own C code, inside the Go library, has its own licence.
		if strings.HasPrefix(name, "github.com/go-gl/glfw/") {
			if p := filepath.Join(m[2], "glfw", "LICENSE.md"); exists(p) {
				section(&b, "GLFW (Window Code, Inside github.com/go-gl/glfw)", p)
			}
		}
	}
	if err := os.WriteFile(*out, b.Bytes(), 0o644); err != nil {
		fail("%v", err)
	}
}

// section adds one notice: a title between lines, then each file's text.
func section(b *bytes.Buffer, title string, files ...string) {
	line := strings.Repeat("=", 78)
	fmt.Fprintf(b, "%s\n%s\n%s\n\n", line, title, line)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fail("%v", err)
		}
		b.WriteString(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n \t"))
		b.WriteString("\n\n")
	}
}

// licenceFiles lists a module's licence files, in name order.
func licenceFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() && licenceName.MatchString(e.Name()) {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	return found
}

// goCommand runs the go tool with env and returns what it printed.
func goCommand(env []string, args ...string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// fail says what went wrong and stops.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "notices: "+format+"\n", args...)
	os.Exit(1)
}
