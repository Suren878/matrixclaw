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

var (
	contextRoot           = verb(CommandContext, "", 0)
	contextInfo           = verb(CommandContext, "info", 0)
	contextClear          = verb(CommandContext, "clear", 0)
	contextClearConfirm   = verb(CommandContext, "clear confirm", 0)
	contextCompact        = verb(CommandContext, "compact", 0)
	contextCompactConfirm = verb(CommandContext, "compact confirm", 0)
)

// CompactRequested reports whether command compacts the context now, which
// may outlast a client's usual wait.
func CompactRequested(command string) bool {
	route, ok := DecodeRoute(command)
	return ok && route.Is(contextCompactConfirm)
}

func (d *Dispatcher) handleContext(ctx context.Context, r Route) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return textResult("Select or create a session first."), nil
	}
	switch r.Verb {
	case contextInfo:
		report, err := d.daemon.SessionContext(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		return Result{Screen: contextInfoData(report)}, nil
	case contextClear:
		return Result{Screen: ConfirmData{
			Message:        "Clear context now?",
			ConfirmLabel:   "Clear",
			CancelLabel:    "Close",
			ConfirmCommand: contextClearConfirm.command(),
			CancelCommand:  contextRoot.command(),
			ConfirmDanger:  true,
		}}, nil
	case contextClearConfirm:
		if _, err := d.daemon.ClearContext(ctx, session.ID); err != nil {
			return Result{}, err
		}
		return Result{Screen: TextData{Text: "Context cleared."}, ReloadSnapshot: true}, nil
	case contextCompact:
		return Result{Screen: ConfirmData{
			Message:        "Compact context now?",
			ConfirmLabel:   "Compact",
			CancelLabel:    "Close",
			ConfirmCommand: contextCompactConfirm.command(),
			CancelCommand:  contextRoot.command(),
		}}, nil
	case contextCompactConfirm:
		result, err := d.daemon.CompactSession(ctx, session.ID)
		if errors.Is(err, core.ErrInvalidInput) {
			return textResult("Nothing to compact yet."), nil
		}
		if err != nil {
			return Result{}, err
		}
		return Result{Screen: TextData{Text: result.Message.Content}, ReloadSnapshot: true}, nil
	}
	report, err := d.daemon.SessionContext(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: NewPickerData(PickerContext, "Context").Items(contextItems(report)...).Build()}, nil
}

func contextItems(report core.ContextReport) []PickerItem {
	return []PickerItem{
		{ID: "clear", Title: "Clear context", Command: contextClear.command(), Role: PickerItemRoleDanger},
		{ID: "compact", Title: "Compact", Command: contextCompact.command()},
		{ID: "info", Title: "Usage", Info: contextTokenLabel(report), Command: contextInfo.command()},
	}
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
