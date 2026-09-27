package viewmodel

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestEngineNotesRenderAsPlainSystemNotes(t *testing.T) {
	text := "Budget note: about 2 steps left."
	note := ToSurfaceMessage(transcript.Message{ID: "n1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: text, Parts: transcript.NormalizeMessageParts(text, nil)})
	if note.Role != surfacemessage.System || !note.IsSummaryMessage || note.Content().Text != text {
		t.Fatalf("engine note = %+v", note)
	}
	plain := ToSurfaceMessage(transcript.Message{ID: "s1", Role: transcript.MessageRoleSystem, Content: "system text"})
	if plain.IsSummaryMessage {
		t.Fatalf("plain system message = %+v", plain)
	}
}

func TestSurfaceMessagesHideModelOnlyEngineNotes(t *testing.T) {
	modelOnly := "Do not call tools. Reply briefly."
	messages := []transcript.Message{
		{ID: "n1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngineModel, Content: modelOnly, Parts: transcript.NormalizeMessageParts(modelOnly, nil)},
		{ID: "n2", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left.", Parts: transcript.NormalizeMessageParts("Budget note: about 2 steps left.", nil)},
	}
	out := ToSurfaceMessages(messages)
	if len(out) != 1 || out[0].ID != "n2" {
		t.Fatalf("surface messages = %+v", out)
	}
}

func TestBoundaryMessagesCarryTheirCompaction(t *testing.T) {
	message := transcript.Message{ID: "b1", Role: transcript.MessageRoleSystem, Content: "Context compacted: ~12k -> ~3.0k tokens.", Compaction: &transcript.Compaction{Summary: "Goal: ship.", TokensBefore: 12_000, TokensAfter: 3_000}}
	surface := ToSurfaceMessage(message)
	if surface.Boundary == nil || surface.Boundary.Summary != "Goal: ship." || surface.Boundary.Cleared || surface.Boundary.TokensBefore != 12_000 {
		t.Fatalf("surface boundary = %+v", surface.Boundary)
	}
	if plain := ToSurfaceMessage(transcript.Message{ID: "s1", Role: transcript.MessageRoleSystem, Content: "🧠 Context compacted: looks like a marker"}); plain.Boundary != nil {
		t.Fatalf("text alone made a boundary: %+v", plain.Boundary)
	}
}
