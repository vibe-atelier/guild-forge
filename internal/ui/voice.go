package ui

// This file is voice in the window: voice channels in the
// channel list with the people in them, a page for each voice channel, a
// bar while connected, mute and deafen by your name, and the Voice page of
// User Settings.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/proto"
	"nexus/internal/rnnoise"
	"nexus/internal/sound"
	"nexus/internal/voice"
)

const iconStroke = `fill="none" stroke="#b8b8c2" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"`

var speakerIcon = svg("speaker.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4z"/><path d="M15.5 9a4 4 0 0 1 0 6"/><path d="M18 6.5a7.5 7.5 0 0 1 0 11"/></g></svg>`)
var micIcon = svg("mic.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><rect x="9" y="3.5" width="6" height="11" rx="3"/><path d="M6 11.5a6 6 0 0 0 12 0M12 17.5V21"/></g></svg>`)
var micOffIcon = svg("mic-off.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#f25f6b" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="3.5" width="6" height="11" rx="3"/><path d="M6 11.5a6 6 0 0 0 12 0M12 17.5V21M4 4l16 16"/></g></svg>`)
var headsetIcon = svg("headset.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><path d="M4.5 15v-3a7.5 7.5 0 0 1 15 0v3"/><rect x="3.5" y="14" width="4" height="6" rx="1.5"/><rect x="16.5" y="14" width="4" height="6" rx="1.5"/></g></svg>`)
var headsetOffIcon = svg("headset-off.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#f25f6b" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 15v-3a7.5 7.5 0 0 1 15 0v3"/><rect x="3.5" y="14" width="4" height="6" rx="1.5"/><rect x="16.5" y="14" width="4" height="6" rx="1.5"/><path d="M4 4l16 16"/></g></svg>`)
var hangUpIcon = svg("hang-up.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g fill="none" stroke="#f25f6b" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 13.5c4.5-4.2 12.5-4.2 17 0l-2 2.5-3-1.2v-2.3c-2.3-.8-4.7-.8-7 0v2.3l-3 1.2z"/></g></svg>`)

// startVoice makes the voice engine for the unlocked user.
func (u *UI) startVoice() {
	u.voice = voice.New(u.guilds, u.user.Profile.Voice, u.guildsChanged)
}

// stopVoice leaves voice (when locking).
func (u *UI) stopVoice() {
	if u.voice != nil {
		u.voice.Leave()
		u.voice.Test(false)
	}
}

// joinVoice joins a voice channel, or opens its page if you are in it.
func (u *UI) joinVoice(g guildconn.GuildView, channel string) {
	u.openPageArg("voice", channel)
	if gid, ch := u.voice.Where(); gid == g.ID && ch == channel {
		return
	}
	gc, err := u.guilds.Get(g.ID)
	if err != nil {
		u.toast(sentence(err.Error()))
		return
	}
	if !gc.HasVoiceKey(channel) {
		u.toast("This voice channel's key hasn't arrived yet. It comes when another member is online")
		return
	}
	go func() {
		err := u.voice.Join(gc, g.ID, channel)
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
			}
			u.redraw()
		})
	}()
}

// voiceSig is what the voice parts of the window show, so they are drawn
// again when it changes (who is talking, mute and deafen).
func (u *UI) voiceSig() string {
	if u.voice == nil {
		return ""
	}
	gid, ch := u.voice.Where()
	m, d := u.voice.Muted()
	var talk []string
	for k := range u.voice.Speaking() {
		talk = append(talk, k)
	}
	if _, me := u.voice.Level(); me {
		talk = append(talk, "me")
	}
	sort.Strings(talk)
	shared, _ := u.voice.Sharing()
	watched, _ := u.voice.Watching()
	blurred, muted := u.voice.SharingHeld()
	return fmt.Sprintf("%s|%s|%v|%v|%s|%s|%s|%s|%v|%v|%d|%s|%v", gid, ch, m, d, strings.Join(talk, ","), u.voice.Problem(), shared, watched, blurred, muted,
		u.voice.PreviewRev(), u.selVoice, u.voice.CameraOn())
}

