package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleUsage(ctx context.Context) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	report, err := d.daemon.SessionUsage(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	info := usageInfoData(report)
	return Result{Handled: true, Info: &info}, nil
}

func usageInfoData(report core.UsageReport) InfoData {
	return InfoData{
		Title: "Token Usage",
		Text:  usageInfoText(report),
		Rows:  usageInfoRows(report.Summary),
	}
}

func usageInfoText(report core.UsageReport) string {
	rows := usageInfoRows(report.Summary)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, row.Label+": "+row.Value)
	}
	return strings.Join(lines, "\n")
}

func usageInfoRows(summary core.UsageSummary) []InfoRow {
	return []InfoRow{
		{Label: "Runs", Value: fmt.Sprintf("%d", summary.Runs)},
		{Label: "Steps", Value: fmt.Sprintf("%d", summary.Steps)},
		{Label: "Prompt", Value: usageTokenLabel(summary.PromptTokens)},
		{Label: "Cache read", Value: usageTokenLabel(summary.CacheReadTokens)},
		{Label: "Cache write", Value: usageTokenLabel(summary.CacheWriteTokens)},
		{Label: "Cache hit", Value: cacheHitLabel(summary)},
		{Label: "Output", Value: usageTokenLabel(summary.OutputTokens)},
		{Label: "Reasoning", Value: usageTokenLabel(summary.ReasoningTokens)},
	}
}

// cacheHitLabel is the share of prompt tokens the provider read from its cache.
func cacheHitLabel(summary core.UsageSummary) string {
	if summary.PromptTokens <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d%%", summary.CacheReadTokens*100/summary.PromptTokens)
}

func usageTokenLabel(value int64) string {
	return formatShortNumber(int(value)) + " tokens"
}
