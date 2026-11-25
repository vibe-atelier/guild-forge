// Command setup prepares the made-up demo used for the guide's pictures
// (docs/guide/screenshots.ps1). It only ever touches the temporary folders it
// is given. Run it as: go run ./docs/guide/setup <step> <args...>.
//
//	user  <app home> <name>                 make a user (password: <name>-pass-2026)
//	pair  <app home> <name> <address> <otp> pair as owner and set the guild up
//	join  <app home> <name> <address>       ask to join (answers the questions)
//	approve <app home> <name> <address>     approve everyone waiting
//	chat  <owner home> <member home> <address>   channels and a short conversation
//	roles <owner home> <member key file> <address>  roles, and Ben as Mod
//	request <alice home> <ben home> <address>      Alice asks Ben to be a contact
//	contacts <alice home> <ben home> <address>     Ben accepts; a short private chat
//	voice <alice home> <address>                   a voice channel and soundboard sounds
//	voicehold <ben home> <address> <seconds>       Ben in voice, sharing a made-up screen
//	telegram <alice home> <ben home> <address>     polls, a spoiler, a forward, pins and more
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"nexus/internal/capture"
	"nexus/internal/guildconn"
	"nexus/internal/proto"
	"nexus/internal/user"
)

func main() {
	if len(os.Args) < 3 {
		fail("usage: setup <step> <args...>")
	}
	switch os.Args[1] {
	case "user":
		makeUser(os.Args[2], os.Args[3])
	case "pair":
		pair(os.Args[2], os.Args[3], os.Args[4], os.Args[5])
	case "join":
		join(os.Args[2], os.Args[3], os.Args[4])
	case "approve":
		approve(os.Args[2], os.Args[3], os.Args[4])
	case "chat":
		chat(os.Args[2], os.Args[3], os.Args[4])
	case "roles":
		roles(os.Args[2], os.Args[3], os.Args[4])
	case "request":
		request(os.Args[2], os.Args[3], os.Args[4])
	case "contacts":
		contacts(os.Args[2], os.Args[3], os.Args[4])
	case "pages":
		pages(os.Args[2], os.Args[3], os.Args[4])
	case "media":
		media(os.Args[2], os.Args[3], os.Args[4])
	case "profiles":
		profiles(os.Args[2], os.Args[3], os.Args[4])
	case "currency":
		currency(os.Args[2], os.Args[3], os.Args[4])
	case "voice":
		voiceChannel(os.Args[2], os.Args[3])
	case "telegram":
		telegram(os.Args[2], os.Args[3], os.Args[4])
	case "voicehold":
		voiceHold(os.Args[2], os.Args[3], os.Args[4])
	default:
		fail("unknown step " + os.Args[1])
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "setup:", msg)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func folder(home, name string) string { return filepath.Join(home, "users", name) }

func makeUser(home, name string) {
	dir := folder(home, name)
	u, err := user.Create(dir, name, name+"-pass-2026")
	check(err)
	list := []map[string]string{{"folder": dir, "name": name, "user_id": u.Profile.UserID}}
	data, _ := json.Marshal(list)
	check(os.WriteFile(filepath.Join(home, "recent.json"), data, 0o600))
	fmt.Print(dir)
}

func open(home, name string) *guildconn.Manager {
	u, err := user.Unlock(folder(home, name), name+"-pass-2026")
	check(err)
	device, err := guildconn.DeviceID(home)
	check(err)
	return guildconn.New(u, device, func() {})
}

// conn waits until the manager's guild is connected.
func conn(m *guildconn.Manager, addr string) *guildconn.Conn {
	c, err := m.Check(addr)
	check(err)
	for i := 0; i < 50; i++ {
		if gc, err := m.Get(c.GuildID); err == nil {
			if _, err := gc.Call(proto.Request{Type: "members"}); err == nil {
				return gc
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	fail("the guild did not connect")
	return nil
}

func must(_ proto.Response, err error) { check(err) }

// picture draws the demo guild's picture (a pixel pattern).
func picture() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			t := float64(x+y) / 1024
			img.Set(x, y, color.NRGBA{uint8(255*(1-t) + 109*t), uint8(77*(1-t) + 93*t), uint8(141*(1-t) + 252*t), 255})
		}
	}
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			if (i*7+j*3)%4 == 0 {
				for y := 0; y < 50; y++ {
					for x := 0; x < 50; x++ {
						img.Set(96+i*56+x, 96+j*56+y, color.NRGBA{255, 255, 255, 235})
					}
				}
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func pair(home, name, addr, otp string) {
	m := open(home, name)
	defer m.CloseAll()
	c, err := m.Check(addr)
	check(err)
	r, err := m.Pair(c.Address, c.GuildID, otp)
	check(err)
	if !r.OK {
		fail(r.Error)
	}
	gc := conn(m, addr)
	gname, about, show := "Pixel Club",
		"A friendly guild for **pixel artists**.\n\n- Share your work\n- Get feedback\n- Join the weekly challenge", true
	must(gc.Call(proto.Request{Type: "update_guild", Name: &gname, About: &about, ShowMemberCount: &show, Picture: picture()}))
	js := proto.JoinSettings{ShowJoinButton: true, ShowRulesButton: true, RulesEnabled: true, RulesPublic: true,
		RequireRules: true, RequireApproval: true, JoinPlacement: "about_after_rules",
		Rules: "## House rules\n\n1. Be kind to everyone.\n2. Share only your **own** art.\n3. No spam or advertising."}
	must(gc.Call(proto.Request{Type: "update_join", Join: &js}))
	must(gc.Call(proto.Request{Type: "captcha_put", Question: &proto.CaptchaQuestion{Type: "text", Text: "Type the name of this guild", Answers: []string{"Pixel Club"}}}))
	must(gc.Call(proto.Request{Type: "captcha_put", Question: &proto.CaptchaQuestion{Type: "radio", Text: "Which of these is a colour?", Options: []string{"Blue", "Chair", "Window"}, Correct: []int{0}}}))
	must(gc.Call(proto.Request{Type: "captcha_settings", Captcha: &proto.CaptchaSettings{Enabled: true, AskCount: 2, PassPercent: 100,
		Welcome: "Two quick questions to keep bots out.", PassMessage: "Welcome to Pixel Club!", FailMessage: "Not quite. Read the questions carefully."}}))
	fmt.Print(c.GuildID)
}

func join(home, name, addr string) {
	m := open(home, name)
	defer m.CloseAll()
	c, err := m.Check(addr)
	check(err)
	r, err := m.JoinStart(c.Address, c.GuildID, "", true)
	check(err)
	if r.Quiz != nil {
		var ans []proto.QuizAnswer
		for _, q := range r.Quiz.Questions {
			if q.Type == "text" {
				ans = append(ans, proto.QuizAnswer{Text: "Pixel Club"})
				continue
			}
			for i, o := range q.Options {
				if o == "Blue" {
					ans = append(ans, proto.QuizAnswer{Choices: []int{i}})
				}
			}
		}
		r, err = m.JoinAnswers(ans)
		check(err)
	}
	fmt.Print(r.Code)
}

func approve(home, name, addr string) {
	m := open(home, name)
	defer m.CloseAll()
	gc := conn(m, addr)
	r, err := gc.Call(proto.Request{Type: "requests"})
	check(err)
	for _, p := range r.People {
		must(gc.Call(proto.Request{Type: "approve", Target: p.UserKey}))
	}
	time.Sleep(time.Second)
}

func send(gc *guildconn.Conn, ch, text, reply string) {
	for i := 0; i < 40; i++ {
		if err := gc.Send(ch, text, reply); err == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	fail("could not send a message")
}

func chat(ownerHome, memberHome, addr string) {
	mo := open(ownerHome, "Alice")
	defer mo.CloseAll()
	mm := open(memberHome, "Ben")
	defer mm.CloseAll()
	alice, ben := conn(mo, addr), conn(mm, addr)
	r, err := alice.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: "category", Name: "Community"}})
	check(err)
	r, err = alice.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: "channel", Name: "general", Parent: r.Message, Topic: "Say hi and share what you are working on"}})
	check(err)
	ch := r.Message
	must(alice.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: "channel", Name: "showcase", Order: 1}}))
	send(alice, ch, "Welcome to **Pixel Club**! Please read the rules, then say hi.", "")
	for i := 0; i < 50; i++ { // Wait for Ben's app to get the channel's key.
		if msgs, _, err := ben.History(ch, ""); err == nil && len(msgs) > 0 && !msgs[0].Locked {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	send(ben, ch, "Hi @Alice, thanks for having me! Here is my first sprite: https://example.com/sprite", "")
	time.Sleep(time.Second)
	msgs, _, err := alice.History(ch, "")
	check(err)
	last := msgs[len(msgs)-1]
	check(alice.React(ch, last.ID, "👍", true))
	check(ben.React(ch, last.ID, "🎉", true))
	send(alice, ch, "Love the *colours*, Ben!", last.ID)
	time.Sleep(time.Second)
}

// roles makes two roles on the same rank, gives Ben the Mod role,
// and makes a staff channel only Mods can see.
func roles(home, benHome, addr string) {
	m := open(home, "Alice")
	defer m.CloseAll()
	gc := conn(m, addr)
	ben, err := user.ReadProfile(folder(benHome, "Ben"))
	check(err)
	put := func(r proto.Role) string {
		resp, err := gc.Call(proto.Request{Type: "role_put", Role: &r})
		check(err)
		if !resp.OK {
			fail(resp.Error)
		}
		return resp.Message
	}
	mod := put(proto.Role{Name: "Mod", Rank: 5, Color: "#8b7bff", Hoist: true, Mentionable: true,
		Perms: []string{proto.PermKick, proto.PermBan, proto.PermApproveJoins, proto.PermManageMessages}})
	put(proto.Role{Name: "Architect", Rank: 5, Color: "#22d3ee", Hoist: true, Perms: []string{proto.PermManageChannels}})
	put(proto.Role{Name: "Artist", Rank: 1, Color: "#f5a524"})
	must(gc.Call(proto.Request{Type: "member_roles", Target: ben.UserID, Roles: []string{mod}}))
	must(gc.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: "channel", Name: "staff", Order: 2,
		Topic: "Only Mods can see this",
		Overrides: []proto.Override{
			{Role: proto.EveryoneRole, Deny: []string{proto.PermViewChannel}},
			{Role: mod, Allow: []string{proto.PermViewChannel}},
		}}}))
	time.Sleep(time.Second)
}

