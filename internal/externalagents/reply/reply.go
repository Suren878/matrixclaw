// Package reply builds the transcript reply of an external agent's turn from
// its events, with bounded tool input, output and reasoning.
package reply

import (
	"strings"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	toolInputLimit         = 64 * 1024
	toolOutputPerItemLimit = 64 * 1024
	toolOutputTotalLimit   = 256 * 1024
	reasoningPartLimit     = 128 * 1024
	truncationMarker       = "\n\n[MatrixClaw: output truncated; the external agent retains the full result]"
	fallbackToolName       = "external_tool"
)

// Apply adds an output event (text, reasoning, tool call or result) to the
// reply and reports whether the reply changed.
func Apply(reply *transcript.Message, event externalagents.Event) bool {
	switch event.Kind {
	case externalagents.EventMessageDelta:
		if event.Text == "" {
			return false
		}
		reply.Content += event.Text
		appendText(reply, event.Text)
		return true
	case externalagents.EventReasoningDelta:
		if event.Text == "" {
			return false
		}
		appendReasoning(reply, event.Text)
		return true
	case externalagents.EventToolStarted:
		if strings.TrimSpace(event.ItemID) == "" {
			return false
		}
		upsertToolCall(reply, event.ItemID, toolName(event.ToolName), event.ToolInput, false)
		return true
	case externalagents.EventToolOutputDelta, externalagents.EventDiffUpdated:
		if strings.TrimSpace(event.ItemID) == "" || event.Text == "" {
			return false
		}
		upsertToolResult(reply, event.ItemID, toolName(event.ToolName), event.Text, false, true)
		return true
	case externalagents.EventToolCompleted:
		if strings.TrimSpace(event.ItemID) == "" {
			return false
		}
		name := toolName(event.ToolName)
		upsertToolCall(reply, event.ItemID, name, event.ToolInput, true)
		failed := strings.TrimSpace(event.Error) != ""
		if strings.TrimSpace(event.Text) != "" || failed {
			upsertToolResult(reply, event.ItemID, name, event.Text, failed, false)
		}
		return true
	default:
		return false
	}
}

func appendText(reply *transcript.Message, delta string) {
	if n := len(reply.Parts); n > 0 {
		if last := &reply.Parts[n-1]; last.Kind == transcript.MessagePartKindText && last.Text != nil {
			last.Text.Text += delta
			return
		}
	}
	reply.Parts = append(reply.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindText,
		Text: &transcript.TextPart{Text: delta},
	})
}

func appendReasoning(reply *transcript.Message, delta string) {
	if n := len(reply.Parts); n > 0 {
		if last := &reply.Parts[n-1]; last.Kind == transcript.MessagePartKindReasoning && last.Reasoning != nil {
			last.Reasoning.Text = clip(last.Reasoning.Text+delta, reasoningPartLimit)
			return
		}
	}
	reply.Parts = append(reply.Parts, transcript.MessagePart{
		Kind:      transcript.MessagePartKindReasoning,
		Reasoning: &transcript.ReasoningPart{Text: clip(delta, reasoningPartLimit)},
	})
}

func upsertToolCall(reply *transcript.Message, id string, name string, input string, finished bool) {
	for i := range reply.Parts {
		call := reply.Parts[i].ToolCall
		if reply.Parts[i].Kind != transcript.MessagePartKindToolCall || call == nil || call.ID != id {
			continue
		}
		call.Name = name
		if strings.TrimSpace(input) != "" {
			call.Input = clip(input, toolInputLimit)
		}
		call.Finished = call.Finished || finished
		return
	}
	reply.Parts = append(reply.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindToolCall,
		ToolCall: &transcript.ToolCallPart{
			ID:       id,
			Name:     name,
			Input:    clip(input, toolInputLimit),
			Finished: finished,
		},
	})
}

// upsertToolResult sets or extends (appendContent) the result of call id.
// A failed result stays failed.
func upsertToolResult(reply *transcript.Message, id string, name string, content string, failed bool, appendContent bool) {
	name = callName(reply, id, name)
	limit := resultLimit(reply, id)
	status := "success"
	if failed {
		status = "error"
	}
	for i := range reply.Parts {
		result := reply.Parts[i].ToolResult
		if reply.Parts[i].Kind != transcript.MessagePartKindToolResult || result == nil || result.ToolCallID != id {
			continue
		}
		if appendContent {
			result.Content = clip(result.Content+content, limit)
		} else if strings.TrimSpace(content) != "" {
			result.Content = clip(content, limit)
		}
		result.Name = name
		if failed || result.Status == "" {
			result.Status = status
		}
		return
	}
	reply.Parts = append(reply.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindToolResult,
		ToolResult: &transcript.ToolResultPart{
			ToolCallID: id,
			Name:       name,
			Content:    clip(content, limit),
			Status:     status,
		},
	})
}

// resultLimit is how many bytes call id's result may hold: its share of the
// reply's total tool output, at most the per-call limit.
func resultLimit(reply *transcript.Message, id string) int {
	remaining := toolOutputTotalLimit
	for _, part := range reply.Parts {
		if part.ToolResult == nil || part.ToolResult.ToolCallID == id {
			continue
		}
		remaining -= len(part.ToolResult.Content)
		if remaining <= 0 {
			return 0
		}
	}
	return min(remaining, toolOutputPerItemLimit)
}

// callName is the name of call id when the reply has it, else fallback.
func callName(reply *transcript.Message, id string, fallback string) string {
	for _, part := range reply.Parts {
		if part.Kind == transcript.MessagePartKindToolCall && part.ToolCall != nil && part.ToolCall.ID == id && strings.TrimSpace(part.ToolCall.Name) != "" {
			return part.ToolCall.Name
		}
	}
	return fallback
}

func toolName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return fallbackToolName
}

// clip keeps value within limit bytes: three quarters head, one quarter
// tail, the truncation marker between, cut on rune boundaries.
func clip(value string, limit int) string {
	if limit <= 0 {
		return strings.TrimSpace(truncationMarker)
	}
	if len(value) <= limit {
		return value
	}
	if limit <= len(truncationMarker) {
		return prefix(truncationMarker, limit)
	}
	available := limit - len(truncationMarker)
	head := available * 3 / 4
	return prefix(value, head) + truncationMarker + suffix(value, available-head)
}

func prefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func suffix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}
