//go:build !windows

package ui

import "os/exec"

// playFile asks Linux to play a .wav file (PulseAudio/PipeWire, or ALSA).
func playFile(path string) {
	for _, p := range []string{"paplay", "pw-play", "aplay"} {
		if bin, err := exec.LookPath(p); err == nil {
			args := []string{path}
			if p == "aplay" {
				args = []string{"-q", path}
			}
			exec.Command(bin, args...).Start()
			return
		}
	}
}
