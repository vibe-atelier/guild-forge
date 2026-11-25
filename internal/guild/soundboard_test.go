package guild

import (
	"bytes"
	"testing"

	"nexus/internal/proto"
)

// TestSoundboard checks adding, listing, fetching, playing and removing sounds.
func TestSoundboard(t *testing.T) {
	_, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "sb-o")
	ben := member(t, addr, "sb-b")
	cara := member(t, addr, "sb-c")
	room := o.must(t, proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge"}}).Message

	data := proto.PackSound([][]byte{{1, 2, 3}, {4, 5}})
	ben.refused(t, proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: "Horn"}, SoundData: data}, "needs Manage Soundboard")
	o.refused(t, proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: "Bad"}, SoundData: []byte{0, 9, 1}}, "damaged")
	long := make([][]byte, proto.MaxSoundPackets+1)
	for i := range long {
		long[i] = []byte{1}
	}
	o.refused(t, proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: "Long"}, SoundData: proto.PackSound(long)}, "over 10 seconds")
	// These bytes are the start of a PNG.
	pic := append([]byte{0x89, 80, 78, 71, 13, 10, 26, 10}, "not really"...)
	o.refused(t, proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: "Horn"}, SoundData: data, Image: []byte("not a picture")}, "bad picture")
	o.must(t, proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: "Horn"}, SoundData: data, Image: pic})
	o.refused(t, proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: "horn"}, SoundData: data}, "same name")

	list := ben.must(t, proto.Request{Type: "sound_list"}).Sounds
	if len(list) != 1 || list[0].Name != "Horn" || list[0].Millis != 40 || list[0].Picture == "" {
		t.Fatalf("list: %+v", list)
	}
	id := list[0].ID
	if got := ben.must(t, proto.Request{Type: "image_get", Hash: list[0].Picture}).Data; !bytes.Equal(got, pic) {
		t.Fatal("the sound's picture should be served")
	}
	if got := ben.must(t, proto.Request{Type: "sound_get", SoundID: id}).Data; !bytes.Equal(got, data) {
		t.Fatal("the sound should come back as it was")
	}

	ben.refused(t, proto.Request{Type: "sound_play", SoundID: id}, "not in voice")
	ben.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	cara.must(t, proto.Request{Type: "voice_join", ChannelID: room})
	ben.must(t, proto.Request{Type: "sound_play", SoundID: id}) // Everyone may use it.
	for _, c := range []*chatter{ben, cara} {
		if ev := c.waitEvent(t, "sound_play"); ev.SoundID != id || ev.User != ben.key {
			t.Fatalf("event: %+v", ev)
		}
	}
	ben.refused(t, proto.Request{Type: "sound_play", SoundID: id}, "too soon")

	o.must(t, proto.Request{Type: "sound_delete", SoundID: id})
	if n := len(ben.must(t, proto.Request{Type: "sound_list"}).Sounds); n != 0 {
		t.Fatalf("%d sounds left", n)
	}
}