func waitRoute(m *guildconn.Manager, peer string) {
	for i := 0; i < 60; i++ {
		for _, g := range m.Views() {
			for _, mem := range g.Members {
				if mem.UserKey == peer && g.Status == "online" {
					return
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	fail("the other person never showed up")
}

func idOf(home, name string) string {
	p, err := user.ReadProfile(folder(home, name))
	check(err)
	return p.UserID
}

// In the request step, Alice sends Ben a contact request.
func request(aHome, bHome, addr string) {
	a := open(aHome, "Alice")
	defer a.CloseAll()
	ben := idOf(bHome, "Ben")
	conn(a, addr)
	waitRoute(a, ben)
	check(a.RequestContact(ben))
	time.Sleep(time.Second)
}

// In the contacts step, Ben accepts, and they chat a little.
func contacts(aHome, bHome, addr string) {
	a, b := open(aHome, "Alice"), open(bHome, "Ben")
	defer a.CloseAll()
	defer b.CloseAll()
	alice, ben := idOf(aHome, "Alice"), idOf(bHome, "Ben")
	conn(a, addr)
	conn(b, addr)
	waitRoute(a, ben)
	waitRoute(b, alice)
	time.Sleep(time.Second) // Ben's app collects the request.
	check(b.AcceptContact(alice))
	time.Sleep(time.Second)
	say := func(m *guildconn.Manager, to, text, reply string) {
		check(m.SendDM(to, text, reply))
		time.Sleep(600 * time.Millisecond)
	}
	say(a, ben, "Hi Ben! Your sprite in #general was **great**.", "")
	say(b, alice, "Thank you! I'm working on a tile set next.", "")
	msgs, _ := a.Chat(ben)
	last := msgs[len(msgs)-1]
	check(a.ReactDM(ben, last.ID, "🎉", true))
	say(a, ben, "Can't wait to see it.", last.ID)
	b.Chat(alice) // Ben reads it, so Alice sees "Read".
	time.Sleep(time.Second)
	check(a.Verify(ben, b.MyCode(alice)))
}

// pages turns on Calendar, Threads and Wiki and fills them a little
// 7), then changes the Rules so Ben is told.
func pages(aHome, bHome, addr string) {
	a, b := open(aHome, "Alice"), open(bHome, "Ben")
	defer a.CloseAll()
	defer b.CloseAll()
	ga, gb := conn(a, addr), conn(b, addr)
	must(ga.Call(proto.Request{Type: "update_pages", Pages: &proto.PageSettings{Calendar: true, Threads: true, Wiki: true}}))
	now := time.Now()
	day := func(d, h int) string {
		return time.Date(now.Year(), now.Month(), d, h, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	must(ga.Call(proto.Request{Type: "event_put", Event: &proto.Event{Title: "Pixel Jam", Start: day(12, 13), StartTime: true,
		StartZone: "Asia/Kolkata", End: day(12, 17), EndTime: true, EndZone: "Asia/Kolkata",
		Details: "## Theme: Winter\n\nMake a **32×32** sprite in four hours.\n\n- Share it in #general\n- Votes on Sunday"}}))
	must(ga.Call(proto.Request{Type: "event_put", Event: &proto.Event{Title: "Palette Swap Week", Start: day(20, 0), End: day(26, 0)}}))
	r, err := ga.Call(proto.Request{Type: "image_put", Image: picture()})
	check(err)
	must(ga.Call(proto.Request{Type: "wiki_put", Wiki: &proto.WikiPage{Title: "Getting Started", Group: "Basics",
		Body: "Welcome to the **Pixel Club** wiki.\n\n![Our logo](nexus:" + r.Message + ")\n\n1. Read [[Palettes]]\n2. Say hi in #general\n3. Join the next jam"}}))
	must(ga.Call(proto.Request{Type: "wiki_put", Wiki: &proto.WikiPage{Title: "Palettes", Group: "Basics", Body: "Limited palettes make sprites read better."}}))
	must(ga.Call(proto.Request{Type: "wiki_put", Wiki: &proto.WikiPage{Title: "Jam Rules", Group: "Events", Body: "Four hours, one theme."}}))
	p, err := gb.Call(proto.Request{Type: "post_put", Post: &proto.Post{Title: "How do you choose a palette?",
		Body: "I keep using too many colours. Any tips?"}})
	check(err)
	c, err := ga.Call(proto.Request{Type: "comment_put", Comment: &proto.Comment{Post: p.Message, Body: "Start with **four** colours and add only when you must."}})
	check(err)
	must(gb.Call(proto.Request{Type: "comment_put", Comment: &proto.Comment{Post: p.Message, Parent: c.Message, Body: "That helps, thanks!"}}))
	must(ga.Call(proto.Request{Type: "vote", ItemID: p.Message, Vote: 1}))
	must(gb.Call(proto.Request{Type: "vote", Comment: &proto.Comment{Post: p.Message, ID: c.Message}, Vote: 1}))
	must(ga.Call(proto.Request{Type: "post_put", Post: &proto.Post{Title: "Winter jam is coming", Body: "See the Calendar!"}}))
	js := proto.JoinSettings{ShowJoinButton: true, ShowRulesButton: true, RulesEnabled: true, RulesPublic: true,
		RequireRules: true, RequireApproval: true, JoinPlacement: "about_after_rules",
		Rules: "## House rules\n\n1. Be kind to everyone.\n2. Share only your **own** art.\n3. No spam or advertising.\n4. Credit palettes you borrow."}
	must(ga.Call(proto.Request{Type: "update_join", Join: &js}))
	time.Sleep(time.Second)
}

// heart draws a small pixel heart for the demo emoji.
func heart() []byte {
	rows := []string{
		"..XX...XX..",
		".XXXX.XXXX.",
		"XXXXXXXXXXX",
		"XXXXXXXXXXX",
		".XXXXXXXXX.",
		"..XXXXXXX..",
		"...XXXXX...",
		"....XXX....",
		".....X.....",
	}
	const s = 8
	img := image.NewNRGBA(image.Rect(0, 0, 11*s, 11*s))
	for y, row := range rows {
		for x, c := range row {
			if c == 'X' {
				for dy := 0; dy < s; dy++ {
					for dx := 0; dx < s; dx++ {
						img.Set(x*s+dx, (y+1)*s+dy, color.NRGBA{255, 77, 141, 255})
					}
				}
			}
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// media adds a guild emoji, then uses it, a picture and a post link in
// #general.
func media(aHome, bHome, addr string) {
	a, b := open(aHome, "Alice"), open(bHome, "Ben")
	defer a.CloseAll()
	defer b.CloseAll()
	ga, gb := conn(a, addr), conn(b, addr)
	check(ga.PutEmoji("pixel_heart", heart()))
	check(ga.PutEmoji("sparkle", picture()))
	var ch string
	for _, v := range a.Views() {
		for _, n := range v.Channels {
			if n.Name == "general" {
				ch = n.ID
			}
		}
	}
	if ch == "" {
		fail("no #general")
	}
	check(gb.SendFile(ch, "winter-sprite.png", picture(), ""))
	send(ga, ch, ":pixel_heart: :pixel_heart:", "")
	r, err := ga.Call(proto.Request{Type: "threads"})
	check(err)
	if len(r.Posts) > 0 {
		send(ga, ch, "Palette tips are here: nexus-post:"+ga.ID()+"/"+r.Posts[len(r.Posts)-1].ID, "")
	}
	time.Sleep(time.Second)
	msgs, _, err := gb.History(ch, "")
	check(err)
	for _, m := range msgs {
		if _, ok := guildconn.ParseFileRef(m.Text); ok {
			for _, e := range ga.GuildEmoji() {
				if e.Name == "pixel_heart" {
					check(ga.React(ch, m.ID, ":pixel_heart:"+e.Hash, true))
				}
			}
		}
	}
	time.Sleep(time.Second)
}

// profiles gives Ben a picture, banner, bio and nickname, Alice a picture
// and a recovery code, and sets some notification choices.
func profiles(aHome, bHome, addr string) {
	a, b := open(aHome, "Alice"), open(bHome, "Ben")
	defer a.CloseAll()
	defer b.CloseAll()
	ga, gb := conn(a, addr), conn(b, addr)
	face := func(r, g, bl uint8, w, h int) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				t := float64(x+y) / float64(w+h)
				img.Set(x, y, color.NRGBA{uint8(float64(r) * (1 - t/2)), uint8(float64(g) * (1 - t/2)), uint8(float64(bl)*(1-t/3)) + uint8(40*t), 255})
				if (x/16+y/16)%3 == 0 && x > w/5 && x < 4*w/5 && y > h/5 && y < 4*h/5 {
					img.Set(x, y, color.NRGBA{255, 255, 255, 220})
				}
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, img)
		return buf.Bytes()
	}
	check(b.SetLook("Pixel artist from **Pune**. I make tiny sprites and big palettes.", face(34, 211, 238, 256, 256), face(245, 165, 36, 960, 320), false, false))
	check(gb.SetNick("", "Benny"))
	check(a.SetLook("Founder of Pixel Club.", face(255, 77, 141, 256, 256), nil, false, false))
	_, err := ga.Call(proto.Request{Type: "update_guild", DefaultNotify: strPtr(proto.NotifyMentions)})
	check(err)
	if _, err := a.User().MakeRecoveryCode(); err != nil {
		check(err)
	}
	time.Sleep(time.Second)
}

func strPtr(s string) *string { return &s }

// currency turns on the guild's currency and Shop, makes and gives coins,
// and has Ben send some to Alice and buy an item.
func currency(aHome, bHome, addr string) {
	a, b := open(aHome, "Alice"), open(bHome, "Ben")
	defer a.CloseAll()
	defer b.CloseAll()
	ga, gb := conn(a, addr), conn(b, addr)
	coin := func() []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				dx, dy := x-32, y-32
				switch d := dx*dx + dy*dy; {
				case d < 22*22:
					img.Set(x, y, color.NRGBA{255, 200, 60, 255})
				case d < 28*28:
					img.Set(x, y, color.NRGBA{214, 150, 20, 255})
				}
				if dx > -6 && dx < 6 && dy > -12 && dy < 12 && (dy < -6 || dy > 6 || dx < -2) {
					img.Set(x, y, color.NRGBA{170, 110, 10, 255})
				}
			}
		}
		var buf bytes.Buffer
		png.Encode(&buf, img)
		return buf.Bytes()
	}
	must(ga.Call(proto.Request{Type: "currency_update", Currency: &proto.CurrencySettings{Enabled: true, Name: "Pixels", Shop: true}, Image: coin()}))
	must(ga.Call(proto.Request{Type: "mint", Amount: 10000}))
	ben, err := user.ReadProfile(folder(bHome, "Ben"))
	check(err)
	must(ga.Call(proto.Request{Type: "give", Target: ben.UserID, Amount: 500}))
	must(ga.Call(proto.Request{Type: "give", Target: a.User().Profile.UserID, Amount: 200}))
	item, err := ga.Call(proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Sprite Review", Price: 120,
		Description: "Alice looks at your sprite and gives **three tips**."}, Image: picture()})
	check(err)
	// Coloured names, sold as roles that can't be worn together.
	red, err := ga.Call(proto.Request{Type: "role_put", Role: &proto.Role{Name: "Red Name", Rank: 1, Color: "#ef4444", Purchasable: true}})
	check(err)
	blue, err := ga.Call(proto.Request{Type: "role_put", Role: &proto.Role{Name: "Blue Name", Rank: 1, Color: "#3b82f6", Purchasable: true, Exclusive: []string{red.Message}}})
	check(err)
	redItem, err := ga.Call(proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Red Name", Price: 150, Type: proto.ShopRole, RoleID: red.Message,
		Description: "Your name in **red**. Can't be worn with Blue Name."}})
	check(err)
	must(ga.Call(proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Blue Name", Price: 150, Type: proto.ShopRole, RoleID: blue.Message,
		Description: "Your name in **blue**. Can't be worn with Red Name."}}))
	must(gb.Call(proto.Request{Type: "transfer", Target: a.User().Profile.UserID, Amount: 50}))
	must(gb.Call(proto.Request{Type: "buy", ItemID: item.Message}))
	must(gb.Call(proto.Request{Type: "buy", ItemID: redItem.Message}))
	// An auction, with a bid from Ben.
	ends := time.Now().AddDate(0, 0, 2)
	ends = time.Date(ends.Year(), ends.Month(), ends.Day(), 20, 0, 0, 0, time.Local)
	auction, err := ga.Call(proto.Request{Type: "shop_put", ShopItem: &proto.ShopItem{Name: "Golden Palette", Price: 80,
		Description: "A **one-of-a-kind** palette file, signed by Alice.", Sale: proto.SaleAuction, Ends: ends.UTC().Format(time.RFC3339)}})
	check(err)
	must(gb.Call(proto.Request{Type: "bid", ItemID: auction.Message, Amount: 95}))
	time.Sleep(time.Second)
}

