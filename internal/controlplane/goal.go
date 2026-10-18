package controlplane

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

const goalUsage = `Usage: /goal <objective> [--check "go test ./..."] [--max-runs N] [--tokens N]
/goal shows the goal; /goal pause, /goal resume, /goal clear`

func (d *Dispatcher) handleGoal(ctx context.Context, args string) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if !core.CapabilitiesForSession(*session).NativeTools {
		return Result{Handled: true, Text: "Goals are kept by Matrixclaw sessions only."}, nil
	}
	args = strings.TrimSpace(args)
	if args != "" && !d.mayUse(*session) {
		return Result{Handled: true, Text: unattendedRefusal}, nil
	}
	switch strings.ToLower(args) {
	case "":
		goal, err := d.daemon.SessionGoal(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		info := goalInfoData(goal)
		return Result{Handled: true, Info: &info}, nil
	case "pause", "resume":
		goal, err := d.daemon.ChangeSessionGoal(ctx, session.ID, core.GoalAction(strings.ToLower(args)))
		if err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Goal " + string(goal.Status) + ".", ReloadSnapshot: true}, nil
	case "clear":
		return Result{Handled: true, Confirm: &ConfirmData{
			Message:        "Clear the session goal?",
			ConfirmLabel:   "Clear",
			CancelLabel:    "Close",
			ConfirmCommand: "/goal clear confirm",
			CancelCommand:  "/goal",
		}}, nil
	case "clear confirm":
		if err := d.daemon.ClearSessionGoal(ctx, session.ID); err != nil {
			return Result{}, err
		}
		return Result{Handled: true, Text: "Goal cleared.", ReloadSnapshot: true}, nil
	}
	input, ok := parseGoalInput(args)
	if !ok {
		return Result{Handled: true, Text: goalUsage}, nil
	}
	if input.CheckCommand != "" && !d.owner() {
		return Result{Handled: true, Text: "Only the owner can set a check command: it runs without asking."}, nil
	}
	goal, err := d.daemon.SetSessionGoal(ctx, session.ID, input)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: fmt.Sprintf("Goal set; the session works on it for up to %d runs.", goal.MaxRuns), ReloadSnapshot: true}, nil
}

// goalFlags are the options after a goal's objective.
var goalFlags = []string{"--check", "--max-runs", "--tokens"}

// parseGoalInput reads "<objective> [--check CMD] [--max-runs N] [--tokens N]";
// the objective is the text before the first option, and option values may be
// quoted.
func parseGoalInput(args string) (core.GoalInput, bool) {
	objective, options := args, ""
	for _, flag := range goalFlags {
		if i := strings.Index(" "+args, " "+flag); i >= 0 && i < len(objective) {
			objective, options = args[:i], args[i:]
		}
	}
	input := core.GoalInput{Objective: strings.TrimSpace(objective)}
	words, ok := splitQuoted(options)
	if !ok || input.Objective == "" || len(words)%2 != 0 {
		return core.GoalInput{}, false
	}
	for i := 0; i < len(words); i += 2 {
		value := words[i+1]
		switch words[i] {
		case "--check":
			input.CheckCommand = value
		case "--max-runs":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return core.GoalInput{}, false
			}
			input.MaxRuns = n
		case "--tokens":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1 {
				return core.GoalInput{}, false
			}
			input.TokenBudget = n
		default:
			return core.GoalInput{}, false
		}
	}
	return input, true
}

// splitQuoted splits text into words at spaces outside single or double quotes.
func splitQuoted(text string) ([]string, bool) {
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	for _, r := range text {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words, inWord = append(words, word.String()), false
				word.Reset()
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words, quote == 0
}

func goalInfoData(goal *core.SessionGoal) InfoData {
	if goal == nil {
		return InfoData{Title: "Goal", Text: "The session has no goal.\n\n" + goalUsage}
	}
	rows := []InfoRow{
		{Label: "Objective", Value: goal.Objective},
		{Label: "Status", Value: string(goal.Status)},
	}
	if goal.Reason != "" {
		label := "Reason"
		if goal.Status == core.GoalComplete {
			label = "Evidence"
		}
		rows = append(rows, InfoRow{Label: label, Value: goal.Reason})
	}
	if goal.CheckCommand != "" {
		check := goal.CheckCommand
		if goal.LastCheck != "" {
			check += " (last " + goal.LastCheck + ")"
		}
		rows = append(rows, InfoRow{Label: "Check", Value: check})
	}
	rows = append(rows, InfoRow{Label: "Runs", Value: fmt.Sprintf("%d of %d", goal.RunsUsed, goal.MaxRuns)})
	if goal.TokenBudget > 0 {
		rows = append(rows, InfoRow{Label: "Tokens", Value: formatShortNumber(int(goal.TokensUsed)) + " of " + formatShortNumber(int(goal.TokenBudget))})
	}
	if goal.LastStop != "" {
		rows = append(rows, InfoRow{Label: "Last run", Value: goal.LastStop})
	}
	if goal.LastBlocker != "" && goal.Status == core.GoalActive {
		rows = append(rows, InfoRow{Label: "Blocker", Value: fmt.Sprintf("%s (reported %d of 3)", goal.LastBlocker, goal.BlockerRepeats)})
	}
	lines := make([]string, 0, len(rows)+2)
	for _, row := range rows {
		lines = append(lines, row.Label+": "+row.Value)
	}
	lines = append(lines, "", goalUsage)
	return InfoData{Title: "Goal", Text: strings.Join(lines, "\n"), Rows: rows}
}

// GoalLine is a goal in one short line for status displays, "" without one.
func GoalLine(goal *core.SessionGoal) string {
	if goal == nil {
		return ""
	}
	return fmt.Sprintf("Goal %s · run %d/%d", goal.Status, goal.RunsUsed, goal.MaxRuns)
}
