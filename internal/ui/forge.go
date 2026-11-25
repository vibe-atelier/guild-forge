package ui

// This file is GuildForge's window: hosting Nexus guilds on this
// computer without the command line. It uses the app's look and building
// blocks. The work itself is done by package host, the same code the
// guild-forge commands use.

import (
	"errors"
	"fmt"
	"image/color"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/host"
	"nexus/internal/platform"
)

type forge struct {
	u       *UI
	dir     string // It is GuildForge's folder.
	version string
	notices string // It is the third-party notices, for the Licenses page.

	// It is how far the Check Updates page has got.
	upd appUpdate

	page      string // It is "home", "create", "manage", "guild", "licenses" or "updates".
	createTab int    // It is 0 for New Guild and 1 for Import Guild.
	guildID   string // It is the guild whose page is open.
	cat       string // It is the guild page's open category.

	guilds []host.GuildInfo
	sig    string // It is what the page shows, so it is drawn again only on change.
	catSig string
	side   *fyne.Container
	body   *fyne.Container
	frame  fyne.CanvasObject
}

var forgeCats = []struct{ key, label, group string }{
	{"overview", "Overview", "GUILD"},
	{"settings", "Settings", "GUILD"},
	{"pairing", "Pairing", "GUILD"},
	{"backups", "Backups", "FILES"},
	{"export", "Export", "FILES"},
}

// RunForge opens GuildForge's window and returns when it is closed.
func RunForge(version, configDir, notices string, logger *log.Logger) {
	runForge(app.NewWithID("app.guildforge"), version, configDir, notices, logger, false)
}

// RunForgeOffscreen draws GuildForge in memory only, for pictures of it.
func RunForgeOffscreen(a fyne.App, version, configDir, notices string, logger *log.Logger) {
	runForge(a, version, configDir, notices, logger, true)
}

func runForge(a fyne.App, version, configDir, notices string, logger *log.Logger, offscreen bool) {
	useEmojiFont()
	appTheme = newTheme()
	a.Settings().SetTheme(appTheme)
	w := a.NewWindow("GuildForge")
	w.SetIcon(logoIcon)
	u := &UI{Version: version, Log: logger, app: a, win: w}
	f := &forge{u: u, dir: configDir, version: version, notices: notices, page: "home"}
	w.Resize(fyne.NewSize(1080, 720))
	w.CenterOnScreen()
	w.SetMaster()
	u.watchLayout()
	f.load()
	f.draw()
	f.startScreens()
	go func() { // Keep the states fresh.
		for range time.Tick(2 * time.Second) {
			fyne.Do(func() {
				f.load()
				f.draw()
			})
		}
	}()
	if offscreen {
		w.Show()
		return
	}
	w.ShowAndRun()
}

func (f *forge) load() {
	list, err := host.Guilds(f.dir)
	if err == nil {
		f.guilds = list
	}
}

func (f *forge) guild() *host.GuildInfo {
	for i := range f.guilds {
		if f.guilds[i].GuildID == f.guildID {
			return &f.guilds[i]
		}
	}
	return nil
}

// go switches to a page and draws it from fresh.
func (f *forge) goTo(page string) {
	f.page, f.sig, f.catSig = page, "", ""
	f.load()
	f.draw()
}

func (f *forge) openGuild(id, cat string) {
	f.guildID, f.cat = id, cat
	f.goTo("guild")
}

func (f *forge) stateSig() string {
	s := f.page + "|" + f.guildID + "|" + f.cat
	if f.page == "licenses" || f.page == "updates" {
		return s + "|" + f.upd.state + f.upd.latest // These pages show no guilds.
	}
	for _, g := range f.guilds {
		s += fmt.Sprintf("|%s%d%v%v%s%v%d%d", g.GuildID, g.Port, g.Running, g.Broadcasting, g.Owner, g.StartOnBoot, g.Members, g.Online)
	}
	return s
}

// draw shows the page again when what it shows has changed. Forms (where
// people type) are only built again when their category changes.
func (f *forge) draw() {
	sig := f.stateSig()
	if sig == f.sig {
		return
	}
	f.sig = sig
	switch f.page {
	case "create":
		if f.frame == nil || f.catSig != "create" {
			f.catSig = "create"
			f.frame = f.createPage()
			f.u.show(f.frame)
		}
	case "manage":
		f.catSig = ""
		f.u.show(f.managePage())
	case "guild":
		if f.guild() == nil {
			f.goTo("manage")
			return
		}
		f.u.show(f.guildPage())
	case "licenses":
		f.u.show(f.licensesPage())
	case "updates":
		f.u.show(f.updatesPage())
	default:
		f.catSig = ""
		f.u.show(f.homePage())
	}
}

// ---- home ---------------------------------------------------------------------

