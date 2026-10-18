package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// recover settles what an interrupted execution of the run left: its unfinished
// reply is sealed, so the model never reads it again, and each call left
// without a result whose approval is not pending or denied is deferred to run
// again, asked about or answered, as Tools.Recovery says.
func (r *run) recover(ctx context.Context) error {
	if err := r.sealInterruptedReply(ctx); err != nil {
		return err
	}
	decisions, err := r.Inbox.Decisions(ctx)
	if err != nil {
		return err
	}
	waits := map[string]bool{}
	for _, decision := range decisions {
		waits[decision.ToolCallID] = decision.Pending || decision.Denied
	}
	for _, req := range r.incompleteCalls() {
		if waits[req.id] {
			continue
		}
		recovery, err := r.Tools.Recovery(ctx, req.name, r.toolCall(req))
		if err != nil {
			return err
		}
		switch recovery.Settle {
		case SettleRerun:
			err = r.deferAgain(ctx, req)
		case SettleAsk:
			err = r.Approvals.Request(ctx, Pending{RunID: r.task.RunID, SessionID: r.task.SessionID, ToolCallID: req.id, ToolName: req.name, Request: recovery.Request})
		case SettleAnswer:
			err = r.finishCall(ctx, req, recovery.Result, true)
		default:
			err = fmt.Errorf("agent: unknown settle %q for call %s", recovery.Settle, req.id)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// incompleteCalls lists the run's calls, in call order, that are neither
// deferred nor answered.
func (r *run) incompleteCalls() []callRequest {
	seen := map[string]bool{}
	var out []callRequest
	for _, message := range r.history.all() {
		if message.RunID != r.task.RunID {
			continue
		}
		for _, part := range message.Parts {
			call := part.ToolCall
			if call == nil || call.Deferred || call.ID == "" || seen[call.ID] || r.history.hasResult(call.ID) {
				continue
			}
			seen[call.ID] = true
			out = append(out, callRequest{id: call.ID, name: call.Name, args: json.RawMessage(call.Input), workingDir: r.task.WorkingDir})
		}
	}
	return out
}

// sealInterruptedReply marks the run's newest message, when it is a reply that
// was cut off while streaming, interrupted by the restart.
func (r *run) sealInterruptedReply(ctx context.Context) error {
	messages := r.history.all()
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.RunID != r.task.RunID {
			continue
		}
		if message.Role != transcript.MessageRoleAssistant || transcript.HasFinishReason(message, "") || hasToolParts(message) || strings.TrimSpace(message.Content) == "" {
			return nil
		}
		return r.sealDropped(ctx, message, "")
	}
	return nil
}

// deferAgain marks a call held back, so it runs through Authorize again once
// nothing of the run is pending.
func (r *run) deferAgain(ctx context.Context, req callRequest) error {
	existing, _ := r.history.message(req.id)
	message := r.callMessage(req, false)
	message.Parts[0].ToolCall.Deferred = true
	message.CreatedAt = existing.CreatedAt
	return r.history.finish(ctx, message)
}

// callRequest is the run's journaled call with the ID, to run it again.
func (r *run) callRequest(callID string) (callRequest, bool) {
	message, ok := r.history.callMessage(callID)
	if !ok {
		return callRequest{}, false
	}
	for _, part := range message.Parts {
		if call := part.ToolCall; call != nil && call.ID == callID {
			return callRequest{id: call.ID, name: call.Name, args: json.RawMessage(call.Input), workingDir: r.task.WorkingDir}, true
		}
	}
	return callRequest{}, false
}

func hasToolParts(message transcript.Message) bool {
	for _, part := range message.Parts {
		if part.ToolCall != nil || part.ToolResult != nil {
			return true
		}
	}
	return false
}
