package ui

import (
	"net/url"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var mdImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)

var bareLink = regexp.MustCompile(`(^|[\s(])(https?://[^\s<>()]+[^\s<>().,!?;:])`)

// linkify wraps bare web addresses in <…> so the Markdown reader links them.
func linkify(s string) string {
	s = bareLink.ReplaceAllString(s, "$1<$2>")
	return postLink.ReplaceAllString(s, "$1[Thread Post]($2)")
}

// postLink is a Threads post pasted into chat.
var postLink = regexp.MustCompile(`(^|[\s(])(nexus-post:[0-9a-zA-Z_-]+/[0-9a-zA-Z_-]+)`)

// markdown shows Markdown text with Fyne's reader. Links open in the
// normal web browser, and only http, https and mailto links work.
func (u *UI) markdown(s string) *widget.RichText {
	// Pictures only come from this computer (guild pictures, saved first):
	// never from other websites, which would see your address.
	s = mdImage.ReplaceAllStringFunc(s, func(m string) string {
		parts := mdImage.FindStringSubmatch(m)
		if strings.HasPrefix(parts[2], "file:") {
			return m
		}
		alt := parts[1]
		if alt == "" {
			alt = "picture"
		}
		return "*[" + alt + "]*"
	})
	rt := widget.NewRichTextFromMarkdown(linkify(s))
	rt.Wrapping = fyne.TextWrapWord
	for _, seg := range rt.Segments {
		if h, ok := seg.(*widget.HyperlinkSegment); ok {
			link := h.URL
			h.OnTapped = func() { u.openLink(link) }
		}
	}
	return rt
}

// openLink opens a safe web link outside Nexus.
func (u *UI) openLink(link *url.URL) {
	if link == nil {
		return
	}
	switch strings.ToLower(link.Scheme) {
	case "nexus-wiki", "nexus-channel", "nexus-post", "nexus-user":
		u.openInside(link)
	case "http", "https", "mailto":
		if err := u.app.OpenURL(link); err != nil {
			u.toast("Could not open the link.")
		}
	default:
		u.toast("Only web and email links can be opened.")
	}
}

// markdownFlush is Markdown text without Fyne's inner padding, so it lines
// up with the headings and buttons around it.
func (u *UI) markdownFlush(s string) fyne.CanvasObject {
	// Taking the padding away in the theme breaks links inside a line, so the
	// text is moved left by that padding instead.
	return flushMarkdown(u.markdown(s))
}

// flushMarkdown moves Markdown text left by its inner padding.
func flushMarkdown(o fyne.CanvasObject) fyne.CanvasObject {
	p := appTheme.Size(theme.SizeNameInnerPadding)
	return container.New(layout.NewCustomPaddedLayout(-p/2, -p/2, -p, 0), o)
}