// voiceChannel adds a voice channel to the Community category.
func voiceChannel(aHome, addr string) {
	a := open(aHome, "Alice")
	defer a.CloseAll()
	ga := conn(a, addr)
	r, err := ga.Call(proto.Request{Type: "channels"})
	check(err)
	parent := ""
	for _, n := range r.Channels {
		if n.Kind == proto.KindCategory {
			parent = n.ID
		}
	}
	must(ga.Call(proto.Request{Type: "channel_put", Node: &proto.ChannelNode{Kind: proto.KindVoice, Name: "Lounge", Parent: parent, Order: 5}}))
	// A few soundboard sounds. Their sound is Opus silence: only
	// their names show in pictures.
	silence := make([][]byte, 50)
	for i := range silence {
		silence[i] = []byte{0xF8, 0xFF, 0xFE}
	}
	for i, name := range []string{"Air Horn", "Applause", "Drum Roll", "Bell", "Laugh", "Sad Trombone"} {
		must(ga.Call(proto.Request{Type: "sound_put", Sound: &proto.Sound{Name: name}, SoundData: proto.PackSound(silence), Image: soundPicture(i)}))
	}
	time.Sleep(time.Second) // Alice's app makes the voice channel's key.
}

// voiceHold puts Ben in the voice channel, sharing a made-up screen, for
// seconds: long enough for a picture of Alice's app.
func voiceHold(bHome, addr, seconds string) {
	b := open(bHome, "Ben")
	defer b.CloseAll()
	gb := conn(b, addr)
	r, err := gb.Call(proto.Request{Type: "channels"})
	check(err)
	room := ""
	for _, n := range r.Channels {
		if n.Kind == proto.KindVoice {
			room = n.ID
		}
	}
	must(gb.Call(proto.Request{Type: "voice_join", ChannelID: room}))
	must(gb.Call(proto.Request{Type: "stream_start"}))
	capture.Demo = true
	shot, err := capture.Grab(capture.Target{ID: "window:1"})
	check(err)
	// A small JPEG of it, as the app's previews are.
	pw, ph := 320, 320*shot.H/shot.W
	img := image.NewRGBA(image.Rect(0, 0, pw, ph))
	for y := 0; y < ph; y++ {
		for x := 0; x < pw; x++ {
			i := ((y*shot.H/ph)*shot.W + x*shot.W/pw) * 4
			img.Set(x, y, color.RGBA{shot.Pix[i+2], shot.Pix[i+1], shot.Pix[i], 255})
		}
	}
	var jpg bytes.Buffer
	check(jpeg.Encode(&jpg, img, &jpeg.Options{Quality: 70}))
	var n int
	fmt.Sscanf(seconds, "%d", &n)
	for i := 0; i < n; i++ {
		if gb.HasVoiceKey(room) {
			gb.SendStream(room, proto.StreamPreview, false, uint32(i+1), jpg.Bytes())
		}
		time.Sleep(time.Second)
	}
}

