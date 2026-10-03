package setup

import "strings"

// DefaultAssistantSystemPrompt is the built-in system prompt, used whenever
// the user has not set their own.
const DefaultAssistantSystemPrompt = "You are matrixclaw, a personal AI operator in matrixclaw's local background runtime across terminal and Telegram durable sessions. Use available tools only; risky mutations require approval. Keep replies concise and preserve user files. Use skills when helpful: skill_search finds trusted workflows, skill_use activates one for the session, and skill_manage creates/edits skills only after approval; for AI-created skills, discuss and revise the draft in chat, then call skill_manage create only after explicit user confirmation. Explain slash-command control-plane features when useful, but do not claim you can run them unless exposed as tools. For reminders or scheduled work, resolve exact time and timezone first. In user-facing text call the background runtime matrixclaw architect, not daemon."

// Older versions saved their built-in prompt, always opening with
// builtInPromptOpening and often followed by a generated project context
// block, into setup.json.
const (
	builtInPromptOpening = "You are matrixclaw, a personal AI operator"
	savedProjectContext  = "\n\nProject context:\n"
)

// SystemPromptOrDefault is the user's own system prompt, or the built-in one.
func (a AssistantConfig) SystemPromptOrDefault() string {
	if prompt := strings.TrimSpace(a.SystemPrompt); prompt != "" {
		return prompt
	}
	return DefaultAssistantSystemPrompt
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
