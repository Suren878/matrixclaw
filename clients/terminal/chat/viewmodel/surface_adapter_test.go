package viewmodel

import (
	"encoding/json"
	"testing"
	"time"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestToSurfaceMessage(t *testing.T) {
	now := time.Now().UTC()
	message := core.Message{
		ID:        "msg-1",
		SessionID: "session-1",
		RunID:     "run-1",
		Role:      core.MessageRoleAssistant,
		Content:   "hello",
		Model:     "gpt-test",
		Provider:  "openai-compatible",
		Parts: []core.MessagePart{
			{
				Kind: core.MessagePartKindText,
				Text: &core.TextPart{Text: "hello"},
			},
			{
				Kind: core.MessagePartKindToolCall,
				ToolCall: &core.ToolCallPart{
					ID:       "tool-1",
					Name:     "write",
					Input:    `{"file_path":"a.txt"}`,
					Finished: true,
				},
			},
			{
				Kind: core.MessagePartKindToolResult,
				ToolResult: &core.ToolResultPart{
					ToolCallID: "tool-1",
					Name:       "write",
					Content:    "ok",
					Metadata:   json.RawMessage(`{"diff":"..."}`),
				},
			},
			{
				Kind: core.MessagePartKindFinish,
				Finish: &core.FinishPart{
					Reason:  "end_turn",
					Message: "done",
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now.Add(2 * time.Second),
	}

	surface := ToSurfaceMessage(message)
	if surface.Role != surfacemessage.Assistant {
		t.Fatalf("surface.Role = %q, want %q", surface.Role, surfacemessage.Assistant)
	}
	if surface.Model != "gpt-test" || surface.Provider != "openai-compatible" {
		t.Fatalf("surface model/provider = (%q,%q), want (%q,%q)", surface.Model, surface.Provider, "gpt-test", "openai-compatible")
	}
	if surface.UpdatedAt != now.Add(2*time.Second).Unix() {
		t.Fatalf("surface.UpdatedAt = %d, want %d", surface.UpdatedAt, now.Add(2*time.Second).Unix())
	}
	if len(surface.ToolCalls()) != 1 || surface.ToolCalls()[0].ID != "tool-1" {
		t.Fatalf("surface.ToolCalls() = %#v", surface.ToolCalls())
	}
	if len(surface.ToolResults()) != 1 || surface.ToolResults()[0].ToolCallID != "tool-1" {
		t.Fatalf("surface.ToolResults() = %#v", surface.ToolResults())
	}
	if surface.FinishReason() != surfacemessage.FinishReasonEndTurn {
		t.Fatalf("surface.FinishReason() = %q, want %q", surface.FinishReason(), surfacemessage.FinishReasonEndTurn)
	}
}

func TestToSurfaceMessageFallsBackToLegacyContent(t *testing.T) {
	now := time.Now().UTC()
	message := core.Message{
		ID:        "msg-legacy",
		SessionID: "session-1",
		Role:      core.MessageRoleAssistant,
		Content:   "legacy text only",
		CreatedAt: now,
		UpdatedAt: now,
	}

	surface := ToSurfaceMessage(message)
	if got := surface.Content().Text; got != "legacy text only" {
		t.Fatalf("surface.Content().Text = %q, want %q", got, "legacy text only")
	}
}
