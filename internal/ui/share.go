package ui

// This file shows screen sharing in the window: the Share Screen
// page (a screen or a window, the quality, Share Sound), LIVE markers, and
// the screen you watch, shown above the people in the voice channel.

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/capture"
	"nexus/internal/guildconn"
	"nexus/internal/voice"
)

var screenIcon = svg("screen.svg", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g `+iconStroke+`><rect x="3" y="4.5" width="18" height="12" rx="2"/><path d="M9 20h6M12 16.5V20"/></g></svg>`)

// livePill is the red LIVE mark of someone sharing their screen.
func livePill() fyne.CanvasObject {
	bg := canvas.NewRectangle(colDanger)
	bg.CornerRadius = 4
	return container.NewStack(bg, container.New(layout.NewCustomPaddedLayout(1, 1, 5, 5), text("LIVE", 10.5, rgb(255, 255, 255), true)))
}

// watchScreen starts watching someone's shared screen.
func (u *UI) watchScreen(user string) {
	go func() {
		err := u.voice.Watch(user)
		fyne.Do(func() {
			if err != nil {
				u.toast(sentence(err.Error()))
			}
			u.redraw()
		})
	}()
}

// screenView is the screen you watch, kept fresh 30 times a second while
// the voice page shows it (nil when you watch nothing).
func (u *UI) screenView(g guildconn.GuildView) fyne.CanvasObject {
	from, _ := u.voice.Watching()
	if from == "" {
		return nil
	}
	var first image.Image = blankFrame()
	if _, f := u.voice.Watching(); f != nil {
		first = f // The page was redrawn, so keep showing the picture.
	}
	img := canvas.NewImageFromImage(first)
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScaleFastest
	img.SetMinSize(fyne.NewSize(720, 405))
	waiting := centred(text("Waiting for the first picture…", 13, colDim, false))
	if _, f := u.voice.Watching(); f != nil {
		waiting.Hide()
	}
	screen := container.NewStack(canvas.NewRectangle(rgb(0, 0, 0)), img, container.NewCenter(waiting))
	stop := widget.NewButton("Stop Watching", func() { u.watchScreen("") })
	head := container.NewBorder(nil, nil, container.NewHBox(icon(screenIcon, 18), container.NewCenter(text(nameOf(g, from)+"'s Screen", 15, colText, true)), container.NewCenter(livePill())), stop)
	go func() {
		var last image.Image
		for {
			time.Sleep(time.Second / 30)
			alive := true
			fyne.DoAndWait(func() {
				who, frame := "", image.Image(nil)
				if u.voice != nil {
					who, frame = u.voice.Watching()
				}
				if who != from || img.Hidden || u.page != "voice" || fyne.CurrentApp().Driver().CanvasForObject(img) == nil {
					alive = false
					return
				}
				if frame != nil && frame != last {
					last = frame
					img.Image = frame
					img.Refresh()
					waiting.Hide()
				}
			})
			if !alive {
				return
			}
		}
	}()
	return stack(fieldGap, head, outlined(screen))
}

func blankFrame() image.Image {
	m := image.NewRGBA(image.Rect(0, 0, 16, 9))
	for i := range m.Pix {
		m.Pix[i] = 0
		if i%4 == 3 {
			m.Pix[i] = 255
		}
	}
	return m
}

// ---- the Share Screen page ---------------------------------------------------------------

func (u *UI) sharePage(g guildconn.GuildView) fyne.CanvasObject {
	channel := u.pageArg
	back := func() { u.openPageArg("voice", channel) }
	targets, err := capture.Targets()
	if err != nil {
		return pageFrame(screenIcon, "Share Screen", backButton(back), para(sentence(err.Error())))
	}
	var screens, windows []capture.Target
	for _, t := range targets {
		if t.Screen {
			screens = append(screens, t)
		} else {
			windows = append(windows, t)
		}
	}
	chosen := -1
	var list []capture.Target
	grid := container.NewGridWrap(fyne.NewSize(220, 170))
	var cards []*tapRow
	var goLive *widget.Button
	fill := func(tab int) {
		list = screens
		if tab == 1 {
			list = windows
		}
		chosen = -1
		goLive.Disable()
		grid.RemoveAll()
		cards = nil
		if len(list) == 0 {
			grid.Add(para("Nothing to share here."))
		}
		for i, t := range list {
			i, t := i, t
			thumb := canvas.NewImageFromImage(blankFrame())
			thumb.FillMode = canvas.ImageFillContain
			thumb.SetMinSize(fyne.NewSize(192, 108))
			go func() { // This makes a small picture of it.
				if p, err := capture.Grab(t); err == nil {
					small := thumbnail(p, 384)
					fyne.Do(func() { thumb.Image = small; thumb.Refresh() })
				}
			}()
			label := widget.NewLabel(t.Name)
			label.Truncation = fyne.TextTruncateEllipsis
			label.Alignment = fyne.TextAlignCenter
			var card *tapRow
			card = newTapRow(container.NewVBox(container.NewStack(canvas.NewRectangle(rgb(0, 0, 0)), thumb), label), false, func() {
				chosen = i
				for _, c := range cards {
					c.active = c == card
					c.rest()
					c.bg.Refresh()
				}
				goLive.Enable()
			})
			card.base = colPanel
			cards = append(cards, card)
			grid.Add(card)
		}
		grid.Refresh()
	}
	quality := widget.NewSelect([]string{voice.Qualities[0].Name, voice.Qualities[1].Name}, nil)
	quality.SetSelectedIndex(0)
	sound, soundBox := option("Share Sound", "Viewers also hear what your computer plays (not the call itself).", true)
	problem := newErrorText()
	goLive = primary("Go Live", func() {
		if chosen < 0 {
			return
		}
		t, q, withSound := list[chosen], voice.Qualities[max(0, quality.SelectedIndex())], sound.Checked
		goLive.Disable()
		goLive.SetText("Starting…")
		go func() {
			err := u.voice.StartSharing(t, q, withSound)
			fyne.Do(func() {
				goLive.Enable()
				goLive.SetText("Go Live")
				if err != nil {
					problem.Set(err)
					return
				}
				back()
			})
		}()
	})
	goLive.Disable()
	tabs := tabBar(nil, []string{"Screens", "Windows"}, 0, fill)
	fill(0)
	return pageFrame(screenIcon, "Share Screen", backButton(back), form(
		titled(panelTitle("Share Screen"), para("Choose a whole screen or one window. People in the voice channel choose whether to watch.")),
		tabs, grid,
		container.NewGridWithColumns(2, field("Quality", quality, "Both at 30 frames a second. 1080p needs a faster computer."), layout.NewSpacer()),
		soundBox, problem.box,
		buttonsRow(widget.NewButton("Cancel", back), goLive)))
}

// thumbnail shrinks a capture to at most w pixels wide (nearest pixel).
func thumbnail(p *capture.Picture, w int) image.Image {
	if p.W < w {
		w = p.W
	}
	h := p.H * w / max(1, p.W)
	out := image.NewRGBA(image.Rect(0, 0, w, max(1, h)))
	for y := 0; y < h; y++ {
		sy := y * p.H / h
		for x := 0; x < w; x++ {
			i := (sy*p.W + x*p.W/w) * 4
			out.SetRGBA(x, y, color.RGBA{p.Pix[i+2], p.Pix[i+1], p.Pix[i], 255})
		}
	}
	return out
}

// sharingNote is the line about your own sharing on the voice page.
func (u *UI) sharingNote() string {
	name, size := u.voice.Sharing()
	if name == "" {
		return ""
	}
	if name == "Your Screen" {
		name = "your screen"
	}
	note := fmt.Sprintf("You are sharing %s at %d×%d.", name, size.X, size.Y)
	switch blurred, muted := u.voice.SharingHeld(); {
	case blurred && muted:
		note += " Blurred and muted now."
	case blurred:
		note += " Blurred now."
	case muted:
		note += " Its sound is muted now."
	}
	return note
}
