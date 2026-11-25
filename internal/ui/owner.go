package ui

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
	"nexus/internal/proto"
)

// This file has the owner's tools: Joining & Rules, the CAPTCHA editor, bans and
// member actions. Labels follow the naming convention: one or two
// capitalised words, with a plain hint underneath.

// option is a switch with a short label and a hint under it.
func option(label, hint string, on bool) (*widget.Check, fyne.CanvasObject) {
	c := widget.NewCheck(label, nil)
	c.SetChecked(on)
	if hint == "" {
		return c, flushCheck(c)
	}
	// The box and the hint both start in the text column.
	return c, stack(2, flushCheck(c), hintText(sentence(hint)))
}

// flushCheck moves a check box so its square starts in the text column,
// under the labels and hints.
func flushCheck(c *widget.Check) fyne.CanvasObject { return flushLeft(c) }

// flushLeft does the same for radio buttons and groups of check boxes.
func flushLeft(o fyne.CanvasObject) fyne.CanvasObject {
	// Fyne also leaves room above and below; take some away, so the gaps
	// around a check box look like the gaps around everything else.
	return container.New(layout.NewCustomPaddedLayout(-checkTrim, -checkTrim, boxInset-checkPad, 0), o)
}

// checkPad is the room Fyne leaves before a check box.
const (
	checkPad  = 9
	checkTrim = 6 // This much room is taken off above and below.
)

func numberEntry(n int) *widget.Entry {
	e := widget.NewEntry()
	e.SetText(strconv.Itoa(n))
	return e
}

func number(e *widget.Entry) int {
	n, _ := strconv.Atoi(strings.TrimSpace(e.Text))
	return n
}

// tabs is a row of buttons that switches what is shown below it.
func tabs(names []string, panes []fyne.CanvasObject) fyne.CanvasObject {
	body := container.NewStack()
	show := func(i int) {
		body.Objects = []fyne.CanvasObject{panes[i]}
		body.Refresh()
	}
	first := devTab
	devTab = 0
	show(first)
	return container.NewVBox(tabBar(nil, names, first, show), gap(8), body)
}

// ---- Joining & Rules ------------------------------------------------------

// which is "whojoins" or "rulesset": the settings page shows one at a time.
func (u *UI) openJoinSettings(g guildconn.GuildView, which string) {
	u.call(g.ID, proto.Request{Type: "join_settings"}, func(r proto.Response, err error) {
		if err != nil || r.JoinSettings == nil {
			u.toast("Could not load the join settings.")
			return
		}
		u.joinSettingsWindow(g, *r.JoinSettings, which)
	})
}

