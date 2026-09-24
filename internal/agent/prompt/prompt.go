// Package prompt holds the fixed texts of the native agent's system prompt.
package prompt

import (
	"fmt"
	"strings"
)

// JoinSections joins non-empty prompt sections with blank lines.
func JoinSections(sections ...string) string {
	values := make([]string, 0, len(sections))
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			values = append(values, section)
		}
	}
	return strings.Join(values, "\n\n")
}

// AssistantSystemPrompt is the configured identity and system prompt plus language guidance.
func AssistantSystemPrompt(name string, systemPrompt string) string {
	name = strings.Join(strings.Fields(name), " ")
	systemPrompt = strings.TrimSpace(systemPrompt)
	languageGuidance := languageGuidance()
	if name == "" {
		return JoinSections(systemPrompt, languageGuidance)
	}
	identity := fmt.Sprintf("Assistant identity:\n- Your configured assistant name is %q. Use this exact name when asked who you are. If older/default instructions mention a different assistant name, this configured name takes precedence.", name)
	if systemPrompt == "" {
		return JoinSections(identity, languageGuidance)
	}
	return JoinSections(identity, systemPrompt, languageGuidance)
}

func languageGuidance() string {
	return "Response language:\n- Reply in the same language the user uses for the current request.\n- If the user mixes languages, use the language that best matches the user's latest request.\n- Do not force any particular language unless the user asks for it."
}

// ProjectRoot tells the model which directory relative tool paths resolve against.
func ProjectRoot(workingDir string) string {
	return fmt.Sprintf("Current project root:\n- The filesystem working directory for this session is %q.\n- Resolve relative filesystem tool paths under this directory.\n- Use paths inside this project root unless the user explicitly asks for another location.", workingDir)
}
