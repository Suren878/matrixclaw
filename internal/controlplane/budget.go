package controlplane

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

const budgetUsage = "Usage: /budget [steps N | time 2h | tokens N | tokens off | reset]"

func (d *Dispatcher) handleBudget(ctx context.Context, args string) (Result, error) {
	sessionID, err := d.currentSessionID(ctx)
	if err != nil {
		return Result{}, err
	}
	if sessionID == "" {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	report, err := d.daemon.SessionBudget(ctx, sessionID)
	if err != nil {
		return Result{}, err
	}
	if fields := strings.Fields(strings.ToLower(args)); len(fields) > 0 {
		override, ok := budgetOverride(report.Override, fields)
		if !ok {
			return Result{Handled: true, Text: budgetUsage}, nil
		}
		if report, err = d.daemon.UpdateSessionBudget(ctx, sessionID, override); err != nil {
			return Result{}, err
		}
	}
	info := budgetInfoData(report)
	return Result{Handled: true, Info: &info}, nil
}

// budgetOverride applies one /budget change to the session's overrides.
func budgetOverride(current core.SessionBudget, fields []string) (core.SessionBudget, bool) {
	if len(fields) == 1 && fields[0] == "reset" {
		return core.SessionBudget{}, true
	}
	if len(fields) != 2 {
		return current, false
	}
	switch fields[0] {
	case "steps":
		steps, err := strconv.Atoi(fields[1])
		if err != nil || steps < 1 {
			return current, false
		}
		current.Steps = &steps
	case "time":
		duration, err := time.ParseDuration(fields[1])
		if err != nil || duration < time.Minute {
			return current, false
		}
		seconds := int64(duration / time.Second)
		current.ActiveSeconds = &seconds
	case "tokens":
		var tokens int64
		if fields[1] != "off" {
			parsed, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || parsed < 1 {
				return current, false
			}
			tokens = parsed
		}
		current.Tokens = &tokens
	default:
		return current, false
	}
	return current, true
}

func budgetInfoData(report core.SessionBudgetReport) InfoData {
	rows := []InfoRow{
		{Label: "Steps", Value: budgetValue(strconv.Itoa(report.Steps), report.Override.Steps != nil)},
		{Label: "Active time", Value: budgetValue(budgetDuration(time.Duration(report.ActiveSeconds)*time.Second), report.Override.ActiveSeconds != nil)},
		{Label: "Tokens", Value: budgetValue(budgetTokens(report.Tokens), report.Override.Tokens != nil)},
	}
	lines := make([]string, 0, len(rows)+2)
	for _, row := range rows {
		lines = append(lines, row.Label+": "+row.Value)
	}
	lines = append(lines, "", budgetUsage)
	return InfoData{Title: "Run Budget", Text: strings.Join(lines, "\n"), Rows: rows}
}

func budgetValue(value string, session bool) string {
	if session {
		return value + " (session)"
	}
	return value + " (default)"
}

func budgetTokens(tokens int64) string {
	if tokens == 0 {
		return "unlimited"
	}
	return formatShortNumber(int(tokens))
}

// budgetDuration renders whole hours and minutes, such as 4h or 1h30m.
func budgetDuration(d time.Duration) string {
	if d <= 0 {
		return "unlimited"
	}
	text := strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}