func (u *UI) joinSettingsWindow(g guildconn.GuildView, j proto.JoinSettings, which string) {
	var p *popup
	showJoin, showJoinBox := option("Join Button", "Turn off to stop taking new members (invited people can still join).", j.ShowJoinButton)
	whitelist, whitelistBox := option("Invite Only", "Only the user IDs below may join.", j.Whitelist)
	allowed := widget.NewMultiLineEntry()
	allowed.SetText(strings.Join(j.Allowed, "\n"))
	allowed.SetPlaceHolder("One User ID Per Line")
	allowed.SetMinRowsVisible(3)
	age := numberEntry(j.MinAccountDays)
	count := numberEntry(j.MinServerCount)
	pass := widget.NewPasswordEntry()
	passState := "No password."
	if j.HasPassword {
		passState = "A password is set. Type a new one to change it."
	}
	var passChange *string
	pass.OnChanged = func(s string) {
		if s == "" {
			passChange = nil
		} else {
			passChange = &s
		}
	}
	passHint := text(passState, 12, colFaint, false)
	clearPass := widget.NewButton("Remove", func() {
		empty := ""
		passChange = &empty
		pass.SetText("")
		passChange = &empty
		passHint.Text = "The password will be removed when you save."
		passHint.Refresh()
	})
	approval, approvalBox := option("Approve Joins", "New people wait in Join Requests until you accept or decline them.", j.RequireApproval)
	wait := numberEntry(j.PostWaitMinutes)

	rulesOn, rulesOnBox := option("Rules Page", "Show a Rules page in the guild.", j.RulesEnabled)
	rules := widget.NewMultiLineEntry()
	rules.SetText(j.Rules)
	rules.Wrapping = fyne.TextWrapWord
	rules.SetMinRowsVisible(18)
	rulesCount := text("", 12, colFaint, false)
	countRules := func(s string) {
		rulesCount.Text = fmt.Sprintf("%d / 10000", utf8.RuneCountInString(s))
		rulesCount.Refresh()
	}
	rules.OnChanged = countRules
	countRules(j.Rules)
	require, requireBox := option("Must Agree", `People tick "I agree to follow these rules." to join.`, j.RequireRules)
	public, publicBox := option("Public Rules", "Anyone with the address can read the rules.", j.RulesPublic)
	showRules, showRulesBox := option("Rules Button", "Show a Rules button on the About page.", j.ShowRulesButton)
	placements := []string{"About Page", "Rules Page", "After Agreeing"}
	values := []string{"about", "rules", "about_after_rules"}
	place := widget.NewSelect(placements, nil)
	place.SetSelectedIndex(0)
	for i, v := range values {
		if v == j.JoinPlacement {
			place.SetSelectedIndex(i)
		}
	}

	who := form(showJoinBox, whitelistBox,
		field("Invited Users", allowed, ""),
		container.NewGridWithColumns(2,
			field("Account Age", age, "Minimum age in days (0 = any)."),
			field("Other Guilds", count, "Must already be in this many (0 = any).")),
		stack(fieldGap, text("Guild Password", 13, colDim, true), container.NewBorder(nil, nil, nil, clearPass, pass), passHint),
		approvalBox,
		field("Waiting Period", wait, "Minutes before new members can post or react. Members with a role don't wait."))
	rulesPane := form(rulesOnBox,
		stack(fieldGap, labelRow("Rules", rulesCount), rules,
			hintText("Markdown works here. Long terms and conditions are welcome.")),
		requireBox, publicBox, showRulesBox,
		field("Join Button", place, "Where the Join button appears for newcomers."))

	problem := newErrorText()
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		js := proto.JoinSettings{
			ShowJoinButton: showJoin.Checked, ShowRulesButton: showRules.Checked, Whitelist: whitelist.Checked,
			Allowed: strings.Fields(allowed.Text), MinAccountDays: number(age), MinServerCount: number(count),
			RulesEnabled: rulesOn.Checked, Rules: rules.Text, RulesPublic: public.Checked, RequireRules: require.Checked,
			JoinPlacement: values[max(0, place.SelectedIndex())], RequireApproval: approval.Checked,
			PostWaitMinutes: number(wait), Password: passChange,
		}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "update_join", Join: &js}, func(_ proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("Conditions Saved")
		})
	})
	title, pane := "Joining Conditions", fyne.CanvasObject(who)
	if which == "rulesset" {
		title, pane = "Rules", rulesPane
	}
	p = u.openPanel(which, 620, panelTitle(title), pane,
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
}

// ---- CAPTCHA editor -----------------------------------------

var questionKinds = []string{"Pick One", "Dropdown", "Pick All", "Typed"}
var questionTypes = []string{"radio", "dropdown", "checkbox", "text"}

func kindName(t string) string {
	for i, v := range questionTypes {
		if v == t {
			return questionKinds[i]
		}
	}
	return t
}

func (u *UI) openCaptcha(g guildconn.GuildView) {
	u.call(g.ID, proto.Request{Type: "captcha_get"}, func(r proto.Response, err error) {
		if err != nil || r.CaptchaFull == nil {
			u.toast("Could not load the CAPTCHA.")
			return
		}
		u.captchaWindow(g, *r.CaptchaFull, r.Questions)
	})
}

