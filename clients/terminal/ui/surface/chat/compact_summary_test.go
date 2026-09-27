package chat

import (
	"testing"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
)

func TestContextBoundariesAreRecognisedByTheirField(t *testing.T) {
	compacted := &surfacemessage.Message{Role: surfacemessage.System, Boundary: &surfacemessage.ContextBoundary{Summary: "Goal", TokensBefore: 12_000, TokensAfter: 3_000}}
	cleared := &surfacemessage.Message{Role: surfacemessage.System, Boundary: &surfacemessage.ContextBoundary{Cleared: true}}
	plain := &surfacemessage.Message{Role: surfacemessage.System, Parts: []surfacemessage.ContentPart{surfacemessage.TextContent{Text: "🧠 Context compacted"}}}

	if !IsCompactSummaryMessage(compacted) || IsContextClearedMessage(compacted) {
		t.Fatal("compaction not recognised")
	}
	if !IsContextClearedMessage(cleared) || IsCompactSummaryMessage(cleared) {
		t.Fatal("clear not recognised")
	}
	if IsCompactSummaryMessage(plain) || IsContextClearedMessage(plain) {
		t.Fatal("marker text alone is a boundary")
	}
	if got := compactSummaryStats(*compacted.Boundary); got != "(~12k -> ~3.0k tokens)" {
		t.Fatalf("stats = %q", got)
	}
}