func (f *forge) homePage() fyne.CanvasObject {
	n := len(f.guilds)
	create := newChoiceCard(plusIcon, "Create Guild",
		[]string{"Make a new guild, or import", "one from a file."}, func() { f.createTab = 0; f.goTo("create") })
	create.off = n >= host.MaxGuilds
	manage := newChoiceCard(cogIcon, "Manage Guild",
		[]string{"Start, stop and change the", "guilds on this computer."}, func() { f.goTo("manage") })
	manage.off = n == 0
	count := fmt.Sprintf("%d of %d guilds made on this computer.", n, host.MaxGuilds)
	if n == 0 {
		count = "No guilds on this computer yet."
	}
	page := container.NewVBox(
		container.NewCenter(icon(logoIcon, 76)),
		gap(10),
		centred(text("GuildForge", 46, colText, true)),
		gap(2),
		centred(text("Host Nexus guilds on this computer.", 16, colDim, false)),
		gap(40),
		container.NewGridWithColumns(2, container.New(layout.NewCustomPaddedLayout(0, 0, 0, 8), create), container.New(layout.NewCustomPaddedLayout(0, 0, 8, 0), manage)),
		gap(24),
		centred(text(count, 13.5, colDim, false)),
		gap(14),
		centred(text("Version "+f.version, 12.5, colFaint, false)),
	)
	// The gear at the bottom right holds Licenses and Check Updates.
	corner := container.New(layout.NewCustomPaddedLayout(0, 14, 0, 16), container.NewHBox(layout.NewSpacer(), newMenuIcon(gearIcon, func() *fyne.Menu {
		return fyne.NewMenu("",
			fyne.NewMenuItem("Licenses", func() { f.goTo("licenses") }),
			fyne.NewMenuItem("Check Updates", func() { f.goTo("updates") }))
	})))
	return container.NewBorder(nil, corner, nil, nil, narrow(760, page))
}

// ---- Create Guild ----------------------------------------------------------------

func (f *forge) createPage() fyne.CanvasObject {
	pane := container.NewStack()
	show := func(i int) {
		f.createTab = i
		if i == 0 {
			pane.Objects = []fyne.CanvasObject{f.newGuildPane()}
		} else {
			pane.Objects = []fyne.CanvasObject{f.importPane()}
		}
		pane.Refresh()
	}
	show(f.createTab)
	return container.NewVScroll(container.New(layout.NewCustomPaddedLayout(40, 40, 0, 0), panel(560,
		backButton(func() { f.goTo("home") }),
		titled(heading("Create Guild"), para("A guild is a Nexus server that runs on this computer.")),
		tabBar(nil, []string{"New Guild", "Import Guild"}, f.createTab, show),
		pane)))
}

func (f *forge) newGuildPane() fyne.CanvasObject {
	pass, pass2 := widget.NewPasswordEntry(), widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	pass2.SetPlaceHolder("Repeat Password")
	strength := newMeter()
	pass.OnChanged = strength.Rate
	port := widget.NewEntry()
	port.SetText(strconv.Itoa(host.SuggestPort(f.dir)))
	folder := widget.NewEntry()
	folder.SetPlaceHolder("GuildForge's Folder")
	problem, problem1 := newErrorText(), newErrorText()
	browse := widget.NewButton("Browse…", func() {
		f.u.pickFolder("Choose where to keep the guild", func(dir string) { folder.SetText(dir) }, problem1)
	})
	var create *widget.Button
	create = primary("Create Guild", func() {
		problem.Set(nil)
		if err := host.CheckNewPassword(pass.Text, pass2.Text); err != nil {
			problem.Set(err)
			return
		}
		p, err := strconv.Atoi(strings.TrimSpace(port.Text))
		if err != nil {
			problem.Set(errors.New("the port must be a number"))
			return
		}
		create.Disable()
		create.SetText("Creating…")
		pw, dir := pass.Text, folder.Text
		go func() {
			e, err := host.CreateGuild(f.dir, pw, dir, p)
			fyne.Do(func() {
				create.Enable()
				create.SetText("Create Guild")
				if err != nil {
					problem.Set(err)
					return
				}
				f.u.toast(fmt.Sprintf("Guild %d created.", e.Index))
				f.openGuild(e.GuildID, "overview")
			})
		}()
	})
	// Two steps: the folder and the password; then the port and
	// the password again.
	step1, step2 := form(), form()
	both, forward, back := twoSteps(step1, step2)
	next := func() {
		problem1.Set(nil)
		if len([]rune(pass.Text)) < host.MinPasswordLength {
			problem1.Set(fmt.Errorf("The password must be at least %d characters.", host.MinPasswordLength))
			return
		}
		forward()
		f.u.win.Canvas().Focus(pass2)
	}
	pass.OnSubmitted = func(string) { next() }
	pass2.OnSubmitted = func(string) { create.OnTapped() }
	step1.Objects = []fyne.CanvasObject{
		field("Folder", container.NewBorder(nil, nil, nil, browse, folder), "Where the guild's files are kept. Leave it empty to use GuildForge's own folder."),
		field("Password", stack(fieldGap, pass, strength.box), fmt.Sprintf("At least %d characters. It protects the guild's settings, exports and backups.", host.MinPasswordLength)),
		problem1.box,
		primary("Next", next)}
	step2.Objects = []fyne.CanvasObject{
		field("Port Number", container.New(fixedWidth{160}, port), fmt.Sprintf("From %d to %d. People type it with this computer's address to join.", host.MinPort, host.MaxPort)),
		field("Confirm Password", pass2, ""),
		problem.box,
		buttonsRow(widget.NewButton("Back", back), create)}
	f.u.focusLater(folder)
	return both
}