// isTalking says whether someone is talking now (you, or others).
func (u *UI) isTalking(key string) bool {
	if u.voice == nil {
		return false
	}
	if key == u.user.Profile.UserID {
		_, me := u.voice.Level()
		return me
	}
	return u.voice.Speaking()[key]
}

// ---- in the channel list ------------------------------------------------------------

func (u *UI) voiceRow(g guildconn.GuildView, n guildconn.ChannelView) fyne.CanvasObject {
	ic := speakerIcon
	if n.Locked {
		ic = lockedVoiceIcon
	}
	r := newTreeRow(ic, n.Name, func() { u.joinVoice(g, n.ID) }, u.channelGear(g, n.ChannelNode))
	open := u.page == "voice" && u.pageArg == n.ID
	r.active = open
	if gid, ch := u.voice.Where(); gid == g.ID && ch == n.ID {
		r.colour = colText
	}
	if len(n.Voice) == 0 {
		return r
	}
	box := container.NewVBox(r)
	for _, v := range sortedVoice(g, n.Voice) {
		box.Add(container.New(layout.NewCustomPaddedLayout(0, 0, 30, 4), u.voicePerson(g, v, 22)))
	}
	return box
}

func sortedVoice(g guildconn.GuildView, list []proto.VoiceMember) []proto.VoiceMember {
	out := append([]proto.VoiceMember{}, list...)
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(nameOf(g, out[i].User)) < strings.ToLower(nameOf(g, out[j].User))
	})
	return out
}

// talkRing is a picture with a green ring while its person talks.
func (u *UI) talkRing(g guildconn.GuildView, key string, size float32) fyne.CanvasObject {
	pic := avatar(nameOf(g, key), key, size)
	if !u.isTalking(key) {
		return container.New(fixedSize{fyne.NewSize(size+4, size+4)}, container.NewCenter(pic))
	}
	ring := canvas.NewCircle(nil)
	ring.StrokeColor = colOnline
	ring.StrokeWidth = 2
	return container.New(fixedSize{fyne.NewSize(size+4, size+4)}, ring, container.NewCenter(pic))
}

// voicePerson is someone in a voice channel: picture, name, and whether
// they muted or deafened themselves.
func (u *UI) voicePerson(g guildconn.GuildView, v proto.VoiceMember, size float32) fyne.CanvasObject {
	icons := container.NewHBox()
	if v.Live {
		icons.Add(container.NewCenter(livePill()))
	}
	if v.Deafened || v.GuildDeafened {
		icons.Add(icon(headsetOffIcon, 14))
	} else if v.Muted || v.GuildMuted {
		icons.Add(icon(micOffIcon, 14))
	}
	return container.NewBorder(nil, nil, container.NewHBox(u.talkRing(g, v.User, size), gap(1)), container.NewCenter(icons),
		container.NewVBox(layout.NewSpacer(), text(nameOf(g, v.User), 13.5, colDim, false), layout.NewSpacer()))
}

// ---- a voice channel's page --------------------------------------------------------------

