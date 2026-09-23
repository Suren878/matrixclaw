package core

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

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
	summary := UsageSummary{SessionID: strings.TrimSpace(sessionID), Runs: len(records)}
	for _, record := range records {
		summary.Steps += record.Steps
		summary.PromptTokens += record.PromptTokens
		summary.CacheReadTokens += record.CacheReadTokens
		summary.CacheWriteTokens += record.CacheWriteTokens
		summary.OutputTokens += record.OutputTokens
		summary.ReasoningTokens += record.ReasoningTokens
	}
	return summary
}

func (c *Core) RunSteps(ctx context.Context, runID string) ([]RunStep, error) {
	runID = normalizeText(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: run id is required", ErrInvalidInput)
	}
	return c.store.ListRunSteps(ctx, runID)
}

// recordRunStep stores one generation of a run. A failed write is logged and
// never fails the run.
func (c *Core) recordRunStep(ctx context.Context, runID string, response providers.Response, stopReason string, latency time.Duration) {
	runID = normalizeText(runID)
	if runID == "" || c.store == nil {
		return
	}
	step := RunStep{
		RunID:            runID,
		Model:            response.Model,
		Provider:         response.Provider,
		PromptTokens:     response.Usage.PromptTokens,
		CacheReadTokens:  response.Usage.CacheReadTokens,
		CacheWriteTokens: response.Usage.CacheWriteTokens,
		OutputTokens:     response.Usage.OutputTokens,
		ReasoningTokens:  response.Usage.ReasoningTokens,
		StopReason:       stopReason,
		LatencyMillis:    latency.Milliseconds(),
		ToolCalls:        len(response.ToolCalls),
		CreatedAt:        c.now().UTC(),
	}
	if err := c.store.SaveRunStep(context.WithoutCancel(ctx), step); err != nil {
		log.Printf("core: record step of run %q: %v", runID, err)
	}
}

func generationStopReason(response providers.Response) string {
	if len(response.ToolCalls) > 0 {
		return "tool_use"
	}
	return "end_turn"
}
