package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleUsage(ctx context.Context, externalKey string) (Result, error) {
	if d.usage == nil {
		return unsupportedRuntime("usage"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	report, err := d.usage.SessionUsage(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	info := usageInfoData(report)
	return Result{Handled: true, Info: &info}, nil
}

func usageInfoData(report core.UsageReport) InfoData {
	rows := []InfoRow{
		{Label: "Runs", Value: fmt.Sprintf("%d", report.Summary.Runs)},
		{Label: "Input", Value: usageTokenLabel(report.Summary.InputTokens)},
		{Label: "Output", Value: usageTokenLabel(report.Summary.OutputTokens)},
		{Label: "Reasoning", Value: usageTokenLabel(report.Summary.ReasoningTokens)},
		{Label: "Cached", Value: usageTokenLabel(report.Summary.CachedTokens)},
		{Label: "Total", Value: usageTokenLabel(report.Summary.TotalTokens)},
	}
	return InfoData{
		Title: "Token Usage",
		Text:  usageInfoText(report),
		Rows:  rows,
	}
}

func usageInfoText(report core.UsageReport) string {
	lines := []string{
		fmt.Sprintf("Runs: %d", report.Summary.Runs),
		"Input: " + usageTokenLabel(report.Summary.InputTokens),
		"Output: " + usageTokenLabel(report.Summary.OutputTokens),
		"Reasoning: " + usageTokenLabel(report.Summary.ReasoningTokens),
		"Cached: " + usageTokenLabel(report.Summary.CachedTokens),
		"Total: " + usageTokenLabel(report.Summary.TotalTokens),
	}
	return strings.Join(lines, "\n")
}

func usageTokenLabel(value int64) string {
	return formatShortNumber(int(value)) + " tokens"
}
