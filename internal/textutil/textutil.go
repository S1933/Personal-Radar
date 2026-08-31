// Package textutil holds small string helpers shared across the radar
// pipeline. Functions here are deliberately allocation-light and free of
// external dependencies so they can be imported from any internal
// package without risk of an import cycle.
package textutil

import "strings"

// Truncate returns at most n runes of s, appending suffix when the
// string was shortened.
//
// Byte slicing (s[:n]) splits multi-byte runes — routine on French
// content, where a single "é" occupies two bytes — and yields invalid
// UTF-8 that renders as U+FFFD in Telegram and in the dashboard.
func Truncate(s string, n int, suffix string) string {
	if n <= 0 {
		return suffix
	}
	count := 0
	// Ranging over a string yields rune start offsets, so s[:i] is
	// always a valid cut.
	for i := range s {
		if count == n {
			return s[:i] + suffix
		}
		count++
	}
	return s
}

// TruncateWords behaves like Truncate but backs off to the last space
// when one is close enough to the cut, avoiding a word chopped in
// half. Returns s unchanged when s is already short enough.
func TruncateWords(s string, n int, suffix string) string {
	cut := Truncate(s, n, "")
	if cut == s {
		return s
	}
	if i := lastSpace(cut); i > len(cut)/2 {
		cut = cut[:i]
	}
	return cut + suffix
}

// lastSpace returns the byte index of the last ASCII space in s, or
// -1 if none. Sticking to ASCII is fine for word boundaries: the
// cut itself is rune-safe, the boundary is by definition between
// runes separated by an ASCII byte.
func lastSpace(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ' ' {
			return i
		}
	}
	return -1
}

// CollapseWhitespace is the small string-cleaning step the web
// dashboard used to do inline: it strips HTML tags, splits on
// unicode whitespace, and rejoins with single spaces. Centralising
// it here keeps the dashboard's excerpt path identical to the
// briefing's rationale path when they fall back to raw content.
func CollapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
