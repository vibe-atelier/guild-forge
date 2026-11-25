package guild

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"nexus/internal/guildstore"
	"nexus/internal/proto"
)

// This file has Calendar, Threads, Wiki, pictures and custom emoji. These pages
// are readable by the guild's host.

const (
	maxTitle      = 120
	maxPageText   = 50000
	maxComment    = 4000
	maxImage      = 2 << 20
	maxEmojiSize  = 256 << 10
	maxGuildEmoji = 50 // A guild has at most 50 emoji.
	maxBorrowed   = 500
	maxEvents     = 2000
	maxWikiPages  = 500
	maxPosts      = 5000
	pagesFile     = "pages.json"
	calendarFile  = "calendar.json"
	wikiFile      = "wiki.json"
	threadsFile   = "threads.json"
	emojiFile     = "emoji.json"
	borrowedFile  = "borrowed.json"
	commentsDir   = "comments"
	imagesDir     = "images"
	emojiDir      = "emoji"
	borrowedDir   = "borrowed"
)

var emojiName = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

type pageState struct {
	settings proto.PageSettings
	events   []proto.Event
	wiki     []proto.WikiPage
	posts    []proto.Post
	comments map[string][]proto.Comment // Comments are kept by post.
	emoji    []proto.Emoji
	borrowed []proto.Emoji
}

func (g *Server) loadPages() error {
	p := &g.pages
	first := false
	if err := guildstore.LoadPage(g.DataDir, pagesFile, &p.settings); err != nil {
		return err
	}
	if !g.fileExists(pagesFile) {
		first = true
	}
	for name, v := range map[string]any{calendarFile: &p.events, wikiFile: &p.wiki, threadsFile: &p.posts,
		emojiFile: &p.emoji, borrowedFile: &p.borrowed} {
		if err := guildstore.LoadPage(g.DataDir, name, v); err != nil {
			return err
		}
	}
	p.comments = map[string][]proto.Comment{}
	for _, post := range p.posts {
		var list []proto.Comment
		guildstore.LoadPage(g.DataDir, commentsDir+"/"+post.ID+".json", &list)
		p.comments[post.ID] = list
	}
	// An older guild without page settings: Everyone may post in Threads.
	if first {
		if r := g.roleLocked(proto.EveryoneRole); r != nil && !contains(r.Perms, proto.PermPostThreads) {
			r.Perms = append(r.Perms, proto.PermPostThreads)
			guildstore.SaveRoles(g.DataDir, g.roles)
		}
		guildstore.SavePage(g.DataDir, pagesFile, p.settings)
	}
	return nil
}

func (g *Server) pagesChangedLocked(kind string) {
	g.notifyLater(g.allConnsLocked(), "pages_changed:"+kind)
}

func (g *Server) nowText() string { return g.now().UTC().Format(time.RFC3339) }

func cleanTitle(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", errors.New("give it a title")
	}
	if utf8.RuneCountInString(s) > maxTitle {
		return "", fmt.Errorf("titles can be at most %d characters", maxTitle)
	}
	return s, nil
}

func checkText(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return "", fmt.Errorf("that is too long (at most %d characters)", max)
	}
	return s, nil
}

// ---- which pages are on ------------------------------------------------------

func (g *Server) updatePages(user string, in *proto.PageSettings) error {
	if in == nil {
		return errors.New("nothing sent")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManagePages); err != nil {
		return err
	}
	g.pages.settings = *in
	if err := guildstore.SavePage(g.DataDir, pagesFile, *in); err != nil {
		return err
	}
	g.notifyLater(g.allConnsLocked(), "guild_updated")
	return nil
}

// pageOnLocked checks that a page is on, except for those who manage pages.
func (g *Server) pageOnLocked(user string, on bool) error {
	if on || g.hasLocked(user, proto.PermManagePages) {
		return nil
	}
	return errors.New("this page is turned off")
}

// ---- Calendar ---------------------------------------------

func parseWhen(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return t, errors.New("a date or time is not valid")
	}
	return t, nil
}

