package textutil

import (
	"strings"
	"testing"
)

func TestFirstNonEmptySkipsBlankAndTrims(t *testing.T) {
	if got := FirstNonEmpty("", "  ", " b ", "c"); got != "b" {
		t.Fatalf("FirstNonEmpty = %q, want %q", got, "b")
	}
	if got := FirstNonEmpty(" ", ""); got != "" {
		t.Fatalf("FirstNonEmpty(blank) = %q, want empty", got)
	}
}

func TestNonBlankTrimsAndDropsBlank(t *testing.T) {
	got := NonBlank(" a ", "", "  ", "b", "a")
	if strings.Join(got, ",") != "a,b,a" {
		t.Fatalf("NonBlank = %q", got)
	}
}

func TestUniqueNonBlankKeepsFirstOccurrence(t *testing.T) {
	got := UniqueNonBlank(" b", "a", "", "b ", "a")
	if strings.Join(got, ",") != "b,a" {
		t.Fatalf("UniqueNonBlank = %q", got)
	}
	if got := UniqueNonBlank(); got == nil {
		t.Fatal("UniqueNonBlank() = nil, want empty slice")
	}
}
