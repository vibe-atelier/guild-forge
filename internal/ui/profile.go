package ui

// This file has profile pictures everywhere, editing your profile, password and
// recovery code, nicknames and notification choices.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
	"nexus/internal/proto"
	"nexus/internal/user"
)

// ---- faces: profile pictures by user ID ------------------------------------------

var (
	faces   = map[string]string{}        // It maps each user ID to a picture file.
	faceRes = map[string]fyne.Resource{} // It maps each file to its loaded picture.
)

func faceResource(path string) fyne.Resource {
	if r, ok := faceRes[path]; ok {
		return r
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	r := fyne.NewStaticResource(filepath.Base(path), data)
	faceRes[path] = r
	return r
}

// faceImage is a round profile picture, or nil if the person has none.
func faceImage(id string, size float32) fyne.CanvasObject {
	path := faces[id]
	if path == "" {
		return nil
	}
	res := faceResource(path)
	if res == nil {
		return nil
	}
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.CornerRadius = size / 2
	img.SetMinSize(fyne.NewSize(size, size))
	return container.NewGridWrap(fyne.NewSize(size, size), img)
}

// learnFaces finds everyone's picture (fetching new ones from their guild);
// it reports whether any changed.
func (u *UI) learnFaces(views []guildconn.GuildView) bool {
	changed := false
	me := u.user.Profile.UserID
	mine := ""
	if u.user.Profile.ProfilePicture != "" {
		mine = filepath.Join(u.user.Dir, "media", "profile", u.user.Profile.ProfilePicture)
	}
	if faces[me] != mine {
		faces[me], changed = mine, true
	}
	seen := map[string]bool{me: true}
	for _, g := range views {
		for _, m := range g.Members {
			if m.BannerHash != "" && !m.IsMe && !u.knownBanner[m.BannerHash] {
				if u.pictureFile(g.ID, m.BannerHash) != "" { // The banner is fetched ahead, for their window.
					if u.knownBanner == nil {
						u.knownBanner = map[string]bool{}
					}
					u.knownBanner[m.BannerHash] = true
				}
			}
			if m.IsMe || m.PictureHash == "" {
				continue
			}
			seen[m.UserKey] = true
			if strings.HasSuffix(faces[m.UserKey], m.PictureHash) {
				continue
			}
			if p := u.pictureFile(g.ID, m.PictureHash); p != "" {
				faces[m.UserKey], changed = p, true
			}
		}
	}
	for id := range faces {
		if !seen[id] {
			delete(faces, id)
			changed = true
		}
	}
	return changed
}

// shownName is someone's name in a guild: their nickname if they have one.
func shownName(m guildconn.MemberView) string {
	if m.Nick != "" {
		return m.Nick
	}
	return m.Name
}

// memberBanner is a member's banner, or nil.
func (u *UI) memberBanner(gid, hash string, h float32) fyne.CanvasObject {
	if hash == "" {
		return nil
	}
	path := u.pictureFile(gid, hash)
	if path == "" {
		return nil
	}
	return bannerImage(path, h)
}

func bannerImage(path string, h float32) fyne.CanvasObject {
	res := faceResource(path)
	if res == nil {
		return nil
	}
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillStretch
	img.SetMinSize(fyne.NewSize(10, h))
	return img
}

// memberHeader is the top of a member's window: banner, picture, name,
// nickname and bio.
func (u *UI) memberHeader(g guildconn.GuildView, m guildconn.MemberView) fyne.CanvasObject {
	name := shownName(m)
	if name == "" {
		name = shortID(m.UserKey)
	}
	lines := container.NewVBox(layout.NewSpacer(), text(name, 22, colText, true))
	if m.Nick != "" && m.Name != "" && m.Nick != m.Name {
		lines.Add(text(m.Name, 13, colFaint, false))
	}
	lines.Add(layout.NewSpacer())
	head := container.NewBorder(nil, nil, container.NewHBox(avatar(name, m.UserKey, 64), gap(1)), nil, lines)
	box := container.NewVBox()
	if b := u.memberBanner(g.ID, m.BannerHash, 110); b != nil {
		box.Add(b)
	}
	box.Add(head)
	if m.Bio != "" {
		box.Add(u.markdown(m.Bio))
	}
	return box
}

// nicknameBox lets someone with Manage Nicknames change a member's
// nickname, from the member window.
func (u *UI) nicknameBox(g guildconn.GuildView, m guildconn.MemberView, problem *errorText, done func()) fyne.CanvasObject {
	nick := widget.NewEntry()
	nick.SetText(m.Nick)
	nick.SetPlaceHolder(m.Name)
	save := widget.NewButton("Save Nickname", func() {
		u.setNick(g, m.UserKey, nick.Text, problem, done)
	})
	return stack(fieldGap, container.NewBorder(nil, nil, nil, save, nick), hintText("Their name in this guild only. Leave it empty to remove it."))
}

func (u *UI) setNick(g guildconn.GuildView, target, nick string, problem *errorText, done func()) {
	go func() {
		gc, err := u.guilds.Get(g.ID)
		if err == nil {
			err = gc.SetNick(target, nick)
		}
		fyne.Do(func() {
			if err != nil {
				problem.Set(err)
				return
			}
			done()
			u.toast("Nickname saved")
		})
	}()
}

// openMyNickname changes your own nickname in a guild (Change Nickname).
func (u *UI) openMyNickname(g guildconn.GuildView) {
	var p *popup
	var me guildconn.MemberView
	for _, m := range g.Members {
		if m.IsMe {
			me = m
		}
	}
	nick := widget.NewEntry()
	nick.SetText(me.Nick)
	nick.SetPlaceHolder(u.user.Profile.DefaultName)
	problem := newErrorText()
	save := primary("Save", func() { u.setNick(g, "", nick.Text, problem, func() { p.Close() }) })
	nick.OnSubmitted = func(string) { save.OnTapped() }
	p = u.openPanel("nickname", 460, text("Nickname", 22, colText, true),
		para("Your name in "+g.Name+" only. Leave it empty to use your own name."),
		field("Nickname", nick, ""), problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
	u.win.Canvas().Focus(nick)
}

// ---- notification choices --------------------------------------------

var notifyLabels = map[string]string{
	proto.NotifyAll:      "All Messages",
	proto.NotifyMentions: "Mentions Only",
	proto.NotifyNothing:  "Nothing",
}

// openNotify sets the notification choice for a channel, or for the whole
// guild when channel is "".
func (u *UI) openNotify(g guildconn.GuildView, channel, channelName string) {
	gc, err := u.guilds.Get(g.ID)
	if err != nil {
		u.toast(sentence(err.Error()))
		return
	}
	var p *popup
	current, chosen := gc.Notify(channel)
	def := "Default"
	note := "When to show a pop-up for " + g.Name + ". Mentions are @you and @your roles, and replies to you."
	if channel != "" {
		note = "When to show a pop-up for #" + channelName + "."
		guildChoice, _ := gc.Notify("")
		def = "Guild's Default (" + notifyLabels[guildChoice] + ")"
	} else {
		d := g.DefaultNotify
		if d == "" {
			d = proto.NotifyMentions
		}
		def = "Default (" + notifyLabels[d] + ")"
		chosen = g.MyNotify != ""
	}
	options := []string{def, "All Messages", "Mentions Only", "Nothing"}
	values := []string{"", proto.NotifyAll, proto.NotifyMentions, proto.NotifyNothing}
	radio := widget.NewRadioGroup(options, nil)
	radio.Selected = def
	if chosen {
		radio.Selected = notifyLabels[current]
	}
	problem := newErrorText()
	save := primary("Save", func() {
		choice := ""
		for i, o := range options {
			if o == radio.Selected {
				choice = values[i]
			}
		}
		problem.Run(func() error { return gc.SetNotify(channel, choice) }, func() {
			p.Close()
			u.toast("Notifications saved")
		})
	})
	title, key := "Notifications", "notify"
	if channel != "" {
		key = "chnotify"
	}
	p = u.openPanel(key, 480, text(title, 22, colText, true), para(note), radio, problem.box,
		buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
}

// ---- Your Profile: editing, password, recovery code --------------------------------

// profileBanner is your banner, or nil.
func (u *UI) profileBanner(h float32) fyne.CanvasObject {
	if u.user.Profile.Banner == "" {
		return nil
	}
	return bannerImage(filepath.Join(u.user.Dir, "media", "profile", u.user.Profile.Banner), h)
}

// forgetOwnLook drops cached copies of your picture and banner after a change.
func (u *UI) forgetOwnLook() {
	for _, f := range []string{"picture", "banner"} {
		delete(faceRes, filepath.Join(u.user.Dir, "media", "profile", f))
	}
	delete(faces, u.user.Profile.UserID)
}

func (u *UI) openEditProfile() {
	var p *popup
	pr := u.user.Profile
	name := widget.NewEntry()
	name.SetText(pr.DefaultName)
	bio := widget.NewMultiLineEntry()
	bio.Wrapping = fyne.TextWrapWord
	bio.SetMinRowsVisible(3)
	bio.SetText(pr.Bio)
	counter := text("", 12, colFaint, false)
	count := func(s string) {
		counter.Text = fmt.Sprintf("%d / %d", utf8.RuneCountInString(s), user.MaxBio)
		counter.Refresh()
	}
	bio.OnChanged = count
	count(pr.Bio)
	problem := newErrorText()

	var newPic, newBanner []byte
	clearPic, clearBanner := false, false
	picBox := container.NewStack(avatar(pr.DefaultName, pr.UserID, 72))
	bannerBox := container.NewStack(u.bannerPreview(u.user.BannerPicture()))
	choose := func(title string, done func(data []byte)) func() {
		return func() {
			go func() {
				path, err := choosePicture(title)
				var data []byte
				if err == nil && path != "" {
					data, err = os.ReadFile(path)
				}
				fyne.Do(func() {
					if err != nil {
						problem.Set(err)
						return
					}
					if data != nil {
						done(data)
					}
				})
			}()
		}
	}
	pickPic := widget.NewButton("Change Picture", choose("Choose your profile picture", func(data []byte) {
		newPic, clearPic = data, false
		img := canvas.NewImageFromResource(fyne.NewStaticResource(fmt.Sprintf("me-%d", time.Now().UnixNano()), data))
		img.FillMode = canvas.ImageFillContain
		img.CornerRadius = 36
		img.SetMinSize(fyne.NewSize(72, 72))
		picBox.Objects = []fyne.CanvasObject{container.NewGridWrap(fyne.NewSize(72, 72), img)}
		picBox.Refresh()
	}))
	removePic := widget.NewButton("Remove", func() {
		newPic, clearPic = nil, true
		picBox.Objects = []fyne.CanvasObject{avatarShape(pr.UserID, 72, `<circle cx="50" cy="50" r="50" fill="url(#a)"/>`, personNode(pr.UserID), "c")}
		picBox.Refresh()
	})
	pickBanner := widget.NewButton("Change Banner", choose("Choose your banner", func(data []byte) {
		newBanner, clearBanner = data, false
		bannerBox.Objects = []fyne.CanvasObject{u.bannerPreview(data)}
		bannerBox.Refresh()
	}))
	removeBanner := widget.NewButton("Remove", func() {
		newBanner, clearBanner = nil, true
		bannerBox.Objects = []fyne.CanvasObject{u.bannerPreview(nil)}
		bannerBox.Refresh()
	})

	var save *widget.Button
	save = primary("Save", func() {
		problem.Set(nil)
		n, b := name.Text, bio.Text
		pic, ban, cp, cb := newPic, newBanner, clearPic, clearBanner
		save.Disable()
		go func() {
			var err error
			if len(pic) > 0 {
				pic, err = imaging.CropPicture(pic, 256, 256, 250<<10)
			}
			if err == nil && len(ban) > 0 {
				ban, err = imaging.CropPicture(ban, 960, 320, 250<<10)
			}
			if err == nil && strings.TrimSpace(n) != u.user.Profile.DefaultName {
				err = u.guilds.SetName(n)
			}
			if err == nil {
				err = u.guilds.SetLook(b, pic, ban, cp, cb)
			}
			fyne.Do(func() {
				save.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				u.forgetOwnLook()
				u.addRecent(recentUser{Folder: u.user.Dir, Name: u.user.Profile.DefaultName, UserID: u.user.Profile.UserID})
				p.Close()
				u.toast("Profile saved")
				u.redraw()
			})
		}()
	})
	picRow := container.NewBorder(nil, nil, container.NewHBox(picBox, gap(1)), nil,
		container.NewVBox(layout.NewSpacer(), container.NewHBox(pickPic, removePic),
			text("PNG or JPG. Made round, 256×256.", 12, colFaint, false), layout.NewSpacer()))
	bioHead := labelRow("Bio", counter)
	p = u.openPanel("profile", 600, text("Profile", 22, colText, true),
		para("Everyone in your guilds, and your friends, see these."),
		picRow,
		bannerBox, container.NewHBox(pickBanner, removeBanner, text("Made 960×320.", 12, colFaint, false)),
		field("Name", name, ""),
		stack(fieldGap, bioHead, bio, hintText("Markdown works here.")),
		problem.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), save))
}

func (u *UI) bannerPreview(data []byte) fyne.CanvasObject {
	if len(data) == 0 {
		r := canvas.NewRectangle(colLine)
		r.CornerRadius = 8
		r.SetMinSize(fyne.NewSize(10, 80))
		return container.NewStack(r, container.NewCenter(text("No banner", 12.5, colFaint, false)))
	}
	img := canvas.NewImageFromResource(fyne.NewStaticResource(fmt.Sprintf("banner-%d", time.Now().UnixNano()), data))
	img.FillMode = canvas.ImageFillStretch
	img.CornerRadius = 8
	img.SetMinSize(fyne.NewSize(10, 80))
	return img
}

func (u *UI) openChangePassword() {
	var p *popup
	old, new1, new2 := widget.NewPasswordEntry(), widget.NewPasswordEntry(), widget.NewPasswordEntry()
	problem, problem1 := newErrorText(), newErrorText()
	var save *widget.Button
	save = primary("Change Password", func() {
		problem.Set(nil)
		if new1.Text != new2.Text {
			problem.Set(errors.New("the two passwords are different"))
			return
		}
		o, n := old.Text, new1.Text
		save.Disable()
		save.SetText("Changing…")
		go func() {
			err := u.user.ChangePassword(o, n)
			fyne.Do(func() {
				save.Enable()
				save.SetText("Change Password")
				if err != nil {
					problem.Set(err)
					return
				}
				p.Close()
				u.toast("Password Changed")
			})
		}()
	})
	step1, step2 := form(), form()
	both, forward, back := twoSteps(step1, step2)
	next := func() {
		problem1.Set(nil)
		if len([]rune(new1.Text)) < user.MinPasswordLength {
			problem1.Set(fmt.Errorf("the new password must be at least %d characters", user.MinPasswordLength))
			return
		}
		forward()
		u.win.Canvas().Focus(new2)
	}
	new1.OnSubmitted = func(string) { next() }
	new2.OnSubmitted = func(string) { save.OnTapped() }
	step1.Objects = []fyne.CanvasObject{panelTitle("Change Password"),
		para("Your user ID, friends and chats stay the same."),
		field("Current Password", old, ""), field("New Password", new1, fmt.Sprintf("At least %d characters.", user.MinPasswordLength)),
		problem1.box, buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), primary("Next", next))}
	step2.Objects = []fyne.CanvasObject{panelTitle("Change Password"), para("Type the new password again."),
		field("Confirm Password", new2, ""), problem.box,
		buttonsRow(widget.NewButton("Back", back), save)}
	p = u.openPanel("account", 600, both)
	u.win.Canvas().Focus(old)
}

