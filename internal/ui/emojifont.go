package ui

// Newer emoji: Google's Noto Color Emoji, the PNG version from its
// noto-emoji project (SIL Open Font License 1.1, see fonts/OFL.txt), takes
// the place of Fyne's own emoji font, which lacks emoji made after 2017.
// Fyne has no setting for its emoji font, so it is set before any text is
// drawn. If the font file isn't there, Fyne's own is kept.

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	_ "unsafe" // This import is needed for go:linkname.

	"fyne.io/fyne/v2"

	"github.com/go-text/typesetting/font"
	"nexus/internal/nexusdata"
)

//go:linkname fyneEmojiFont fyne.io/fyne/v2/theme.emoji
var fyneEmojiFont *fyne.StaticResource

const emojiFontFile = "NotoColorEmoji.ttf"

// useEmojiFont looks for the font in this PC's nexus-data (where the program
// writes its built-in copy), then next to the program (fonts/), then
// in the folder it was started from, and uses the first it finds.
func useEmojiFont() {
	places := []string{filepath.Join(nexusdata.Path(nexusdata.FontsPart), emojiFontFile)}
	if exe, err := os.Executable(); err == nil {
		places = append(places, filepath.Join(filepath.Dir(exe), "fonts", emojiFontFile))
	}
	places = append(places, filepath.Join("fonts", emojiFontFile), emojiFontFile)
	for _, p := range places {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if fixed, err := narrowEmojiSpace(data); err == nil {
			fyneEmojiFont = &fyne.StaticResource{StaticName: emojiFontFile, StaticContent: fixed}
			return
		}
	}
}

// narrowEmojiSpace gives the font's space an ordinary width. It has an
// emoji-wide one, which text after an emoji would otherwise use. Only this
// copy in memory changes, not the file.
func narrowEmojiSpace(data []byte) ([]byte, error) {
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	gid, ok := face.NominalGlyph(' ')
	if !ok {
		return data, nil
	}
	out := append([]byte(nil), data...)
	n := int(binary.BigEndian.Uint16(out[4:]))
	var hmtx, hhea, head []byte
	for i := 0; i < n; i++ {
		e := out[12+16*i:]
		off, l := binary.BigEndian.Uint32(e[8:]), binary.BigEndian.Uint32(e[12:])
		if int(off+l) > len(out) {
			continue
		}
		switch string(e[:4]) {
		case "hmtx":
			hmtx = out[off : off+l]
		case "hhea":
			hhea = out[off : off+l]
		case "head":
			head = out[off : off+l]
		}
	}
	if hmtx == nil || len(hhea) < 36 || len(head) < 20 {
		return data, nil
	}
	long := int(binary.BigEndian.Uint16(hhea[34:]))
	if int(gid) >= long || 4*int(gid)+2 > len(hmtx) {
		return data, nil
	}
	em := binary.BigEndian.Uint16(head[18:])
	binary.BigEndian.PutUint16(hmtx[4*int(gid):], em*27/100) // This is about the width of a normal space.
	return out, nil
}
