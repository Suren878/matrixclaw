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
