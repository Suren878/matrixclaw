package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleContext(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.contextRuntime == nil {
		return unsupportedRuntime("context"), nil
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

	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "info":
		report, err := d.contextRuntime.SessionContext(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		info := contextInfoData(report)
		return Result{Handled: true, Info: &info}, nil
	case "compact":
		return Result{
			Handled: true,
			Confirm: &ConfirmData{
				Message:        "Compact context now?",
				ConfirmLabel:   "Compact",
				CancelLabel:    "Cancel",
				ConfirmCommand: contextCompactConfirmCommand(),
				CancelCommand:  contextCommand(),
			},
		}, nil
	case "compact confirm":
		result, err := d.contextRuntime.CompactSession(ctx, session.ID)
		if errors.Is(err, core.ErrInvalidInput) {
			return Result{Handled: true, Text: "Nothing to compact yet."}, nil
		}
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: result.Message.Content, ReloadSnapshot: true}, nil
	}

	report, err := d.contextRuntime.SessionContext(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerContext, "Context").Context(session.ID).HideBack(true).Items(contextItems(report)...).Ptr(),
	}, nil
}

func contextItems(report core.ContextReport) []PickerItem {
	info := contextTokenLabel(report)
	items := []PickerItem{
		{ID: "info", Title: "Usage", Info: info, Command: contextInfoCommand()},
		{ID: "compact", Title: "Compact", Command: contextCompactCommand()},
		CloseItem(),
	}
	return items
}

func contextInfoData(report core.ContextReport) InfoData {
	rows := []InfoRow{
		{Label: "Total", Value: contextTokenLabel(report)},
		{Label: "Messages", Value: fmt.Sprintf("%d", report.MessageCount)},
	}
	for _, block := range report.Blocks {
		rows = append(rows, InfoRow{
			Label: contextBlockTitle(block.Kind),
			Value: "~" + formatShortNumber(block.TokenEstimate) + " tokens",
		})
	}
	if report.LastProviderUsage != nil {
		rows = append(rows, InfoRow{Label: "Last Provider Usage", Value: providerUsageLabel(*report.LastProviderUsage)})
	}
	rows = append(rows, InfoRow{Label: "Compact", Value: compactInfo(report.Compact)})
	if reason := strings.TrimSpace(report.Compact.Reason); reason != "" {
		rows = append(rows, InfoRow{Label: "Reason", Value: reason})
	}
	return InfoData{
		Title:        "Context Usage",
		Text:         contextInfoText(report),
		Rows:         rows,
		CloseCommand: contextCommand(),
	}
}

func contextInfoText(report core.ContextReport) string {
	lines := []string{
		"Context: " + contextTokenLabel(report),
		fmt.Sprintf("Messages: %d", report.MessageCount),
	}
	for _, block := range report.Blocks {
		lines = append(lines, fmt.Sprintf("- %s: ~%s tokens", contextBlockTitle(block.Kind), formatShortNumber(block.TokenEstimate)))
	}
	if report.LastProviderUsage != nil {
		lines = append(lines, "Last provider usage: "+providerUsageLabel(*report.LastProviderUsage))
	}
	lines = append(lines, "Compact: "+compactInfo(report.Compact))
	if strings.TrimSpace(report.Compact.Reason) != "" {
		lines = append(lines, report.Compact.Reason)
	}
	return strings.Join(lines, "\n")
}

func contextBlockTitle(kind core.ContextBlockKind) string {
	switch kind {
	case core.ContextBlockSystemPrompt:
		return "System prompt"
	case core.ContextBlockCustomInstructions:
		return "User prompt"
	case core.ContextBlockCompactSummary:
		return "Compact summary"
	case core.ContextBlockMessages:
		return "Messages"
	case core.ContextBlockToolSchemas:
		return "Tool schemas"
	default:
		return string(kind)
	}
}

func compactInfo(compact core.ContextCompact) string {
	if compact.Recommended {
		return "recommended"
	}
	return "not needed"
}

func contextWindowLabel(tokens int) string {
	return formatShortNumber(tokens)
}

func contextTokenLabel(report core.ContextReport) string {
	used := "~" + formatShortNumber(report.TokenEstimate)
	if report.WindowTokens <= 0 {
		return used + " tokens"
	}
	return used + " / " + contextWindowLabel(report.WindowTokens) + " tokens"
}

func providerUsageLabel(usage core.ProviderUsage) string {
	if usage.TotalTokens > 0 {
		return fmt.Sprintf("%s total", formatShortNumber(int(usage.TotalTokens)))
	}
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		return fmt.Sprintf("%s in / %s out", formatShortNumber(int(usage.InputTokens)), formatShortNumber(int(usage.OutputTokens)))
	}
	return "reported"
}

func formatShortNumber(value int) string {
	return core.FormatShortNumber(value)
}
