package ui

// Cameras in the window: Start Camera on the
// voice page, cameras on the tiles (kept moving while the page shows them),
// the corner for your camera while sharing, and the Camera choice in User
// Settings, Voice.

import (
	"image"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/camera"
	"nexus/internal/voice"
)

var cornerNames = []string{"Top Left", "Top Right", "Bottom Left", "Bottom Right"}

// cameraTile is someone's camera on their tile, kept fresh 30 times a
// second while it is on the screen.
func (u *UI) cameraTile(user string) fyne.CanvasObject {
	me := u.user.Profile.UserID
	var first image.Image = blankFrame()
	if f := u.voice.CameraImage(user, me); f != nil {
		first = f
	}
	img := canvas.NewImageFromImage(first)
	img.FillMode = canvas.ImageFillContain
	img.ScaleMode = canvas.ImageScaleFastest
	img.SetMinSize(fyne.NewSize(101, 76)) // This fits a square tile.
	go func() {
		var last image.Image
		for {
			time.Sleep(time.Second / 30)
			alive := true
			fyne.DoAndWait(func() {
				if u.voice == nil || u.page != "voice" || fyne.CurrentApp().Driver().CanvasForObject(img) == nil {
					alive = false
					return
				}
				f := u.voice.CameraImage(user, me)
				if f != nil && (user == me || f != last) {
					last = f
					img.Image = f
					img.Refresh()
				}
			})
			if !alive {
				return
			}
		}
	}()
	return img
}

// cornerPicker chooses where your camera goes on your shared screen;
// it can change while you are live.
func (u *UI) cornerPicker() fyne.CanvasObject {
	set := u.voice.Settings()
	pick := widget.NewSelect(cornerNames, nil)
	pick.SetSelectedIndex(3)
	for i, c := range voice.Corners {
		if c == set.CameraCorner {
			pick.SetSelectedIndex(i)
		}
	}
	pick.OnChanged = func(string) {
		s := u.voice.Settings()
		s.CameraCorner = voice.Corners[max(0, pick.SelectedIndex())]
		u.voice.Apply(s)
		if err := u.user.SetVoice(s); err != nil {
			u.toast(sentence(err.Error()))
		}
	}
	return field("Camera Corner", pick, "Where your camera shows on the screen you share.")
}

// cameraChoice is the Camera choice in User Settings, Voice.
func cameraChoice(chosen string, pick func(string)) *widget.Select {
	list, _ := camera.Devices()
	names, ids := []string{"Default"}, []string{""}
	for _, d := range list {
		names, ids = append(names, d.Name), append(ids, d.ID)
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