func (g *Server) eventPut(user string, in *proto.Event) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	e := *in
	var err error
	if e.Title, err = cleanTitle(e.Title); err != nil {
		return "", err
	}
	if e.Details, err = checkText(e.Details, maxPageText); err != nil {
		return "", err
	}
	start, err := parseWhen(e.Start)
	if err != nil {
		return "", err
	}
	if e.End != "" {
		end, err := parseWhen(e.End)
		if err != nil {
			return "", err
		}
		if end.Before(start) {
			return "", errors.New("the end is before the start")
		}
	}
	if len(e.StartZone) > 64 || len(e.EndZone) > 64 {
		return "", errors.New("a time zone name is too long")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageEvents); err != nil {
		return "", err
	}
	if err := g.pageOnLocked(user, g.pages.settings.Calendar); err != nil {
		return "", err
	}
	e.Author, e.Updated = user, g.nowText()
	list := g.pages.events
	if e.ID == "" {
		if len(list) >= maxEvents {
			return "", errors.New("the Calendar is full")
		}
		e.ID = newID()
		list = append(list, e)
	} else {
		found := false
		for i := range list {
			if list[i].ID == e.ID {
				list[i], found = e, true
			}
		}
		if !found {
			return "", errors.New("there is no such event")
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Start < list[j].Start })
	g.pages.events = list
	if err := guildstore.SavePage(g.DataDir, calendarFile, list); err != nil {
		return "", err
	}
	g.pagesChangedLocked("calendar")
	return e.ID, nil
}

func (g *Server) eventDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageEvents); err != nil {
		return err
	}
	var kept []proto.Event
	for _, e := range g.pages.events {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	g.pages.events = kept
	g.pagesChangedLocked("calendar")
	return guildstore.SavePage(g.DataDir, calendarFile, kept)
}

// ---- Wiki ------------------------------------------------------------

func (g *Server) wikiList(user string) ([]proto.WikiPage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.pageOnLocked(user, g.pages.settings.Wiki); err != nil {
		return nil, err
	}
	out := make([]proto.WikiPage, 0, len(g.pages.wiki))
	for _, w := range g.pages.wiki {
		w.Body = ""
		out = append(out, w)
	}
	return out, nil
}

func (g *Server) wikiGet(user, id string) (*proto.WikiPage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.pageOnLocked(user, g.pages.settings.Wiki); err != nil {
		return nil, err
	}
	for _, w := range g.pages.wiki {
		// A page is found by its ID, or by its title for [[links]].
		if w.ID == id || strings.EqualFold(w.Title, id) {
			w := w
			return &w, nil
		}
	}
	return nil, errors.New("there is no such page")
}

