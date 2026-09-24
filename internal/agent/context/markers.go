package agentcontext

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

const compactMessagePrefix = "🧠 Context compacted"
const contextClearedMessagePrefix = "🧹 Context cleared"
const contextClearedSummary = "Context cleared by user."
const compactBackoffMinimumSavingsPercent = 10

// Marker is the newest compaction or clear marker and the history after it.
type Marker struct {
	Summary   string
	Effective []transcript.Message
	Cleared   bool
}

// LatestMarker finds the newest marker; without one Effective is the whole history.
func LatestMarker(messages []transcript.Message) Marker {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != transcript.MessageRoleSystem {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if summary, ok := compactMarkerSummary(content); ok {
			return Marker{Summary: summary, Effective: messages[i+1:]}
		}
		if summary, ok := clearMarkerSummary(content); ok {
			return Marker{Summary: summary, Effective: messages[i+1:], Cleared: true}
		}
	}
	return Marker{Effective: messages}
}

// LatestSummary returns the newest marker's summary and the history after it.
func LatestSummary(messages []transcript.Message) (string, []transcript.Message) {
	marker := LatestMarker(messages)
	return marker.Summary, marker.Effective
}

// ClearedMarkerContent is the system message content written by /clear.
func ClearedMarkerContent() string {
	return contextClearedMessagePrefix + "\n\n" + contextClearedSummary
}

// LatestSummaryForRun is LatestSummary but keeps the run's own messages written before a compaction marker.
func LatestSummaryForRun(messages []transcript.Message, currentRunID string) (string, []transcript.Message) {
	currentRunID = strings.TrimSpace(currentRunID)
	if currentRunID == "" {
		return LatestSummary(messages)
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != transcript.MessageRoleSystem {
			continue
		}
		content := strings.TrimSpace(message.Content)
		summary, ok := compactMarkerSummary(content)
		cleared := false
		if !ok {
			summary, ok = clearMarkerSummary(content)
			cleared = ok
		}
		if !ok {
			continue
		}
		effective := make([]transcript.Message, 0, len(messages)-i-1)
		if !cleared {
			for _, prior := range messages[:i] {
				if strings.TrimSpace(prior.RunID) == currentRunID {
					effective = append(effective, prior)
				}
			}
		}
		effective = append(effective, messages[i+1:]...)
		return summary, effective
	}
	return "", messages
}

func compactMarkerSummary(content string) (string, bool) {
	if !strings.HasPrefix(content, compactMessagePrefix) {
		return "", false
	}
	summary := strings.TrimSpace(strings.TrimPrefix(content, compactMessagePrefix))
	if strings.HasPrefix(summary, ":") {
		if _, tail, ok := strings.Cut(summary, "\n\n"); ok {
			summary = strings.TrimSpace(tail)
		}
	}
	return summary, true
}

func clearMarkerSummary(content string) (string, bool) {
	if !strings.HasPrefix(content, contextClearedMessagePrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(content, contextClearedMessagePrefix)), true
}

var compactStatsPattern = regexp.MustCompile(`~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s*->\s*~([0-9]+(?:\.[0-9]+)?[kKmM]?)\s+tokens`)

// CompactBackoffActive reports whether the last two compactions each saved under 10%.
func CompactBackoffActive(messages []transcript.Message) bool {
	checked := 0
	for i := len(messages) - 1; i >= 0 && checked < 2; i-- {
		content := strings.TrimSpace(messages[i].Content)
		if !strings.HasPrefix(content, compactMessagePrefix) {
			continue
		}
		checked++
		before, after, ok := parseCompactStats(content)
		if !ok || before <= 0 {
			return false
		}
		saved := before - after
		if saved*100 >= before*compactBackoffMinimumSavingsPercent {
			return false
		}
	}
	return checked >= 2
}

func parseCompactStats(content string) (int, int, bool) {
	match := compactStatsPattern.FindStringSubmatch(content)
	if len(match) != 3 {
		return 0, 0, false
	}
	before, ok := parseShortTokenNumber(match[1])
	if !ok {
		return 0, 0, false
	}
	after, ok := parseShortTokenNumber(match[2])
	if !ok {
		return 0, 0, false
	}
	return before, after, true
}

func parseShortTokenNumber(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToLower(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "k"):
		multiplier = 1_000
		value = strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier = 1_000_000
		value = strings.TrimSuffix(value, "m")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return int(parsed * multiplier), true
}