func (u *UI) openRecoveryCode() {
	var p *popup
	problem := newErrorText()
	body := form()
	note := "A recovery code lets you set a new password if you forget yours. Write it down or print it, and keep it somewhere safe: anyone with the code and your user folder can open your account."
	if u.user.HasRecoveryCode() {
		note += "\n\nYou already have one. Making a new code stops the old one working."
	}
	var make *widget.Button
	make = primary("Make Code", func() {
		make.Disable()
		make.SetText("Making…")
		go func() {
			code, err := u.user.MakeRecoveryCode()
			fyne.Do(func() {
				if err != nil {
					make.Enable()
					make.SetText("Make Code")
					problem.Set(err)
					return
				}
				show := text(code, 20, colText, true)
				show.TextStyle.Monospace = true
				show.Alignment = fyne.TextAlignCenter
				copyBtn := widget.NewButtonWithIcon("Copy", copyIcon, func() {
					u.app.Clipboard().SetContent(code)
					u.toast("Code copied")
				})
				body.Objects = []fyne.CanvasObject{
					card(16, container.NewVBox(show)),
					para("This is the only time Nexus shows it."),
					buttonsRow(copyBtn, primary("Done", func() { p.Close(); u.redraw() })),
				}
				body.Refresh()
			})
		}()
	})
	body.Add(buttonsRow(widget.NewButton("Cancel", func() { p.Close() }), make))
	p = u.openPanel("account", 600, panelTitle("Recovery Code"), para(note), problem.box, body)
}

