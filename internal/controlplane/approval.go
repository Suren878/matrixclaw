package controlplane

import (
	"context"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

const approvalUsage = "Usage: /approval deny <approval id> [reason]"

var approvalDeny = valueVerb(CommandApproval, "deny", 1)

// DenyWithReasonPrompt asks for the reason a client sends with a denial.
func DenyWithReasonPrompt(approvalID string) PromptData {
	return PromptData{
		Title:       "Why deny it?",
		Placeholder: "Reason the agent reads (optional)",
		Submit:      approvalDeny.command(approvalID),
	}
}

// handleApproval answers "/approval deny <id> [reason]": the call is denied and
// the model reads the reason as its result.
func (d *Dispatcher) handleApproval(ctx context.Context, r Route) (Result, error) {
	if !r.Is(approvalDeny) || r.arg(0) == "" {
		return textResult(approvalUsage), nil
	}
	approval, err := d.daemon.ResolveApproval(ctx, r.arg(0), core.ApprovalResolveRequest{Reason: r.Value})
	if err != nil {
		return Result{}, err
	}
	text := "❌ Denied " + textutil.FirstNonEmpty(approval.ToolName, "the call")
	if approval.Reason != "" {
		text += ": " + approval.Reason
	}
	return Result{Screen: TextData{Text: text}, ReloadSnapshot: true}, nil
}

// DeniedApproval returns the approval that a "/approval deny <id> [reason]"
// command, or the command its prompt submits, denies.
func DeniedApproval(command string) (string, bool) {
	route, ok := DecodeRoute(command)
	if !ok || !route.Is(approvalDeny) || route.arg(0) == "" {
		return "", false
	}
	return route.arg(0), true
}