// soundPicture draws a small picture for a demo soundboard button: a
// coloured square with a white shape.
func soundPicture(i int) []byte {
	colours := []color.NRGBA{{242, 95, 107, 255}, {250, 176, 59, 255}, {87, 181, 120, 255}, {88, 140, 240, 255}, {176, 108, 230, 255}, {60, 190, 200, 255}}
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			c := colours[i%len(colours)]
			dx, dy := x-32, y-32
			if dx*dx+dy*dy < 14*14 { // This draws a dot in the middle.
				c = color.NRGBA{255, 255, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	check(png.Encode(&b, img))
	return b.Bytes()
}

// telegram fills the chat with message extras: a poll and a quiz with
// votes, a spoiler, a forwarded message, a pin, an edited message, a
// disappearing message and a scheduled one.
func telegram(aHome, bHome, addr string) {
	mo := open(aHome, "Alice")
	defer mo.CloseAll()
	mm := open(bHome, "Ben")
	defer mm.CloseAll()
	alice, ben := conn(mo, addr), conn(mm, addr)
	r, err := alice.Call(proto.Request{Type: "channels"})
	check(err)
	general, showcase := "", ""
	for _, n := range r.Channels {
		switch n.Name {
		case "general":
			general = n.ID
		case "showcase":
			showcase = n.ID
		}
	}
	last := func(gc *guildconn.Conn, ch string) guildconn.ChatView {
		msgs, _, err := gc.History(ch, "")
		check(err)
		return msgs[len(msgs)-1]
	}
	// Forward a message from #showcase.
	send(alice, showcase, "The **Retro Sprites** pack is out: 200 free sprites!", "")
	time.Sleep(500 * time.Millisecond)
	news := last(alice, showcase)
	check(alice.Forward(general, guildconn.ForwardText(news.Text, "Alice", "Pixel Club / #showcase", news.Time)))
	// Post a message, then edit it.
	send(alice, general, "Game jam meetup on Saturday.", "")
	time.Sleep(300 * time.Millisecond)
	check(alice.Edit(general, last(alice, general).ID, "Game jam meetup on **Sunday**."))
	// Post a poll, with votes, and pin it.
	check(alice.SendPoll(general, "Which theme for the jam?", []string{"Space", "Ocean", "Forest"}, -1, guildconn.SendOptions{}))
	time.Sleep(500 * time.Millisecond)
	poll := last(alice, general)
	check(alice.Vote(general, poll.ID, 0))
	for i := 0; i < 30; i++ { // Wait until Ben's app sees the poll.
		if v := last(ben, general); v.Poll != nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	check(ben.Vote(general, poll.ID, 0))
	check(alice.Pin(general, poll.ID, true))
	// Post a quiz that Ben answers.
	check(alice.SendPoll(general, "How many colours can the NES show?", []string{"54", "64", "256"}, 0, guildconn.SendOptions{}))
	time.Sleep(500 * time.Millisecond)
	quiz := last(alice, general)
	check(ben.Vote(general, quiz.ID, 0))
	// Post a spoiler, and a message that disappears.
	check(ben.SendWith(general, "Finished the demo! The last boss is ||a giant pixel cat||.", "", guildconn.SendOptions{}))
	check(alice.SendWith(general, "Meetup door code: 4821.", "", guildconn.SendOptions{Disappear: 24 * time.Hour}))
	// Schedule a message for tomorrow at 18:00.
	at := time.Now().AddDate(0, 0, 1)
	at = time.Date(at.Year(), at.Month(), at.Day(), 18, 0, 0, 0, time.Local)
	check(alice.SendWith(general, "The jam starts now. Good luck, everyone!", "", guildconn.SendOptions{At: at}))
	time.Sleep(time.Second)
}
