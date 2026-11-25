package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"nexus/internal/user"
)

// TestFitText checks that a name that fits is whole; a long one ends in "…" and
// stays inside its space.
func TestFitText(t *testing.T) {
	test.NewApp()
	appTheme = newTheme()
	short := fitText("Alice", true, colText).(*fyne.Container)
	short.Resize(fyne.NewSize(200, 20))
	if got := short.Objects[0].(*canvas.Text).Text; got != "Alice" {
		t.Fatalf("short name: %q", got)
	}
	long := fitText("Alexandria Konstantinopoulou-Wellington", true, colText).(*fyne.Container)
	long.Resize(fyne.NewSize(120, 20))
	txt := long.Objects[0].(*canvas.Text)
	if !strings.HasSuffix(txt.Text, "…") || fyne.MeasureText(txt.Text, txt.TextSize, txt.TextStyle).Width > 120 {
		t.Fatalf("long name: %q", txt.Text)
	}
}

func TestMeBarName(t *testing.T) {
	test.NewApp()
	appTheme = newTheme()
	u := &UI{user: &user.User{Profile: user.Profile{DefaultName: "Alice", UserID: "ABC"}}, home: &home{meVoice: container.NewStack(widget.NewButtonWithIcon("", micIcon, nil), widget.NewButtonWithIcon("", headsetIcon, nil))}}
	bar := u.meBar()
	w := test.NewWindow(bar)
	w.Resize(fyne.NewSize(264, 60))
	var find func(o fyne.CanvasObject)
	find = func(o fyne.CanvasObject) {
		if c, ok := o.(*fyne.Container); ok {
			if _, ok := c.Layout.(*fitLayout); ok {
				t.Logf("name box %v, text %q", c.Size(), c.Objects[0].(*canvas.Text).Text)
				if c.Objects[0].(*canvas.Text).Text != "Alice" {
					t.Errorf("a short name should fit beside the buttons")
				}
			}
			for _, k := range c.Objects {
				find(k)
			}
		}
		if wd, ok := o.(fyne.Widget); ok {
			r := test.WidgetRenderer(wd)
			for _, k := range r.Objects() {
				find(k)
			}
		}
	}
	find(bar)
}
