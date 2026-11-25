package guild

import (
	"testing"

	"nexus/internal/proto"
)

var fakePNG = []byte("\x89PNG\r\n\x1a\nnot a real picture but starts like one")

func TestPages(t *testing.T) {
	g, addr, owner := pairedGuild(t)
	o := connectChatter(t, addr, owner, "pc-o")
	ben := member(t, addr, "pc-b")

	// Pages are off until turned on.
	ben.refused(t, proto.Request{Type: "threads"}, "Threads is off")
	ben.refused(t, proto.Request{Type: "update_pages", Pages: &proto.PageSettings{Threads: true}}, "no Manage Pages")
	o.must(t, proto.Request{Type: "update_pages", Pages: &proto.PageSettings{Calendar: true, Threads: true, Wiki: true}})

	// In Threads, Everyone may post; there are nested replies and one vote each.
	post := ben.must(t, proto.Request{Type: "post_put", Post: &proto.Post{Title: "My first sprite", Body: "Look!"}}).Message
	c1 := o.must(t, proto.Request{Type: "comment_put", Comment: &proto.Comment{Post: post, Body: "Nice"}}).Message
	ben.must(t, proto.Request{Type: "comment_put", Comment: &proto.Comment{Post: post, Parent: c1, Body: "Thanks!"}})
	o.must(t, proto.Request{Type: "vote", ItemID: post, Vote: 1})
	ben.must(t, proto.Request{Type: "vote", ItemID: post, Vote: -1})
	o.must(t, proto.Request{Type: "vote", ItemID: post, Vote: 1}) // Voting again still counts as one vote.
	list := o.must(t, proto.Request{Type: "threads"}).Posts
	if len(list) != 1 || list[0].Likes != 1 || list[0].Dislikes != 1 || list[0].MyVote != 1 || list[0].Comments != 2 || list[0].Votes != nil {
		t.Fatalf("threads: %+v", list)
	}
	r := o.must(t, proto.Request{Type: "thread_get", ItemID: post})
	if len(r.Comments) != 2 || r.Comments[1].Parent != c1 {
		t.Fatalf("comments: %+v", r.Comments)
	}
	o.refused(t, proto.Request{Type: "post_put", Post: &proto.Post{ID: post, Title: "mine now"}}, "only the author edits")
	// Deleting needs Manage Pages, which the owner has.
	o.must(t, proto.Request{Type: "post_delete", ItemID: post})

	// The Wiki needs Edit Wiki, and pages are found by title for [[links]].
	ben.refused(t, proto.Request{Type: "wiki_put", Wiki: &proto.WikiPage{Title: "Rules"}}, "no Edit Wiki")
	o.must(t, proto.Request{Type: "wiki_put", Wiki: &proto.WikiPage{Title: "Getting Started", Group: "Basics", Body: "See [[Palettes]]."}})
	o.refused(t, proto.Request{Type: "wiki_put", Wiki: &proto.WikiPage{Title: "getting started"}}, "same title")
	if w := ben.must(t, proto.Request{Type: "wiki_get", ItemID: "getting started"}).Wiki; w == nil || w.Body == "" {
		t.Fatal("a page should be found by its title")
	}
	if l := ben.must(t, proto.Request{Type: "wiki_list"}).Wikis; len(l) != 1 || l[0].Body != "" {
		t.Fatalf("wiki list: %+v", l)
	}

	// The Calendar needs Manage Events, and an event's end can't be before the start.
	ev := proto.Event{Title: "Pixel Jam", Start: "2026-12-05T18:00:00Z", StartTime: true, StartZone: "Asia/Kolkata"}
	ben.refused(t, proto.Request{Type: "event_put", Event: &ev}, "no Manage Events")
	bad := ev
	bad.End = "2026-12-04T18:00:00Z"
	o.refused(t, proto.Request{Type: "event_put", Event: &bad}, "end before start")
	o.must(t, proto.Request{Type: "event_put", Event: &ev})
	if l := ben.must(t, proto.Request{Type: "events"}).Events; len(l) != 1 || l[0].StartZone != "Asia/Kolkata" {
		t.Fatalf("events: %+v", l)
	}

	// Test pictures and emoji.
	ben.refused(t, proto.Request{Type: "image_put", Image: []byte("<svg>")}, "not a picture")
	h := ben.must(t, proto.Request{Type: "image_put", Image: fakePNG}).Message
	if d := ben.must(t, proto.Request{Type: "image_get", Hash: h}).Data; string(d) != string(fakePNG) {
		t.Fatal("the picture should come back")
	}
	ben.refused(t, proto.Request{Type: "emoji_put", EmojiName: "heart", Image: fakePNG}, "no Manage Guild")
	o.refused(t, proto.Request{Type: "emoji_put", EmojiName: "Bad Name", Image: fakePNG}, "bad name")
	o.must(t, proto.Request{Type: "emoji_put", EmojiName: "pixel_heart", Image: fakePNG})
	other := append([]byte{}, fakePNG...)
	other = append(other, '!')
	ben.must(t, proto.Request{Type: "emoji_borrow", EmojiName: "wave", ItemID: "OTHERGUILD", Image: other})
	if l := ben.must(t, proto.Request{Type: "emoji_list"}).Emojis; len(l) != 2 || !l[1].Borrowed {
		t.Fatalf("emoji: %+v", l)
	}

	// Changing the Rules text records when.
	o.must(t, proto.Request{Type: "update_join", Join: &proto.JoinSettings{ShowJoinButton: true, RulesEnabled: true, Rules: "Be kind.", JoinPlacement: "about"}})
	o.must(t, proto.Request{Type: "update_join", Join: &proto.JoinSettings{ShowJoinButton: true, RulesEnabled: true, Rules: "Be kind. No spam.", JoinPlacement: "about"}})
	g.mu.Lock()
	updated := g.join.RulesUpdated
	g.mu.Unlock()
	if updated == "" {
		t.Fatal("a Rules change should be recorded")
	}
}
