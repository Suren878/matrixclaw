package core

import (
	"errors"
	"strings"
	"testing"
)

func TestCompactMessageGroupsKeepToolCallResultTogether(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "tool-1",
					Name:  "read_file",
					Input: `{"path":"README.md"}`,
				},
			}},
		},
		{
			Role: MessageRoleTool,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "tool-1",
					Name:       "read_file",
					Content:    "README contents",
				},
			}},
		},
		{Role: MessageRoleUser, Content: "next"},
	}

	groups := compactMessageGroups(messages)
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if len(groups[0].messages) != 2 {
		t.Fatalf("len(groups[0].messages) = %d, want tool call and result together", len(groups[0].messages))
	}
}

func TestCompactHistoryPromptFormatsToolCallAndResultTogether(t *testing.T) {
	t.Parallel()

	prompt := compactHistoryPrompt([]Message{
		{
			Role: MessageRoleAssistant,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolCall,
				ToolCall: &ToolCallPart{
					ID:    "tool-1",
					Name:  "write_file",
					Input: `{"path":"app.go"}`,
				},
			}},
		},
		{
			Role: MessageRoleTool,
			Parts: []MessagePart{{
				Kind: MessagePartKindToolResult,
				ToolResult: &ToolResultPart{
					ToolCallID: "tool-1",
					Name:       "write_file",
					Content:    "saved app.go",
				},
			}},
		},
	})

	call := strings.Index(prompt, "tool call: write_file")
	result := strings.Index(prompt, "tool result: write_file")
	if call < 0 || result < 0 || result < call {
		t.Fatalf("compactHistoryPrompt() = %q, want paired tool call before result", prompt)
	}
}

func TestCompactHistoryPromptSkipsInternalPlanPrompts(t *testing.T) {
	t.Parallel()

	prompt := compactHistoryPrompt([]Message{
		{Role: MessageRoleUser, Content: "Execute the current session plan."},
		{Role: MessageRoleUser, Content: "real user request"},
	})
	if strings.Contains(prompt, "Execute the current session plan") {
		t.Fatalf("compactHistoryPrompt() kept internal plan prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "real user request") {
		t.Fatalf("compactHistoryPrompt() = %q, want real user request", prompt)
	}
}

func TestCompactSummarySystemPromptRejectsSecretsAndRawDumps(t *testing.T) {
	t.Parallel()

	prompt := compactSummarySystemPrompt()
	for _, want := range []string{"secrets", "API keys", "OAuth tokens", "raw tool dumps", "tool call/result pair"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compactSummarySystemPrompt() missing %q in:\n%s", want, prompt)
		}
	}
}

func TestContextLengthExceededErrorRecognizesCodexContextWindowMessage(t *testing.T) {
	t.Parallel()

	err := errors.New("openai-codex: Your input exceeds the context window of this model. Please adjust your input and try again.")
	if !isContextLengthExceededError(err) {
		t.Fatal("isContextLengthExceededError() = false, want true for Codex context window error")
	}
}

func TestLatestCompactSummaryForRunKeepsCurrentRunMessagesBeforeSummary(t *testing.T) {
	t.Parallel()

	summary, effective := latestCompactSummaryForRun([]Message{
		{RunID: "old-run", Role: MessageRoleUser, Content: "old user"},
		{RunID: "run-1", Role: MessageRoleUser, Content: "current user"},
		{Role: MessageRoleSystem, Content: compactMessagePrefix + "\n\nsummary"},
	}, "run-1")
	if summary != "summary" {
		t.Fatalf("summary = %q, want summary", summary)
	}
	if len(effective) != 1 || effective[0].Content != "current user" {
		t.Fatalf("effective = %#v, want current run user preserved", effective)
	}
}