func (f *forge) importPane() fyne.CanvasObject {
	path := widget.NewEntry()
	path.SetPlaceHolder("Exported File Or Folder")
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	problem := newErrorText()
	pickFile := widget.NewButton("Browse File", func() {
		go func() {
			p, err := chooseGuildFile("Choose a .nexus file")
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
				} else if p != "" {
					path.SetText(p)
				}
			})
		}()
	})
	pickFolder := widget.NewButton("Browse Folder", func() {
		f.u.pickFolder("Choose a guild folder", func(dir string) { path.SetText(dir) }, problem)
	})
	var imp *widget.Button
	imp = primary("Import Guild", func() {
		problem.Set(nil)
		imp.Disable()
		imp.SetText("Importing…")
		p, pw := path.Text, pass.Text
		go func() {
			e, err := host.ImportGuild(f.dir, p, pw)
			fyne.Do(func() {
				imp.Enable()
				imp.SetText("Import Guild")
				if err != nil {
					problem.Set(err)
					return
				}
				f.u.toast(fmt.Sprintf("Imported as guild %d. Give it a port under Settings.", e.Index))
				f.openGuild(e.GuildID, "settings")
			})
		}()
	})
	return form(
		para("A guild is a folder of files. Export packs that folder into one sealed .nexus file, to carry it to another computer; importing unpacks it into a new folder here. A guild folder already on this computer can be added back as it is."),
		field("Location", container.NewBorder(nil, nil, nil, container.NewHBox(pickFile, pickFolder), path), ""),
		field("Password", pass, "The password the guild had when it was exported. A folder needs none."),
		problem.box,
		imp)
}

// ---- Manage Guild: the list -------------------------------------------------------

func (f *forge) managePage() fyne.CanvasObject {
	list := stack(4) // This is a column, so text lines up.
	for i, g := range f.guilds {
		g := g
		if i > 0 {
			list.Add(sideLineH())
		}
		// Plain words, not the command line's GUILD#1@50000.
		title := forgeTitle(g)
		words := stack(2, text(title, 16, colText, true))
		if g.Port != 0 {
			words.Add(text(fmt.Sprintf("Port Number %d", g.Port), 12.5, colFaint, false))
		}
		if g.Owner != "" {
			words.Add(text("Owner "+shortID(g.Owner), 12.5, colFaint, false))
		}
		row := container.NewBorder(nil, nil,
			container.NewHBox(container.New(fixedWidth{26}, container.NewCenter(text(fmt.Sprintf("%d.", g.Index), 16, colDim, true))),
				avatarSquare(title, g.GuildID, 44), gap(2)),
			stateBadge(g), container.NewVBox(layout.NewSpacer(), words, layout.NewSpacer()))
		r := newTapRow(container.New(layout.NewCustomPaddedLayout(8, 8, 10, 14), row), false, func() { f.openGuild(g.GuildID, "overview") })
		r.base = colPanel
		list.Add(r)
	}
	create := primary("Create Guild", func() { f.createTab = 0; f.goTo("create") })
	if len(f.guilds) >= host.MaxGuilds {
		create.Disable()
	}
	head := container.NewBorder(nil, nil, backButton(func() { f.goTo("home") }), create)
	return forgeFrame(cogIcon, "Manage Guild", head,
		titled(heading("Your Guilds"), para(fmt.Sprintf("%d of %d. Pick one to start, stop or change it.", len(f.guilds), host.MaxGuilds))),
		card(10, list))
}

// stateBadge is a guild's state with its coloured dot, centred together.
func stateBadge(g host.GuildInfo) fyne.CanvasObject {
	state, colour := forgeState(g)
	dot := canvas.NewCircle(colour)
	t := text(state, 13.5, colDim, false)
	h := t.MinSize().Height
	// The dot sits in the middle of the words' height.
	// Text keeps room below for letters like g and y, so the middle of the
	// words sits a little below the middle of its box: the dot goes there.
	dotBox := container.New(fixedSize{fyne.NewSize(9, h)}, container.New(layout.NewCustomPaddedLayout(h*0.18, 0, 0, 0), container.NewCenter(container.New(fixedSize{fyne.NewSize(9, 9)}, dot))))
	return container.NewCenter(container.NewHBox(dotBox, t))
}

// forgeState is a guild's state in a few words, with its colour.
func forgeState(g host.GuildInfo) (string, color.Color) {
	switch {
	case g.Broadcasting:
		return "Broadcasting… (" + g.BroadcastEnds.Local().Format("15:04") + ")", colIdle
	case g.Running:
		return fmt.Sprintf("Running, %d of %d online", g.Online, g.Members), colOnline
	}
	return "Offline", colFaint
}

// ---- one guild's page ---------------------------------------------------------------

