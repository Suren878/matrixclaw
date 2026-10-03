package textutil

import "testing"

func TestFirstNonEmptySkipsBlankAndTrims(t *testing.T) {
	if got := FirstNonEmpty("", "  ", " b ", "c"); got != "b" {
		t.Fatalf("FirstNonEmpty = %q, want %q", got, "b")
	}
	if got := FirstNonEmpty(" ", ""); got != "" {
		t.Fatalf("FirstNonEmpty(blank) = %q, want empty", got)
	}
}
