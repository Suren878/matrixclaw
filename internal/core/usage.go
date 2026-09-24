package core

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
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
func (c *Core) recordRunStep(ctx context.Context, step agent.Step) {
	runID := normalizeText(step.RunID)
	if runID == "" || c.store == nil {
		return
	}
	row := RunStep{
		RunID:            runID,
		Model:            step.Model,
		Provider:         step.Provider,
		PromptTokens:     step.Usage.PromptTokens,
		CacheReadTokens:  step.Usage.CacheReadTokens,
		CacheWriteTokens: step.Usage.CacheWriteTokens,
		OutputTokens:     step.Usage.OutputTokens,
		ReasoningTokens:  step.Usage.ReasoningTokens,
		StopReason:       step.StopReason,
		LatencyMillis:    step.Latency.Milliseconds(),
		ToolCalls:        step.ToolCalls,
		CreatedAt:        c.now().UTC(),
	}
	if err := c.store.SaveRunStep(context.WithoutCancel(ctx), row); err != nil {
		log.Printf("core: record step of run %q: %v", runID, err)
	}
}

func generationStopReason(response providers.Response) string {
	return string(providers.ResolveStopReason(response.StopReason, len(response.ToolCalls)))
}
