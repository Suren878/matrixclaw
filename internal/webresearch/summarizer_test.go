package webresearch

import (
	"context"
	"strings"
	"testing"
)

func TestDeterministicSummarizerExtractsRatingAndReviewCount(t *testing.T) {
	summarizer := NewDeterministicSummarizer()
	out, err := summarizer.Summarize(context.Background(), SummaryInput{
		Task:  "Find hotel rating and review count",
		Query: "hotel rating reviews",
		Sources: []SourceDocument{{
			Source: Source{ID: "src1", Title: "Example Hotel"},
			Text:   "Example Hotel is rated 4.6 out of 5 based on 1,234 reviews from travelers.",
		}},
	})
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if len(out.Facts) != 1 {
		t.Fatalf("facts = %d, want 1", len(out.Facts))
	}
	claim := out.Facts[0].Claim
	for _, want := range []string{"Example Hotel", "rating 4.6/5", "1,234 reviews"} {
		if !strings.Contains(claim, want) {
			t.Fatalf("claim %q missing %q", claim, want)
		}
	}
	if out.Facts[0].Confidence < 0.8 {
		t.Fatalf("confidence = %v, want deterministic high confidence", out.Facts[0].Confidence)
	}
}
