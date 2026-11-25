package voice

// Soundboard files: a WAV or Ogg Opus file is read, made mono at
// 48,000 samples a second, and squeezed into 20 ms Opus packets, which is
// what the guild keeps and sends. Both formats are read here, with Go's
// own tools and the Opus library Nexus already has.

import (
	"encoding/binary"
	"errors"
	"math"

	"nexus/internal/opus"
	"nexus/internal/proto"
	"nexus/internal/sound"
)

var (
	errSoundFormat = errors.New("use a WAV or Ogg Opus file")
	errSoundBroken = errors.New("that sound file is damaged")
	errSoundLong   = errors.New("sounds can be at most 10 seconds")
)

// LoadSound turns a WAV or Ogg Opus file into a soundboard sound: the
// packed Opus packets (proto.PackSound) and how long it lasts.
func LoadSound(data []byte) (packed []byte, millis int, err error) {
	var pcm []float32
	switch {
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		var rate int
		if pcm, rate, err = readWAV(data); err != nil {
			return nil, 0, err
		}
		pcm = resample(pcm, rate, sound.Rate)
	case len(data) > 4 && string(data[:4]) == "OggS":
		if pcm, err = readOggOpus(data); err != nil {
			return nil, 0, err
		}
	default:
		return nil, 0, errSoundFormat
	}
	if len(pcm) == 0 {
		return nil, 0, errors.New("that sound is empty")
	}
	frames := (len(pcm) + sound.Frame - 1) / sound.Frame
	if frames > proto.MaxSoundPackets {
		return nil, 0, errSoundLong
	}
	enc, err := opus.NewEncoder(64000)
	if err != nil {
		return nil, 0, err
	}
	defer enc.Close()
	packets := make([][]byte, 0, frames)
	for f := 0; f < frames; f++ {
		frame := make([]int16, sound.Frame)
		for i := range frame {
			if j := f*sound.Frame + i; j < len(pcm) {
				frame[i] = int16(max(-32768, min(32767, pcm[j]*32767)))
			}
		}
		p, err := enc.Encode(frame)
		if err != nil {
			return nil, 0, err
		}
		packets = append(packets, append([]byte{}, p...))
	}
	packed = proto.PackSound(packets)
	if len(packed) > proto.MaxSoundBytes {
		return nil, 0, errors.New("that sound is too big")
	}
	return packed, frames * 20, nil
}

// readWAV reads a WAV file's samples (-1 to 1), mixed down to one channel.
func readWAV(data []byte) ([]float32, int, error) {
	var format, channels, bits int
	var rate int
	var body []byte
	for p := 12; p+8 <= len(data); {
		id, size := string(data[p:p+4]), int(binary.LittleEndian.Uint32(data[p+4:p+8]))
		p += 8
		if size < 0 || p+size > len(data) {
			size = len(data) - p // Some programs write a wrong length for the last chunk.
		}
		chunk := data[p : p+size]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, errSoundBroken
			}
			format = int(binary.LittleEndian.Uint16(chunk[0:]))
			channels = int(binary.LittleEndian.Uint16(chunk[2:]))
			rate = int(binary.LittleEndian.Uint32(chunk[4:]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:]))
			if format == 0xFFFE && len(chunk) >= 26 { // For "extensible", the real format is further in.
				format = int(binary.LittleEndian.Uint16(chunk[24:]))
			}
		case "data":
			body = chunk
		}
		p += size + size%2
	}
	if channels < 1 || channels > 8 || rate < 8000 || rate > 192000 || body == nil {
		return nil, 0, errSoundBroken
	}
	size := bits / 8
	if (format != 1 && format != 3) || (format == 1 && (size < 1 || size > 4)) || (format == 3 && size != 4) {
		return nil, 0, errors.New("that WAV file's kind of sound can't be read; save it as 16-bit PCM")
	}
	step := size * channels
	n := len(body) / step
	if n > rate*proto.MaxSoundMillis/1000+rate/10 {
		return nil, 0, errSoundLong
	}
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		var sum float32
		for c := 0; c < channels; c++ {
			b := body[i*step+c*size:]
			var v float32
			switch {
			case format == 3:
				v = math.Float32frombits(binary.LittleEndian.Uint32(b))
			case size == 1:
				v = (float32(b[0]) - 128) / 128
			case size == 2:
				v = float32(int16(binary.LittleEndian.Uint16(b))) / 32768
			case size == 3:
				v = float32(int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24)>>8) / 8388608
			default:
				v = float32(int32(binary.LittleEndian.Uint32(b))) / 2147483648
			}
			sum += v
		}
		out[i] = sum / float32(channels)
	}
	return out, rate, nil
}

// resample changes the number of samples a second (straight lines between
// samples, which is plenty for short sounds).
func resample(in []float32, from, to int) []float32 {
	if from == to || len(in) == 0 {
		return in
	}
	n := int(int64(len(in)) * int64(to) / int64(from))
	out := make([]float32, n)
	for i := range out {
		pos := float64(i) * float64(from) / float64(to)
		j := int(pos)
		frac := float32(pos - float64(j))
		a := in[min(j, len(in)-1)]
		b := in[min(j+1, len(in)-1)]
		out[i] = a + (b-a)*frac
	}
	return out
}

// readOggOpus reads an Ogg Opus file (RFC 7845) into samples at 48,000 a
// second, in one channel.
func readOggOpus(data []byte) ([]float32, error) {
	packets, err := oggPackets(data)
	if err != nil {
		return nil, err
	}
	if len(packets) < 2 || len(packets[0]) < 19 || string(packets[0][:8]) != "OpusHead" {
		return nil, errSoundFormat
	}
	head := packets[0]
	if head[9] < 1 || head[9] > 2 || (len(head) > 18 && head[18] != 0) {
		return nil, errors.New("only mono or stereo Ogg Opus files can be used")
	}
	skip := int(binary.LittleEndian.Uint16(head[10:12]))
	dec, err := opus.NewDecoder()
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	var out []float32
	for _, p := range packets[2:] { // The packets after OpusHead and OpusTags are the sound.
		pcm, err := dec.DecodeAny(p)
		if err != nil {
			return nil, errSoundBroken
		}
		for _, v := range pcm {
			out = append(out, float32(v)/32768)
		}
		if len(out) > sound.Rate*(proto.MaxSoundMillis/1000)+skip+sound.Rate/10 {
			return nil, errSoundLong
		}
	}
	if skip >= len(out) {
		return nil, nil
	}
	return out[skip:], nil
}

// oggPackets joins an Ogg file's pages back into packets (the first stream
// only).
func oggPackets(data []byte) ([][]byte, error) {
	var out [][]byte
	var cur []byte
	serial, first := uint32(0), true
	for p := 0; p < len(data); {
		if p+27 > len(data) || string(data[p:p+4]) != "OggS" {
			return nil, errSoundBroken
		}
		s := binary.LittleEndian.Uint32(data[p+14:])
		segs := int(data[p+26])
		if p+27+segs > len(data) {
			return nil, errSoundBroken
		}
		table := data[p+27 : p+27+segs]
		body := p + 27 + segs
		if first {
			serial, first = s, false
		}
		for _, n := range table {
			if body+int(n) > len(data) {
				return nil, errSoundBroken
			}
			if s == serial {
				cur = append(cur, data[body:body+int(n)]...)
				if n < 255 { // A packet ends here.
					out = append(out, cur)
					cur = nil
				}
			}
			body += int(n)
		}
		p = body
		if len(out) > 6000 {
			return nil, errSoundLong
		}
	}
	return out, nil
}
