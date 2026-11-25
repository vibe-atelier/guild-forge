package ui

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"nexus/internal/camera"
	"nexus/internal/capture"
	"nexus/internal/guildconn"
	"nexus/internal/proto"
	"nexus/internal/user"
	"nexus/internal/voice"
)

// These are switches for the guide's pictures and for checking screens. They
// are only read from the environment of a test run (docs/guide/screenshots.ps1);
// a normal start of Nexus sets none of them.
//
//	NEXUS_SCREEN           create | load | welcome | licenses | updates |
//	                       unlock:<user folder>
//	NEXUS_SCREEN_PASSWORD  unlocks the test user straight away
//	NEXUS_SCREEN_GUILD     <guild ID>[:<what>] opens a guild, where <what> is
//	                       settings, menu, leave, joinset, captcha, rules,
//	                       requests, member, chat, editor, newchannel,
//	                       roles, role, chanperms, calendar, event, editevent,
//	                       newevent, threads, post, newpost, wiki, wikipage,
//	                       editwiki, newwiki, pagesset; with the ID "home":
//	                       dm, verify, dmmenu, contactmenu, addcontact, blocked,
//	                       join:<address>, pair:<address>, quiz:<address>, passed
//	NEXUS_SHOT             saves a picture of the window there, then closes
//	NEXUS_SHOT_DELAY       milliseconds to wait before the picture (3000)

func (u *UI) startScreens() {
	switch s := os.Getenv("NEXUS_SCREEN"); {
	case s == "create":
		u.show(u.createUser())
	case s == "load":
		u.show(u.loadUser())
	case s == "licenses":
		u.show(u.licensesScreen())
	case s == "updates":
		u.show(u.updatesScreen())
	case strings.HasPrefix(s, "forgot:"):
		dir := strings.TrimPrefix(s, "forgot:")
		if p, err := user.ReadProfile(dir); err == nil {
			u.show(u.forgotPassword(dir, p))
		}
	case strings.HasPrefix(s, "unlock:"):
		dir := strings.TrimPrefix(s, "unlock:")
		u.openUser(dir, newErrorText())
		if pw := os.Getenv("NEXUS_SCREEN_PASSWORD"); pw != "" {
			if usr, err := user.Unlock(dir, pw); err == nil {
				u.signIn(usr)
				if gs := os.Getenv("NEXUS_SCREEN_GUILD"); gs != "" {
					go func() {
						// Wait for the guild to connect (or 5 seconds at most).
						id, _, _ := strings.Cut(gs, ":")
						for i := 0; i < 50 && id != "home"; i++ {
							var online bool
							fyne.DoAndWait(func() { g := u.findGuild(id); online = g != nil && g.Status == "online" })
							if online {
								break
							}
							time.Sleep(100 * time.Millisecond)
						}
						time.Sleep(250 * time.Millisecond)
						fyne.Do(func() { u.screenGuild(gs) })
					}()
				}
			}
		}
	}
	if shot := os.Getenv("NEXUS_SHOT"); shot != "" {
		capture.Demo = true // Pictures never show the real desktop.
		camera.Demo = true  // They never show the real camera either.
		delay := 3000
		if d, err := strconv.Atoi(os.Getenv("NEXUS_SHOT_DELAY")); err == nil {
			delay = d
		}
		go u.shootWhenStill(shot, time.Duration(delay)*time.Millisecond)
	}
}

