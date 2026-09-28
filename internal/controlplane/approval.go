package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

const approvalUsage = "Usage: /approval deny <approval id> [reason]"

// DenyWithReasonPrompt asks for the reason a client sends with a denial.
func DenyWithReasonPrompt(approvalID string) PromptData {
	return PromptData{
		Title:               "Why deny it?",
		Placeholder:         "Reason the agent reads (optional)",
		SubmitCommandPrefix: controlplaneCommand("approval", "deny", approvalID) + " ",
	}
}

// handleApproval answers "/approval deny <id> [reason]": the call is denied and
// the model reads the reason as its result.
func (d *Dispatcher) handleApproval(ctx context.Context, args string) (Result, error) {
	if d.approvals == nil {
		return unsupportedRuntime("approval"), nil
	}
	action, rest := cutWord(args)
	approvalID, reason := cutWord(rest)
	if !strings.EqualFold(action, "deny") || approvalID == "" {
		return Result{Handled: true, Text: approvalUsage}, nil
	}
	approval, err := d.approvals.ResolveApproval(ctx, approvalID, core.ApprovalResolveRequest{Reason: reason})
	if err != nil {
		return Result{}, err
	}
	text := "❌ Denied " + firstNonEmptyTrimmed(approval.ToolName, "the call")
	if approval.Reason != "" {
		text += ": " + approval.Reason
	}
	return Result{Handled: true, Text: text, ReloadSnapshot: true}, nil
}

// cutWord splits text into its first word and the trimmed rest.
func cutWord(text string) (string, string) {
	word, rest, _ := strings.Cut(strings.TrimSpace(text), " ")
	return word, strings.TrimSpace(rest)
}