func (f *forge) guildPage() fyne.CanvasObject {
	g := f.guild()
	// The side bar shows the guild, its categories, and Delete Guild.
	back := widget.NewButtonWithIcon("Back", backIcon, func() { f.goTo("manage") })
	back.Importance = widget.LowImportance
	side := container.NewVBox(container.NewHBox(back),
		container.New(layout.NewCustomPaddedLayout(6, 0, 10, 8), text(forgeTitle(*g), 16, colText, true)))
	group := ""
	for _, c := range forgeCats {
		c := c
		if c.group != group {
			group = c.group
			side.Add(container.New(layout.NewCustomPaddedLayout(14, 4, 10, 8), text(group, 11.5, colFaint, true)))
		}
		b := widget.NewButton(c.label, func() { f.openGuild(g.GuildID, c.key) })
		b.Alignment = widget.ButtonAlignLeading
		b.Importance = widget.LowImportance
		if c.key == f.cat {
			b.Importance = widget.MediumImportance
		}
		side.Add(b)
	}
	side.Add(container.New(layout.NewCustomPaddedLayout(10, 10, 0, 0), sideLineH()))
	del := widget.NewButton("Delete Guild", func() { f.openGuild(g.GuildID, "delete") })
	del.Alignment = widget.ButtonAlignLeading
	del.Importance = widget.LowImportance
	if f.cat == "delete" {
		del.Importance = widget.MediumImportance
	}
	side.Add(newTint(del, colorTheme{Theme: appTheme, text: colDanger}))
	sideBG := canvas.NewRectangle(colPanel)
	sidePane := container.NewStack(sideBG, container.NewVScroll(container.New(layout.NewCustomPaddedLayout(8, 8, 8, 8), side)))

	// The chosen category. Forms are kept while their category is open, so
	// the state refreshing every two seconds never wipes what is typed.
	formCat := f.cat == "settings" || f.cat == "export" || f.cat == "delete" || f.cat == "backups"
	key := f.guildID + "|" + f.cat
	if !formCat {
		key += "|" + f.sig
	}
	if f.body == nil || f.catSig != key {
		f.catSig = key
		f.body = container.NewStack(f.catBody(*g))
	}
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(14, 14, 24, 24), container.NewBorder(nil, nil,
			container.NewHBox(icon(gearIcon, 18), text("Guild Settings", 16, colText, true)),
			stateBadge(*g))))
	main := container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil,
		container.NewVScroll(container.New(layout.NewCustomPaddedLayout(24, 40, 32, 32), narrowTop(740, f.body))))
	return container.NewBorder(nil, nil, container.New(fixedWidth{260}, sidePane), nil, main)
}

func (f *forge) catBody(g host.GuildInfo) fyne.CanvasObject {
	switch f.cat {
	case "settings":
		return f.settingsCat(g)
	case "pairing":
		return f.pairingCat(g)
	case "backups":
		return f.backupsCat(g)
	case "export":
		return f.exportCat(g)
	case "delete":
		return f.deleteCat(g)
	}
	return f.overviewCat(g)
}

// busy runs work away from the window, then shows its error or note.
func (f *forge) busy(b *widget.Button, label string, problem *errorText, work func() error, done string) {
	problem.Set(nil)
	if b != nil {
		b.Disable()
	}
	go func() {
		err := work()
		fyne.Do(func() {
			if b != nil {
				b.Enable()
				b.SetText(label)
			}
			if err != nil {
				problem.Set(err)
				return
			}
			if done != "" {
				f.u.toast(done)
			}
			f.sig, f.catSig = "", ""
			f.load()
			f.draw()
		})
	}()
}

func copyRow(u *UI, title, value, note string) fyne.CanvasObject {
	b := widget.NewButtonWithIcon("Copy", copyIcon, func() {
		u.app.Clipboard().SetContent(value)
		u.toast(note)
	})
	v := text(value, 13, colDim, false)
	v.TextStyle.Monospace = true
	return container.New(layout.NewCustomPaddedLayout(12, 12, 0, 0),
		container.NewBorder(nil, nil, nil, container.NewCenter(b), stack(2, text(title, 15, colText, true), v)))
}

func (f *forge) overviewCat(g host.GuildInfo) fyne.CanvasObject {
	problem := newErrorText()
	var toggle *widget.Button
	if g.Running {
		toggle = widget.NewButton("Stop Guild", nil)
		toggle.OnTapped = func() {
			toggle.SetText("Stopping…")
			f.busy(toggle, "Stop Guild", problem, func() error { return host.StopGuild(f.dir, g.GuildID) }, "Guild stopped")
		}
	} else {
		toggle = primary("Start Guild", nil)
		toggle.OnTapped = func() {
			toggle.SetText("Starting…")
			f.busy(toggle, "Start Guild", problem, func() error { return host.StartGuild(f.dir, g.GuildID) }, "Guild started")
		}
		if g.Port == 0 {
			toggle.Disable()
		}
	}
	state, _ := forgeState(g)
	about := "Offline. Nobody can connect until it is started."
	switch {
	case g.Port == 0:
		about = "It has no port yet. Set one under Settings."
	case g.Running:
		about = fmt.Sprintf("Running. %d of %d members are connected.", g.Online, g.Members)
	}
	owner := "Nobody yet. Pair an owner under Pairing."
	if g.Owner != "" {
		owner = shortID(g.Owner)
	}
	rows := []fyne.CanvasObject{
		copyRow(f.u, "Guild ID", g.GuildID, "Guild ID copied."),
		settingRow("Owner", owner, nil),
		settingRow("Folder", g.DataDir, nil)}
	parts := []fyne.CanvasObject{panelTitle("Overview"), settingRow(state, about, toggle), problem.box}
	if g.Port != 0 {
		parts = append(parts, f.addressList(g))
	}
	return form(append(parts, settingList("DETAILS", rows...))...)
}

