package core

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) Usage(ctx context.Context, filter UsageFilter) (UsageReport, error) {
	filter.SessionID = normalizeText(filter.SessionID)
	filter.RunID = normalizeText(filter.RunID)
	records, err := c.store.ListUsageRecords(ctx, filter)
	if err != nil {
		return UsageReport{}, err
	}
	return UsageReport{
		Summary: summarizeUsage(records, filter.SessionID),
		Records: records,
	}, nil
}

func summarizeUsage(records []UsageRecord, sessionID string) UsageSummary {
	summary := UsageSummary{SessionID: strings.TrimSpace(sessionID)}
	seenRuns := map[string]bool{}
	for _, record := range records {
		if record.RunID != "" && !seenRuns[record.RunID] {
			seenRuns[record.RunID] = true
			summary.Runs++
		}
		summary.InputTokens += record.InputTokens
		summary.OutputTokens += record.OutputTokens
		total := record.TotalTokens
		if total == 0 {
			total = record.InputTokens + record.OutputTokens
		}
		summary.TotalTokens += total
		summary.CachedTokens += record.CachedTokens
		summary.ReasoningTokens += record.ReasoningTokens
	}
	return summary
}

func (c *Core) saveRunUsage(ctx context.Context, run Run, message Message, usage providers.Usage) {
	if providerUsageIsZero(usage) || c == nil || c.store == nil {
		return
	}
	record := UsageRecord{
		ID:              c.newID("usage"),
		SessionID:       strings.TrimSpace(run.SessionID),
		RunID:           strings.TrimSpace(run.ID),
		MessageID:       strings.TrimSpace(message.ID),
		Provider:        strings.TrimSpace(message.Provider),
		Model:           strings.TrimSpace(message.Model),
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		TotalTokens:     usage.TotalTokens,
		CachedTokens:    usage.CachedTokens,
		ReasoningTokens: usage.ReasoningTokens,
		ProviderRaw:     string(usage.ProviderRaw),
		CreatedAt:       c.now().UTC(),
	}
	_ = c.store.SaveUsageRecord(ctx, record)
}
