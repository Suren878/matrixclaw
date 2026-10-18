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

// Persona opens the prompt: the user's own system prompt when set, else the
// built-in one, coding for a session with a working directory.
func Persona(name string, userPrompt string, coding bool) string {
	name = strings.Join(strings.Fields(name), " ")
	if userPrompt = strings.TrimSpace(userPrompt); userPrompt != "" {
		if name == "" {
			return userPrompt
		}
		return JoinSections(fmt.Sprintf("Your name is %q; use it when asked who you are, even if the instructions below give another name.", name), userPrompt)
	}
	you := "You are"
	if name != "" {
		you = fmt.Sprintf("You are %s,", name)
	}
	if coding {
		return you + " an autonomous software engineering agent running on the user's own machine through matrixclaw. You work in the user's projects with tools: you read and search code, edit files, run commands and verify the result. The user may be at a terminal or on Telegram and is often not watching, so work on your own and report when you are done."
	}
	return you + " the user's personal AI assistant, running on their own machine through matrixclaw and reachable from the terminal and Telegram. Get things done with the tools you have; changes may wait for the user's approval. Keep replies concise and preserve the user's files. For reminders and scheduled tasks, settle the exact time and timezone first. The user controls sessions, models and settings with slash commands; do not claim you can run those unless they are offered to you as tools. In user-facing text call the background runtime matrixclaw architect, not daemon."
}

// SubagentPersona opens a child's prompt.
func SubagentPersona(coding bool, readonly bool) string {
	switch {
	case coding && readonly:
		return "You are a matrixclaw subagent: a software engineering agent that a parent agent started on one research or review task. You work with read-only tools: you read and search code and report what you find."
	case coding:
		return "You are a matrixclaw subagent: an autonomous software engineering agent that a parent agent started on one task. You work with tools: you read and search code, edit files, run commands and verify the result."
	}
	return "You are a matrixclaw subagent: a parent agent started you on one task, which you carry out with the tools you have."
}

// LanguageGuidance asks for replies in the user's language.
func LanguageGuidance() string {
	return "Response language:\n- Reply in the same language the user uses for the current request.\n- If the user mixes languages, use the language that best matches the user's latest request.\n- Do not force any particular language unless the user asks for it."
}
