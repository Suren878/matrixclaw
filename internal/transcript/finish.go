package transcript

import "strings"

// Finish reasons of assistant messages.
const (
	FinishReasonEndTurn       = "end_turn"
	FinishReasonToolCalls     = "tool_calls"
	FinishReasonMaxTokens     = "max_tokens"
	FinishReasonError         = "error"
	FinishReasonCanceled      = "canceled"
	FinishReasonDaemonRestart = "daemon_restart"
)

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

// restartNote is the finish message of a reply a daemon restart cut off.
const restartNote = "Generation was interrupted by a daemon restart; a recovered continuation follows."

// SealInterrupted ends message with a daemon_restart finish part, which keeps
// it out of every later request; note says why ("" for a daemon restart).
func SealInterrupted(message Message, note string) Message {
	if note == "" {
		note = restartNote
	}
	message.Parts = append(NormalizeMessageParts(message.Content, message.Parts), MessagePart{
		Kind:   MessagePartKindFinish,
		Finish: &FinishPart{Reason: FinishReasonDaemonRestart, Message: note},
	})
	return message
}