func (g *Server) wikiPut(user string, in *proto.WikiPage) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	w := *in
	var err error
	if w.Title, err = cleanTitle(w.Title); err != nil {
		return "", err
	}
	if w.Body, err = checkText(w.Body, maxPageText); err != nil {
		return "", err
	}
	if w.Group, err = checkText(w.Group, 60); err != nil {
		return "", err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermEditWiki); err != nil {
		return "", err
	}
	if err := g.pageOnLocked(user, g.pages.settings.Wiki); err != nil {
		return "", err
	}
	for _, x := range g.pages.wiki {
		if x.ID != w.ID && strings.EqualFold(x.Title, w.Title) {
			return "", errors.New("there is already a page with that title")
		}
	}
	w.Author, w.Updated = user, g.nowText()
	if w.ID == "" {
		if len(g.pages.wiki) >= maxWikiPages {
			return "", errors.New("the Wiki is full")
		}
		w.ID = newID()
		g.pages.wiki = append(g.pages.wiki, w)
	} else {
		found := false
		for i := range g.pages.wiki {
			if g.pages.wiki[i].ID == w.ID {
				g.pages.wiki[i], found = w, true
			}
		}
		if !found {
			return "", errors.New("there is no such page")
		}
	}
	sort.SliceStable(g.pages.wiki, func(i, j int) bool {
		a, b := g.pages.wiki[i], g.pages.wiki[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
	if err := guildstore.SavePage(g.DataDir, wikiFile, g.pages.wiki); err != nil {
		return "", err
	}
	g.pagesChangedLocked("wiki")
	return w.ID, nil
}

func (g *Server) wikiDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.hasLocked(user, proto.PermEditWiki) && !g.hasLocked(user, proto.PermManagePages) {
		return errors.New("you don't have permission to do that")
	}
	var kept []proto.WikiPage
	for _, w := range g.pages.wiki {
		if w.ID != id {
			kept = append(kept, w)
		}
	}
	g.pages.wiki = kept
	g.pagesChangedLocked("wiki")
	return guildstore.SavePage(g.DataDir, wikiFile, kept)
}

// ---- Threads --------------------------------------------------

func tally(votes map[string]int, user string) (likes, dislikes, mine int) {
	for u, v := range votes {
		if v > 0 {
			likes++
		} else if v < 0 {
			dislikes++
		}
		if u == user {
			mine = v
		}
	}
	return
}

func (g *Server) viewPostLocked(p proto.Post, user string) proto.Post {
	p.Likes, p.Dislikes, p.MyVote = tally(p.Votes, user)
	p.Votes = nil
	p.Name = g.members[p.Author].Name
	n := 0
	for _, c := range g.pages.comments[p.ID] {
		if !c.Deleted {
			n++
		}
	}
	p.Comments = n
	if p.Deleted {
		p.Body, p.Title = "", "(deleted)"
	}
	return p
}

func (g *Server) threadList(user string) ([]proto.Post, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.pageOnLocked(user, g.pages.settings.Threads); err != nil {
		return nil, err
	}
	out := make([]proto.Post, 0, len(g.pages.posts))
	for i := len(g.pages.posts) - 1; i >= 0; i-- { // Posts are listed newest first.
		p := g.viewPostLocked(g.pages.posts[i], user)
		p.Body = firstChars(p.Body, 300)
		out = append(out, p)
	}
	return out, nil
}

func firstChars(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (g *Server) threadGet(user, id string) (*proto.Post, []proto.Comment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.pageOnLocked(user, g.pages.settings.Threads); err != nil {
		return nil, nil, err
	}
	for _, p := range g.pages.posts {
		if p.ID == id {
			v := g.viewPostLocked(p, user)
			var cs []proto.Comment
			for _, c := range g.pages.comments[id] {
				c.Likes, c.Dislikes, c.MyVote = tally(c.Votes, user)
				c.Votes = nil
				c.Name = g.members[c.Author].Name
				if c.Deleted {
					c.Body = ""
				}
				cs = append(cs, c)
			}
			return &v, cs, nil
		}
	}
	return nil, nil, errors.New("there is no such post")
}

func (g *Server) postIndexLocked(id string) int {
	for i := range g.pages.posts {
		if g.pages.posts[i].ID == id {
			return i
		}
	}
	return -1
}

func (g *Server) postPut(user string, in *proto.Post) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	title, err := cleanTitle(in.Title)
	if err != nil {
		return "", err
	}
	body, err := checkText(in.Body, maxPageText)
	if err != nil {
		return "", err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermPostThreads); err != nil {
		return "", err
	}
	if err := g.pageOnLocked(user, g.pages.settings.Threads); err != nil {
		return "", err
	}
	if in.ID == "" {
		if len(g.pages.posts) >= maxPosts {
			return "", errors.New("Threads is full")
		}
		p := proto.Post{ID: newID(), Author: user, Title: title, Body: body, Time: g.nowText()}
		g.pages.posts = append(g.pages.posts, p)
		in = &p
	} else {
		i := g.postIndexLocked(in.ID)
		if i < 0 || g.pages.posts[i].Deleted {
			return "", errors.New("there is no such post")
		}
		if g.pages.posts[i].Author != user {
			return "", errors.New("you can only edit your own posts")
		}
		g.pages.posts[i].Title, g.pages.posts[i].Body, g.pages.posts[i].Edited = title, body, true
	}
	if err := guildstore.SavePage(g.DataDir, threadsFile, g.pages.posts); err != nil {
		return "", err
	}
	g.pagesChangedLocked("threads")
	return in.ID, nil
}

func (g *Server) postDelete(user, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.postIndexLocked(id)
	if i < 0 {
		return errors.New("there is no such post")
	}
	if g.pages.posts[i].Author != user && !g.hasLocked(user, proto.PermManagePages) {
		return errors.New("you can only delete your own posts")
	}
	p := &g.pages.posts[i]
	p.Deleted, p.Body, p.Votes = true, "", nil
	g.pagesChangedLocked("threads")
	return guildstore.SavePage(g.DataDir, threadsFile, g.pages.posts)
}

func (g *Server) commentPut(user string, in *proto.Comment) (string, error) {
	if in == nil {
		return "", errors.New("nothing sent")
	}
	body, err := checkText(in.Body, maxComment)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "", errors.New("write something first")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermPostThreads); err != nil {
		return "", err
	}
	if err := g.pageOnLocked(user, g.pages.settings.Threads); err != nil {
		return "", err
	}
	pi := g.postIndexLocked(in.Post)
	if pi < 0 || g.pages.posts[pi].Deleted {
		return "", errors.New("there is no such post")
	}
	list := g.pages.comments[in.Post]
	id := in.ID
	if id == "" {
		if in.Parent != "" {
			ok := false
			for _, c := range list {
				ok = ok || c.ID == in.Parent
			}
			if !ok {
				return "", errors.New("the comment you reply to is gone")
			}
		}
		if len(list) >= 5000 {
			return "", errors.New("this post has too many comments")
		}
		id = newID()
		list = append(list, proto.Comment{ID: id, Post: in.Post, Parent: in.Parent, Author: user, Body: body, Time: g.nowText()})
	} else {
		found := false
		for i := range list {
			if list[i].ID == id {
				if list[i].Author != user || list[i].Deleted {
					return "", errors.New("you can only edit your own comments")
				}
				list[i].Body, list[i].Edited, found = body, true, true
			}
		}
		if !found {
			return "", errors.New("there is no such comment")
		}
	}
	g.pages.comments[in.Post] = list
	if err := guildstore.SavePage(g.DataDir, commentsDir+"/"+in.Post+".json", list); err != nil {
		return "", err
	}
	g.pagesChangedLocked("threads")
	return id, nil
}

