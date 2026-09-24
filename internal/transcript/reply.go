package transcript

import (
	"slices"
	"strings"
)

// RunReply is the text of the run's last reply: its latest assistant text with the
// replies the output limit cut before it prepended, engine notes between them
// skipped. Replies keep the whitespace at a cut, so the parts join as they are.
func RunReply(messages []Message, runID string) string {
	runID = strings.TrimSpace(runID)
	last := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if isRunReply(messages[i], runID) {
			last = i
			break
		}
	}
	if last < 0 {
		return ""
	}
	parts := []string{messages[last].Content}
	for i := last - 1; i >= 0; i-- {
		message := messages[i]
		if strings.TrimSpace(message.RunID) == runID && message.Origin.IsEngine() {
			continue
		}
		if !isRunReply(message, runID) || !HasFinishReason(message, "max_tokens") {
			break
		}
		parts = append(parts, message.Content)
	}
	slices.Reverse(parts)
	return strings.TrimSpace(strings.Join(parts, ""))
}

func isRunReply(message Message, runID string) bool {
	return strings.TrimSpace(message.RunID) == runID && message.Role == MessageRoleAssistant && strings.TrimSpace(message.Content) != ""
}
