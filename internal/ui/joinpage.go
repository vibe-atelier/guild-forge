package ui

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
)

// Joining a guild takes the whole main area: the guild's About
// page, its Rules, then its CAPTCHA, each a full page. Guilds use About to
// show who they are, and Rules can be long terms and conditions, so neither
// is squeezed into a window.

type joinView struct {
	c     *guildconn.PairCheck
	stage string // It is "about", "rules", "welcome", "question" or "result".
	n     int    // It goes up when the page must be drawn again.

	agreed, rulesRead bool
	password          string
	problem           string

	quiz    *proto.Quiz
	qi      int
	answers []proto.QuizAnswer

	pass                 bool
	message, sub, result string // These are for the result page; result is the guild to open.
}

// startJoin opens a checked guild's About page.
func (u *UI) startJoin(c *guildconn.PairCheck) {
	u.leaveChat()
	u.settings = nil
	u.selected = ""
	u.joining = &joinView{c: c, stage: "about"}
	if u.devOpenRules { // This is only for the guide's pictures.
		u.devOpenRules = false
		u.joining.stage = "rules"
	}
	u.redraw()
}

// startQuiz opens the CAPTCHA, after Join.
func (u *UI) startQuiz(c *guildconn.PairCheck, q *proto.Quiz) {
	j := u.joining
	if j == nil || j.c.GuildID != c.GuildID {
		u.leaveChat()
		u.settings, u.selected = nil, ""
		j = &joinView{c: c}
		u.joining = j
	}
	j.quiz, j.qi, j.answers = q, 0, nil
	j.stage = "welcome"
	if q.Welcome == "" || u.devSkipWelcome {
		j.stage = "question"
	}
	u.joinGo(j.stage)
}

// joinGo moves to another stage of joining.
func (u *UI) joinGo(stage string) {
	if j := u.joining; j != nil {
		j.stage = stage
		j.n++
		u.redraw()
	}
}

// endJoin leaves the join pages, for a guild or home.
func (u *UI) endJoin(guildID string) {
	if j := u.joining; j != nil {
		j.quiz = nil // It is finished, so there is nothing to cancel.
	}
	u.joining = nil
	if guildID != "" {
		u.selectGuild(guildID)
		return
	}
	u.redraw()
}

func (u *UI) cancelJoin() {
	if j := u.joining; j != nil && j.quiz != nil {
		go u.guilds.JoinCancel()
	}
	u.endJoin("")
}

func (u *UI) joinSig() string {
	j := u.joining
	return fmt.Sprintf("join|%s|%s|%d", j.c.GuildID, j.stage, j.n)
}

func (u *UI) joinPage() fyne.CanvasObject {
	j := u.joining
	switch j.stage {
	case "rules":
		return u.joinRulesPage(j)
	case "welcome", "question":
		return u.quizPage(j)
	case "result":
		return u.quizResultPage(j)
	}
	return u.joinAboutPage(j)
}

// joinFrame is a join page: the guild's name at the top, and the content
// scrolling under it.
func (u *UI) joinFrame(j *joinView, parts ...fyne.CanvasObject) (fyne.CanvasObject, *container.Scroll) {
	return pageFrameScroll(userPlusIcon, j.c.Name, parts...)
}

func (u *UI) joinPicture(j *joinView, size float32) fyne.CanvasObject {
	if len(j.c.Picture) > 0 {
		return pictureFromBytes("check-"+j.c.GuildID, j.c.Picture, size)
	}
	return avatarSquare(j.c.Name, j.c.GuildID, size)
}

// ---- About ------------------------------------------------------------------

