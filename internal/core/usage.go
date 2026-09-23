package core

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
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

func (c *Core) saveRunUsage(ctx context.Context, run Run, message transcript.Message, usage providers.Usage) {
	if providerUsageIsZero(usage) || c == nil || c.store == nil {
		return
	}
	// run_usage stores one aggregate per run. Rebuild it from durable tool
	// turn metadata rather than incrementing a counter, so retries/recovery
	// cannot double-count a turn or let the final answer overwrite its usage.
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	if history, err := c.store.ListMessages(ctx, run.SessionID, 0); err == nil {
		for _, previous := range history {
			if previous.RunID != run.ID || previous.ID == message.ID || previous.Role != transcript.MessageRoleAssistant {
				continue
			}
			for _, part := range previous.Parts {
				if part.Finish == nil || part.Finish.Reason != "tool_calls" {
					continue
				}
				var details struct {
					Usage ProviderUsage `json:"usage"`
				}
				if json.Unmarshal(part.Finish.Details, &details) != nil {
					continue
				}
				prior := details.Usage
				usage.InputTokens += prior.InputTokens
				usage.OutputTokens += prior.OutputTokens
				usage.CachedTokens += prior.CachedTokens
				usage.ReasoningTokens += prior.ReasoningTokens
				if prior.TotalTokens == 0 {
					prior.TotalTokens = prior.InputTokens + prior.OutputTokens
				}
				usage.TotalTokens += prior.TotalTokens
			}
		}
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
