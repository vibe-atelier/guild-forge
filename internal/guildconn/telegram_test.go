package guildconn

import (
	"testing"

	"nexus/internal/proto"
)

// TestBody checks that polls and forwarded messages travel inside the sealed text.
func TestBody(t *testing.T) {
	if b := ParseBody("just text"); b.Text != "just text" || b.Poll != nil {
		t.Fatalf("plain: %+v", b)
	}
	if b := ParseBody(`{"nexus-body": oops`); b.Text != `{"nexus-body": oops` {
		t.Fatal("broken packets show as they are")
	}
	fw := ParseBody(ForwardText("hi", "Ben", "Pixel Club / #general", "2026-11-21T10:00:00Z"))
	if fw.Text != "hi" || fw.Forward == nil || fw.Forward.Name != "Ben" {
		t.Fatalf("forward: %+v", fw)
	}

	// A quiz: the right answer shows only once you have voted.
	text := Body{Poll: &PollBody{Question: "2+2?", Choices: []string{"3", "4"}, Answer: 1}}.encode()
	m := proto.ChatMessage{ID: "m", Poll: &proto.PollInfo{Choices: 2, Quiz: true}, Votes: [][]string{{"ben"}, nil}}
	v := ChatView{Text: text}
	fillExtras(&v, m, "me", map[string]bool{"m": true})
	if v.Poll == nil || v.Poll.Voted || v.Poll.Choices[1].Right || v.Poll.Total != 1 || !v.Pinned {
		t.Fatalf("before voting: %+v", v.Poll)
	}
	m.Votes = [][]string{{"ben", "me"}, nil}
	v = ChatView{Text: text}
	fillExtras(&v, m, "me", nil)
	if !v.Poll.Voted || !v.Poll.Choices[0].Mine || !v.Poll.Choices[1].Right || v.Poll.Choices[0].Count != 2 {
		t.Fatalf("after voting: %+v", v.Poll)
	}
	if previewText(text) != "Poll: 2+2?" {
		t.Fatal("a poll's preview is its question")
	}
}