func (u *UI) joinAboutPage(j *joinView) fyne.CanvasObject {
	c := j.c
	info := proto.JoinInfo{}
	if c.Join != nil {
		info = *c.Join
	}
	count := ""
	if c.MemberCount >= 0 {
		count = fmt.Sprintf("%d %s", c.MemberCount, map[bool]string{true: "member", false: "members"}[c.MemberCount == 1])
	}
	head := container.NewBorder(nil, nil, container.NewHBox(u.joinPicture(j, 88), gap(1)), nil,
		container.NewVBox(layout.NewSpacer(), text(c.Name, 26, colText, true), text(count, 13.5, colDim, false), layout.NewSpacer()))
	var about fyne.CanvasObject = para("This guild has no About text yet.")
	if c.About != "" {
		about = u.markdownFlush(c.About)
	}

	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	pass.SetText(j.password)
	pass.OnChanged = func(s string) { j.password = s }

	hasRules := info.RulesEnabled && c.Rules != ""
	membersOnly := info.RequireRules && !info.RulesPublic
	mustAgree := hasRules && (info.RequireRules || info.JoinPlacement == "about_after_rules")
	canJoin, why := true, ""
	switch {
	case c.Known:
		canJoin, why = false, "This guild is already in your list."
	case !info.ShowJoinButton && !info.Whitelist:
		canJoin, why = false, "This guild is not taking new members right now."
	case mustAgree && !j.agreed:
		canJoin, why = false, "Read the Rules and agree to them to join."
	case info.JoinPlacement == "rules" && hasRules && !j.rulesRead:
		canJoin, why = false, "Read the Rules to join."
	}

	joinLabel := "Join"
	if !info.ShowJoinButton && info.Whitelist {
		joinLabel = "Join (Invited)"
	}
	problem := newErrorText()
	if j.problem != "" {
		problem.Set(errors.New(j.problem))
	}
	var joinBtn *widget.Button
	joinBtn = primary(joinLabel, func() {
		problem.Set(nil)
		j.problem = ""
		joinBtn.Disable()
		password := j.password
		go func() {
			r, err := u.guilds.JoinStart(c.Address, c.GuildID, password, j.agreed || membersOnly)
			fyne.Do(func() {
				joinBtn.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				u.joinAnswer(j, r, problem)
			})
		}()
	})
	if !canJoin {
		joinBtn.Disable()
	}
	buttons := []fyne.CanvasObject{widget.NewButton("Cancel", u.cancelJoin)}
	if hasRules {
		buttons = append(buttons, widget.NewButtonWithIcon("Rules", shieldIcon, func() { u.joinGo("rules") }))
	}
	buttons = append(buttons, joinBtn)

	parts := []fyne.CanvasObject{head, about}
	if info.NeedsPassword {
		parts = append(parts, field("Guild Password", pass, "The owner gave it to you."))
	}
	if membersOnly {
		parts = append(parts, para("This guild shows its Rules to members only. By joining, you agree to follow them, and you can read them once you are in."))
	}
	if why != "" {
		parts = append(parts, hintText(why))
	}
	parts = append(parts, problem.box, buttonsRow(buttons...))
	page, _ := u.joinFrame(j, card(24, plain(formGap, parts...)))
	return page
}

// joinAnswer acts on what the guild said to Join or to the answers.
func (u *UI) joinAnswer(j *joinView, r *guildconn.JoinResult, problem *errorText) {
	switch r.Code {
	case "captcha":
		u.startQuiz(j.c, r.Quiz)
	case "joined":
		msg := "You joined " + j.c.Name + "!"
		if r.Message != "" {
			msg = r.Message + " You joined " + j.c.Name + "."
		}
		u.toast(msg)
		u.endJoin(r.GuildID)
	case "pending":
		u.toast("Request sent. The owner will approve it.")
		u.endJoin(r.GuildID)
	case "captcha_failed":
		sub := r.Error
		if r.TriesLeft > 0 {
			sub = fmt.Sprintf("%d %s left today.", r.TriesLeft, map[bool]string{true: "try", false: "tries"}[r.TriesLeft == 1])
		}
		u.showQuizResult(false, r.Message, sub, "")
	default:
		msg, ok := joinRefusals[r.Code]
		if !ok {
			msg = r.Error
		}
		if problem != nil {
			problem.Set(errors.New(msg))
		} else {
			u.showQuizResult(false, "", sentence(msg), "")
		}
	}
}

