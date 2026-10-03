package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

// recover settles what an interrupted execution of the run left: its unfinished
// reply is sealed, so the model never reads it again, and each interrupted call
// is deferred to run again, asked about or answered, as the task says.
func (r *run) recover(ctx context.Context) error {
	if err := r.sealInterruptedReply(ctx); err != nil {
		return err
	}
	for _, interrupted := range r.task.Interrupted {
		message, ok := r.history.callMessage(interrupted.ToolCallID)
		if !ok || message.RunID != r.task.RunID || r.history.hasResult(interrupted.ToolCallID) {
			continue
		}
		req, ok := callRequestOf(message, interrupted.ToolCallID, r.task.WorkingDir)
		if !ok {
			continue
		}
		var err error
		switch interrupted.Settle {
		case SettleRerun:
			err = r.deferAgain(ctx, req)
		case SettleAsk:
			err = r.Approvals.Request(ctx, Pending{RunID: r.task.RunID, SessionID: r.task.SessionID, ToolCallID: req.id, ToolName: req.name, Request: interrupted.Request})
		case SettleAnswer:
			err = r.finishCall(ctx, req, interrupted.Result)
		default:
			err = fmt.Errorf("agent: unknown settle %q for call %s", interrupted.Settle, req.id)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
		message.Parts = append(transcript.NormalizeMessageParts(message.Content, message.Parts), transcript.MessagePart{
			Kind:   transcript.MessagePartKindFinish,
			Finish: &transcript.FinishPart{Reason: transcript.FinishReasonDaemonRestart, Message: "Generation was interrupted by a daemon restart; a recovered continuation follows."},
		})
		message.UpdatedAt = r.Now()
		return r.history.finish(ctx, message)
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

func callRequestOf(message transcript.Message, callID string, workingDir string) (callRequest, bool) {
	for _, part := range message.Parts {
		if call := part.ToolCall; call != nil && call.ID == callID {
			return callRequest{id: call.ID, name: call.Name, args: json.RawMessage(call.Input), workingDir: workingDir}, true
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
