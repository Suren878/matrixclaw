package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
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

func TestClipCutsOnRunesWithTheMarkInsideTheLimit(t *testing.T) {
	for _, tc := range []struct {
		text, mark string
		max        int
		want       string
	}{
		{"short", "…", 10, "short"},
		{"привет мир", "…", 7, "привет…"},
		{"abcdef", "...", 5, "ab..."},
		{"abcdef", "", 3, "abc"},
		{"abcdef", "[cut]", 3, "[cu"},
		{"abcdef", "…", 0, "abcdef"},
		{"🙂🙂🙂", "", 2, "🙂🙂"},
	} {
		got := Clip(tc.text, tc.max, tc.mark)
		if got != tc.want {
			t.Errorf("Clip(%q, %d, %q) = %q, want %q", tc.text, tc.max, tc.mark, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("Clip(%q) = %q is not valid UTF-8", tc.text, got)
		}
	}
}

func TestClipBytesNeverSplitsARune(t *testing.T) {
	if got := ClipBytes("héllo", 2); got != "h" {
		t.Fatalf("ClipBytes = %q, want %q", got, "h")
	}
	if got := ClipBytes("héllo", 3); got != "hé" {
		t.Fatalf("ClipBytes = %q, want %q", got, "hé")
	}
	if got := ClipBytes("abc", 10); got != "abc" {
		t.Fatalf("ClipBytes = %q, want whole text", got)
	}
}