func (u *UI) voicePage(g guildconn.GuildView) fyne.CanvasObject {
	var n *guildconn.ChannelView
	for i := range g.Channels {
		if g.Channels[i].ID == u.pageArg && g.Channels[i].Kind == proto.KindVoice {
			n = &g.Channels[i]
		}
	}
	if n == nil {
		return pageFrame(speakerIcon, "Voice", para("This voice channel is gone."))
	}
	gid, ch := u.voice.Where()
	in := gid == g.ID && ch == n.ID
	me := u.user.Profile.UserID
	// The tiles are square.
	tiles := container.NewGridWrap(fyne.NewSize(voiceTile, voiceTile))
	var chosen *proto.VoiceMember
	var mine proto.VoiceMember
	for _, v := range sortedVoice(g, n.Voice) {
		v := v
		if v.User == me {
			mine = v
		}
		if v.User == u.selVoice && v.User != me {
			chosen = &v
		}
		var face fyne.CanvasObject = u.talkRing(g, v.User, 72)
		if v.Camera && in { // Their camera shows for everyone, with no Watch needed.
			face = u.cameraTile(v.User)
		} else if pic := u.voice.Preview(v.User); v.Live && pic != nil { // This shows what they share.
			img := canvas.NewImageFromImage(pic)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(128, 72))
			face = img
		}
		// One line under the name shows LIVE, and muted or deafened.
		marks := container.NewHBox()
		if v.Live {
			marks.Add(container.NewCenter(livePill()))
		}
		switch {
		case v.Deafened || v.GuildDeafened:
			marks.Add(icon(headsetOffIcon, 16))
		case v.Muted || v.GuildMuted:
			marks.Add(icon(micOffIcon, 16))
		}
		tile := container.NewVBox(layout.NewSpacer(), container.New(fixedSize{fyne.NewSize(voiceTile-24, 76)}, container.NewCenter(face)), gap(2),
			container.NewCenter(text(nameOf(g, v.User), 14, colText, true)), container.NewCenter(marks))
		if watched, _ := u.voice.Watching(); v.Live && in && v.User != me && watched != v.User {
			tile.Add(container.NewCenter(widget.NewButton("Watch", func() { u.watchScreen(v.User) })))
		}
		tile.Add(layout.NewSpacer())
		if v.User == me {
			tiles.Add(card(10, tile))
			continue
		}
		// Choosing someone shows their volume and moderation under the people.
		tap := newTapRow(tile, v.User == u.selVoice, func() {
			if u.selVoice == v.User {
				u.selVoice = ""
			} else {
				u.selVoice = v.User
			}
			u.redraw()
		})
		tap.base = colPanel
		tiles.Add(card(4, tap))
	}
	count := fmt.Sprintf("%d %s", len(n.Voice), map[bool]string{true: "person", false: "people"}[len(n.Voice) == 1])
	head := []fyne.CanvasObject{text(count, 13, colDim, false)}
	if n.Locked {
		head = append(head, text("Locked. Nobody new can join.", 13, colDim, false))
	}
	parts := []fyne.CanvasObject{titled(heading(n.Name), head...)}
	if in {
		if view := u.screenView(g); view != nil {
			parts = append(parts, view)
		}
	}
	if len(n.Voice) == 0 {
		parts = append(parts, para("Nobody is here yet."))
	} else {
		parts = append(parts, tiles)
	}
	if chosen != nil {
		parts = append(parts, u.voicePersonPanel(g, n, *chosen))
	}
	if in {
		if note := mutedByModerator(mine); note != "" {
			parts = append(parts, wrapText(note, colDanger))
		}
		if board := u.soundboardFor(g, n); board != nil && u.showBoard {
			parts = append(parts, board)
		}
		if note := u.sharingNote(); note != "" {
			parts = append(parts, hintText(note))
		}
		if name, _ := u.voice.Sharing(); name != "" && u.voice.CameraOn() {
			parts = append(parts, u.cornerPicker())
		}
		if p := u.voice.Problem(); p != "" {
			parts = append(parts, wrapText("Sound device problem: "+sentence(p), colDanger))
		}
	}
	// The buttons are icons at the bottom of the window when you are in the
	// channel; Join Voice otherwise.
	var controls fyne.CanvasObject
	if in {
		controls = u.voiceControls(g, n)
	} else {
		join := []fyne.CanvasObject{primary("Join Voice", func() { u.joinVoice(g, n.ID) })}
		if b := u.lockButton(g, n); b != nil {
			join = append(join, b)
		}
		controls = container.NewCenter(container.NewHBox(join...))
	}
	top := container.NewStack(canvas.NewRectangle(colBG),
		container.New(layout.NewCustomPaddedLayout(14, 14, 24, 24), container.NewHBox(icon(speakerIcon, 18), text(n.Name, 16, colText, true))))
	bottom := container.NewVBox(sideLineH(), container.New(layout.NewCustomPaddedLayout(10, 12, 24, 24), controls))
	body := container.New(layout.NewCustomPaddedLayout(20, 30, 24, 24), narrowTop(760, form(parts...)))
	return container.NewBorder(container.NewVBox(top, sideLineH()), bottom, nil, nil, container.NewVScroll(body))
}

