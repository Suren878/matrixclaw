// Package textutil holds small string helpers shared across packages.
package textutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// FirstNonEmpty returns the first value that is not blank, trimmed.
// Use cmp.Or instead when the values are already trimmed.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// NonBlank returns the values trimmed, without the blank ones.
func NonBlank(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// UniqueNonBlank is NonBlank without repeats; the first occurrence stays.
func UniqueNonBlank(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// Clip cuts text to at most maxRunes runes, mark included, ending the cut
// with mark ("…", "..." or a notice); maxRunes <= 0 keeps text whole.
func Clip(text string, maxRunes int, mark string) string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	markRunes := []rune(mark)
	if len(markRunes) >= maxRunes {
		return string(markRunes[:maxRunes])
	}
	runes := []rune(text)
	return strings.TrimRightFunc(string(runes[:maxRunes-len(markRunes)]), unicode.IsSpace) + mark
}

// ClipBytes cuts text to at most maxBytes bytes on a rune boundary, for
// limits counted in bytes (a websocket close reason).
func ClipBytes(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	end := max(maxBytes, 0)
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end]
}
