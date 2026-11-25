package ui

import (
	"errors"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/user"
)

// This file has Create User, Load User, unlock and lock.

// ---- Create User ----------------------------------------------------------

func (u *UI) createUser() fyne.CanvasObject {
	name := widget.NewEntry()
	name.SetPlaceHolder("Enter Name")
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	pass2 := widget.NewPasswordEntry()
	pass2.SetPlaceHolder("Repeat Password")
	folder := widget.NewEntry()
	strength := newMeter()
	pass.OnChanged = strength.Rate
	problem := newErrorText()

	// The folder follows the name until the person changes it themselves.
	touched, filling := false, false
	fill := func(s string) {
		filling = true
		folder.SetText(s)
		filling = false
	}
	suggest := func() {
		if touched {
			return
		}
		n := strings.TrimSpace(name.Text)
		if n == "" {
			n = "Your name"
		}
		fill(filepath.Join(defaultUsersFolder(), user.FolderName(n)))
	}
	name.OnChanged = func(string) { suggest() }
	folder.OnChanged = func(string) {
		if !filling {
			touched = true
		}
	}
	suggest()

	browse := widget.NewButton("Browse…", func() {
		u.pickFolder("Choose where to keep your user folder", func(dir string) {
			if n := strings.TrimSpace(name.Text); n != "" {
				dir = filepath.Join(dir, user.FolderName(n))
			}
			fill(dir)
			touched = true
		}, problem)
	})

	var submit *widget.Button
	create := func() {
		problem.Set(nil)
		n := strings.TrimSpace(name.Text)
		switch {
		case n == "":
			problem.Set(errors.New("Please choose a name."))
			u.win.Canvas().Focus(name)
			return
		case len([]rune(pass.Text)) < 8:
			problem.Set(errors.New("The password must be at least 8 characters."))
			u.win.Canvas().Focus(pass)
			return
		case pass.Text != pass2.Text:
			problem.Set(errors.New("The two passwords are different."))
			u.win.Canvas().Focus(pass2)
			return
		}
		checked, err := user.CheckName(n)
		if err != nil {
			problem.Set(err)
			return
		}
		var dir string
		if strings.TrimSpace(folder.Text) == "" {
			dir = uniqueFolder(filepath.Join(defaultUsersFolder(), user.FolderName(checked)))
		} else if dir, err = cleanFolder(folder.Text); err != nil {
			problem.Set(err)
			return
		}
		password := pass.Text
		submit.Disable()
		submit.SetText("Creating…")
		go func() { // Making the keys takes a moment.
			usr, err := user.Create(dir, checked, password)
			fyne.Do(func() {
				submit.Enable()
				submit.SetText("Create User")
				if err != nil {
					problem.Set(err)
					return
				}
				u.Log.Printf("created user %s in %s", usr.Profile.UserID, dir)
				u.signIn(usr)
			})
		}()
	}
	submit = primary("Create User", create)

	// Two steps: name and password first; then the password again
	// and the folder.
	step1, step2 := form(), form()
	problem1 := newErrorText()
	next := func() {
		problem1.Set(nil)
		switch {
		case strings.TrimSpace(name.Text) == "":
			problem1.Set(errors.New("Please choose a name."))
			u.win.Canvas().Focus(name)
			return
		case len([]rune(pass.Text)) < 8:
			problem1.Set(errors.New("The password must be at least 8 characters."))
			u.win.Canvas().Focus(pass)
			return
		}
		step1.Hide()
		step2.Show()
		u.win.Canvas().Focus(pass2)
	}
	name.OnSubmitted = func(string) { next() }
	pass.OnSubmitted = func(string) { next() }
	pass2.OnSubmitted = func(string) { create() }
	folder.OnSubmitted = func(string) { create() }

	step1.Objects = []fyne.CanvasObject{
		backButton(func() { u.show(u.welcome()) }),
		titled(heading("Create User"), para("Your identity is a key pair made on this computer. No account, no email, no company in the middle.")),
		field("Name", name, ""),
		field("Password", stack(fieldGap, pass, strength.box), "At least 8 characters."),
		problem1.box,
		primary("Next", next),
	}
	step2.Objects = []fyne.CanvasObject{
		backButton(func() { step2.Hide(); step1.Show(); u.win.Canvas().Focus(pass) }),
		titled(heading("Create User"), para("Type your password again, and choose where your user is kept.")),
		field("Confirm Password", pass2, ""),
		field("Folder", container.NewBorder(nil, nil, nil, browse, folder),
			"Everything about you is kept in this folder. Keep it safe, and back it up."),
		problem.box,
		submit,
	}
	step2.Hide()
	page := panel(500, container.NewStack(step1, step2))
	defer u.focusLater(name)
	return page
}

// ---- Load User ------------------------------------------------------------

