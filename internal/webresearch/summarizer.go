package webresearch

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type DeterministicSummarizer struct {
	now func() time.Time
}

func NewDeterministicSummarizer() *DeterministicSummarizer {
	return &DeterministicSummarizer{now: func() time.Time { return time.Now().UTC() }}
}

func (s *DeterministicSummarizer) Summarize(_ context.Context, input SummaryInput) (SummaryOutput, error) {
	now := time.Now().UTC()
	if s != nil && s.now != nil {
		now = s.now().UTC()
	}
	var facts []Fact
	var answerLines []string
	focus := strings.TrimSpace(input.Task + " " + input.Query)
	for _, doc := range input.Sources {
		source := doc.Source
		claim, confidence := deterministicClaim(focus, source, doc.Text)
		if claim == "" {
			continue
		}
		facts = append(facts, Fact{
			ID:         "",
			Claim:      claim,
			SourceIDs:  []string{source.ID},
			Confidence: confidence,
			CreatedAt:  now,
		})
		if len(answerLines) < 5 {
			answerLines = append(answerLines, "- "+claim)
		}
	}
	if len(answerLines) == 0 {
		return SummaryOutput{
			Answer:  "No reliable facts were extracted from the available web sources.",
			Summary: "No extracted facts.",
			Facts:   nil,
		}, nil
	}
	title := strings.TrimSpace(input.Task)
	if title == "" {
		title = strings.TrimSpace(input.Query)
	}
	if title == "" {
		title = "web research"
	}
	answer := "Research summary for " + title + ":\n" + strings.Join(answerLines, "\n")
	summary := fmt.Sprintf("Extracted %d fact(s) from %d source(s).", len(facts), len(input.Sources))
	return SummaryOutput{Answer: answer, Summary: summary, Facts: facts}, nil
}

func deterministicClaim(focus string, source Source, text string) (string, float64) {
	if claim := ratingReviewClaim(focus, source, text); claim != "" {
		return claim, 0.86
	}
	return firstUsefulClaim(source, text), 0.72
}

var ratingPattern = regexp.MustCompile(`(?i)(?:rating|rated|score)[^0-9]{0,24}([0-9]+(?:\.[0-9]+)?)\s*(?:/|out of)\s*(5|10)`)
var ratingPatternLoose = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(?:/|out of)\s*(5|10)\s*(?:stars?|rating|score)?`)
var reviewsPattern = regexp.MustCompile(`(?i)([0-9][0-9,. ]*)\s+(?:reviews?|ratings?)`)

func ratingReviewClaim(focus string, source Source, text string) string {
	if !wantsRatingReview(focus) {
		return ""
	}
	haystack := strings.Join(strings.Fields(source.Title+" "+source.Snippet+" "+boundText(text, 8000)), " ")
	if haystack == "" {
		return ""
	}
	rating := findRating(haystack)
	reviews := findReviews(haystack)
	if rating == "" && reviews == "" {
		return ""
	}
	var details []string
	if rating != "" {
		details = append(details, "rating "+rating)
	}
	if reviews != "" {
		details = append(details, reviews+" reviews")
	}
	prefix := strings.Join(strings.Fields(source.Title), " ")
	if prefix != "" {
		return trimClaim(prefix + ": " + strings.Join(details, ", "))
	}
	return trimClaim(strings.Join(details, ", "))
}

func wantsRatingReview(focus string) bool {
	focus = strings.ToLower(focus)
	for _, marker := range []string{"rating", "ratings", "review", "reviews", "score", "stars"} {
		if strings.Contains(focus, marker) {
			return true
		}
	}
	return false
}

func findRating(text string) string {
	if match := ratingPattern.FindStringSubmatch(text); len(match) == 3 {
		return match[1] + "/" + match[2]
	}
	if match := ratingPatternLoose.FindStringSubmatch(text); len(match) == 3 {
		return match[1] + "/" + match[2]
	}
	return ""
}

func findReviews(text string) string {
	if match := reviewsPattern.FindStringSubmatch(text); len(match) == 2 {
		return strings.Join(strings.Fields(match[1]), "")
	}
	return ""
}

func firstUsefulClaim(source Source, text string) string {
	title := strings.Join(strings.Fields(source.Title), " ")
	snippet := strings.Join(strings.Fields(source.Snippet), " ")
	text = strings.Join(strings.Fields(text), " ")
	switch {
	case title != "" && snippet != "":
		return trimClaim(title + ": " + snippet)
	case snippet != "":
		return trimClaim(snippet)
	case title != "" && text != "":
		return trimClaim(title + ": " + text)
	case title != "":
		return trimClaim(title)
	case text != "":
		return trimClaim(text)
	default:
		return ""
	}
}

func trimClaim(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const max = 320
	if len(value) <= max {
		return value
	}
	cut := strings.LastIndexAny(value[:max], ".!?;")
	if cut < 120 {
		cut = strings.LastIndex(value[:max], " ")
	}
	if cut < 120 {
		cut = max
	}
	return strings.TrimSpace(value[:cut]) + "..."
}
