package core

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) generateCompactSummary(ctx context.Context, session Session, messages []Message) (string, error) {
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return "", err
	}
	content := "Compact this session history:\n\n" + compactHistoryPrompt(messages)
	if planSnapshot := c.compactSessionPlanSnapshot(ctx, session.ID); planSnapshot != "" {
		content = "Current session plan:\n" + planSnapshot + "\n\n" + content
	}

	response, err := runtime.Generate(ctx, providers.Request{
		SessionID:    session.ID,
		SystemPrompt: compactSummarySystemPrompt(),
		Messages: []providers.Message{{
			Role:    "user",
			Content: content,
		}},
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(response.Text)
	if text == "" {
		return "", errors.New("compact summary is empty")
	}
	return text, nil
}

func (c *Core) compactSessionPlanSnapshot(ctx context.Context, sessionID string) string {
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return ""
	}
	return planToolSummary(plan)
}

func compactSummarySystemPrompt() string {
	return strings.TrimSpace(`You compact matrixclaw chat histories into durable working context for future assistant turns.

Write a concise, factual summary. Preserve only information that helps future turns continue correctly:
- Current user goal and task state.
- Decisions already made and constraints the user gave.
- Files, modules, commands, tools, providers, or services that were changed or investigated.
- Important tool results, failures, approvals, pending actions, and unresolved risks.
- Active plan/task status when it matters for continuing work.

Do not include filler, greetings, speculation, duplicated logs, raw tool dumps, secrets, API keys, OAuth tokens, or long code/output blocks.
If a tool call/result pair matters, summarize the action and outcome together.
Reply in English. Use compact bullet points only when they add clarity.`)
}

func compactHistoryPrompt(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	groups := compactMessageGroups(messages)
	tailStart := compactTailGroupStart(groups, maxCompactPromptRunes*3/4)
	var builder strings.Builder
	if tailStart > 0 {
		builder.WriteString("[older history summarized from pruned excerpts]\n\n")
	}
	for i, group := range groups {
		text := compactGroupTextForSummary(group, i >= tailStart)
		if text == "" {
			continue
		}
		builder.WriteString(text)
		builder.WriteString("\n\n")
	}
	return trimRunesFromStart(builder.String(), maxCompactPromptRunes)
}

type compactMessageGroup struct {
	messages []Message
}

func compactMessageGroups(messages []Message) []compactMessageGroup {
	filtered := make([]Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == MessageRoleSystem || IsPlanRunPromptMessage(message) {
			continue
		}
		filtered = append(filtered, message)
	}
	groups := make([]compactMessageGroup, 0, len(filtered))
	for i := 0; i < len(filtered); i++ {
		message := filtered[i]
		group := compactMessageGroup{messages: []Message{message}}
		toolCallIDs := messageToolCallIDs(message)
		for len(toolCallIDs) > 0 && i+1 < len(filtered) && messageIsToolResultFor(filtered[i+1], toolCallIDs) {
			i++
			group.messages = append(group.messages, filtered[i])
		}
		groups = append(groups, group)
	}
	return groups
}

func compactTailGroupStart(groups []compactMessageGroup, tailRuneBudget int) int {
	if tailRuneBudget <= 0 {
		return len(groups)
	}
	used := 0
	lastUserIndex := -1
	for i := len(groups) - 1; i >= 0; i-- {
		if compactGroupHasUserMessage(groups[i]) && lastUserIndex < 0 {
			lastUserIndex = i
		}
		text := compactGroupTextForSummary(groups[i], true)
		next := utf8.RuneCountInString(text) + 16
		if used > 0 && used+next > tailRuneBudget && (lastUserIndex < 0 || i < lastUserIndex) {
			return i + 1
		}
		used += next
	}
	return 0
}

func compactGroupHasUserMessage(group compactMessageGroup) bool {
	for _, message := range group.messages {
		if message.Role == MessageRoleUser {
			return true
		}
	}
	return false
}

func compactGroupTextForSummary(group compactMessageGroup, tail bool) string {
	parts := make([]string, 0, len(group.messages))
	for _, message := range group.messages {
		role := strings.TrimSpace(string(message.Role))
		text := compactMessageTextForSummary(message, tail)
		if role == "" || text == "" {
			continue
		}
		parts = append(parts, role+": "+text)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func compactMessageTextForSummary(message Message, tail bool) string {
	text := strings.TrimSpace(message.Content)
	if len(message.Parts) > 0 {
		text = compactMessagePartsTextForSummary(message.Parts, tail)
	}
	if tail {
		return trimRunesEnd(text, 8_000)
	}
	return trimRunesEnd(text, 1_500)
}

func messageToolCallIDs(message Message) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, part := range message.Parts {
		if part.ToolCall == nil {
			continue
		}
		if id := strings.TrimSpace(part.ToolCall.ID); id != "" {
			ids[id] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func messageIsToolResultFor(message Message, ids map[string]struct{}) bool {
	if len(ids) == 0 || message.Role != MessageRoleTool {
		return false
	}
	for _, part := range message.Parts {
		if part.ToolResult == nil {
			continue
		}
		if _, ok := ids[strings.TrimSpace(part.ToolResult.ToolCallID)]; ok {
			return true
		}
	}
	return false
}

func compactMessagePartsTextForSummary(parts []MessagePart, tail bool) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != nil:
			values = append(values, trimRunesEnd(part.Text.Text, compactTextPartLimit(tail)))
		case part.Image != nil:
			values = append(values, "image: "+imagePartLabel(*part.Image))
		case part.ToolCall != nil:
			input := trimRunesEnd(part.ToolCall.Input, compactToolPartLimit(tail))
			values = append(values, "tool call: "+part.ToolCall.Name+" "+input)
		case part.ToolResult != nil:
			content := trimRunesEnd(part.ToolResult.Content, compactToolPartLimit(tail))
			values = append(values, "tool result: "+part.ToolResult.Name+" "+content)
		}
	}
	return strings.TrimSpace(strings.Join(values, "\n"))
}

func compactTextPartLimit(tail bool) int {
	if tail {
		return 8_000
	}
	return 1_500
}

func compactToolPartLimit(tail bool) int {
	if tail {
		return 4_000
	}
	return 800
}

func trimRunesEnd(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "\n[truncated]"
}

func trimRunesFromStart(value string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return "[older history truncated]\n" + string(runes[len(runes)-maxRunes:])
}