func (u *UI) screenGuild(gs string) {
	id, what, _ := strings.Cut(gs, ":")
	what, arg, _ := strings.Cut(what, ":")
	if what == "join" || what == "pair" || what == "joinrules" {
		if arg != "" {
			arg = strings.TrimPrefix(gs, id+":"+what+":")
		}
		u.devAddress = arg
		u.devOpenRules = what == "joinrules"
		if what == "joinrules" {
			what = "join"
		}
		u.openAddGuildOn(what)
		return
	}
	if what == "quiz" || what == "quizq" {
		addr := strings.TrimPrefix(strings.TrimPrefix(gs, id+":quiz:"), id+":quizq:")
		u.devSkipWelcome = what == "quizq"
		go func() {
			c, err := u.guilds.Check(addr)
			if err != nil {
				return
			}
			r, err := u.guilds.JoinStart(c.Address, c.GuildID, "", true)
			if err != nil || r.Quiz == nil {
				return
			}
			fyne.Do(func() { u.startQuiz(c, r.Quiz) })
		}()
		return
	}
	if what == "passed" {
		u.joining = &joinView{c: &guildconn.PairCheck{Name: "Pixel Club", GuildID: id}}
		u.showQuizResult(true, "Welcome to Pixel Club!", "Your request now waits for the owner's approval.", "")
		return
	}
	if id == "home" {
		u.screenContacts(what)
		return
	}
	u.selectGuild(id)
	g := u.findGuild(id)
	if g == nil {
		return
	}
	if u.screenMore(*g, what) {
		return
	}
	switch what {
	case "settings":
		u.openGuildSettings(*g, "overview")
	case "menu":
		u.openGuildSettings(*g, "")
	case "members":
		u.openPage("members")
	case "leave":
		u.openLeave(*g)
	case "joinset":
		u.openGuildSettings(*g, "whojoins")
	case "captcha":
		u.openGuildSettings(*g, "questions")
	case "rules", "requests":
		u.openPage(what)
	case "member":
		for _, m := range g.Members {
			if !m.IsMe {
				u.openMember(*g, m)
				break
			}
		}
	case "newchannel":
		u.openChannelEditor(*g, nil)
	case "roles":
		u.openGuildSettings(*g, "roles")
	case "role":
		u.openGuildSettings(*g, "roles")
		for _, r := range g.Roles {
			if r.ID != "everyone" {
				r := r
				u.roleWindow(*g, &r)
				break
			}
		}
	case "chanperms":
		for _, n := range g.Channels {
			if len(n.Overrides) > 0 {
				u.openChannelSettings(*g, n.ChannelNode, "chanperms")
				break
			}
		}
	case "chat", "editor":
		for _, n := range g.Channels {
			if n.Kind == "channel" {
				if what == "editor" {
					u.openChannelEditor(*g, &n.ChannelNode)
				} else {
					u.openChat(n.ID)
				}
				break
			}
		}
	}
}

// devTab is the tab a window opens on, for pictures only (see tabs).
var devTab int