func (g *Server) commentDelete(user, post, id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	list := g.pages.comments[post]
	for i := range list {
		if list[i].ID == id {
			if list[i].Author != user && !g.hasLocked(user, proto.PermManagePages) {
				return errors.New("you can only delete your own comments")
			}
			list[i].Deleted, list[i].Body, list[i].Votes = true, "", nil
			g.pagesChangedLocked("threads")
			return guildstore.SavePage(g.DataDir, commentsDir+"/"+post+".json", list)
		}
	}
	return errors.New("there is no such comment")
}

// vote likes (1), dislikes (-1) or clears (0) a post or comment.
func (g *Server) vote(user, post, comment string, v int) error {
	if v < -1 || v > 1 {
		return errors.New("a vote is 1, -1 or 0")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, member := g.members[user]; !member {
		return errors.New("only members can vote")
	}
	set := func(m map[string]int) map[string]int {
		if m == nil {
			m = map[string]int{}
		}
		if v == 0 {
			delete(m, user)
		} else {
			m[user] = v
		}
		return m
	}
	pi := g.postIndexLocked(post)
	if pi < 0 || g.pages.posts[pi].Deleted {
		return errors.New("there is no such post")
	}
	if comment == "" {
		g.pages.posts[pi].Votes = set(g.pages.posts[pi].Votes)
		g.pagesChangedLocked("threads")
		return guildstore.SavePage(g.DataDir, threadsFile, g.pages.posts)
	}
	list := g.pages.comments[post]
	for i := range list {
		if list[i].ID == comment && !list[i].Deleted {
			list[i].Votes = set(list[i].Votes)
			g.pagesChangedLocked("threads")
			return guildstore.SavePage(g.DataDir, commentsDir+"/"+post+".json", list)
		}
	}
	return errors.New("there is no such comment")
}

// ---- pictures ------------------------------------------------

func isPicture(b []byte) bool {
	return bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) || bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) ||
		bytes.HasPrefix(b, []byte("GIF8"))
}

