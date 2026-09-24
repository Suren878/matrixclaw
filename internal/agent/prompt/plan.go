package prompt

import (
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// IsPlanRunPrompt reports whether message is an internal plan runner prompt; such
// prompts stay in history for audit but the provider already sees the plan.
func IsPlanRunPrompt(message transcript.Message) bool {
	if message.Role != transcript.MessageRoleUser {
		return false
	}
	content := strings.TrimSpace(message.Content)
	return strings.HasPrefix(content, "Execute the current session plan.") ||
		strings.HasPrefix(content, "Execute the next session plan item.") ||
		strings.HasPrefix(content, "The session plan was updated.")
}
