package agent

import (
	"context"
	"slices"
)

// settle turns a step into the run's outcome, or reports that the run goes on.
// A run that asked for approval parks only while a decision is still pending.
func (r *run) settle(ctx context.Context, result stepResult) (Outcome, bool, error) {
	if result.assistant != nil && canceled(ctx) {
		return Outcome{Status: StatusCanceled, Assistant: result.assistant, AssistantSaved: result.saved}, true, nil
	}
	if result.err != nil {
		outcome := Outcome{Status: StatusFailed, Err: result.err, StopReason: result.stop}
		if result.markErrored && result.assistant != nil {
			outcome.Assistant, outcome.AssistantSaved, outcome.MarkErrored = result.assistant, result.saved, true
		}
		return outcome, true, nil
	}
	switch result.kind {
	case stepWaitingApproval:
		decisions, err := r.Inbox.Decisions(ctx)
		if err != nil {
			return Outcome{}, true, err
		}
		if !slices.ContainsFunc(decisions, func(d Decision) bool { return d.Pending }) {
			return Outcome{}, false, nil
		}
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingApproval}, true, nil
	case stepWaitingEvents:
		if err := r.checkpoint(ctx, PhaseModel, nil); err != nil {
			return Outcome{}, true, err
		}
		return Outcome{Status: StatusWaitingEvents}, true, nil
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		return Outcome{Status: StatusCompleted, StopReason: result.stopReason(), Assistant: &reply, AssistantSaved: result.saved}, true, nil
	default:
		return Outcome{}, false, nil
	}
}

// interrupted is the outcome of a step its context stopped: what the step
// reached, with the reply to seal, for core to commit or recover from.
func (r *run) interrupted(result stepResult) Outcome {
	outcome := Outcome{Interrupted: true, Assistant: result.assistant, AssistantSaved: result.saved}
	if result.err != nil {
		return outcome
	}
	switch result.kind {
	case stepDone:
		reply := finalReply(*result.assistant, result.response)
		outcome.Status, outcome.Assistant, outcome.StopReason = StatusCompleted, &reply, result.stopReason()
	case stepWaitingApproval:
		outcome.Status = StatusWaitingApproval
	case stepWaitingEvents:
		outcome.Status = StatusWaitingEvents
	}
	return outcome
}
