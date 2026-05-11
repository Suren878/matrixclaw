package core

import (
	"regexp"
	"strings"
)

var (
	assistantReasoningTags = []string{
		"think",
		"thought",
		"thinking",
		"reasoning",
		"analysis",
		"scratchpad",
		"internal_monologue",
		"chain_of_thought",
		"cot",
	}
	assistantThinkingBlockPattern = regexp.MustCompile(`(?is)(^|\n)\s*Thinking\.\.\..*?\.{0,3}\s*done\s+Thinking!\s*`)
	assistantBlankLinesPattern    = regexp.MustCompile(`\n{3,}`)
)

func sanitizeAssistantOutput(text string) string {
	text = stripAssistantXMLReasoningBlocks(text)
	text = assistantThinkingBlockPattern.ReplaceAllString(text, "\n")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = assistantBlankLinesPattern.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func stripAssistantXMLReasoningBlocks(text string) string {
	for {
		start, end, ok := findAssistantReasoningBlock(text)
		if !ok {
			return text
		}
		text = text[:start] + text[end:]
	}
}

func findAssistantReasoningBlock(text string) (int, int, bool) {
	lower := strings.ToLower(text)
	bestStart := -1
	bestEnd := -1
	bestTag := ""
	for _, tag := range assistantReasoningTags {
		start, openEnd, ok := findOpeningReasoningTag(lower, tag)
		if !ok {
			continue
		}
		if bestStart == -1 || start < bestStart {
			bestStart = start
			bestEnd = openEnd
			bestTag = tag
		}
	}
	if bestStart == -1 {
		return 0, 0, false
	}

	closeToken := "</" + bestTag + ">"
	closeStart := strings.Index(lower[bestEnd:], closeToken)
	if closeStart < 0 {
		return bestStart, len(text), true
	}
	return bestStart, bestEnd + closeStart + len(closeToken), true
}

func findOpeningReasoningTag(lower string, tag string) (int, int, bool) {
	offset := 0
	token := "<" + tag
	for {
		idx := strings.Index(lower[offset:], token)
		if idx < 0 {
			return 0, 0, false
		}
		start := offset + idx
		afterName := start + len(token)
		if afterName >= len(lower) {
			return start, len(lower), true
		}
		next := lower[afterName]
		if next != '>' && next != ' ' && next != '\t' && next != '\n' && next != '\r' {
			offset = afterName
			continue
		}
		end := strings.IndexByte(lower[afterName:], '>')
		if end < 0 {
			return start, len(lower), true
		}
		return start, afterName + end + 1, true
	}
}

type assistantStreamSanitizer struct {
	raw     string
	emitted string
}

func newAssistantStreamSanitizer() *assistantStreamSanitizer {
	return &assistantStreamSanitizer{}
}

func (s *assistantStreamSanitizer) Push(delta string) string {
	if delta == "" {
		return ""
	}
	s.raw += delta
	clean := holdPotentialReasoningPrefix(sanitizeAssistantOutput(s.raw))
	if len(clean) <= len(s.emitted) {
		return ""
	}
	out := clean[len(s.emitted):]
	s.emitted = clean
	return out
}

func holdPotentialReasoningPrefix(text string) string {
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	cut := len(text)
	if idx := strings.LastIndexByte(lower, '<'); idx >= 0 && couldStartReasoningTag(lower[idx:]) {
		cut = idx
	}
	if idx := possibleThinkingPrefixStart(lower[:cut]); idx >= 0 {
		cut = idx
	}
	return text[:cut]
}

func couldStartReasoningTag(suffix string) bool {
	for _, tag := range assistantReasoningTags {
		open := "<" + tag
		close := "</" + tag
		if strings.HasPrefix(open, suffix) || strings.HasPrefix(close, suffix) {
			return true
		}
	}
	return false
}

func possibleThinkingPrefixStart(lower string) int {
	pattern := "thinking..."
	for i := maxInt(0, len(lower)-len(pattern)+1); i < len(lower); i++ {
		if strings.HasPrefix(pattern, lower[i:]) {
			return i
		}
	}
	return -1
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