// imagePut stores a picture for a page; anyone who can write a page may.
func (g *Server) imagePut(user string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > maxImage || !isPicture(data) {
		return "", errors.New("pictures must be PNG, JPG or GIF, up to 2 MB")
	}
	g.mu.Lock()
	ok := false
	for _, p := range []string{proto.PermPostThreads, proto.PermEditWiki, proto.PermManageEvents, proto.PermManageJoining, proto.PermManageGuild} {
		ok = ok || g.hasLocked(user, p)
	}
	g.mu.Unlock()
	if !ok {
		return "", errors.New("you don't have permission to add pictures")
	}
	return guildstore.SaveBlob(g.DataDir, imagesDir, data)
}

// ---- custom emoji ------------------------------------------------------------

func (g *Server) emojiList() []proto.Emoji {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append(append([]proto.Emoji{}, g.pages.emoji...), g.pages.borrowed...)
}

func (g *Server) emojiPut(user, name string, data []byte) error {
	if !emojiName.MatchString(name) {
		return errors.New("emoji names are 2 to 32 small letters, digits or _")
	}
	if len(data) == 0 || len(data) > maxEmojiSize || !isPicture(data) {
		return errors.New("emoji must be PNG, JPG or GIF, up to 256 KB")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageGuild); err != nil {
		return err
	}
	for _, e := range g.pages.emoji {
		if e.Name == name {
			return errors.New("there is already an emoji with that name")
		}
	}
	if len(g.pages.emoji) >= maxGuildEmoji {
		return errors.New("a guild can have at most 50 emoji")
	}
	h, err := guildstore.SaveBlob(g.DataDir, emojiDir, data)
	if err != nil {
		return err
	}
	g.pages.emoji = append(g.pages.emoji, proto.Emoji{Name: name, Hash: h})
	g.notifyLater(g.allConnsLocked(), "emoji_changed")
	return guildstore.SavePage(g.DataDir, emojiFile, g.pages.emoji)
}

func (g *Server) emojiDelete(user, name string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.needLocked(user, proto.PermManageGuild); err != nil {
		return err
	}
	var kept []proto.Emoji
	for _, e := range g.pages.emoji {
		if e.Name == name {
			guildstore.RemoveBlob(g.DataDir, emojiDir, e.Hash)
			continue
		}
		kept = append(kept, e)
	}
	g.pages.emoji = kept
	g.notifyLater(g.allConnsLocked(), "emoji_changed")
	return guildstore.SavePage(g.DataDir, emojiFile, kept)
}

// emojiBorrow keeps a copy of another guild's emoji, so members here can
// see it when someone who is in that guild uses it here.
func (g *Server) emojiBorrow(user, name, from string, data []byte) (string, error) {
	if !emojiName.MatchString(name) || len(data) == 0 || len(data) > maxEmojiSize || !isPicture(data) {
		return "", errors.New("that is not a usable emoji")
	}
	h := guildstore.HashOf(data)
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, member := g.members[user]; !member {
		return "", errors.New("only members can do that")
	}
	for _, e := range g.pages.borrowed {
		if e.Hash == h {
			return h, nil
		}
	}
	if len(g.pages.borrowed) >= maxBorrowed {
		return "", errors.New("this guild can't keep more borrowed emoji")
	}
	if _, err := guildstore.SaveBlob(g.DataDir, borrowedDir, data); err != nil {
		return "", err
	}
	g.pages.borrowed = append(g.pages.borrowed, proto.Emoji{Name: name, Hash: h, Borrowed: true, From: from})
	return h, guildstore.SavePage(g.DataDir, borrowedFile, g.pages.borrowed)
}

// pictureGet returns a stored picture: a page picture or an emoji.
func (g *Server) pictureGet(hash string) ([]byte, error) {
	for _, dir := range []string{imagesDir, emojiDir, borrowedDir, profilesDir, soundPicsDir} {
		if guildstore.HasBlob(g.DataDir, dir, hash) {
			return guildstore.LoadBlob(g.DataDir, dir, hash)
		}
	}
	return nil, errors.New("there is no such picture")
}

// ---- requests -------------------------------------------------------------------------

