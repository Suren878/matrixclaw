package transcript

import "strings"

// HasFinishReason reports whether the message has a finish part with the reason;
// an empty reason matches any finish part.
func HasFinishReason(message Message, reason string) bool {
	reason = strings.TrimSpace(reason)
	for _, part := range message.Parts {
		if part.Finish == nil {
			continue
		}
		if reason == "" || strings.TrimSpace(part.Finish.Reason) == reason {
			return true
		}
	}
	return false
}
