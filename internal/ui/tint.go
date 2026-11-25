package ui

// tint is Nexus's own theme override, for text in another colour or with
// less padding, to save memory.
//
// Fyne's container.NewThemeOverride gives every override, and every redraw
// of one, a new name. Fyne keeps a font cache per name and never frees it,
// so each coloured label loaded the fonts again, the 10 MB emoji font among
// them: memory grew with every page. Our variants only change colours and
// spacing, never fonts, so a tint uses the app's own name ("") and shares
// the one font cache. It is applied the way Fyne applies its own: when made,
// when drawn, and when refreshed.

import (
	_ "unsafe" // This import is needed for go:linkname.

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// fyneScope is laid out exactly like Fyne's internal overrideScope.
type fyneScope struct {
	th      fyne.Theme
	cacheID string
}

// fyneOverrideTheme is Fyne's internal overrideTheme: it gives an object,
// and everything inside it, a theme scope.
//
//go:linkname fyneOverrideTheme fyne.io/fyne/v2/internal/cache.overrideTheme
func fyneOverrideTheme(o fyne.CanvasObject, s *fyneScope)

type tint struct {
	widget.BaseWidget
	content fyne.CanvasObject
	scope   *fyneScope
}

// newTint shows obj with theme th (which must use the app's fonts).
func newTint(obj fyne.CanvasObject, th fyne.Theme) *tint {
	t := &tint{content: obj, scope: &fyneScope{th: th}} // The cacheID "" means the app's font cache.
	t.ExtendBaseWidget(t)
	fyneOverrideTheme(obj, t.scope)
	obj.Refresh() // It was drawn before with the app's theme.
	return t
}

func (t *tint) CreateRenderer() fyne.WidgetRenderer {
	fyneOverrideTheme(t.content, t.scope)
	return widget.NewSimpleRenderer(container.NewStack(t.content))
}

func (t *tint) Refresh() {
	fyneOverrideTheme(t.content, t.scope) // Parts made since then get it too.
	t.content.Refresh()
	t.BaseWidget.Refresh()
}
