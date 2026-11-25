package guildconn

// This file searches private chats. Private chats are kept on this computer,
// sealed with your password, so search reads only what is already here: it
// asks no guild and stores nothing. Guild channels have no search (pinned
// messages instead).

import (
	"strings"
	"unicode"
)

// searchWords splits a search into lower-case words.
func searchWords(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return unicode.IsSpace(r) || r == '|' // A "|" is a split point too, because "||" marks spoilers.
	})
}

// searchText is the words a message can be found by: its text, a poll's
// question and choices, and a file's name.
func searchText(v DMView) string {
	b := ParseBody(v.Text)
	parts := []string{b.Text, v.File}
	if b.Poll != nil {
		parts = append(parts, b.Poll.Question)
		parts = append(parts, b.Poll.Choices...)
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

// MatchDM reports whether a message holds every word of the search, in any
// order, ignoring capitals. Deleted messages and empty searches match nothing.
func MatchDM(v DMView, query string) bool {
	words := searchWords(query)
	if v.Deleted || len(words) == 0 {
		return false
	}
	text := searchText(v)
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// SearchDMs returns the messages that match, oldest first.
func SearchDMs(msgs []DMView, query string) []DMView {
	var out []DMView
	for _, m := range msgs {
		if MatchDM(m, query) {
			out = append(out, m)
		}
	}
	return out
}
