package agentcontext

import (
	"strings"
	"testing"
)

func TestEstimateCountsOtherScriptsDenserThanLatin(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{strings.Repeat("a", 400), 100},
		{strings.Repeat("я", 400), 160},
		{"abяя", 2},
		{"é", 1},
	} {
		if got := EstimateTextTokens(tc.text); got != tc.want {
			t.Errorf("EstimateTextTokens(%d runes of %q...) = %d, want %d", len([]rune(tc.text)), firstRune(tc.text), got, tc.want)
		}
	}
}

func firstRune(text string) string {
	for _, r := range text {
		return string(r)
	}
	return ""
}

func TestHeadTailKeepsBothEndsWithinTheLimit(t *testing.T) {
	text := "HEAD" + strings.Repeat("x", 100_000) + "TAIL"
	got := HeadTail(text, 1_000)
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || !strings.Contains(got, "tokens omitted") || EstimateTextTokens(got) > 1_000 {
		t.Fatalf("HeadTail = %d tokens: %.60q ... %.60q", EstimateTextTokens(got), got, got[len(got)-60:])
	}
	if got := HeadTail("short", 1_000); got != "short" {
		t.Fatalf("HeadTail of a short text = %q", got)
	}
}