func (u *UI) captchaWindow(g guildconn.GuildView, s proto.CaptchaSettings, questions []proto.CaptchaQuestion) {
	var p *popup
	on, onBox := option("Enable CAPTCHA", "Ask newcomers your questions before they can join.", s.Enabled)
	ask := numberEntry(s.AskCount)
	passMark := numberEntry(s.PassPercent)
	welcome := widget.NewMultiLineEntry()
	welcome.SetText(s.Welcome)
	welcome.SetMinRowsVisible(2)
	passMsg := widget.NewEntry()
	passMsg.SetText(s.PassMessage)
	failMsg := widget.NewEntry()
	failMsg.SetText(s.FailMessage)

	list := stack(4) // This is a column, so text lines up.
	fill := func() {
		list.RemoveAll()
		if len(questions) == 0 {
			list.Add(para("No questions yet. Add one, then turn the CAPTCHA on."))
		}
		for _, q := range questions {
			q := q
			title := q.Text
			if title == "" {
				title = "(picture only)"
			}
			if q.HasImage {
				title = "🖼 " + title
			}
			b := widget.NewButton(kindName(q.Type)+"  ·  "+title, func() { u.questionWindow(g, &q) })
			b.Alignment = widget.ButtonAlignLeading
			list.Add(b)
		}
	}
	fill()
	add := widget.NewButton("Add Question", func() { u.questionWindow(g, nil) })
	if len(questions) >= 16 {
		add.Disable()
	}

	problem := newErrorText()
	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		cs := proto.CaptchaSettings{Enabled: on.Checked, AskCount: number(ask), PassPercent: number(passMark),
			Welcome: welcome.Text, PassMessage: passMsg.Text, FailMessage: failMsg.Text}
		save.Disable()
		u.call(g.ID, proto.Request{Type: "captcha_settings", Captcha: &cs}, func(_ proto.Response, err error) {
			save.Enable()
			if err != nil {
				problem.Set(err)
				return
			}
			p.Close()
			u.toast("CAPTCHA Saved")
		})
	})
	settings := form(onBox,
		container.NewGridWithColumns(2, field("Questions Asked", ask, "How many to ask, from 1 to 10."), field("Pass Mark", passMark, "Percent right to pass.")),
		field("Welcome", welcome, "Shown on the first page."),
		container.NewGridWithColumns(2, field("Passed", passMsg, ""), field("Failed", failMsg, "")))
	bank := form(container.NewBorder(nil, nil, text(fmt.Sprintf("QUESTIONS (%d / 16)", len(questions)), 12, colDim, true), add), list)
	p = u.openPanel("questions", 640, panelTitle("CAPTCHA"),
		tabs([]string{"Settings", "Question Bank"}, []fyne.CanvasObject{settings, bank}),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
}