const voiceTile = 176 // The tiles are square.

// muteButton and deafenButton are your own (with words on the voice page,
// icons only in the bar at the bottom).
func (u *UI) muteButton(words bool) *widget.Button {
	m, _ := u.voice.Muted()
	label, ic := "", micIcon
	if words {
		label = "Mute"
	}
	if m {
		ic = micOffIcon
		if words {
			label = "Unmute"
		}
	}
	b := widget.NewButtonWithIcon(label, ic, func() { u.voice.SetMute(!m) })
	if !words {
		b.Importance = widget.LowImportance
	}
	return b
}

func (u *UI) deafenButton(words bool) *widget.Button {
	_, d := u.voice.Muted()
	label, ic := "", headsetIcon
	if words {
		label = "Deafen"
	}
	if d {
		ic = headsetOffIcon
		if words {
			label = "Undeafen"
		}
	}
	b := widget.NewButtonWithIcon(label, ic, func() { u.voice.SetDeafen(!d) })
	if !words {
		b.Importance = widget.LowImportance
	}
	return b
}

// voiceBar shows, above your name, which voice channel you are in.
func (u *UI) voiceBar() fyne.CanvasObject {
	if u.voice == nil {
		return container.NewStack()
	}
	gid, ch := u.voice.Where()
	if gid == "" {
		return container.NewStack()
	}
	g := u.findGuild(gid)
	where := "Voice"
	if g != nil {
		for _, n := range g.Channels {
			if n.ID == ch {
				where = n.Name + " / " + g.Name
			}
		}
	}
	state, colour := "Voice Connected", colOnline
	if u.voice.Problem() != "" {
		state, colour = "Sound Problem", colDanger
	}
	words := stack(2, text(state, 13.5, colour, true), text(where, 12, colDim, false))
	open := newTapRow(words, false, func() {
		if g != nil {
			u.selectGuild(gid)
			u.joinVoice(*g, ch)
		}
	})
	open.base = rgb(0x05, 0x05, 0x07)
	leave := widget.NewButtonWithIcon("", hangUpIcon, func() { go u.voice.Leave() })
	leave.Importance = widget.LowImportance
	bg := canvas.NewRectangle(rgb(0x05, 0x05, 0x07))
	return container.NewStack(bg, container.NewBorder(sideLineH(), nil, nil, nil,
		container.New(layout.NewCustomPaddedLayout(6, 4, 6, 6), container.NewBorder(nil, nil, nil, container.NewCenter(leave), open))))
}

// voiceButtons are mute and deafen, next to the gear by your name.
func (u *UI) voiceButtons() fyne.CanvasObject {
	if u.voice == nil {
		return container.NewHBox()
	}
	return container.NewHBox(u.muteButton(false), u.deafenButton(false))
}

// ---- User Settings: Voice ----------------------------------------------------

