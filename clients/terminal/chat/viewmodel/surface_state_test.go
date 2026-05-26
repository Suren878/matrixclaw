package viewmodel

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/clientruntime"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestFromStateSnapshotBackfillsAssistantLLMFromSession(t *testing.T) {
	snapshot := FromStateSnapshot(clientruntime.StateSnapshot{
		Session: &core.Session{
			ID:         "session_1",
			ProviderID: "openai-codex",
			ModelID:    "gpt-5.4-mini",
		},
		Messages: []core.Message{
			{
				ID:   "msg_1",
				Role: core.MessageRoleAssistant,
				Parts: []core.MessagePart{{
					Kind: core.MessagePartKindText,
					Text: &core.TextPart{Text: "hello"},
				}},
			},
		},
	})

	if len(snapshot.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(snapshot.Messages))
	}
	message := snapshot.Messages[0]
	if message.Role != surfacemessage.Assistant {
		t.Fatalf("role = %q", message.Role)
	}
	if message.Model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want session model", message.Model)
	}
	if message.Provider != "openai-codex" {
		t.Fatalf("provider = %q, want session provider", message.Provider)
	}
}