// ---- Rules --------------------------------------------------------------------

// joinRulesPage shows the Rules in full. When agreeing is needed, I Agree
// unlocks once they have been scrolled to the end.
func (u *UI) joinRulesPage(j *joinView) fyne.CanvasObject {
	info := proto.JoinInfo{}
	if j.c.Join != nil {
		info = *j.c.Join
	}
	needAgree := (info.RequireRules || info.JoinPlacement == "about_after_rules") && !j.agreed
	agree := primary("I Agree", func() {
		j.agreed, j.rulesRead = true, true
		u.joinGo("about")
	})
	back := widget.NewButton("Back", func() {
		j.rulesRead = true
		u.joinGo("about")
	})
	hint := hintText("Scroll to the end of the Rules to agree.")
	buttons := []fyne.CanvasObject{back}
	if needAgree {
		agree.Disable()
		buttons = append(buttons, agree)
	} else {
		hint.Hide()
	}
	page, sc := u.joinFrame(j, backButton(func() { j.rulesRead = true; u.joinGo("about") }),
		card(24, plain(formGap, panelTitle("Rules"), u.markdownFlush(j.c.Rules))), hint, buttonsRow(buttons...))
	if needAgree {
		unlock := func() {
			agree.Enable()
			hint.Hide()
			sc.Content.Refresh()
		}
		sc.OnScrolled = func(pos fyne.Position) {
			if pos.Y+sc.Size().Height >= sc.Content.MinSize().Height-8 {
				unlock()
			}
		}
		go func() { // Rules that fit without scrolling count as read.
			time.Sleep(400 * time.Millisecond)
			fyne.Do(func() {
				if sc.Content.MinSize().Height <= sc.Size().Height+8 {
					unlock()
				}
			})
		}()
	}
	return page
}

// ---- the CAPTCHA, one question per page (no going back) -------------

func (u *UI) quizPage(j *joinView) fyne.CanvasObject {
	q := j.quiz
	if j.stage == "welcome" {
		page, _ := u.joinFrame(j, card(24, form(
			titled(panelTitle("CAPTCHA"), wrapText(q.Welcome, colText)),
			para("Once you move on, an answer can't be changed."),
			buttonsRow(widget.NewButton("Cancel", u.cancelJoin), primary("Start", func() { u.joinGo("question") })))))
		return page
	}
	i := j.qi
	qq := q.Questions[i]
	problem := newErrorText()
	parts := []fyne.CanvasObject{titled(panelTitle("CAPTCHA"), text(fmt.Sprintf("Question %d of %d", i+1, len(q.Questions)), 13, colFaint, false))}
	if len(qq.Image) > 0 {
		parts = append(parts, container.NewHBox(pictureFromBytes(fmt.Sprintf("quiz-%d", i), qq.Image, 260)))
	}
	qt, qtBox := wrapLabel(qq.Text, colText)
	qt.TextStyle.Bold = true
	parts = append(parts, qtBox)

	var read func() (proto.QuizAnswer, bool)
	switch qq.Type {
	case "text":
		e := widget.NewEntry()
		e.SetPlaceHolder("Your Answer")
		parts = append(parts, e)
		u.focusLater(e)
		read = func() (proto.QuizAnswer, bool) { return proto.QuizAnswer{Text: e.Text}, true }
	case "checkbox":
		g := widget.NewCheckGroup(qq.Options, nil)
		parts = append(parts, flushLeft(g))
		read = func() (proto.QuizAnswer, bool) {
			var picked []int
			for n, o := range qq.Options {
				for _, s := range g.Selected {
					if s == o {
						picked = append(picked, n)
					}
				}
			}
			return proto.QuizAnswer{Choices: picked}, len(picked) > 0
		}
	default: // This handles radio and dropdown questions.
		var sel *widget.Select
		var rg *widget.RadioGroup
		if qq.Type == "dropdown" {
			sel = widget.NewSelect(qq.Options, nil)
			sel.PlaceHolder = "Choose…"
			parts = append(parts, sel)
		} else {
			rg = widget.NewRadioGroup(qq.Options, nil)
			parts = append(parts, flushLeft(rg))
		}
		read = func() (proto.QuizAnswer, bool) {
			n := -1
			if sel != nil {
				n = sel.SelectedIndex()
			} else {
				for k, o := range qq.Options {
					if o == rg.Selected {
						n = k
					}
				}
			}
			return proto.QuizAnswer{Choices: []int{n}}, n >= 0
		}
	}
	label := "Next"
	if i == len(q.Questions)-1 {
		label = "Submit"
	}
	var next *widget.Button
	next = primary(label, func() {
		a, ok := read()
		if !ok {
			problem.Set(errors.New("Choose an answer."))
			return
		}
		j.answers = append(j.answers, a)
		if i+1 < len(q.Questions) {
			j.qi++
			u.joinGo("question")
			return
		}
		next.Disable()
		next.SetText("Checking…")
		go func() {
			r, err := u.guilds.JoinAnswers(j.answers)
			fyne.Do(func() {
				if err != nil {
					u.showQuizResult(false, "", sentence(err.Error()), "")
					return
				}
				switch r.Code {
				case "joined":
					u.showQuizResult(true, r.Message, "You joined "+j.c.Name+".", r.GuildID)
				case "pending":
					u.showQuizResult(true, r.Message, "Your request now waits for the owner's approval.", r.GuildID)
				default:
					u.joinAnswer(j, r, nil)
				}
			})
		}()
	})
	parts = append(parts, problem.box, buttonsRow(widget.NewButton("Cancel", u.cancelJoin), next))
	page, _ := u.joinFrame(j, card(24, form(parts...)))
	return page
}