// voicePanel is the Voice category of User Settings: devices, the voice
// activity level with a live meter, and Push-To-Talk.
func (u *UI) voicePanel() {
	set := u.voice.Settings()
	save := func() {
		u.voice.Apply(set)
		if err := u.user.SetVoice(set); err != nil {
			u.toast(sentence(err.Error()))
		}
	}
	devices := func(list []sound.Device, chosen string, pick func(string)) *widget.Select {
		names := []string{"Default"}
		ids := []string{""}
		for _, d := range list {
			if d.ID == "" {
				continue
			}
			names = append(names, d.Name)
			ids = append(ids, d.ID)
		}
		s := widget.NewSelect(names, nil)
		s.SetSelectedIndex(0)
		for i, id := range ids {
			if id == chosen {
				s.SetSelectedIndex(i)
			}
		}
		s.OnChanged = func(string) { pick(ids[max(0, s.SelectedIndex())]) }
		return s
	}
	ins, _ := sound.Inputs()
	outs, _ := sound.Outputs()
	input := devices(ins, set.Input, func(id string) { set.Input = id; save() })
	output := devices(outs, set.Output, func(id string) { set.Output = id; save() })

	// For the level, drag it to just below your voice. The bar shows your
	// microphone now, green while it counts as talking.
	level := widget.NewSlider(-80, 0)
	level.Step = 1
	level.SetValue(set.Level())
	levelText := text(fmt.Sprintf("%.0f dB", set.Level()), 12.5, colDim, false)
	level.OnChanged = func(v float64) {
		levelText.Text = fmt.Sprintf("%.0f dB", v)
		levelText.Refresh()
	}
	level.OnChangeEnded = func(v float64) { set.Sensitivity = v; save() }
	meterBG := canvas.NewRectangle(colRaised)
	meterBG.CornerRadius = 3
	meterFill := canvas.NewRectangle(colFaint)
	meterFill.CornerRadius = 3
	meter := container.New(&levelMeter{fill: meterFill}, meterBG, meterFill)
	u.voice.Test(true)
	n := u.settings.n
	go func() {
		for {
			time.Sleep(80 * time.Millisecond)
			stop := false
			fyne.DoAndWait(func() {
				if u.settings == nil || u.settings.cat != "voice" || u.settings.n != n || u.voice == nil {
					stop = true
					return
				}
				db, talking := u.voice.Level()
				m := meter.Layout.(*levelMeter)
				m.frac = float32((db + 80) / 80)
				meterFill.FillColor = colFaint
				if talking {
					meterFill.FillColor = colOnline
				}
				meter.Refresh()
			})
			if stop {
				if u.voice != nil {
					u.voice.Test(false)
				}
				return
			}
		}
	}()

	ptt := toggle(set.PushToTalk, nil)
	keyBtn := widget.NewButton(keyLabel(set.Key()), nil)
	keyBtn.OnTapped = func() {
		keyBtn.SetText("Press Key…")
		u.win.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
			u.win.Canvas().SetOnTypedKey(nil)
			set.PTTKey = string(k.Name)
			keyBtn.SetText(keyLabel(set.Key()))
			save()
		})
	}
	ptt.OnChanged = func(on bool) { set.PushToTalk = on; save() }
	problem := hintText("")
	problem.Hide()
	if p := u.voice.Problem(); p != "" {
		problem = wrapText("Sound device problem: "+sentence(p), colDanger)
	}
	talkRows := []fyne.CanvasObject{
		settingRow("Enable Push-To-Talk", "Send only while you hold a key, instead of whenever you talk.", ptt),
		settingRow("Push-To-Talk Key", "On Windows it works even when Nexus isn't the window in front.", keyBtn)}
	if u.mayAnywhere(proto.PermPrioritySpeaker) { // This is Priority Speaker.
		talkRows = append(talkRows, settingRow("Priority Speaker Key", "Hold it while you talk, and others in the channel are quieter.", u.pushKey(&set.PriorityKey, save)))
	}
	// Soundboard Volume changes only what you hear.
	boardPct := text(fmt.Sprintf("%d%%", set.SoundboardVolume()), 12.5, colDim, false)
	board := widget.NewSlider(0, 100)
	board.Step = 5
	board.SetValue(float64(set.SoundboardVolume()))
	board.OnChanged = func(v float64) { boardPct.Text = fmt.Sprintf("%.0f%%", v); boardPct.Refresh() }
	board.OnChangeEnded = func(v float64) { set.SoundboardCut = 100 - int(v); save() }
	u.openPanel("voice", 600, panelTitle("Voice"),
		container.NewGridWithColumns(2, field("Input Device", input, ""), field("Output Device", output, "")),
		field("Camera", cameraChoice(set.Camera, func(id string) { set.Camera = id; save() }), ""),
		stack(fieldGap, labelRow("Input Sensitivity", levelText), level,
			container.New(layout.NewCustomPaddedLayout(0, 0, boxInset, boxInset), meter),
			hintText("Your microphone sends while you are louder than this. Talk, and set it just below where the bar turns green.")),
		settingList("VOICE PROCESSING",
			settingRow("Echo Cancellation", "Stops others hearing themselves when you use speakers instead of headphones.", toggle(!set.NoEchoCancel, func(on bool) { set.NoEchoCancel = !on; save() })),
			settingRow("Noise Suppression", noiseNote(), u.noiseChoice(&set, save)),
			settingRow("Automatic Gain", "Keeps your voice at an even loudness.", toggle(!set.NoAutoGain, func(on bool) { set.NoAutoGain = !on; save() }))),
		settingList("", talkRows...),
		stack(fieldGap, labelRow("Soundboard Volume", boardPct), board, hintText("How loud soundboard sounds are for you.")),
		settingList("SCREEN SHARING",
			settingRow("Push-To-Blur Key", "Hold it to blur your shared screen, so nothing on it can be made out.", u.pushKey(&set.BlurKey, save)),
			settingRow("Push-To-Mute Key", "Hold it to silence the sound you share. Your microphone keeps working.", u.pushKey(&set.MuteKey, save))),
		problem)
}