func (g *Server) pagesRequest(user string, req proto.Request) (resp proto.Response, ok bool) {
	resp = proto.Response{Type: req.Type, OK: true}
	var err error
	switch req.Type {
	case "pages":
		g.mu.Lock()
		s := g.pages.settings
		g.mu.Unlock()
		resp.Pages = &s
	case "update_pages":
		err = g.updatePages(user, req.Pages)
	case "events":
		g.mu.Lock()
		if err = g.pageOnLocked(user, g.pages.settings.Calendar); err == nil {
			resp.Events = append([]proto.Event{}, g.pages.events...)
		}
		g.mu.Unlock()
	case "event_put":
		resp.Message, err = g.eventPut(user, req.Event)
	case "event_delete":
		err = g.eventDelete(user, req.ItemID)
	case "wiki_list":
		resp.Wikis, err = g.wikiList(user)
	case "wiki_get":
		resp.Wiki, err = g.wikiGet(user, req.ItemID)
	case "wiki_put":
		resp.Message, err = g.wikiPut(user, req.Wiki)
	case "wiki_delete":
		err = g.wikiDelete(user, req.ItemID)
	case "threads":
		resp.Posts, err = g.threadList(user)
	case "thread_get":
		resp.Post, resp.Comments, err = g.threadGet(user, req.ItemID)
	case "post_put":
		resp.Message, err = g.postPut(user, req.Post)
	case "post_delete":
		err = g.postDelete(user, req.ItemID)
	case "comment_put":
		resp.Message, err = g.commentPut(user, req.Comment)
	case "comment_delete":
		if req.Comment == nil {
			err = errors.New("nothing sent")
		} else {
			err = g.commentDelete(user, req.Comment.Post, req.Comment.ID)
		}
	case "vote":
		post, comment := req.ItemID, ""
		if req.Comment != nil {
			post, comment = req.Comment.Post, req.Comment.ID
		}
		err = g.vote(user, post, comment, req.Vote)
	case "image_put":
		resp.Message, err = g.imagePut(user, req.Image)
	case "image_get":
		resp.Data, err = g.pictureGet(req.Hash)
	case "attach_put":
		resp.Message, err = g.attachPut(user, req.ChannelID, req.Image)
	case "attach_get":
		resp.Data, err = g.attachGet(user, req.ChannelID, req.Hash)
	case "emoji_list":
		resp.Emojis = g.emojiList()
	case "emoji_put":
		err = g.emojiPut(user, req.EmojiName, req.Image)
	case "emoji_delete":
		err = g.emojiDelete(user, req.EmojiName)
	case "emoji_borrow":
		resp.Message, err = g.emojiBorrow(user, req.EmojiName, req.ItemID, req.Image)
	default:
		return proto.Response{}, false
	}
	if err != nil {
		return proto.Response{Type: req.Type, Code: "refused", Error: err.Error()}, true
	}
	return resp, true
}

func (g *Server) fileExists(name string) bool {
	_, err := os.Stat(filepath.Join(g.DataDir, name))
	return err == nil
}

// ---- channel attachments -------------------------------------------------------
// Pictures and files in channel messages are encrypted by the sender's app
// with the channel's key; the guild only keeps the sealed bytes, in
// attachments/<channel>/<hash>.

const maxAttachment = 3 << 20

func (g *Server) attachPut(user, channel string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > maxAttachment {
		return "", errors.New("files can be at most 3 MB")
	}
	g.mu.Lock()
	ok := g.isChannelLocked(channel) && g.canSeeLocked(user, channel) && g.canLocked(user, channel, proto.PermSendMessages)
	g.mu.Unlock()
	if !ok {
		return "", errors.New("you can't send files in this channel")
	}
	return guildstore.SaveBlob(g.DataDir, "attachments/"+channel, data)
}

func (g *Server) attachGet(user, channel, hash string) ([]byte, error) {
	g.mu.Lock()
	ok := g.isChannelLocked(channel) && g.canSeeLocked(user, channel)
	g.mu.Unlock()
	if !ok {
		return nil, errors.New("you can't read this channel")
	}
	return guildstore.LoadBlob(g.DataDir, "attachments/"+channel, hash)
}
