package ui

// This file has voice moderation in the window: choosing someone on a voice page shows their
// volume and, for moderators, Mute Member, Deafen Member, Move
// Member and Disconnect Member; Lock Channel; the
// soundboard under the people and its settings page.

import (
	"fmt"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/guildconn"
	"nexus/internal/imaging"
	"nexus/internal/proto"
	"nexus/internal/voice"
)

var lockedVoiceIcon = svg("voice-locked.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><rect x="5" y="10.5" width="14" height="9.5" rx="2"/><path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/></g></svg>`)

// outranksIn reports whether I may act on this member. The guild checks again.
func outranksIn(g guildconn.GuildView, key string) bool {
	m := memberOf(g, key)
	if m == nil || m.IsOwner || m.IsMe {
		return false
	}
	return myRank(g) > rankOf(g, m.Roles)
}

// voiceDo runs a moderator's action and draws the page again.
func (u *UI) voiceDo(g guildconn.GuildView, act func(gc *guildconn.Conn) error) {
	gc, err := u.guilds.Get(g.ID)
	if err != nil {
		u.toast(sentence(err.Error()))
		return
	}
	go func() {
		err := act(gc)
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
			}
			u.redraw()
		})
	}()
}

// voicePersonPanel is shown under the people when one is chosen: their
// volume, and what a moderator may do to them.
func (u *UI) voicePersonPanel(g guildconn.GuildView, n *guildconn.ChannelView, v proto.VoiceMember) fyne.CanvasObject {
	name := nameOf(g, v.User)
	set := u.voice.Settings()
	pct := text(fmt.Sprintf("%d%%", set.VolumeOf(v.User)), 12.5, colDim, false)
	vol := widget.NewSlider(0, 200)
	vol.Step = 5
	vol.SetValue(float64(set.VolumeOf(v.User)))
	vol.OnChanged = func(x float64) { pct.Text = fmt.Sprintf("%.0f%%", x); pct.Refresh() }
	vol.OnChangeEnded = func(x float64) {
		s := u.voice.Settings().WithVolume(v.User, int(x))
		u.voice.Apply(s)
		if err := u.user.SetVoice(s); err != nil {
			u.toast(sentence(err.Error()))
		}
	}
	parts := []fyne.CanvasObject{text(name, 16, colText, true),
		stack(fieldGap, labelRow("Volume", pct), vol, hintText("Only for you."))}

	var buttons []fyne.CanvasObject
	mod := outranksIn(g, v.User)
	if mod && canIn(g, n.ID, proto.PermMuteMembers) {
		label := "Mute Member"
		if v.GuildMuted {
			label = "Unmute Member"
		}
		on := !v.GuildMuted
		buttons = append(buttons, widget.NewButton(label, func() {
			u.voiceDo(g, func(gc *guildconn.Conn) error { return gc.VoiceMute(v.User, on) })
		}))
	}
	if mod && canIn(g, n.ID, proto.PermDeafenMembers) {
		label := "Deafen Member"
		if v.GuildDeafened {
			label = "Undeafen Member"
		}
		on := !v.GuildDeafened
		buttons = append(buttons, widget.NewButton(label, func() {
			u.voiceDo(g, func(gc *guildconn.Conn) error { return gc.VoiceDeafen(v.User, on) })
		}))
	}
	if mod && canIn(g, n.ID, proto.PermDisconnectMembers) {
		b := widget.NewButton("Disconnect Member", func() {
			u.selVoice = ""
			u.voiceDo(g, func(gc *guildconn.Conn) error { return gc.VoiceDisconnect(v.User) })
		})
		b.Importance = widget.DangerImportance
		buttons = append(buttons, b)
	}
	if len(buttons) > 0 {
		parts = append(parts, buttonsRow(buttons...))
	}
	if mod && canIn(g, n.ID, proto.PermMoveMembers) {
		var names, ids []string
		for _, c := range g.Channels {
			if c.Kind == proto.KindVoice && c.ID != n.ID && canIn(g, c.ID, proto.PermMoveMembers) {
				names, ids = append(names, c.Name), append(ids, c.ID)
			}
		}
		if len(ids) > 0 {
			to := widget.NewSelect(names, nil)
			to.SetSelectedIndex(0)
			move := widget.NewButton("Move Member", func() {
				dest := ids[max(0, to.SelectedIndex())]
				u.selVoice = ""
				u.voiceDo(g, func(gc *guildconn.Conn) error { return gc.VoiceMove(v.User, dest) })
			})
			parts = append(parts, field("Move To", container.NewBorder(nil, nil, nil, move, to), ""))
		}
	}
	return card(16, form(parts...))
}