// pushKey is a button that sets an optional key (Push-To-Blur, Push-To-Mute),
// with Remove once one is set.
func (u *UI) pushKey(key *string, save func()) fyne.CanvasObject {
	box := container.NewHBox()
	var draw func()
	draw = func() {
		label := "Choose Key"
		if *key != "" {
			label = keyLabel(*key)
		}
		var b *widget.Button
		b = widget.NewButton(label, func() {
			b.SetText("Press Key…")
			u.win.Canvas().SetOnTypedKey(func(k *fyne.KeyEvent) {
				u.win.Canvas().SetOnTypedKey(nil)
				*key = string(k.Name)
				save()
				draw()
			})
		})
		box.Objects = []fyne.CanvasObject{b}
		if *key != "" {
			remove := widget.NewButton("Remove", func() { *key = ""; save(); draw() })
			remove.Importance = widget.LowImportance
			box.Objects = append(box.Objects, remove)
		}
		box.Refresh()
	}
	draw()
	return box
}

// keyLabel is how a key is shown on its button.
func keyLabel(k string) string {
	if k == "`" {
		return "` (Backquote)"
	}
	return k
}

// levelMeter draws the microphone bar: the background, and the fill.
type levelMeter struct {
	fill *canvas.Rectangle
	frac float32
}

func (m *levelMeter) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(10, 8) }

func (m *levelMeter) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	objs[0].Resize(s)
	f := max(0, min(1, m.frac))
	objs[1].Resize(fyne.NewSize(s.Width*f, s.Height))
}

// watchPushToTalk tells the voice engine about the Push-To-Talk key while
// Nexus is the window in front (on Windows the engine also reads it itself).
func (u *UI) watchPushToTalk() {
	dc, ok := u.win.Canvas().(desktop.Canvas)
	if !ok {
		return
	}
	dc.SetOnKeyDown(func(k *fyne.KeyEvent) {
		if u.voice != nil {
			u.voice.SetKeyHeld(string(k.Name), true)
		}
	})
	dc.SetOnKeyUp(func(k *fyne.KeyEvent) {
		if u.voice != nil {
			u.voice.SetKeyHeld(string(k.Name), false)
		}
	})
}

// noiseChoice is Noise Suppression: Off, Standard (SpeexDSP) or Strong
// (RNNoise, when its model is there).
func (u *UI) noiseChoice(set *voice.Settings, save func()) fyne.CanvasObject {
	strong := rnnoise.Default() != nil
	names, levels := []string{"Off", "Standard"}, []string{"off", "standard"}
	if strong {
		names, levels = append(names, "Strong"), append(levels, "strong")
	}
	pick := widget.NewSelect(names, nil)
	now := set.NoiseLevel(strong)
	for i, l := range levels {
		if l == now {
			pick.SetSelectedIndex(i)
		}
	}
	pick.OnChanged = func(string) {
		set.Denoise, set.NoDenoise = levels[max(0, pick.SelectedIndex())], false
		save()
	}
	return container.New(fixedWidth{140}, pick)
}

func noiseNote() string {
	if rnnoise.Default() != nil {
		return "Turns down background noise. Strong is trained on real noise (keyboards, fans, traffic); Standard asks less of the computer."
	}
	return "Turns down steady background noise like fans and hum."
}
