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