// addressList is where people can reach the guild: on this
// network, and on the internet over IPv4 and IPv6.
func (f *forge) addressList(g host.GuildInfo) fyne.CanvasObject {
	copied := "Address copied. Send it to people you want to invite."
	hostPort := func(a string) string { return net.JoinHostPort(a, strconv.Itoa(g.Port)) }
	var rows []fyne.CanvasObject
	st := g.Public
	local := host.LocalAddresses()
	if st != nil {
		local = st.Local
	}
	for _, a := range local {
		rows = append(rows, addressRow(f.u, "Home Network", hostPort(a), "For people on the same Wi-Fi or network as this computer.", copied))
	}
	if st == nil {
		rows = append(rows, settingRow("Internet", "Start the guild to see its internet addresses.", nil))
		return settingList("ADDRESSES", rows...)
	}
	// IPv4 addresses go through the router.
	switch {
	case st.External != "" && st.CGNAT:
		rows = append(rows, settingRow("Internet (IPv4)", "Your internet provider shares one address among many homes (CGNAT), so people on the internet can't reach this guild over IPv4. They can over IPv6, below, if both sides have it.", nil))
	case st.External != "" && st.Open:
		rows = append(rows, addressRow(f.u, "Internet (IPv4)", hostPort(st.External), "The router opened the port. People anywhere can use this address.", copied))
	case st.External != "" && st.Off:
		rows = append(rows, addressRow(f.u, "Internet (IPv4)", hostPort(st.External), "Port Forwarding is off: forward the port by hand (see the guide) for this address to work.", copied))
	case st.External != "":
		rows = append(rows, addressRow(f.u, "Internet (IPv4)", hostPort(st.External), st.Problem, copied))
	case st.Off:
		rows = append(rows, settingRow("Internet (IPv4)", "Port Forwarding is off. Forward the port by hand (see the guide), then give people your router's internet address.", nil))
	default:
		rows = append(rows, settingRow("Internet (IPv4)", st.Problem+" Your router's own settings page shows its internet address.", nil))
	}
	// IPv6 uses this computer's own address.
	if len(st.IPv6) == 0 {
		rows = append(rows, settingRow("Internet (IPv6)", "This computer has no internet IPv6 address right now.", nil))
	}
	for _, a := range st.IPv6 {
		note := "If people can't connect, allow this port in your router's IPv6 firewall (see the guide)."
		if st.IPv6Open {
			note = "The router's firewall lets the port in. People with IPv6 can use this address."
		}
		rows = append(rows, addressRow(f.u, "Internet (IPv6)", hostPort(a), note, copied))
	}
	return settingList("ADDRESSES", rows...)
}

// addressRow is an address to copy, with a line about it.
func addressRow(u *UI, title, value, about, toastText string) fyne.CanvasObject {
	b := widget.NewButtonWithIcon("Copy", copyIcon, func() {
		u.app.Clipboard().SetContent(value)
		u.toast(toastText)
	})
	v := text(value, 13, colDim, false)
	v.TextStyle.Monospace = true
	words := stack(2, text(title, 15, colText, true), v)
	if about != "" {
		words.Add(hintText(about))
	}
	return container.New(layout.NewCustomPaddedLayout(12, 12, 0, 0), container.NewBorder(nil, nil, nil, container.NewCenter(b), words))
}

