// Command make-icon gives the Windows programs their icon and details. It draws
// the Nexus logo (internal/ui/logo.svg) at the sizes Windows uses into an .ico
// file, writes a resource description with the product name, version,
// description and copyright, and has MSYS2's windres turn both into a resource
// file (.syso) next to each program's main.go, which Go then builds in by
// itself.
//
// The .syso files are kept in the project, so building Nexus doesn't need
// windres; run this again after changing the logo or a version:
//
//	go run ./tools/make-icon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
)

// The copyright holder shown in Properties → Details.
const holder = "vibe-atelier"

// program is one Windows program that gets the icon and details. Its
// version is read from versionFile.
type program struct {
	dir, exe, name, description, versionFile string
}

var programs = []program{
	{"cmd/nexus", "nexus_chat-client_Windows.exe", "Nexus", "Nexus, the decentralised chat app", "internal/host/version.go"},
	{"cmd/guild-forge-window", "GuildForge-Windows_GUI.exe", "GuildForge", "GuildForge, the Nexus guild host", "internal/host/version.go"},
	{"cmd/guild-forge-cmd", "guild-forge_Windows-cmd.exe", "guild-forge", "guild-forge, the Nexus guild host's commands", "internal/host/version.go"},
}

// sizes are the icon sizes Windows picks from (in pixels).
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	svg, err := os.ReadFile("internal/ui/logo.svg")
	if err != nil {
		fail("run this from the project folder: %v", err)
	}
	ico, err := makeICO(svg)
	if err != nil {
		fail("could not draw the logo: %v", err)
	}
	windres := findWindres()
	for _, p := range programs {
		icoPath := filepath.Join(p.dir, "icon.ico")
		if err := os.WriteFile(icoPath, ico, 0o644); err != nil {
			fail("%v", err)
		}
		version := readVersion(p.versionFile)
		rc := filepath.Join(p.dir, "resource.rc")
		if err := os.WriteFile(rc, []byte(resourceText(p, version)), 0o644); err != nil {
			fail("%v", err)
		}
		syso := filepath.Join(p.dir, "resource_windows_amd64.syso")
		cmd := exec.Command(windres, "-i", "resource.rc", "-o", "resource_windows_amd64.syso", "-O", "coff", "-F", "pe-x86-64")
		cmd.Dir = p.dir // The .rc file names the icon relative to its own folder.
		if out, err := cmd.CombinedOutput(); err != nil {
			fail("windres failed for %s: %v\n%s", p.exe, err, out)
		}
		fmt.Printf("Made %s (%s %s)\n", syso, p.name, version)
	}
}

// makeICO draws the logo at every size and packs the pictures into one .ico
// file, each kept as PNG (which Windows has read inside .ico files since
// Windows Vista).
func makeICO(svg []byte) ([]byte, error) {
	var pngs [][]byte
	for _, s := range sizes {
		icon, err := oksvg.ReadIconStream(bytes.NewReader(svg))
		if err != nil {
			return nil, err
		}
		icon.SetTarget(0, 0, float64(s), float64(s))
		img := image.NewRGBA(image.Rect(0, 0, s, s))
		icon.Draw(rasterx.NewDasher(s, s, rasterx.NewScannerGV(s, s, img, img.Bounds())), 1)
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			return nil, err
		}
		pngs = append(pngs, b.Bytes())
	}
	var out bytes.Buffer
	// The header: reserved, type 1 (icon), and the number of pictures.
	binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(pngs))})
	offset := 6 + 16*len(pngs)
	for i, p := range pngs {
		side := byte(sizes[i])
		if sizes[i] >= 256 {
			side = 0 // In an .ico file, 0 means 256.
		}
		// Each entry holds the width, height, colour count and a reserved
		// byte, then the planes and bits per pixel, then the picture's
		// length and where it starts.
		out.Write([]byte{side, side, 0, 0})
		binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32})
		binary.Write(&out, binary.LittleEndian, [2]uint32{uint32(len(p)), uint32(offset)})
		offset += len(p)
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes(), nil
}

// readVersion finds `const version = "..."` (or `Version`) in a Go file.
func readVersion(path string) string {
	src, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	m := regexp.MustCompile(`const [vV]ersion = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		fail("no version in %s", path)
	}
	return string(m[1])
}

// resourceText is the resource description windres reads: the icon, and the
// details Windows shows in Properties → Details.
func resourceText(p program, version string) string {
	nums := regexp.MustCompile(`\d+`).FindAllString(version, 3)
	for len(nums) < 3 {
		nums = append(nums, "0")
	}
	quad := strings.Join(nums, ",") + ",0"
	return fmt.Sprintf(`// Made by tools/make-icon; run it again rather than editing this.
1 ICON "icon.ico"
1 VERSIONINFO
FILEVERSION %[1]s
PRODUCTVERSION %[1]s
FILEOS 0x40004
FILETYPE 0x1
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904B0"
    BEGIN
      VALUE "CompanyName", "%[2]s"
      VALUE "FileDescription", "%[3]s"
      VALUE "FileVersion", "%[4]s"
      VALUE "InternalName", "%[5]s"
      VALUE "LegalCopyright", "Copyright (C) 2026 %[2]s"
      VALUE "OriginalFilename", "%[6]s"
      VALUE "ProductName", "%[5]s"
      VALUE "ProductVersion", "%[4]s"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x409, 1200
  END
END
`, quad, holder, p.description, version, p.name, p.exe)
}

// findWindres finds MSYS2's windres, on PATH or in its usual place.
func findWindres() string {
	if p, err := exec.LookPath("windres"); err == nil {
		return p
	}
	p := `C:\msys64\mingw64\bin\windres.exe`
	if _, err := os.Stat(p); err == nil {
		return p
	}
	fail("windres was not found; install MSYS2's toolchain (see build.ps1)")
	return ""
}

// fail says what went wrong and stops.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "make-icon: "+format+"\n", args...)
	os.Exit(1)
}