// questionWindow adds or edits one question (q == nil adds).
func (u *UI) questionWindow(g guildconn.GuildView, q *proto.CaptchaQuestion) {
	var p *popup
	back := func() { devTab = 1; p.Close() } // Go back to the Question Bank.
	kind := widget.NewSelect(questionKinds, nil)
	question := widget.NewMultiLineEntry()
	question.SetMinRowsVisible(2)
	alt := widget.NewEntry()
	alt.SetPlaceHolder("Optional")
	picNote := "PNG or JPG, up to 10 MB."
	caseSensitive := widget.NewCheck("Match Case", nil)
	// Weightage and Always Ask.
	weights := []string{"Must Always Get Right", "Regular", "Shouldn't Count"}
	weightValues := []string{proto.WeightMust, "", proto.WeightNone}
	weight := widget.NewSelect(weights, nil)
	weight.SetSelectedIndex(1)
	always := widget.NewCheck("Always ask this question.", nil)

	var opts []*widget.Entry
	var right []*widget.Check
	optBox := stack(fieldGap)
	addOpt := func(s string, ok bool) {
		e := widget.NewEntry()
		e.SetText(s)
		e.SetPlaceHolder("Choice")
		c := widget.NewCheck("", nil)
		c.SetChecked(ok)
		opts, right = append(opts, e), append(right, c)
		optBox.Add(container.NewBorder(nil, nil, c, nil, e))
	}
	answers := []*widget.Entry{widget.NewEntry(), widget.NewEntry(), widget.NewEntry(), widget.NewEntry()}
	ansBox := stack(fieldGap)
	for _, a := range answers {
		a.SetPlaceHolder("Accepted Answer")
		ansBox.Add(a)
	}

	kind.SetSelectedIndex(0)
	if q != nil {
		for i, t := range questionTypes {
			if t == q.Type {
				kind.SetSelectedIndex(i)
			}
		}
		question.SetText(q.Text)
		alt.SetText(q.ImageAlt)
		caseSensitive.SetChecked(q.CaseSensitive)
		for i, v := range weightValues {
			if v == q.Weight {
				weight.SetSelectedIndex(i)
			}
		}
		always.SetChecked(q.AlwaysAsk)
		for i, o := range q.Options {
			ok := false
			for _, c := range q.Correct {
				ok = ok || c == i
			}
			addOpt(o, ok)
		}
		for i, a := range q.Answers {
			if i < len(answers) {
				answers[i].SetText(a)
			}
		}
	}
	for len(opts) < 2 {
		addOpt("", false)
	}
	moreOpt := widget.NewButton("Add Choice", func() {
		if len(opts) >= 10 {
			u.toast("At most 10 choices")
			return
		}
		addOpt("", false)
	})
	choices := stack(fieldGap, text("Choices", 13, colDim, true), optBox, hintText("Tick the right answers."), container.NewHBox(moreOpt))
	typed := stack(fieldGap, text("Accepted Answers", 13, colDim, true), ansBox, hintText("Up to 4. Any of them is right."), flushCheck(caseSensitive))
	kind.OnChanged = func(string) {
		if questionTypes[kind.SelectedIndex()] == "text" {
			choices.Hide()
			typed.Show()
		} else {
			typed.Hide()
			choices.Show()
		}
	}
	kind.OnChanged("")

	// It is the question's new picture.
	var newImage []byte
	clearImage := false
	picBox := container.NewStack()
	if q != nil && q.HasImage {
		u.call(g.ID, proto.Request{Type: "captcha_image", QuestionID: q.ID}, func(r proto.Response, err error) {
			if err == nil && len(r.Image) > 0 && newImage == nil && !clearImage {
				picBox.Objects = []fyne.CanvasObject{pictureFromBytes("q-"+q.ID, r.Image, 120)}
				picBox.Refresh()
			}
		})
	}
	problem := newErrorText()
	choose := widget.NewButton("Add Picture", func() {
		go func() {
			path, err := choosePicture("Choose a picture for the question")
			fyne.Do(func() {
				if err != nil || path == "" {
					if err != nil {
						problem.Set(err)
					}
					return
				}
				data, err := os.ReadFile(path)
				if err == nil && len(data) > 10<<20 {
					err = errors.New("The picture is larger than 10 MB.")
				}
				if err != nil {
					problem.Set(err)
					return
				}
				newImage, clearImage = data, false
				picBox.Objects = []fyne.CanvasObject{pictureFromBytes(fmt.Sprintf("qn-%d", time.Now().UnixNano()), data, 120)}
				picBox.Refresh()
			})
		}()
	})
	removePic := widget.NewButton("Remove", func() {
		newImage, clearImage = nil, true
		picBox.Objects = nil
		picBox.Refresh()
	})

	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		t := questionTypes[max(0, kind.SelectedIndex())]
		cq := proto.CaptchaQuestion{Type: t, Text: question.Text, ImageAlt: alt.Text, CaseSensitive: caseSensitive.Checked,
			Weight: weightValues[max(0, weight.SelectedIndex())], AlwaysAsk: always.Checked}
		if q != nil {
			cq.ID = q.ID
		}
		if t == "text" {
			for _, a := range answers {
				if a.Text != "" {
					cq.Answers = append(cq.Answers, a.Text)
				}
			}
		} else {
			for i, e := range opts {
				if strings.TrimSpace(e.Text) == "" {
					continue
				}
				if right[i].Checked {
					cq.Correct = append(cq.Correct, len(cq.Options))
				}
				cq.Options = append(cq.Options, e.Text)
			}
		}
		img := newImage
		save.Disable()
		go func() {
			req := proto.Request{Type: "captcha_put", Question: &cq, ClearImage: clearImage}
			var err error
			if len(img) > 0 {
				req.Image, err = imaging.FitPicture(img, 800, 900*1024)
			}
			fyne.Do(func() {
				if err != nil {
					save.Enable()
					problem.Set(err)
					return
				}
				u.call(g.ID, req, func(_ proto.Response, err error) {
					save.Enable()
					if err != nil {
						problem.Set(err)
						return
					}
					u.toast("Question saved")
					back()
				})
			})
		}()
	})
	buttons := []fyne.CanvasObject{widget.NewButton("Back", back)}
	if q != nil {
		del := widget.NewButton("Delete", func() {
			u.call(g.ID, proto.Request{Type: "captcha_delete", QuestionID: q.ID}, func(_ proto.Response, err error) {
				if err != nil {
					problem.Set(err)
					return
				}
				u.toast("Question deleted")
				back()
			})
		})
		del.Importance = widget.DangerImportance
		buttons = append([]fyne.CanvasObject{del}, buttons...)
	}
	title := "New Question"
	if q != nil {
		title = "Edit Question"
	}
	p = u.openPanel("questions", 600, panelTitle(title),
		container.NewGridWithColumns(2, field("Kind", kind, ""), field("Weightage", weight, "")),
		field("Question", question, ""),
		field("Picture", stack(fieldGap, picBox, container.NewHBox(choose, removePic)), picNote),
		field("Picture Description", alt, "Read out to people who can't see the picture."),
		choices, typed,
		flushCheck(always),
		problem.box, buttonsRow(append(buttons, save)...))
}

// ---- bans and member actions ------------------------------------

