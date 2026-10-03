package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
)

func (d *Dispatcher) handleContext(ctx context.Context, args string) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}

	args = strings.TrimSpace(args)
	switch strings.ToLower(args) {
	case "info":
		report, err := d.daemon.SessionContext(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		info := contextInfoData(report)
		return Result{Handled: true, Info: &info}, nil
	case "clear":
		return Result{
			Handled: true,
			Confirm: &ConfirmData{
				Message:        "Clear context now?",
				ConfirmLabel:   "Clear",
				CancelLabel:    "Close",
				ConfirmCommand: contextClearConfirmCommand(),
				CancelCommand:  contextCommand(),
				ConfirmDanger:  true,
			},
		}, nil
	case "clear confirm":
		if _, err := d.daemon.ClearContext(ctx, session.ID); err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Context cleared.", ReloadSnapshot: true}, nil
	case "compact":
		return Result{
			Handled: true,
			Confirm: &ConfirmData{
				Message:        "Compact context now?",
				ConfirmLabel:   "Compact",
				CancelLabel:    "Close",
				ConfirmCommand: contextCompactConfirmCommand(),
				CancelCommand:  contextCommand(),
			},
		}, nil
	case "compact confirm":
		result, err := d.daemon.CompactSession(ctx, session.ID)
		if errors.Is(err, core.ErrInvalidInput) {
			return Result{Handled: true, Text: "Nothing to compact yet."}, nil
		}
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: result.Message.Content, ReloadSnapshot: true}, nil
	}

	report, err := d.daemon.SessionContext(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Handled: true,
		Picker:  NewPickerData(PickerContext, "Context").Context(session.ID).Items(contextItems(report)...).Ptr(),
	}, nil
}

func contextItems(report core.ContextReport) []PickerItem {
	info := contextTokenLabel(report)
	items := []PickerItem{
		{ID: "clear", Title: "Clear context", Command: contextClearCommand(), Role: PickerItemRoleDanger},
		{ID: "compact", Title: "Compact", Command: contextCompactCommand()},
		{ID: "info", Title: "Usage", Info: info, Command: contextInfoCommand()},
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
		Title: "Context Usage",
		Text:  contextInfoText(report),
		Rows:  rows,
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
	case core.ContextBlockClearMarker:
		return "Clear marker"
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

func providerUsageLabel(usage providers.Usage) string {
	if usage.PromptTokens == 0 && usage.OutputTokens == 0 {
		return "reported"
	}
	prompt := formatShortNumber(int(usage.PromptTokens)) + " in"
	if usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
		prompt += fmt.Sprintf(" (%s cached, %s written)", formatShortNumber(int(usage.CacheReadTokens)), formatShortNumber(int(usage.CacheWriteTokens)))
	}
	return prompt + " / " + formatShortNumber(int(usage.OutputTokens)) + " out"
}

func formatShortNumber(value int) string {
	return agentcontext.FormatShortNumber(value)
}