// showQuizResult shows passed or failed, with the owner's own message.
func (u *UI) showQuizResult(pass bool, message, sub, guildID string) {
	j := u.joining
	if j == nil {
		return
	}
	j.quiz = nil // Nothing is left to cancel.
	j.pass, j.message, j.sub, j.result = pass, message, sub, guildID
	u.joinGo("result")
}

func (u *UI) quizResultPage(j *joinView) fyne.CanvasObject {
	ic, fallback := crossIcon, "Not quite."
	if j.pass {
		ic, fallback = checkIcon, "Well done!"
	}
	message := j.message
	if message == "" {
		message = fallback
	}
	m := widget.NewLabel(message)
	m.Wrapping = fyne.TextWrapWord
	m.Alignment = fyne.TextAlignCenter
	m.TextStyle.Bold = true
	s := widget.NewLabel(j.sub)
	s.Wrapping = fyne.TextWrapWord
	s.Alignment = fyne.TextAlignCenter
	done := primary("Done", func() { u.endJoin(j.result) })
	page, _ := u.joinFrame(j, card(24, stack(fieldGap, container.NewCenter(icon(ic, 56)), m, dim(s), gap(4), container.NewCenter(done))))
	return page
}

// pageFrameScroll is pageFrame, also giving its scroll.
func pageFrameScroll(ic fyne.Resource, title string, parts ...fyne.CanvasObject) (fyne.CanvasObject, *container.Scroll) {
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(14, 14, 24, 24), container.NewHBox(icon(ic, 18), text(title, 16, colText, true))))
	body := container.New(layout.NewCustomPaddedLayout(20, 30, 24, 24), narrowTop(760, container.New(&column{gap: 12}, parts...)))
	sc := container.NewVScroll(body)
	return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, sc), sc
}

// dropJoin leaves the join pages when you go somewhere else.
func (u *UI) dropJoin() {
	if j := u.joining; j != nil {
		if j.quiz != nil {
			go u.guilds.JoinCancel()
		}
		u.joining = nil
	}
}
