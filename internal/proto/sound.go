package proto

import "errors"

// Soundboard sounds are kept and sent as their 20 ms Opus packets,
// one after another, each with its length in front (two bytes, big end
// first). The guild checks the shape without being able to play them.

const (
	MaxSoundMillis  = 10_000
	MaxSoundPackets = MaxSoundMillis / 20
	MaxSoundBytes   = 160 << 10
	MaxSounds       = 25 // This fills a 5 × 5 board.
)

// PackSound joins Opus packets into one sound.
func PackSound(packets [][]byte) []byte {
	var out []byte
	for _, p := range packets {
		out = append(out, byte(len(p)>>8), byte(len(p)))
		out = append(out, p...)
	}
	return out
}

// UnpackSound splits a sound into its Opus packets.
func UnpackSound(data []byte) ([][]byte, error) {
	var out [][]byte
	for len(data) > 0 {
		if len(data) < 2 {
			return nil, errors.New("that sound is damaged")
		}
		n := int(data[0])<<8 | int(data[1])
		data = data[2:]
		if n == 0 || n > len(data) || n > 1500 {
			return nil, errors.New("that sound is damaged")
		}
		out = append(out, data[:n])
		data = data[n:]
		if len(out) > MaxSoundPackets {
			return nil, errors.New("sounds can be at most 10 seconds")
		}
	}
	if len(out) == 0 {
		return nil, errors.New("that sound is empty")
	}
	return out, nil
}