func (f *forge) settingsCat(g host.GuildInfo) fyne.CanvasObject {
	problem := newErrorText()
	port := widget.NewEntry()
	if g.Port != 0 {
		port.SetText(strconv.Itoa(g.Port))
	}
	port.SetPlaceHolder("50000")
	var savePort *widget.Button
	savePort = widget.NewButton("Save Port", func() {
		p, err := strconv.Atoi(strings.TrimSpace(port.Text))
		if err != nil {
			problem.Set(errors.New("the port must be a number"))
			return
		}
		savePort.SetText("Saving…")
		f.busy(savePort, "Save Port", problem, func() error { return host.SetGuildPort(f.dir, g.GuildID, p) }, "Port saved")
	})
	boot := toggle(g.StartOnBoot, nil)
	boot.OnChanged = func(on bool) {
		f.busy(nil, "", problem, func() error { return host.SetGuildBoot(f.dir, g.GuildID, on) }, map[bool]string{true: "It will start by itself", false: "It won't start by itself"}[on])
	}
	autoPort := toggle(!g.NoAutoPort, nil) // This is Port Forwarding.
	autoPort.OnChanged = func(on bool) {
		f.busy(nil, "", problem, func() error { return host.SetAutoPort(f.dir, g.GuildID, on) },
			map[bool]string{true: "The router will be asked to open the port", false: "The router won't be asked"}[on])
	}
	bootNote := "Start this guild whenever this computer starts."
	if strings.Contains(strings.ToLower(platform.BootNote), "sign in") {
		bootNote = "Start this guild whenever you sign in to this computer."
	}

	cur, next, again := widget.NewPasswordEntry(), widget.NewPasswordEntry(), widget.NewPasswordEntry()
	cur.SetPlaceHolder("Enter Password")
	next.SetPlaceHolder("Enter Password")
	again.SetPlaceHolder("Repeat Password")
	passProblem := newErrorText()
	var savePass *widget.Button
	savePass = primary("Change Password", func() {
		if err := host.CheckNewPassword(next.Text, again.Text); err != nil {
			passProblem.Set(err)
			return
		}
		c, n := cur.Text, next.Text
		savePass.SetText("Changing…")
		f.busy(savePass, "Change Password", passProblem, func() error { return host.ChangeGuildPassword(f.dir, g.GuildID, c, n) }, "Password changed")
	})
	// Two steps: the current and new password; then the new one again.
	step1, step2 := form(), form()
	passSteps, forward, back := twoSteps(step1, step2)
	firstProblem := newErrorText()
	goOn := func() {
		firstProblem.Set(nil)
		if len([]rune(next.Text)) < host.MinPasswordLength {
			firstProblem.Set(fmt.Errorf("The new password must be at least %d characters.", host.MinPasswordLength))
			return
		}
		forward()
		f.u.win.Canvas().Focus(again)
	}
	next.OnSubmitted = func(string) { goOn() }
	step1.Objects = []fyne.CanvasObject{field("Current Password", cur, ""),
		field("New Password", next, fmt.Sprintf("At least %d characters.", host.MinPasswordLength)),
		firstProblem.box, buttonsRow(widget.NewButton("Next", goOn))}
	step2.Objects = []fyne.CanvasObject{field("Confirm Password", again, "Type the new password again."), passProblem.box,
		buttonsRow(widget.NewButton("Back", back), savePass)}
	return form(
		panelTitle("Settings"),
		field("Port", container.NewHBox(container.New(fixedWidth{160}, port), savePort), fmt.Sprintf("From %d to %d. A running guild restarts on the new port.", host.MinPort, host.MaxPort)),
		settingList("", settingRow("Start Automatically", bootNote, boot),
			settingRow("Port Forwarding", "Ask the router to forward the port to this computer while the guild runs, so people on the internet can reach it. It is closed again when the guild stops.", autoPort)),
		problem.box,
		sideLineH(),
		titled(text("Password", 18, colText, true), para("It protects the guild's settings, exports, backups and deleting it.")),
		passSteps)
}

func (f *forge) pairingCat(g host.GuildInfo) fyne.CanvasObject {
	problem := newErrorText()
	if g.Owner != "" {
		pass := widget.NewPasswordEntry()
		pass.SetPlaceHolder("Enter Password")
		var remove *widget.Button
		remove = widget.NewButton("Remove Owner", func() {
			pw := pass.Text
			f.u.confirm("Remove Owner", "The guild will have no owner until someone pairs again. Members and messages stay.", "Remove", true, func() {
				remove.SetText("Removing…")
				f.busy(remove, "Remove Owner", problem, func() error { return host.RemoveOwner(f.dir, g.GuildID, pw) }, "Owner removed")
			})
		})
		remove.Importance = widget.DangerImportance
		return form(
			panelTitle("Pairing"),
			para("This guild has an owner, "+shortID(g.Owner)+". Pairing is only for a guild with no owner."),
			field("Password", pass, "To remove the owner, so someone else can pair."),
			problem.box,
			buttonsRow(remove))
	}
	if !g.Broadcasting {
		var start *widget.Button
		start = primary("Start Broadcast", func() {
			start.SetText("Starting…")
			f.busy(start, "Start Broadcast", problem, func() error { return host.Broadcast(f.dir, g.GuildID, true) }, "Broadcasting")
		})
		if g.Port == 0 {
			start.Disable()
		}
		return form(
			panelTitle("Pairing"),
			para("The person who will own this guild pairs with it from the Nexus app: Add Guild, then Pair Owner. Start a broadcast here first. It lasts an hour."),
			problem.box,
			buttonsRow(start))
	}
	otp := text("…", 30, colText, true)
	otp.TextStyle.Monospace = true
	otp.Alignment = fyne.TextAlignCenter
	until := centred(text("", 13, colDim, false))
	makeOTP := func() {
		go func() {
			code, exp, err := host.NewOTP(f.dir, g.GuildID)
			fyne.Do(func() {
				if err != nil {
					problem.Set(err)
					return
				}
				otp.Text = code
				otp.Refresh()
				until.Text = "Valid until " + exp.Local().Format("15:04") + ". Making a new one cancels this one."
				until.Refresh()
			})
		}()
	}
	makeOTP()
	again := widget.NewButton("Make Another", makeOTP)
	var stop *widget.Button
	stop = widget.NewButton("Stop Broadcast", func() {
		stop.SetText("Stopping…")
		f.busy(stop, "Stop Broadcast", problem, func() error { return host.Broadcast(f.dir, g.GuildID, false) }, "Broadcast stopped")
	})
	var addrs []fyne.CanvasObject
	for _, a := range host.LocalAddresses() {
		addrs = append(addrs, copyRow(f.u, "Address", fmt.Sprintf("%s:%d", a, g.Port), "Address copied."))
	}
	return form(
		panelTitle("Pairing"),
		para(fmt.Sprintf("Broadcasting until %s. In the Nexus app, the owner chooses Add Guild, then Pair Owner, types this computer's address and port, and then this one-time password.", g.BroadcastEnds.Local().Format("15:04"))),
		card(20, stack(fieldGap, centred(text("ONE-TIME PASSWORD", 11.5, colFaint, true)), otp, until)),
		problem.box,
		buttonsRow(stop, again),
		settingList("ADDRESSES", addrs...))
}