func (u *UI) loadUser() fyne.CanvasObject {
	problem := newErrorText()
	list := stack(4) // This is a column, so text lines up.
	scroll := container.NewVScroll(list)

	var refill func()
	refill = func() {
		list.RemoveAll()
		people := u.recent()
		if len(people) == 0 {
			l := widget.NewLabel("No users opened on this computer yet.")
			l.Alignment = fyne.TextAlignCenter
			list.Add(newTint(l, colorTheme{Theme: appTheme, text: colFaint}))
		}
		for _, r := range people {
			r := r
			list.Add(newPersonRow(r,
				func() { u.openUser(r.Folder, problem) },
				func() { u.forgetRecent(r.Folder); refill() }))
		}
		rows := float32(max(1, min(len(people), 4)))
		scroll.SetMinSize(fyne.NewSize(0, rows*86))
		list.Refresh()
	}
	refill()

	path := widget.NewEntry()
	path.SetPlaceHolder("Folder Path")
	open := func() {
		if strings.TrimSpace(path.Text) == "" {
			problem.Set(errors.New("Type or browse to a user folder."))
			u.win.Canvas().Focus(path)
			return
		}
		u.openUser(path.Text, problem)
	}
	path.OnSubmitted = func(string) { open() }
	browse := widget.NewButton("Browse…", func() {
		u.pickFolder("Choose a user folder", func(dir string) {
			path.SetText(dir)
			open()
		}, problem)
	})

	return panel(540,
		backButton(func() { u.show(u.welcome()) }),
		titled(heading("Load User"), para("Pick a user from this computer, or open a user folder.")),
		scroll,
		sideLineH(),
		container.NewBorder(nil, nil, nil, container.NewHBox(browse, primary("Open", open)), path),
		problem.box,
	)
}

// openUser checks a folder holds a user, then asks for its password.
func (u *UI) openUser(folder string, problem *errorText) {
	problem.Set(nil)
	dir, err := cleanFolder(folder)
	if err != nil {
		problem.Set(err)
		return
	}
	p, err := user.ReadProfile(dir)
	if err != nil {
		problem.Set(err)
		return
	}
	u.show(u.unlockUser(dir, p))
}

// ---- unlock ---------------------------------------------------------------

func (u *UI) unlockUser(dir string, p *user.Profile) fyne.CanvasObject {
	if p.ProfilePicture != "" && !strings.ContainsAny(p.ProfilePicture, `/\`) {
		faces[p.UserID] = filepath.Join(dir, "media", "profile", p.ProfilePicture)
	}
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	problem := newErrorText()

	var submit *widget.Button
	unlock := func() {
		problem.Set(nil)
		password := pass.Text
		submit.Disable()
		submit.SetText("Unlocking…")
		go func() { // Checking the password takes a moment, on purpose.
			usr, err := user.Unlock(dir, password)
			fyne.Do(func() {
				submit.Enable()
				submit.SetText("Unlock")
				if err != nil {
					problem.Set(err)
					pass.SetText("")
					u.win.Canvas().Focus(pass)
					return
				}
				u.signIn(usr)
			})
		}()
	}
	submit = primary("Unlock", unlock)
	pass.OnSubmitted = func(string) { unlock() }

	id := text(shortID(p.UserID), 13, colDim, false)
	id.TextStyle.Monospace = true
	id.Alignment = fyne.TextAlignCenter
	parts := []fyne.CanvasObject{
		backButton(func() { u.show(u.loadUser()) }),
		stack(titleGap, container.NewCenter(avatar(p.DefaultName, p.UserID, 84)), gap(2),
			centred(text("Welcome back, "+p.DefaultName+"!", 24, colText, true)), id),
		field("Password", pass, ""),
		problem.box,
		submit,
	}
	if p.RecoveryKeys != nil {
		forgot := widget.NewButton("Forgot Password", func() { u.show(u.forgotPassword(dir, p)) })
		forgot.Importance = widget.LowImportance
		parts = append(parts, container.NewCenter(forgot))
	}
	page := panel(440, parts...)
	defer u.focusLater(pass)
	return page
}

// ---- signing in and out ---------------------------------------------------

func (u *UI) signIn(usr *user.User) {
	u.user = usr
	u.addRecent(recentUser{Folder: usr.Dir, Name: usr.Profile.DefaultName, UserID: usr.Profile.UserID})
	u.enterHome()
}

// lock closes the guild connections and forgets the unlocked user (their
// keys leave memory with it).
func (u *UI) lock() {
	if m := u.guilds; m != nil {
		go m.CloseAll()
	}
	u.stopVoice()
	u.leaveChat()
	u.dm = nil
	u.guilds, u.home, u.user, u.voice = nil, nil, nil, nil
	u.show(u.welcome())
}

// ---- helpers --------------------------------------------------------------

// pickFolder shows the system's folder chooser. It runs outside the window,
// so the answer comes back on the window's own thread.
func (u *UI) pickFolder(title string, chosen func(dir string), problem *errorText) {
	go func() {
		dir, err := chooseFolder(title)
		fyne.Do(func() {
			if err != nil {
				problem.Set(err)
			} else if dir != "" {
				chosen(dir)
			}
		})
	}()
}

// focusLater puts the typing cursor in an entry once the screen is shown.
func (u *UI) focusLater(e fyne.Focusable) {
	go fyne.Do(func() { u.win.Canvas().Focus(e) })
}
