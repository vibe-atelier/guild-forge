package ui

// This file shows pop-up notifications and plays sounds.

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"

	"nexus/internal/guildconn"
)

// watchFocus remembers whether the Nexus window is in front.
func (u *UI) watchFocus() {
	u.focused = true
	lc := u.app.Lifecycle()
	lc.SetOnEnteredForeground(func() { u.focused = true })
	lc.SetOnExitedForeground(func() { u.focused = false })
}

// onNotice shows a pop-up and plays a sound for a notice (any goroutine).
func (u *UI) onNotice(n guildconn.Notice) {
	fyne.Do(func() {
		if u.user == nil {
			return
		}
		looking := u.focused && ((n.Kind == "dm" && u.selected == "dm:"+n.Channel) ||
			(n.Kind != "dm" && u.selected == n.Guild && u.chat != nil && u.chat.channel == n.Channel))
		if looking {
			return
		}
		// There is at most one sound a second, so a busy channel isn't a drum roll.
		if !u.user.Profile.MuteSounds && time.Since(u.lastSound) > time.Second {
			u.lastSound = time.Now()
			kind := "message"
			if n.Kind == "mention" || n.Kind == "reply" {
				kind = "mention"
			}
			if path := u.soundFile(kind); path != "" {
				go playFile(path)
			}
		}
		if u.focused {
			return // The window is in front, so the bold channel is enough.
		}
		title := n.Name
		switch n.Kind {
		case "dm":
		case "mention":
			title = n.Name + " mentioned you in " + n.Where + " (" + n.GuildName + ")"
		case "reply":
			title = n.Name + " replied in " + n.Where + " (" + n.GuildName + ")"
		default:
			title = n.Name + " in " + n.Where + " (" + n.GuildName + ")"
		}
		u.app.SendNotification(fyne.NewNotification(title, n.Text))
	})
}

// soundFile returns a built-in sound, made the first time it's needed.
func (u *UI) soundFile(kind string) string {
	path := filepath.Join(u.ConfigDir, "sounds", kind+".wav")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	notes := []float64{880, 1320} // A message gets a soft two-note chime.
	if kind == "mention" {
		notes = []float64{988, 1319, 1760} // A mention gets three notes, brighter.
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	if os.WriteFile(path, chime(notes), 0o600) != nil {
		return ""
	}
	return path
}

// chime makes a short .wav: each note 90 ms, fading out.
func chime(notes []float64) []byte {
	const rate = 22050
	noteLen := 0.09
	var samples []int16
	for i, f := range notes {
		n := int(rate * noteLen)
		if i == len(notes)-1 {
			n *= 3 // The last note rings longer.
		}
		for s := 0; s < n; s++ {
			t := float64(s) / rate
			fade := math.Exp(-t * 18 / float64(1+2*boolInt(i == len(notes)-1)))
			attack := math.Min(1, float64(s)/(rate*0.004))
			v := math.Sin(2*math.Pi*f*t) * fade * attack * 0.35
			samples = append(samples, int16(v*32767))
		}
	}
	var b bytes.Buffer
	size := len(samples) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+size))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(size))
	binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