// ---- Forgot Password ------------------------------------------------------------

func (u *UI) forgotPassword(dir string, p *user.Profile) fyne.CanvasObject {
	code := widget.NewEntry()
	code.SetPlaceHolder("XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX")
	new1, new2 := widget.NewPasswordEntry(), widget.NewPasswordEntry()
	problem, problem1 := newErrorText(), newErrorText()
	var submit *widget.Button
	submit = primary("Set Password", func() {
		problem.Set(nil)
		if new1.Text != new2.Text {
			problem.Set(errors.New("the two passwords are different"))
			return
		}
		c, n := code.Text, new1.Text
		submit.Disable()
		submit.SetText("Checking…")
		go func() {
			usr, err := user.Recover(dir, c, n)
			fyne.Do(func() {
				submit.Enable()
				submit.SetText("Set Password")
				if err != nil {
					problem.Set(err)
					return
				}
				u.signIn(usr)
				u.toast("Password Changed")
			})
		}()
	})
	step1, step2 := form(), form()
	both, forward, back := twoSteps(step1, step2)
	next := func() {
		problem1.Set(nil)
		switch {
		case strings.TrimSpace(code.Text) == "":
			problem1.Set(errors.New("Type the recovery code you wrote down."))
			return
		case len([]rune(new1.Text)) < user.MinPasswordLength:
			problem1.Set(fmt.Errorf("The new password must be at least %d characters.", user.MinPasswordLength))
			return
		}
		forward()
		u.win.Canvas().Focus(new2)
	}
	new1.OnSubmitted = func(string) { next() }
	new2.OnSubmitted = func(string) { submit.OnTapped() }
	step1.Objects = []fyne.CanvasObject{
		backButton(func() { u.show(u.unlockUser(dir, p)) }),
		titled(heading("Forgot Password"), para("Type the recovery code you wrote down, then choose a new password.")),
		field("Recovery Code", code, ""),
		field("New Password", new1, fmt.Sprintf("At least %d characters.", user.MinPasswordLength)),
		problem1.box, primary("Next", next)}
	step2.Objects = []fyne.CanvasObject{
		backButton(back),
		titled(heading("Forgot Password"), para("Type the new password again.")),
		field("Confirm Password", new2, ""),
		problem.box, submit}
	defer u.focusLater(code)
	return panel(460, both)
}
