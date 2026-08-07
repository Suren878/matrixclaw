package model

import "testing"

func TestNonEmptyHighlightRangeAcrossItems(t *testing.T) {
	if !nonEmptyHighlightRange(2, 0, 0, 3, 0, 0) {
		t.Fatal("selection across different chat items must not be treated as empty")
	}
	if nonEmptyHighlightRange(2, 0, 0, 2, 0, 0) {
		t.Fatal("selection at the same position in one chat item must be empty")
	}
	if nonEmptyHighlightRange(-1, 0, 0, 2, 0, 1) {
		t.Fatal("selection with an invalid start item must be empty")
	}
}
