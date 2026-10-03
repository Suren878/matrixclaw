package agentcontext

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

func stepCallMessage(id string) transcript.Message {
	return transcript.Message{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: "read", Input: `{}`}}}}
}

func stepResultMessage(id string, content string, failed bool) transcript.Message {
	status := "success"
	if failed {
		status = "error"
	}
	return transcript.Message{Role: transcript.MessageRoleTool, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindToolResult, ToolResult: &transcript.ToolResultPart{ToolCallID: id, Name: "read", Content: content, Status: status}}}}
}

func TestToolStepIsReplayedAsOneAssistantMessage(t *testing.T) {
	stepEnd := transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Compare a and b"},
		{Role: transcript.MessageRoleAssistant, Content: "Reading both.", Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{RedactedData: "enc-1"}},
			{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: "Reading both."}},
			stepEnd,
		}},
		stepCallMessage("a"), stepResultMessage("a", "A", false),
		stepCallMessage("b"), stepResultMessage("b", "missing", true),
		{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{stepEnd}},
		stepCallMessage("c"), stepResultMessage("c", "C", false),
		{Role: transcript.MessageRoleAssistant, Content: "Done"},
	}
	conversation, err := Conversation(context.Background(), history, nil, false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 7 {
		t.Fatalf("conversation=%+v, want user, step, 2 results, step, result, reply", conversation)
	}
	first := conversation[1]
	if first.Content != "Reading both." || len(first.ToolCalls) != 2 || first.ToolCalls[1].ID != "b" || len(first.Reasoning) != 1 || first.Reasoning[0].RedactedData != "enc-1" || first.ReasoningContent != nil {
		t.Fatalf("first step=%+v", first)
	}
	if conversation[2].ToolCallID != "a" || conversation[2].IsError || conversation[3].ToolCallID != "b" || !conversation[3].IsError {
		t.Fatalf("results=%+v %+v", conversation[2], conversation[3])
	}
	if second := conversation[4]; len(second.ToolCalls) != 1 || second.ToolCalls[0].ID != "c" || conversation[5].ToolCallID != "c" || conversation[6].Content != "Done" {
		t.Fatalf("second step and reply=%+v", conversation[4:])
	}
}

func TestPlainReasoningOnOlderToolCallsStaysReasoningContent(t *testing.T) {
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect"},
		{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: "thinking"}},
			{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: "a", Name: "read", Input: `{}`}},
		}},
		stepResultMessage("a", "A", false),
	}
	conversation, err := Conversation(context.Background(), history, nil, false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	step := conversation[1]
	if step.ReasoningContent == nil || *step.ReasoningContent != "thinking" || len(step.Reasoning) != 0 {
		t.Fatalf("step=%+v", step)
	}
}

func TestOlderToolStepsWithoutReplyStaySeparatePerResponse(t *testing.T) {
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect"},
		stepCallMessage("a"), stepResultMessage("a", "A", false),
		stepCallMessage("b"), stepResultMessage("b", "B", false),
	}
	conversation, err := Conversation(context.Background(), history, nil, false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 5 {
		t.Fatalf("conversation=%+v, want user, call a, result a, call b, result b", conversation)
	}
	if len(conversation[1].ToolCalls) != 1 || conversation[1].ToolCalls[0].ID != "a" || conversation[2].ToolCallID != "a" ||
		len(conversation[3].ToolCalls) != 1 || conversation[3].ToolCalls[0].ID != "b" || conversation[4].ToolCallID != "b" {
		t.Fatalf("conversation=%+v", conversation)
	}
}

func TestLateResultAfterSteerStaysWithItsToolStep(t *testing.T) {
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect"},
		{Role: transcript.MessageRoleAssistant, Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}}},
		stepCallMessage("a"), stepCallMessage("b"), stepResultMessage("a", "A", false),
		{Role: transcript.MessageRoleUser, Content: "Also check c"},
		stepResultMessage("b", "B", false),
	}
	conversation, err := Conversation(context.Background(), history, nil, false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 5 {
		t.Fatalf("conversation=%+v, want user, step, result a, result b, steer", conversation)
	}
	if len(conversation[1].ToolCalls) != 2 || conversation[2].ToolCallID != "a" || conversation[3].ToolCallID != "b" || conversation[4].Content != "Also check c" {
		t.Fatalf("conversation=%+v", conversation)
	}
}

func TestSignedReasoningIsReplayedOnlyToTheModelThatProducedIt(t *testing.T) {
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect"},
		{Role: transcript.MessageRoleAssistant, Provider: "anthropic-compatible", Model: "claude-a", Parts: []transcript.MessagePart{
			{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: "plain"}},
			{Kind: transcript.MessagePartKindReasoning, Reasoning: &transcript.ReasoningPart{Text: "signed", Signature: "sig-a"}},
			{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}},
		}},
		stepCallMessage("a"), stepResultMessage("a", "A", false),
	}
	for _, tc := range []struct {
		name       string
		identity   Identity
		wantSigned int
	}{
		{name: "same model", identity: Identity{Provider: "anthropic-compatible", Model: "claude-a"}, wantSigned: 1},
		{name: "other model", identity: Identity{Provider: "anthropic-compatible", Model: "claude-b"}, wantSigned: 0},
		{name: "other provider", identity: Identity{Provider: "gemini", Model: "claude-a"}, wantSigned: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conversation, err := Conversation(context.Background(), history, nil, false, tc.identity)
			if err != nil {
				t.Fatal(err)
			}
			step := conversation[1]
			if len(step.ToolCalls) != 1 || len(step.Reasoning) != tc.wantSigned || step.ReasoningContent == nil || *step.ReasoningContent != "plain" {
				t.Fatalf("step=%+v", step)
			}
		})
	}
}

func TestEngineNotesReachTheModelAsUserText(t *testing.T) {
	stepEnd := transcript.MessagePart{Kind: transcript.MessagePartKindFinish, Finish: &transcript.FinishPart{Reason: "tool_calls"}}
	note := transcript.Message{Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left."}
	modelNote := transcript.Message{Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngineModel, Content: "Do not call tools."}
	history := []transcript.Message{
		{Role: transcript.MessageRoleUser, Content: "Inspect a"},
		{Role: transcript.MessageRoleSystem, Content: "a plain system row stays hidden"},
		{Role: transcript.MessageRoleAssistant, Content: "Reading.", Parts: []transcript.MessagePart{{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: "Reading."}}, stepEnd}},
		stepCallMessage("a"), stepResultMessage("a", "A", false),
		note, modelNote,
	}

	conversation, err := Conversation(context.Background(), history, nil, false, Identity{})
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation) != 5 || len(conversation[1].ToolCalls) != 1 || conversation[2].ToolCallID != "a" {
		t.Fatalf("conversation = %+v, want user, step, result and both notes", conversation)
	}
	for i, want := range []transcript.Message{note, modelNote} {
		if got := conversation[3+i]; got.Role != "user" || got.Content != want.Content {
			t.Fatalf("note %d = %+v", i, got)
		}
	}

	text := TextOnlyConversation(history)
	if n := len(text); n < 2 || text[n-2].Content != note.Content || text[n-1].Role != "user" || text[n-1].Content != modelNote.Content {
		t.Fatalf("text-only conversation = %+v", text)
	}
}
