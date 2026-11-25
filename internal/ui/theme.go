package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// The AMOLED black colours, with stronger contrast: panels,
// lines and dimmed text are lighter than before, so every area and every
// word stands out against the black.
var (
	colBG         = rgb(0x00, 0x00, 0x00)
	colPanel      = rgb(0x12, 0x12, 0x17)
	colRaised     = rgb(0x1e, 0x1e, 0x25)
	colHover      = rgb(0x28, 0x28, 0x31)
	colLine       = color.NRGBA{255, 255, 255, 34}
	colLineStrong = color.NRGBA{255, 255, 255, 64}
	colText       = rgb(0xf7, 0xf7, 0xf9)
	colDim        = rgb(0xb8, 0xb8, 0xc2)
	colFaint      = rgb(0x8e, 0x8e, 0x9a)
	// The colours are blood red: deep for buttons, lighter for links so they read on
	// black. Dangerous actions are scarlet, so they still stand apart.
	colAccent     = rgb(0xb3, 0x0d, 0x1e)
	colAccentLink = rgb(0xf0, 0x4a, 0x55)
	colDanger     = rgb(0xff, 0x5a, 0x36)
	colOnline     = rgb(0x22, 0xc5, 0x5e)
	colIdle       = rgb(0xf5, 0xa5, 0x24)
	colYellow     = rgb(0xfa, 0xcc, 0x15) // It is Idle (traffic lights).
	colRed        = rgb(0xef, 0x44, 0x44) // It is Do Not Disturb.
	colMenu       = rgb(0x1a, 0x1a, 0x21)
	colAmberSoft  = color.NRGBA{245, 165, 36, 16}
)

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }

// nexusTheme is the app's look: always dark, whatever Windows is set to.
type nexusTheme struct {
	regular, bold, italic, boldItalic, mono fyne.Resource
}

func newTheme() *nexusTheme {
	t := &nexusTheme{}
	// On Windows, use Windows' own font (Segoe UI), as the HTML app did.
	// Elsewhere, Fyne's built-in font is used.
	if runtime.GOOS == "windows" {
		dir := filepath.Join(os.Getenv("WINDIR"), "Fonts")
		t.regular = fontFile(dir, "segoeui.ttf")
		t.bold = fontFile(dir, "seguisb.ttf") // Semibold reads better than bold.
		t.italic = fontFile(dir, "segoeuii.ttf")
		t.boldItalic = fontFile(dir, "seguisbi.ttf")
		t.mono = fontFile(dir, "consola.ttf")
	}
	return t
}

func fontFile(dir, name string) fyne.Resource {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil
	}
	return fyne.NewStaticResource(name, data)
}

func (t *nexusTheme) Color(n fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch n {
	case theme.ColorNameBackground:
		return colBG
	case theme.ColorNameForeground:
		return colText
	case theme.ColorNameButton:
		return colRaised
	case theme.ColorNameDisabledButton:
		return colPanel
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return colFaint
	case theme.ColorNameHover:
		return color.NRGBA{255, 255, 255, 14}
	case theme.ColorNamePressed:
		return color.NRGBA{255, 255, 255, 24}
	case theme.ColorNameInputBackground, theme.ColorNameHeaderBackground:
		return colPanel
	case theme.ColorNameInputBorder:
		return colLineStrong
	case theme.ColorNamePrimary:
		return colAccent
	case theme.ColorNameHyperlink:
		return colAccentLink
	case theme.ColorNameForegroundOnPrimary:
		return rgb(0xff, 0xff, 0xff)
	case theme.ColorNameFocus:
		return color.NRGBA{240, 74, 85, 170}
	case theme.ColorNameSelection:
		return color.NRGBA{179, 13, 30, 90}
	case theme.ColorNameScrollBar:
		return color.NRGBA{255, 255, 255, 40}
	case theme.ColorNameSeparator:
		return colLine
	case theme.ColorNameShadow:
		return color.NRGBA{0, 0, 0, 160}
	case theme.ColorNameOverlayBackground, theme.ColorNameMenuBackground:
		return colMenu
	case theme.ColorNameError:
		return colDanger
	case theme.ColorNameSuccess:
		return colOnline
	case theme.ColorNameWarning:
		return colIdle
	}
	return theme.DefaultTheme().Color(n, theme.VariantDark)
}

func (t *nexusTheme) Font(s fyne.TextStyle) fyne.Resource {
	var f fyne.Resource
	switch {
	case s.Monospace:
		f = t.mono
	case s.Bold && s.Italic:
		f = t.boldItalic
	case s.Bold:
		f = t.bold
	case s.Italic:
		f = t.italic
	default:
		f = t.regular
	}
	if f == nil {
		return theme.DefaultTheme().Font(s)
	}
	return f
}

func (t *nexusTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }

func (t *nexusTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText:
		return 14.5
	case theme.SizeNameHeadingText:
		return 24
	case theme.SizeNameSubHeadingText:
		return 18
	case theme.SizeNameInputRadius:
		return 12
	case theme.SizeNameSelectionRadius:
		return 8
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNamePopupRadius:
		return 20
	}
	return theme.DefaultTheme().Size(n)
}

// colorTheme is the app's theme with a different text colour, for dimmed
// text inside widgets (used with newTint).
type colorTheme struct {
	fyne.Theme
	text color.Color
}

func (t colorTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameForeground {
		return t.text
	}
	return t.Theme.Color(n, v)
}

// tightTheme is colorTheme without the space Fyne puts around a label's
// text, so wrapped text lines up with headings and inputs.
type tightTheme struct {
	fyne.Theme
	text color.Color
}

func (t tightTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if n == theme.ColorNameForeground {
		return t.text
	}
	return t.Theme.Color(n, v)
}

func (t tightTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNameInnerPadding {
		return 0
	}
	return t.Theme.Size(n)
}

// compactTheme draws buttons with less room around their icons, for the
// row of four by your name, so your name has space beside them.
type compactTheme struct{ fyne.Theme }

func (t compactTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameInnerPadding:
		return 5
	case theme.SizeNamePadding:
		return 1
	}
	return t.Theme.Size(n)
}