func (u *UI) openBans(g guildconn.GuildView) {
	var p *popup
	list := stack(4, para("Loading…")) // This is a column, so text lines up.
	var load func()
	load = func() {
		u.call(g.ID, proto.Request{Type: "bans"}, func(r proto.Response, err error) {
			list.RemoveAll()
			if err != nil {
				list.Add(para(sentence(err.Error())))
				return
			}
			if len(r.People) == 0 {
				list.Add(para("Nobody is banned."))
			}
			for _, b := range r.People {
				b := b
				when := b.At
				if t, err := time.Parse(time.RFC3339, b.At); err == nil {
					when = t.Local().Format("2 Jan 2006")
				}
				if b.IP != "" {
					when = "Address blocked too · " + when
				}
				list.Add(personLine(b, when, widget.NewButton("Unban", func() {
					u.call(g.ID, proto.Request{Type: "unban", Target: b.UserKey}, func(_ proto.Response, err error) {
						if err != nil {
							u.toast(sentence(err.Error()))
							return
						}
						u.toast("Unbanned")
						load()
					})
				})))
			}
			list.Refresh()
		})
	}
	load()
	p = u.openPanel("bans", 480, text("Bans", 22, colText, true), list, buttonsRow(widget.NewButton("Close", func() { p.Close() })))
}

func (u *UI) openMember(g guildconn.GuildView, m guildconn.MemberView) {
	var p *popup
	name := m.Name
	if name == "" {
		name = shortID(m.UserKey)
	}
	blockIP, blockBox := option("Block Address", "Also block the internet address they last used. This can block other people who share it (family, school, mobile networks).", false)
	problem := newErrorText()
	act := func(kind, done string) func() {
		return func() {
			u.call(g.ID, proto.Request{Type: kind, Target: m.UserKey, BanIP: kind == "ban" && blockIP.Checked}, func(_ proto.Response, err error) {
				if err != nil {
					problem.Set(err)
					return
				}
				p.Close()
				u.toast(done)
			})
		}
	}
	// The short ID, with Copy for the whole one.
	id := text(shortID(m.UserKey), 13, colDim, false)
	id.TextStyle.Monospace = true
	copyID := widget.NewButtonWithIcon("Copy ID", copyIcon, func() {
		u.app.Clipboard().SetContent(m.UserKey)
		u.toast("ID Copied")
	})
	copyID.Importance = widget.LowImportance
	parts := []fyne.CanvasObject{u.memberHeader(g, m), container.NewHBox(container.NewCenter(id), copyID)}
	section := func(title string, content ...fyne.CanvasObject) {
		parts = append(parts, sideLineH(), stack(fieldGap+2, append([]fyne.CanvasObject{text(title, 11.5, colFaint, true)}, content...)...))
	}
	buttons := []fyne.CanvasObject{widget.NewButton("Close", func() { p.Close() })}

	// Everyone gets the contact tools.
	var state string
	if c := u.contactView(m.UserKey); c != nil {
		state = c.State
	}
	people := container.NewHBox()
	switch state {
	case "":
		people.Add(primary("Add Friend", func() {
			p.Close()
			u.contactAction(func() error { return u.guilds.RequestContact(m.UserKey) }, "Friend request sent.")
		}))
	case guildconn.ContactActive:
		people.Add(primary("Message", func() { p.Close(); u.selectGuild(""); u.openDM(m.UserKey) }))
	case guildconn.ContactReceived:
		people.Add(primary("Accept Request", func() {
			p.Close()
			u.contactAction(func() error { return u.guilds.AcceptContact(m.UserKey) }, "You are now friends.")
		}))
	case guildconn.ContactSent:
		sent := widget.NewButton("Request Sent", nil)
		sent.Disable()
		people.Add(sent)
	}
	if state == guildconn.ContactBlocked {
		people.Add(widget.NewButton("Unblock", func() { u.guilds.Unblock(m.UserKey); p.Close(); u.toast("Unblocked") }))
	} else {
		block := widget.NewButton("Block", func() { p.Close(); u.blockPerson(m.UserKey, name) })
		block.Importance = widget.LowImportance
		people.Add(block)
	}
	if g.Currency != nil && !m.IsMe {
		people.Add(widget.NewButton("Transfer", func() { p.Close(); u.openSendCoins(g, m.UserKey, 0) }))
	}
	parts = append(parts, people)

	// Account facts, for staff: from the person's own app, and the
	// day the guild saw them join.
	if m.Created != "" || m.Guilds > 0 {
		var facts []fyne.CanvasObject
		if t, err := time.Parse(time.RFC3339, m.Created); err == nil {
			facts = append(facts, factLine("Account Created", t.Local().Format("2 January 2006")))
		}
		if m.Guilds > 0 {
			facts = append(facts, factLine("Guilds", itoa(m.Guilds)))
		}
		if t, err := time.Parse(time.RFC3339, m.Joined); err == nil {
			facts = append(facts, factLine("Joined", t.Local().Format("2 January 2006")))
		}
		section("ACCOUNT", facts...)
	}

	// Moderation is only for people ranked below you.
	mod := !m.IsOwner && myRank(g) > rankOf(g, m.Roles)
	if mod && can(g, proto.PermManageRoles) {
		box, read := memberRolesBox(g, m)
		saveRoles := primary("Save Roles", func() {
			u.call(g.ID, proto.Request{Type: "member_roles", Target: m.UserKey, Roles: read()}, func(_ proto.Response, err error) {
				if err != nil {
					problem.Set(err)
					return
				}
				p.Close()
				u.toast("Roles saved")
			})
		})
		// Save Roles only when there are roles to give (at the left, like the
		// other buttons in this window).
		if len(g.Roles) > 1 {
			section("ROLES", box, container.NewHBox(saveRoles))
		} else {
			section("ROLES", box)
		}
	}
	if mod && can(g, proto.PermManageNicknames) {
		section("NICKNAME", u.nicknameBox(g, m, problem, func() { p.Close() }))
	}
	if mod && (can(g, proto.PermKick) || can(g, proto.PermBan) || can(g, proto.PermTimeout)) {
		var modParts []fyne.CanvasObject
		if can(g, proto.PermKick) || can(g, proto.PermBan) {
			modParts = append(modParts, para("Kick removes them, and they may join again. Ban stops them from ever joining again."))
		}
		if can(g, proto.PermBan) {
			modParts = append(modParts, blockBox) // It goes with Ban, at the bottom.
		}
		if can(g, proto.PermTimeout) {
			modParts = append([]fyne.CanvasObject{u.timeoutBox(g, m, problem, func() { p.Close() })}, modParts...)
		}
		section("MODERATION", modParts...)
	}
	if mod && can(g, proto.PermKick) {
		buttons = append(buttons, widget.NewButton("Kick", act("kick", "Removed from the guild")))
	}
	if mod && can(g, proto.PermBan) {
		ban := widget.NewButton("Ban", act("ban", "Banned"))
		ban.Importance = widget.DangerImportance
		buttons = append(buttons, ban)
	}
	parts = append(parts, problem.box, buttonsRow(buttons...))
	p = u.openPopup(480, parts...)
}

