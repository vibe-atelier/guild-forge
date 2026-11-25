// Package ui is the Nexus app's window, drawn with Fyne.
// The window calls the app's Go code directly: there is no local web server.
package ui

import (
	"image/png"
	"log"
	"os"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"

	"nexus/internal/guildconn"
	"nexus/internal/single"
	"nexus/internal/user"
	"nexus/internal/voice"
)

// appTheme is loaded once and shared (it holds the font files).
var appTheme *nexusTheme

// UI is the running app window.
type UI struct {
	Version   string
	ConfigDir string // It is the app's own settings folder (recent users).
	Log       *log.Logger

	app  fyne.App
	win  fyne.Window
	user *user.User // It is the unlocked user, or nil.

	shotPending bool // It is for pictures only: a screen step is still to come.

	// These fields are set while a user is unlocked.
	guilds     *guildconn.Manager // These are the live guild connections.
	home       *home
	selected   string // It is the guild shown ("" means home, and "+" means add a guild).
	homeBtn    *railButton
	meStatus   *canvas.Text
	meDot      *canvas.Circle
	page       string                   // It is the guild page shown: about, rules or requests.
	pageSig    string                   // It changes when the page's own data changes.
	pageData   any                      // It is the data loaded for the page (the Rules text, join requests).
	pageArg    string                   // It is which post, event or Wiki page is shown.
	pageKey    string                   // It is what pageData was loaded for.
	pageRev    int                      // It is the guild's PagesRev when it was loaded.
	loading    string                   // It is a load on its way.
	calMonth   time.Time                // It is the month the Calendar shows.
	fetching   map[string]bool          // These are guild pictures being fetched.
	emojiCache map[string]fyne.Resource // These are custom emoji pictures, by hash.
	files      map[string]string        // These are channel files opened, by hash.
	chat       *chatState               // It is the open channel, if any.
	dm         *dmChat                  // It is the open private chat, if any.
	collapsed  map[string]bool          // These are the folded categories and folders.

	devAddress     string // For the guide's pictures only, it is an address to fill in and check (screens.go).
	devSkipWelcome bool   // For the guide's pictures only, the quiz opens on its first question.
	devOpenRules   bool   // For the guide's pictures only, the join preview opens its Rules.

	focused     bool              // It is true while the window is in front.
	lastSound   time.Time         // Sounds play at most once a second.
	knownBanner map[string]bool   // These are banners already kept on this computer.
	noticeShown map[string]string // These are the Rules notices shown since the app opened.

	settings   *settingsView // It is a settings page, when one is open.
	editor     *editorView   // It is an editor page, when one is open.
	joining    *joinView     // It is joining a guild, while that is under way.
	voice      *voice.Engine // It is voice on this computer.
	selVoice   string        // It is the person chosen on a voice page.
	showBoard  bool          // It is true while the soundboard is open on the voice page.
	sideScroll struct {      // It is where the guild side bar is scrolled to.
		guild string
		at    fyne.Position
	}
	upd        appUpdate // It is how far Check Updates has got.
	homeView   string    // It is the home view: "" (Friends) or "profile".
	friendsTab int       // It is the Friends page's tab: 0 Contacts, 1 Requests, 2 Blocked or 3 Add Friend.
}

// Run opens the window and returns when it is closed.
func Run(version, configDir string, logger *log.Logger) {
	// One copy at a time: a second start brings the first
	// copy's window to the front and closes.
	already, wake, stop := single.Claim(configDir)
	// After Restart Nexus, the old copy is still closing for a moment, so the
	// new one waits for it (up to 10 seconds) instead of giving way to it.
	if slices.Contains(os.Args[1:], RestartArg) {
		for i := 0; already && i < 50; i++ {
			time.Sleep(200 * time.Millisecond)
			already, wake, stop = single.Claim(configDir)
		}
	}
	if already {
		logger.Printf("already running: showed that window instead")
		return
	}
	defer stop()
	showWake = wake
	run(app.NewWithID("app.nexus.desktop"), version, configDir, logger, false)
}

// showWake tells the window to come to the front (a second start).
var showWake <-chan struct{}

// RunOffscreen draws the app in memory only, for the guide's pictures
// (docs/shots): no window pops up. It returns once the window is set up;
// the picture switches end the program (screens.go).
func RunOffscreen(a fyne.App, version, configDir string, logger *log.Logger) {
	run(a, version, configDir, logger, true)
}

func run(a fyne.App, version, configDir string, logger *log.Logger, offscreen bool) {
	useEmojiFont() // This sets up newer emoji before any text is drawn.
	appTheme = newTheme()
	a.Settings().SetTheme(appTheme)

	w := a.NewWindow("Nexus")
	w.SetIcon(logoIcon)
	u := &UI{Version: version, ConfigDir: configDir, Log: logger, app: a, win: w}
	u.show(u.welcome())
	w.Resize(fyne.NewSize(1180, 760))
	w.CenterOnScreen()
	w.SetMaster()

	u.watchFocus()
	u.watchLayout()
	u.startScreens() // This is for the guide's pictures (screens.go).
	if offscreen {
		w.Show()
		return
	}
	if showWake != nil {
		go func() {
			for range showWake {
				fyne.Do(func() { w.Show(); w.RequestFocus() })
			}
		}()
	}
	w.ShowAndRun()
}

// show replaces what the window shows.
func (u *UI) show(o fyne.CanvasObject) { u.win.SetContent(o) }

// savePicture saves a PNG picture of the window.
func (u *UI) savePicture(path string) {
	f, err := os.Create(path)
	if err != nil {
		u.Log.Printf("picture: %v", err)
		return
	}
	defer f.Close()
	png.Encode(f, u.win.Canvas().Capture())
}