func (f *forge) backupsCat(g host.GuildInfo) fyne.CanvasObject {
	problem := newErrorText()
	backups, err := host.Backups(f.dir, g.GuildID)
	if err != nil {
		problem.Set(err)
	}
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	var rows []fyne.CanvasObject
	for _, b := range backups {
		b := b
		var restore *widget.Button
		restore = widget.NewButton("Restore", func() {
			pw := pass.Text
			f.u.confirm("Restore Backup", "Put the guild back to how it was on "+b.Date.Format("2 January 2006 at 15:04")+"? Its state now is saved as a new backup first.", "Restore", true, func() {
				restore.SetText("Restoring…")
				f.busy(restore, "Restore", problem, func() error { return host.RestoreBackup(f.dir, g.GuildID, pw, b.Name) }, "Backup Restored")
			})
		})
		var del *widget.Button
		del = widget.NewButton("Delete", func() {
			pw := pass.Text
			f.u.confirm("Delete Backup", "Delete the backup from "+b.Date.Format("2 January 2006 at 15:04")+"? This cannot be undone.", "Delete", true, func() {
				f.busy(del, "Delete", problem, func() error {
					if err := host.CheckGuildPassword(f.dir, g.GuildID, pw); err != nil {
						return err
					}
					return host.DeleteBackup(f.dir, g.GuildID, b.Name)
				}, "Backup Deleted")
			})
		})
		about := b.Size + ", made by hand."
		if b.Auto {
			about = b.Size + ", made automatically."
		}
		rows = append(rows, settingRow(b.Date.Format("2 January 2006, 15:04"), about, container.NewHBox(restore, del)))
	}
	// The list opens under the buttons, from Restore Backup.
	list := form(settingList(fmt.Sprintf("SNAPSHOTS (%d)", len(rows)), rows...), field("Password", pass, "Needed to restore or delete a backup."))
	list.Hide()
	restoreBtn := widget.NewButton("Restore Backup", nil)
	restoreBtn.OnTapped = func() {
		if list.Visible() {
			list.Hide()
		} else {
			list.Show()
		}
	}
	if len(rows) == 0 {
		restoreBtn.Disable()
	}
	var make *widget.Button
	make = primary("Make Backup", func() {
		make.SetText("Saving…")
		f.busy(make, "Make Backup", problem, func() error {
			_, err := host.MakeBackup(f.dir, g.GuildID)
			return err
		}, "Backup Saved")
	})
	// This is the Automatic Backups setting.
	names := []string{}
	for _, m := range host.AutoBackupModes {
		names = append(names, host.AutoBackupName(m))
	}
	auto := widget.NewSelect(names, nil)
	auto.SetSelected(host.AutoBackupName(g.AutoBackups))
	auto.OnChanged = func(name string) {
		f.busy(nil, "", problem, func() error { return host.SetAutoBackups(f.dir, g.GuildID, name) }, "Automatic Backups: "+name)
	}
	return form(
		panelTitle("Backups"),
		para("A backup is a copy of the whole guild, kept in GuildForge's folder. Restoring one puts the guild back to how it was then."),
		settingList("", settingRow("Automatic Backups", fmt.Sprintf("Made while the guild runs. The last %d are kept; backups you make by hand stay until you delete them.", host.KeepAutoBackups), container.New(fixedWidth{130}, auto))),
		buttonsRow(restoreBtn, make),
		problem.box,
		list)
}