// screenMore handles the extra screens used for pictures of every screen.
func (u *UI) screenMore(g guildconn.GuildView, what string) bool {
	later := func(f func()) { // This runs after the chat has loaded.
		go func() {
			// Until the chat has its messages (3 seconds at most).
			for i := 0; i < 30; i++ {
				var ready bool
				fyne.DoAndWait(func() { ready = u.chat != nil && !u.chat.loading && len(u.chat.msgs) > 0 })
				if ready {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			time.Sleep(200 * time.Millisecond)
			fyne.Do(f)
		}()
	}
	firstChannel := func() string {
		for _, n := range g.Channels {
			if n.Kind == "channel" {
				return n.ID
			}
		}
		return ""
	}
	lastMsg := func(mine bool) *guildconn.ChatView {
		if u.chat == nil {
			return nil
		}
		for i := len(u.chat.msgs) - 1; i >= 0; i-- {
			if !mine || u.chat.msgs[i].Mine {
				return &u.chat.msgs[i]
			}
		}
		return nil
	}
	switch what {
	case "rulestab":
		u.openGuildSettings(g, "rulesset")
	case "bank":
		devTab = 1
		u.openGuildSettings(g, "questions")
	case "question", "newquestion":
		u.openGuildSettings(g, "questions") // The editor goes into this page.
		if what == "newquestion" {
			go func() { time.Sleep(time.Second); fyne.Do(func() { u.questionWindow(g, nil) }) }()
			return true
		}
		u.call(g.ID, proto.Request{Type: "captcha_get"}, func(r proto.Response, err error) {
			if err == nil && len(r.Questions) > 0 {
				q := r.Questions[len(r.Questions)-1]
				go func() { time.Sleep(time.Second); fyne.Do(func() { u.questionWindow(g, &q) }) }()
			}
		})
	case "bans":
		u.openGuildSettings(g, "bans")
	case "transfer":
		u.openGuildSettings(g, "ownership")
	case "newrole":
		u.openGuildSettings(g, "roles")
		u.roleWindow(g, nil)
	case "chanmessages":
		for _, n := range g.Channels {
			if n.Kind == "channel" {
				u.openChannelSettings(g, n.ChannelNode, "chanmessages")
				break
			}
		}
	case "everyone":
		u.openGuildSettings(g, "roles")
		for _, r := range g.Roles {
			if r.ID == proto.EveryoneRole {
				r := r
				u.roleWindow(g, &r)
			}
		}
	case "toast":
		u.toast("Role saved")
	case "notify":
		u.openGuildSettings(g, "notify")
	case "chnotify":
		u.openChat(firstChannel())
		later(func() {
			for _, n := range g.Channels {
				if n.ID == firstChannel() {
					u.openNotify(g, n.ID, n.Name)
				}
			}
		})
	case "nick":
		u.openGuildSettings(g, "nickname")
	case "emoji":
		u.openGuildSettings(g, "emoji")
	case "share":
		for _, n := range g.Channels {
			if n.Kind == proto.KindVoice {
				u.openPageArg("share", n.ID)
				return true
			}
		}
	case "soundboard":
		u.openGuildSettings(g, "soundboard")
	case "voice", "voicein", "voicelive", "voicemod", "voicecam":
		for _, n := range g.Channels {
			if n.Kind == proto.KindVoice {
				id := n.ID
				if what == "voice" {
					u.openPageArg("voice", id)
					return true
				}
				go func() { // Wait until this app has the voice key.
					for i := 0; i < 40; i++ {
						if gc, err := u.guilds.Get(g.ID); err == nil && gc.HasVoiceKey(id) {
							break
						}
						time.Sleep(100 * time.Millisecond)
					}
					fyne.Do(func() { u.joinVoice(g, id) })
					if what == "voicelive" { // Share a made-up screen.
						time.Sleep(700 * time.Millisecond)
						if list, err := capture.Targets(); err == nil {
							u.voice.StartSharing(list[0], voice.Qualities[0], false)
						}
					}
					if what == "voicecam" { // This shows the made-up camera, and sharing with it in a corner.
						time.Sleep(700 * time.Millisecond)
						u.voice.StartCamera()
						if list, err := capture.Targets(); err == nil {
							u.voice.StartSharing(list[0], voice.Qualities[0], false)
						}
					}
					if what == "voicemod" { // Choose Ben, who is sharing, with the soundboard open.
						u.showBoard = true
						time.Sleep(time.Second)
						fyne.Do(func() {
							if gv := u.findGuild(g.ID); gv != nil {
								for _, c := range gv.Channels {
									for _, v := range c.Voice {
										if c.ID == id && v.User != u.user.Profile.UserID {
											u.selVoice = v.User
										}
									}
								}
							}
							u.redraw()
						})
					}
				}()
				return true
			}
		}
	// These are the message extras' screens.
	case "pollnew", "schedule", "scheduled", "pinned", "edithistory", "forward":
		u.openChat(firstChannel())
		later(func() {
			if u.chat == nil {
				return
			}
			edited := func() *guildconn.ChatView {
				for i := range u.chat.msgs {
					if u.chat.msgs[i].EditCount > 0 {
						return &u.chat.msgs[i]
					}
				}
				return lastMsg(true)
			}
			switch what {
			case "pollnew":
				u.openPollEditor()
			case "schedule":
				u.openSchedule()
			case "scheduled":
				u.openScheduledList()
			case "pinned":
				u.openPinned()
			case "edithistory":
				if m := edited(); m != nil {
					u.openEditHistory(*m)
				}
			case "forward":
				if m := edited(); m != nil {
					u.openForward(*m)
				}
			}
		})
	case "emojipick":
		u.openChat(firstChannel())
		later(func() {
			if u.chat != nil {
				u.openEmojiPicker(&u.chat.entry.Entry, g.ID, nil)
			}
		})
	case "bid": // This is the Bid window on the demo auction.
		u.openPage("shop")
		go func() {
			gc, err := u.guilds.Get(g.ID)
			if err != nil {
				return
			}
			r, err := gc.Call(proto.Request{Type: "shop"})
			if err != nil {
				return
			}
			for _, it := range r.ShopItems {
				if it.Sale == proto.SaleAuction {
					it := it
					time.Sleep(time.Second)
					fyne.Do(func() { u.openBid(g, it) })
					return
				}
			}
		}()
	case "calendar", "threads", "wiki", "wallet", "shop", "balances":
		u.openPage(what)
	case "coinbank":
		u.openBank(g)
	case "currency":
		u.openGuildSettings(g, "currency")
	case "shopitem":
		u.openShopItem(g, nil)
	case "sendcoins":
		for _, m := range g.Members {
			if !m.IsMe {
				u.openSendCoins(g, m.UserKey, 50)
				break
			}
		}
	case "pagesset":
		u.openGuildSettings(g, "pages")
	case "newevent":
		u.openEventEditor(g, nil)
	case "newpost":
		u.openPostEditor(g, nil)
	case "newwiki":
		u.openWikiEditor(g, nil)
	case "event", "editevent":
		u.call(g.ID, proto.Request{Type: "events"}, func(r proto.Response, err error) {
			if err == nil && len(r.Events) > 0 {
				e := r.Events[0]
				u.calMonth = time.Time{}
				if what == "editevent" {
					u.openEventEditor(g, &e)
					return
				}
				u.openPageArg("event", e.ID)
			}
		})
	case "post":
		u.call(g.ID, proto.Request{Type: "threads"}, func(r proto.Response, err error) {
			if err == nil && len(r.Posts) > 0 {
				best := r.Posts[0] // Find the post with the most comments.
				for _, p := range r.Posts {
					if p.Comments > best.Comments {
						best = p
					}
				}
				u.openPageArg("post", best.ID)
			}
		})
	case "wikipage", "editwiki":
		u.call(g.ID, proto.Request{Type: "wiki_get", ItemID: "Getting Started"}, func(r proto.Response, err error) {
			if err == nil && r.Wiki != nil {
				if what == "editwiki" {
					u.openWikiEditor(g, r.Wiki)
					return
				}
				u.openPageArg("wikipage", r.Wiki.ID)
			}
		})
	case "msgmenu", "reply", "editing", "mention", "confirm":
		u.openChat(firstChannel())
		later(func() {
			switch what {
			case "msgmenu":
				if m := lastMsg(false); m != nil {
					u.messageMenu(m, nil)
				}
			case "reply":
				if m := lastMsg(false); m != nil {
					u.startReply(m)
				}
			case "editing":
				if m := lastMsg(true); m != nil {
					u.startEdit(m)
				}
			case "mention":
				u.chat.entry.SetText("Thanks @")
			case "confirm":
				u.confirm("Delete Message", "Delete this message? This cannot be undone.", "Delete", true, func() {})
			}
		})
	default:
		return false
	}
	return true
}

// screenContacts opens the contact screens for pictures.
func (u *UI) screenContacts(what string) {
	var first *guildconn.ContactView
	for _, c := range u.guilds.Contacts() {
		if c.State == guildconn.ContactActive {
			c := c
			first = &c
			break
		}
	}
	switch what {
	case "addcontact":
		u.openAddContact()
	case "editprofile":
		u.openUserSettings("profile")
	case "settings", "privacy", "notifications", "updates", "licenses":
		u.openUserSettings(map[string]string{"settings": "account"}[what] + map[bool]string{true: what}[what != "settings"])
	case "profile":
		u.homeView = "profile"
		u.redraw()
	case "requests":
		u.friendsTab = 1
		u.redraw()
	case "voiceset":
		u.openUserSettings("voice")
	case "password":
		u.openUserSettings("account")
		u.openChangePassword()
	case "recovery":
		u.openUserSettings("account")
		u.openRecoveryCode()
	case "blocked":
		u.friendsTab = 2
		u.redraw()
	case "dm", "verify", "dmmenu", "dmsearch", "clearchat", "contactmenu":
		if first == nil {
			return
		}
		u.openDM(first.UserID)
		u.shotPending = true // The picture waits for the step below.
		go func() {
			time.Sleep(1200 * time.Millisecond)
			fyne.Do(func() {
				defer func() { u.shotPending = false }()
				switch what {
				case "verify":
					u.openVerify(*first)
				case "clearchat":
					u.openClearChat(*first)
				case "contactmenu":
					u.contactMenu(*first, nil)
				case "dmsearch":
					u.openDMSearch()
					u.dm.searchEntry.SetText("tile")
				case "dmmenu":
					if u.dm != nil && len(u.dm.msgs) > 0 {
						u.dmMenu(&u.dm.msgs[len(u.dm.msgs)-1], nil)
					}
				}
			})
		}()
	}
}

// shootWhenStill saves the picture as soon as the window has stopped
// changing (the same picture three times in a row, a quarter of a second
// apart), instead of always waiting the longest time a screen might need.
// The given delay plus two seconds is the most it waits.
func (u *UI) shootWhenStill(shot string, delay time.Duration) {
	start := time.Now()
	var last [32]byte
	same := 0
	for time.Since(start) < delay+2*time.Second {
		time.Sleep(250 * time.Millisecond)
		var sum [32]byte
		var pending bool
		fyne.DoAndWait(func() { sum = pictureSum(u.win.Canvas().Capture()) })
		fyne.DoAndWait(func() { pending = u.shotPending })
		if sum == last {
			same++
		} else {
			same = 0
		}
		last = sum
		if same >= 3 && !pending && time.Since(start) > time.Second {
			break
		}
	}
	fyne.DoAndWait(func() { u.savePicture(shot) })
	os.Exit(0) // This is a test run, so there is no need to close politely.
}

// pictureSum is a fingerprint of a picture, to tell whether it changed.
func pictureSum(img image.Image) [32]byte {
	switch im := img.(type) {
	case *image.NRGBA:
		return sha256.Sum256(im.Pix)
	case *image.RGBA:
		return sha256.Sum256(im.Pix)
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return sha256.Sum256(b.Bytes())
}
