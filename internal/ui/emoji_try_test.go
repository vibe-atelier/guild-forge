package ui

import (
	"image/png"
	"os"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
)

// TestEmojiFont draws old and new emoji with Noto Color Emoji, when the
// font is in the project folder (EMOJI_SHOT names a picture to save).
func TestEmojiFont(t *testing.T) {
	if _, err := os.Stat("../../" + emojiFontFile); err != nil {
		t.Skip("no emoji font here")
	}
	wd, _ := os.Getwd()
	os.Chdir("../..")
	useEmojiFont()
	os.Chdir(wd)
	if fyneEmojiFont == nil || fyneEmojiFont.StaticName != emojiFontFile {
		t.Fatal("the emoji font should be in use")
	}
	if out := os.Getenv("EMOJI_SHOT"); out != "" {
		test.NewApp()
		appTheme = newTheme()
		w := test.NewWindow(container.NewVBox(text("Old 😀 🎉 👍  New 🥹 🫠 🫶 🪿", 32, colText, false), text("Small 😀 🥹 🪿 in a line of text.", 14, colText, false)))
		w.Resize(fyne.NewSize(640, 110))
		f, _ := os.Create(out)
		png.Encode(f, w.Canvas().Capture())
		f.Close()
	}
}
