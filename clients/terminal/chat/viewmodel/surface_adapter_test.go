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

func TestSurfaceMessagesHideTheContinuationNote(t *testing.T) {
	continuation := "Your reply was cut by the output limit. Continue exactly where you stopped, without repeating what you already wrote."
	messages := []transcript.Message{
		{ID: "n1", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: continuation, Parts: transcript.NormalizeMessageParts(continuation, nil)},
		{ID: "n2", Role: transcript.MessageRoleSystem, Origin: transcript.OriginEngine, Content: "Budget note: about 2 steps left.", Parts: transcript.NormalizeMessageParts("Budget note: about 2 steps left.", nil)},
	}
	out := ToSurfaceMessages(messages)
	if len(out) != 1 || out[0].ID != "n2" {
		t.Fatalf("surface messages = %+v", out)
	}
}