func (f *forge) exportCat(g host.GuildInfo) fyne.CanvasObject {
	problem := newErrorText()
	folder := widget.NewEntry()
	if home, err := os.UserHomeDir(); err == nil {
		folder.SetText(home)
	}
	browse := widget.NewButton("Browse…", func() {
		f.u.pickFolder("Choose where to save the file", func(dir string) { folder.SetText(dir) }, problem)
	})
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	done := hintText("")
	done.Hide()
	var exp *widget.Button
	exp = primary("Export Guild", func() {
		problem.Set(nil)
		dir, pw := folder.Text, pass.Text
		exp.Disable()
		exp.SetText("Exporting…")
		go func() {
			path, err := host.ExportGuild(f.dir, g.GuildID, pw, dir)
			fyne.Do(func() {
				exp.Enable()
				exp.SetText("Export Guild")
				if err != nil {
					problem.Set(err)
					return
				}
				f.u.toast("Saved " + path)
			})
		}()
	})
	return form(
		panelTitle("Export"),
		para("Save the whole guild as one encrypted .nexus file, to move it to another computer. There, use Create Guild, then Import Guild."),
		field("Folder", container.NewBorder(nil, nil, nil, browse, folder), ""),
		field("Password", pass, "The file is encrypted with it. It holds the guild's secret key, so keep it safe."),
		problem.box,
		buttonsRow(exp))
}

func (f *forge) deleteCat(g host.GuildInfo) fyne.CanvasObject {
	problem := newErrorText()
	pass := widget.NewPasswordEntry()
	pass.SetPlaceHolder("Enter Password")
	erase, eraseBox := option("Erase Folder", "Also delete the guild's folder and everything in it. This can't be undone.", false)
	drop, dropBox := option("Delete Backups", "Also delete its backups.", false)
	var del *widget.Button
	del = widget.NewButton("Delete Guild", func() {
		pw, e, d := pass.Text, erase.Checked, drop.Checked
		f.u.confirm("Delete Guild", "Take "+forgeTitle(g)+" off this computer? It stops at once.", "Delete", true, func() {
			del.SetText("Deleting…")
			problem.Set(nil)
			del.Disable()
			go func() {
				err := host.DeleteGuild(f.dir, g.GuildID, pw, e, d)
				fyne.Do(func() {
					del.Enable()
					del.SetText("Delete Guild")
					if err != nil {
						problem.Set(err)
						return
					}
					f.u.toast("Guild deleted")
					f.load()
					if len(f.guilds) == 0 {
						f.goTo("home")
					} else {
						f.goTo("manage")
					}
				})
			}()
		})
	})
	del.Importance = widget.DangerImportance
	return form(
		panelTitle("Delete Guild"),
		para("The guild is stopped and taken off this computer. Unless you erase it, its folder stays, and Import Guild can add it back."),
		eraseBox, dropBox,
		field("Password", pass, ""),
		problem.box,
		buttonsRow(del))
}

// forgeFrame is a GuildForge page with a title bar and scrolling content.
func forgeFrame(ic fyne.Resource, title string, parts ...fyne.CanvasObject) fyne.CanvasObject {
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(14, 14, 24, 24), container.NewHBox(icon(ic, 18), text(title, 16, colText, true))))
	body := container.New(layout.NewCustomPaddedLayout(20, 30, 24, 24), narrowTop(760, container.New(&column{gap: formGap}, parts...)))
	return container.NewBorder(container.NewVBox(top, sideLineH()), nil, nil, nil, container.NewVScroll(body))
}

// fixedSize gives its content an exact size.
type fixedSize struct{ s fyne.Size }

func (l fixedSize) MinSize([]fyne.CanvasObject) fyne.Size { return l.s }
func (l fixedSize) Layout(objs []fyne.CanvasObject, _ fyne.Size) {
	for _, o := range objs {
		o.Resize(l.s)
		o.Move(fyne.NewPos(0, 0))
	}
}

// startScreens opens a page for pictures of it (FORGE_SCREEN):
// "create", "import", "manage", "licenses", "updates", "updates-available",
// or "guild:N:category".
func (f *forge) startScreens() {
	s := os.Getenv("FORGE_SCREEN")
	switch {
	case s == "create" || s == "import":
		f.createTab = map[string]int{"create": 0, "import": 1}[s]
		f.goTo("create")
	case s == "manage":
		f.goTo("manage")
	case s == "licenses" || s == "updates":
		f.goTo(s)
	case s == "updates-available": // This shows a newer version without asking GitHub.
		f.upd = appUpdate{state: "available", latest: "0.2.0"}
		f.goTo("updates")
	case strings.HasPrefix(s, "guild:"):
		parts := strings.Split(s, ":")
		n, _ := strconv.Atoi(parts[1])
		for _, g := range f.guilds {
			if g.Index == n {
				f.openGuild(g.GuildID, parts[2])
			}
		}
	}
	if shot := os.Getenv("NEXUS_SHOT"); shot != "" {
		go f.u.shootWhenStill(shot, 2500*time.Millisecond)
	}
}

// forgeTitle is a guild's name in the window: its own name, or "Guild 1"
// until it has one.
func forgeTitle(g host.GuildInfo) string {
	if g.GuildName != "" {
		return g.GuildName
	}
	return fmt.Sprintf("Guild %d", g.Index)
}
