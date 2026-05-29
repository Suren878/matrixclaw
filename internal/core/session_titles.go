package core

import (
	"context"
	"strings"
	"unicode"
)

const fallbackAutoSessionTitle = "New chat"

func (c *Core) firstMessageAutoTitle(ctx context.Context, session Session, text string) string {
	if !isAutomaticSessionTitle(session.Title) {
		return ""
	}
	messages, err := c.store.ListMessages(ctx, session.ID, 1)
	if err != nil || len(messages) > 0 {
		return ""
	}
	title := titleFromUserMessage(text)
	if title == "" {
		return fallbackAutoSessionTitle
	}
	return title
}

func (c *Core) applyAutoSessionTitle(ctx context.Context, session Session, title string) {
	title = normalizeText(title)
	if title == "" || strings.TrimSpace(session.Title) == title {
		return
	}
	session.Title = title
	session.UpdatedAt = c.now().UTC()
	_ = c.store.UpdateSession(ctx, session)
}

func isAutomaticSessionTitle(title string) bool {
	normalized := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(title)), " "))
	switch normalized {
	case "", "main", "new chat", "session":
		return true
	}
	if strings.HasPrefix(normalized, "local chat ") || strings.HasPrefix(normalized, "telegram chat ") {
		return true
	}
	if strings.HasPrefix(normalized, "chat ") {
		for _, r := range strings.TrimSpace(strings.TrimPrefix(normalized, "chat ")) {
			if !unicode.IsDigit(r) {
				return false
			}
		}
		return true
	}
	return false
}

func titleFromUserMessage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = firstTitleClause(text)
	words := titleWords(text, 4)
	if len(words) == 0 {
		return ""
	}
	for i := range words {
		words[i] = titleWord(words[i])
	}
	return strings.Join(words, " ")
}

func firstTitleClause(text string) string {
	for i, r := range text {
		switch r {
		case '\n', '\r', '.', '?', '!':
			if i > 0 {
				return strings.TrimSpace(text[:i])
			}
		}
	}
	return text
}

func titleWords(text string, limit int) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	words := make([]string, 0, limit)
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" || isTitleStopWord(field) {
			continue
		}
		words = append(words, field)
		if len(words) >= limit {
			break
		}
	}
	return words
}

func isTitleStopWord(word string) bool {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "a", "an", "the", "to", "in", "on", "for", "with", "and", "or",
		"can", "could", "would", "should", "you", "me", "my", "please", "pls",
		"help", "look", "into", "at":
		return true
	default:
		return false
	}
}

func titleWord(word string) string {
	runes := []rune(strings.TrimSpace(word))
	if len(runes) == 0 {
		return ""
	}
	if isAllUpperWord(runes) {
		return string(runes)
	}
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

func isAllUpperWord(runes []rune) bool {
	hasLetter := false
	for _, r := range runes {
		if !unicode.IsLetter(r) {
			continue
		}
		hasLetter = true
		if unicode.IsLower(r) {
			return false
		}
	}
	return hasLetter
}
