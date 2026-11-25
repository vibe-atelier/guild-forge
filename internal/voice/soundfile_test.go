package voice

import (
	"encoding/binary"
	"math"
	"testing"

	"nexus/internal/opus"
	"nexus/internal/proto"
)

// wavFile makes a 16-bit stereo WAV file of a tone.
func wavFile(rate int, seconds float64) []byte {
	n := int(float64(rate) * seconds)
	body := make([]byte, n*4)
	for i := 0; i < n; i++ {
		v := int16(12000 * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(body[i*4:], uint16(v))
		binary.LittleEndian.PutUint16(body[i*4+2:], uint16(v))
	}
	h := make([]byte, 44)
	copy(h, "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(body)))
	copy(h[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // The format is PCM.
	binary.LittleEndian.PutUint16(h[22:], 2)
	binary.LittleEndian.PutUint32(h[24:], uint32(rate))
	binary.LittleEndian.PutUint32(h[28:], uint32(rate*4))
	binary.LittleEndian.PutUint16(h[32:], 4)
	binary.LittleEndian.PutUint16(h[34:], 16)
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(body)))
	return append(h, body...)
}

// oggFile wraps packets into Ogg pages, one packet a page.
func oggFile(packets [][]byte) []byte {
	var out []byte
	for i, p := range packets {
		page := make([]byte, 27)
		copy(page, "OggS")
		binary.LittleEndian.PutUint32(page[14:], 7) // This is the stream's serial number.
		binary.LittleEndian.PutUint32(page[18:], uint32(i))
		var table []byte
		n := len(p)
		for n >= 255 {
			table = append(table, 255)
			n -= 255
		}
		table = append(table, byte(n))
		page[26] = byte(len(table))
		out = append(append(append(out, page...), table...), p...)
	}
	return out
}

func soundLevel(t *testing.T, packed []byte) (packets int, level float64) {
	t.Helper()
	list, err := proto.UnpackSound(packed)
	if err != nil {
		t.Fatal(err)
	}
	dec, _ := opus.NewDecoder()
	defer dec.Close()
	var sum float64
	var n int
	for _, p := range list {
		f, err := dec.Decode(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range f {
			sum += float64(v) * float64(v)
			n++
		}
	}
	return len(list), math.Sqrt(sum / float64(n))
}

func TestLoadWAV(t *testing.T) {
	packed, ms, err := LoadSound(wavFile(44100, 1))
	if err != nil {
		t.Fatal(err)
	}
	if ms != 1000 {
		t.Fatalf("one second, got %d ms", ms)
	}
	if n, level := soundLevel(t, packed); n != 50 || level < 5000 {
		t.Fatalf("%d packets, level %.0f", n, level)
	}
	if _, _, err := LoadSound(wavFile(48000, 10.5)); err == nil {
		t.Fatal("over 10 seconds is refused")
	}
	if _, _, err := LoadSound([]byte("ID3 an mp3 file")); err == nil {
		t.Fatal("other files are refused")
	}
}

func TestLoadOggOpus(t *testing.T) {
	enc, _ := opus.NewEncoder(64000)
	defer enc.Close()
	head := []byte("OpusHead\x01\x01\x38\x01\x80\xbb\x00\x00\x00\x00\x00")
	packets := [][]byte{head, []byte("OpusTags\x00\x00\x00\x00\x00\x00\x00\x00")}
	for f := 0; f < 25; f++ { // This is half a second.
		frame := make([]int16, 960)
		for i := range frame {
			frame[i] = int16(12000 * math.Sin(2*math.Pi*440*float64(f*960+i)/48000))
		}
		p, err := enc.Encode(frame)
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, append([]byte{}, p...))
	}
	packed, ms, err := LoadSound(oggFile(packets))
	if err != nil {
		t.Fatal(err)
	}
	if ms < 460 || ms > 500 { // The length is less the pre-skip.
		t.Fatalf("about half a second, got %d ms", ms)
	}
	if _, level := soundLevel(t, packed); level < 4000 {
		t.Fatalf("level %.0f", level)
	}
}
