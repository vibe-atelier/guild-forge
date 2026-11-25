package guildconn

import "testing"

func TestMatchDM(t *testing.T) {
	poll := Body{Text: "", Poll: &PollBody{Question: "Where tonight?", Choices: []string{"Cinema", "Park"}, Answer: -1}}.encode()
	cases := []struct {
		v     DMView
		query string
		want  bool
	}{
		{DMView{Text: "See you at the Cinema later"}, "cinema", true},
		{DMView{Text: "See you at the Cinema later"}, "later see", true}, // The words may be in any order.
		{DMView{Text: "See you at the Cinema later"}, "cinema park", false},
		{DMView{Text: "The code is ||4471||"}, "4471", true},
		{DMView{Text: "anything"}, "   ", false},
		{DMView{Text: "secret plans", Deleted: true}, "secret", false},
		{DMView{File: "Holiday Photos.zip"}, "holiday", true},
		{DMView{Text: poll}, "park", true},
		{DMView{Text: poll}, "tonight", true},
		{DMView{Text: poll}, "nexus-body", false}, // The message's inner format is never found.
	}
	for _, c := range cases {
		if got := MatchDM(c.v, c.query); got != c.want {
			t.Errorf("MatchDM(%q, %q) = %v, want %v", c.v.Text+c.v.File, c.query, got, c.want)
		}
	}
	got := SearchDMs([]DMView{{ID: "1", Text: "hello there"}, {ID: "2", Text: "bye"}, {ID: "3", Text: "Hello again"}}, "hello")
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "3" {
		t.Errorf("SearchDMs = %+v", got)
	}
}
