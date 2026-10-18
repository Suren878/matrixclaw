package reply

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func text(s string) transcript.MessagePart {
	return transcript.MessagePart{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: s}}
}

func reasoning(s string) transcript.MessagePart {
	return transcript.MessagePart{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: s}}
}

func call(id, name, input string, finished bool) transcript.MessagePart {
	return transcript.MessagePart{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: name, Input: input, Finished: finished}}
}

func result(id, name, content, status string) transcript.MessagePart {
	return transcript.MessagePart{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: id, Name: name, Content: content, Status: status}}
}

func TestApplyBuildsReply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		events  []externalagents.Event
		content string
		parts   []transcript.MessagePart
	}{
		{
			name: "text deltas join one part",
			events: []externalagents.Event{
				{Kind: externalagents.EventMessageDelta, Text: "Hel"},
				{Kind: externalagents.EventMessageDelta, Text: "lo"},
			},
			content: "Hello",
			parts:   []transcript.MessagePart{text("Hello")},
		},
		{
			name: "reasoning then text are separate parts",
			events: []externalagents.Event{
				{Kind: externalagents.EventReasoningDelta, Text: "think"},
				{Kind: externalagents.EventReasoningDelta, Text: "ing"},
				{Kind: externalagents.EventMessageDelta, Text: "done"},
			},
			content: "done",
			parts:   []transcript.MessagePart{reasoning("thinking"), text("done")},
		},
		{
			name: "tool call streams output and completes",
			events: []externalagents.Event{
				{Kind: externalagents.EventToolStarted, ItemID: "c1", ToolName: "bash", ToolInput: `{"command":"ls"}`},
				{Kind: externalagents.EventToolOutputDelta, ItemID: "c1", Text: "a\n"},
				{Kind: externalagents.EventToolOutputDelta, ItemID: "c1", Text: "b\n"},
				{Kind: externalagents.EventToolCompleted, ItemID: "c1", ToolName: "bash"},
			},
			parts: []transcript.MessagePart{call("c1", "bash", `{"command":"ls"}`, true), result("c1", "bash", "a\nb\n", "success")},
		},
		{
			name: "completed output replaces streamed output",
			events: []externalagents.Event{
				{Kind: externalagents.EventToolOutputDelta, ItemID: "c1", ToolName: "bash", Text: "partial"},
				{Kind: externalagents.EventToolCompleted, ItemID: "c1", ToolName: "bash", Text: "full"},
			},
			parts: []transcript.MessagePart{result("c1", "bash", "full", "success"), call("c1", "bash", "", true)},
		},
		{
			name: "failed tool is an error result",
			events: []externalagents.Event{
				{Kind: externalagents.EventToolStarted, ItemID: "c1", ToolName: "edit"},
				{Kind: externalagents.EventToolCompleted, ItemID: "c1", ToolName: "edit", Error: "declined"},
			},
			parts: []transcript.MessagePart{call("c1", "edit", "", true), result("c1", "edit", "", "error")},
		},
		{
			name: "diff result takes the call's name",
			events: []externalagents.Event{
				{Kind: externalagents.EventToolStarted, ItemID: "c1", ToolName: "apply_patch"},
				{Kind: externalagents.EventDiffUpdated, ItemID: "c1", ToolName: "edit", Text: "+x"},
			},
			parts: []transcript.MessagePart{call("c1", "apply_patch", "", false), result("c1", "apply_patch", "+x", "success")},
		},
		{
			name:   "unnamed tool gets a fallback name",
			events: []externalagents.Event{{Kind: externalagents.EventToolStarted, ItemID: "c1"}},
			parts:  []transcript.MessagePart{call("c1", fallbackToolName, "", false)},
		},
		{
			name: "events without content or item change nothing",
			events: []externalagents.Event{
				{Kind: externalagents.EventMessageDelta},
				{Kind: externalagents.EventToolStarted, ToolName: "bash"},
				{Kind: externalagents.EventToolOutputDelta, ItemID: "c1"},
				{Kind: externalagents.EventHeartbeat, Text: "x"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reply transcript.Message
			for _, event := range tc.events {
				Apply(&reply, event)
			}
			if reply.Content != tc.content {
				t.Fatalf("content = %q, want %q", reply.Content, tc.content)
			}
			if got, want := describe(reply.Parts), describe(tc.parts); got != want {
				t.Fatalf("parts:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func describe(parts []transcript.MessagePart) string {
	var b strings.Builder
	for _, part := range parts {
		switch {
		case part.Text != nil:
			b.WriteString("text " + part.Text.Text)
		case part.Reasoning != nil:
			b.WriteString("reasoning " + part.Reasoning.Text)
		case part.ToolCall != nil:
			c := part.ToolCall
			b.WriteString("call " + c.ID + " " + c.Name + " " + c.Input)
			if c.Finished {
				b.WriteString(" finished")
			}
		case part.ToolResult != nil:
			r := part.ToolResult
			b.WriteString("result " + r.ToolCallID + " " + r.Name + " " + r.Status + " " + r.Content)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestApplyBoundsToolOutput(t *testing.T) {
	var reply transcript.Message
	delta := strings.Repeat("д", toolOutputPerItemLimit)
	for i := range 8 {
		for range 4 {
			Apply(&reply, externalagents.Event{Kind: externalagents.EventToolOutputDelta, ItemID: string(rune('a' + i)), Text: delta})
		}
	}
	total, results := 0, 0
	for _, part := range reply.Parts {
		if part.ToolResult == nil {
			continue
		}
		results++
		content := part.ToolResult.Content
		total += len(content)
		if len(content) > toolOutputPerItemLimit || !utf8.ValidString(content) {
			t.Fatalf("result %q: %d bytes, valid UTF-8 %t; want at most %d valid", part.ToolResult.ToolCallID, len(content), utf8.ValidString(content), toolOutputPerItemLimit)
		}
	}
	if results != 8 || total > toolOutputTotalLimit+results*len(truncationMarker) {
		t.Fatalf("%d results holding %d bytes, want 8 within the total limit", results, total)
	}
}

func TestClip(t *testing.T) {
	long := "HEAD-" + strings.Repeat("я", 100) + "-TAIL"
	for _, tc := range []struct {
		name  string
		value string
		limit int
		check func(string) bool
	}{
		{"fits", "short", 10, func(s string) bool { return s == "short" }},
		{"keeps head and tail", long, 120, func(s string) bool {
			return len(s) <= 120 && strings.HasPrefix(s, "HEAD-") && strings.HasSuffix(s, "-TAIL") && strings.Contains(s, "truncated")
		}},
		{"no room is the marker alone", long, 0, func(s string) bool { return s == strings.TrimSpace(truncationMarker) }},
		{"room for part of the marker", long, 10, func(s string) bool { return len(s) <= 10 }},
	} {
		got := clip(tc.value, tc.limit)
		if !utf8.ValidString(got) || !tc.check(got) {
			t.Fatalf("%s: clip = %q", tc.name, got)
		}
	}
}
