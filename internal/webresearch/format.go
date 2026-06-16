package webresearch

import (
	"fmt"
	"strings"
)

func FormatResult(result ResearchResult) string {
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "research_id: %s\n", result.ResearchID)
	_, _ = fmt.Fprintf(&b, "status: %s\n", firstNonEmpty(result.Status, StatusPending))
	if result.Answer != "" {
		b.WriteString("\nanswer:\n")
		b.WriteString(boundText(result.Answer, 2200))
		b.WriteByte('\n')
	}
	if result.Summary != "" {
		b.WriteString("\nsummary: ")
		b.WriteString(boundText(result.Summary, 700))
		b.WriteByte('\n')
	}
	if len(result.Facts) > 0 {
		b.WriteString("\nfacts:\n")
		for i, fact := range boundedFacts(result.Facts) {
			_, _ = fmt.Fprintf(&b, "%d. %s", i+1, fact.Claim)
			if len(fact.SourceIDs) > 0 {
				b.WriteString(" [")
				b.WriteString(strings.Join(fact.SourceIDs, ", "))
				b.WriteString("]")
			}
			b.WriteByte('\n')
		}
	}
	if len(result.Sources) > 0 {
		b.WriteString("\nsources:\n")
		for i, source := range boundedSources(result.Sources) {
			title := firstNonEmpty(source.Title, source.URL)
			_, _ = fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, title, source.URL)
		}
	}
	if len(result.Warnings) > 0 {
		b.WriteString("\nwarnings:\n")
		for _, warning := range boundedStrings(result.Warnings, 8) {
			b.WriteString("- ")
			b.WriteString(warning)
			b.WriteByte('\n')
		}
	}
	if len(result.NextActions) > 0 {
		b.WriteString("\nnext_actions:\n")
		for _, action := range boundedStrings(result.NextActions, 6) {
			b.WriteString("- ")
			b.WriteString(action)
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}