// timeoutLengths are the lengths a Timeout can have, in minutes.
var timeoutLengths = []struct {
	name    string
	minutes int
}{{"1 Minute", 1}, {"5 Minutes", 5}, {"10 Minutes", 10}, {"1 Hour", 60}, {"1 Day", 24 * 60}, {"1 Week", 7 * 24 * 60}}

// timeoutBox silences a member for a while, or lifts their timeout.
func (u *UI) timeoutBox(g guildconn.GuildView, m guildconn.MemberView, problem *errorText, done func()) fyne.CanvasObject {
	send := func(minutes int, note string) {
		u.call(g.ID, proto.Request{Type: "timeout", Target: m.UserKey, Minutes: minutes}, func(_ proto.Response, err error) {
			if err != nil {
				problem.Set(err)
				return
			}
			done()
			u.toast(note)
		})
	}
	if until := timeoutEnds(m); until != "" {
		lift := widget.NewButton("Remove Timeout", func() { send(0, "Timeout removed") })
		return field("Timeout", container.NewHBox(lift), "Timed out until "+until+". They can read, but can't write, react or post.")
	}
	var names []string
	for _, l := range timeoutLengths {
		names = append(names, l.name)
	}
	length := widget.NewSelect(names, nil)
	length.SetSelectedIndex(2)
	start := widget.NewButton("Timeout", func() {
		l := timeoutLengths[max(0, length.SelectedIndex())]
		send(l.minutes, "Timed out for "+strings.ToLower(l.name))
	})
	return field("Timeout", container.NewBorder(nil, nil, nil, start, length), "They stay in the guild and can read, but can't write, react or post until it ends.")
}

// factLine is "Title" at the left and its value beside it.
func factLine(title, value string) fyne.CanvasObject {
	return container.NewHBox(container.New(fixedWidth{140}, text(title, 13.5, colDim, false)), text(value, 13.5, colText, false))
}