// lockButton locks or unlocks a voice channel (Lock Channel).
func (u *UI) lockButton(g guildconn.GuildView, n *guildconn.ChannelView) fyne.CanvasObject {
	if !canIn(g, n.ID, proto.PermLockChannel) {
		return nil
	}
	label := "Lock Channel"
	if n.Locked {
		label = "Unlock Channel"
	}
	on := !n.Locked
	return widget.NewButton(label, func() {
		u.voiceDo(g, func(gc *guildconn.Conn) error { return gc.VoiceLock(n.ID, on) })
	})
}

// mutedByModerator says so when a moderator muted or deafened you.
func mutedByModerator(v proto.VoiceMember) string {
	switch {
	case v.GuildMuted && v.GuildDeafened:
		return "A moderator muted and deafened you."
	case v.GuildMuted:
		return "A moderator muted you."
	case v.GuildDeafened:
		return "A moderator deafened you."
	}
	return ""
}

// soundboardFor is the soundboard under the people: square
// pictures, each name showing on hover; a click plays a sound to
// everyone in the channel. It is nil when there is nothing to play.
func (u *UI) soundboardFor(g guildconn.GuildView, n *guildconn.ChannelView) fyne.CanvasObject {
	if !canIn(g, n.ID, proto.PermUseSoundboard) {
		return nil
	}
	gc, err := u.guilds.Get(g.ID)
	if err != nil {
		return nil
	}
	sounds := gc.Sounds()
	if len(sounds) == 0 {
		return nil
	}
	grid := container.NewGridWrap(fyne.NewSize(soundTileSize, soundTileSize))
	for _, s := range sounds {
		s := s
		grid.Add(newSoundTile(u, s.Name, u.soundPicture(g.ID, s.Picture), func() {
			go func() {
				if err := u.voice.PlaySound(s.ID); err != nil {
					fyne.Do(func() { u.toast(sentence(err.Error())) })
				}
			}()
		}))
	}
	return stack(fieldGap, text("SOUNDBOARD", 11.5, colFaint, true), grid)
}

// soundPicture is a sound button's picture (nil when it has none, or while
// it is fetched; the page is drawn again when it arrives).
func (u *UI) soundPicture(gid, hash string) fyne.Resource {
	if hash == "" {
		return nil
	}
	if u.emojiCache == nil {
		u.emojiCache = map[string]fyne.Resource{}
	}
	if r, ok := u.emojiCache["sound-"+hash]; ok {
		return r
	}
	path := u.pictureFile(gid, hash)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	r := fyne.NewStaticResource("sound-"+hash, data)
	u.emojiCache["sound-"+hash] = r
	return r
}

// ---- Server Settings: Soundboard -----------------------------------------------------------

