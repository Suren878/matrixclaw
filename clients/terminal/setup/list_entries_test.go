package setup

import "testing"

func TestViewportBoundsCentersSelection(t *testing.T) {
	start, end := viewportBounds(5, 20, 6)
	if start != 2 || end != 8 {
		t.Fatalf("viewportBounds() = (%d, %d), want (2, 8)", start, end)
	}
}

func TestSelectedEntryRowSkipsHeadersAndDividers(t *testing.T) {
	entries := []listEntry{
		{Kind: listEntryHeader, Text: "Configured"},
		rowEntry("OpenAI", "Active", 1),
		{Kind: listEntryDivider},
		{Kind: listEntryHeader, Text: "Available"},
		rowEntry("Anthropic", "", 4),
	}

	if got := selectedEntryRow(entries, 4); got != 4 {
		t.Fatalf("selectedEntryRow(..., 4) = %d, want 4", got)
	}
	if got := selectedEntryRow(entries, 99); got != len(entries)-1 {
		t.Fatalf("selectedEntryRow(..., 99) = %d, want %d", got, len(entries)-1)
	}
}
