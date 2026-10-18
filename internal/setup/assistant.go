package setup

import "strings"

// Older versions saved their built-in prompt, always opening with
// builtInPromptOpening and often followed by a generated project context
// block, into setup.json.
const (
	builtInPromptOpening = "You are matrixclaw, a personal AI operator"
	savedProjectContext  = "\n\nProject context:\n"
)

// DefaultAssistantName is the assistant's name unless the user picks another.
const DefaultAssistantName = "matrixclaw"

// NameOrDefault is the user's assistant name, or the built-in one.
func (a AssistantConfig) NameOrDefault() string {
	if name := strings.TrimSpace(a.Name); name != "" {
		return name
	}
	return DefaultAssistantName
}

// userSystemPrompt keeps only the user's own part of a stored system prompt.
func userSystemPrompt(stored string) string {
	stored = strings.TrimSpace(stored)
	if idx := strings.Index(stored, savedProjectContext); idx >= 0 {
		stored = strings.TrimSpace(stored[:idx])
	}
	if strings.HasPrefix(stored, builtInPromptOpening) {
		return ""
	}
	return stored
}
