package runtime

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func TestVisibleContextStartsAtTheLatestBoundary(t *testing.T) {
	text := func(value string) surfacemessage.Message {
		return surfacemessage.Message{Role: surfacemessage.User, Parts: []surfacemessage.ContentPart{surfacemessage.TextContent{Text: value}}}
	}
	messages := []surfacemessage.Message{
		text("forgotten forgotten forgotten forgotten"),
		{Role: surfacemessage.System, Boundary: &surfacemessage.ContextBoundary{Summary: "abcd"}},
		text("abcdefgh"),
	}

	tokens, marker := estimateVisibleContextTokens(messages)

	if tokens != 3 || marker != headerContextMarkerCompact {
		t.Fatalf("tokens = %d marker = %v, want 3 (summary 1 + message 2) after a compaction", tokens, marker)
	}
}