// openSoundboard lists the guild's sounds, with a form to add one: a name,
// a picture if wanted, then the sound file.
func (u *UI) openSoundboard(g guildconn.GuildView) {
	gc, err := u.guilds.Get(g.ID)
	if err != nil {
		u.toast(sentence(err.Error()))
		return
	}
	list := stack(0)
	problem := newErrorText()
	count := text("", 13, colDim, false)
	var fill func()
	fill = func() {
		list.RemoveAll()
		sounds := gc.Sounds()
		count.Text = fmt.Sprintf("%d of %d", len(sounds), proto.MaxSounds)
		count.Refresh()
		if len(sounds) == 0 {
			list.Add(para("No sounds yet."))
		}
		for i, s := range sounds {
			s := s
			del := widget.NewButtonWithIcon("", xIcon, func() {
				problem.Run(func() error { return gc.DeleteSound(s.ID) }, fill)
			})
			del.Importance = widget.LowImportance
			if i > 0 {
				list.Add(sideLineH())
			}
			row := settingRow(s.Name, fmt.Sprintf("%.1f seconds.", float64(s.Millis)/1000), del)
			if res := u.soundPicture(g.ID, s.Picture); res != nil {
				row = container.NewBorder(nil, nil, container.NewCenter(icon(res, 32)), nil, row)
			}
			list.Add(row)
		}
		list.Refresh()
	}
	fill()
	name := widget.NewEntry()
	name.SetPlaceHolder("Air Horn")

	// The picture is optional, and made small like custom emoji.
	var picture []byte
	picBox := container.NewHBox()
	var drawPic func()
	drawPic = func() {
		pick := widget.NewButton("Choose Picture", func() {
			problem.Set(nil)
			go func() {
				path, err := choosePicture("Choose a picture for the sound")
				var data []byte
				if err == nil && path != "" {
					if data, err = os.ReadFile(path); err == nil {
						data, err = imaging.FitPicture(data, 128, 250*1024)
					}
				}
				fyne.Do(func() {
					if err != nil {
						problem.Set(err)
						return
					}
					if data != nil {
						picture = data
						drawPic()
					}
				})
			}()
		})
		picBox.Objects = []fyne.CanvasObject{pick}
		if picture != nil {
			remove := widget.NewButton("Remove", func() { picture = nil; drawPic() })
			remove.Importance = widget.LowImportance
			picBox.Objects = []fyne.CanvasObject{container.NewCenter(icon(fyne.NewStaticResource("new-sound-picture", picture), 32)), pick, remove}
		}
		picBox.Refresh()
	}
	drawPic()

	var add *widget.Button
	add = primary("Choose File", func() {
		problem.Set(nil)
		n := strings.TrimSpace(name.Text)
		if n == "" {
			problem.Set(fmt.Errorf("Give the sound a name first."))
			return
		}
		pic := picture
		add.Disable()
		go func() {
			path, err := chooseSound("Choose a WAV or Ogg Opus file")
			if err == nil && path != "" {
				var data []byte
				if data, err = os.ReadFile(path); err == nil {
					var packed []byte
					if packed, _, err = voice.LoadSound(data); err == nil {
						err = gc.PutSound(n, pic, packed)
					}
				}
			}
			fyne.Do(func() {
				add.Enable()
				if err != nil {
					problem.Set(err)
					return
				}
				if path == "" {
					return
				}
				name.SetText("")
				picture = nil
				drawPic()
				fill()
				u.toast("Sound added")
			})
		}()
	})
	var p *popup
	p = u.openPanel("soundboard", 600, titled(panelTitle("Soundboard"), count),
		hintText("Up to 25 sounds, each up to 10 seconds, from WAV or Ogg Opus files. Members with Use Soundboard play them in voice channels."),
		list,
		field("Name", name, ""),
		field("Picture", picBox, "Shown on the sound's button. Leave it out for the name alone."),
		problem.box, buttonsRow(add, widget.NewButton("Close", func() { p.Close() })))
}

// mayAnywhere reports whether I have perm in any guild, anywhere in it.
func (u *UI) mayAnywhere(perm string) bool {
	for _, g := range u.guilds.Views() {
		if g.IsOwner {
			return true
		}
		for _, list := range g.MyPerms {
			if has(list, perm) {
				return true
			}
		}
	}
	return false
}
