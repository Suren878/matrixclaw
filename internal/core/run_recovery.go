package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	maxRunRecoveryAttempts            = 8
	runInterruptionPersistenceTimeout = 5 * time.Second
)

// claimedRun is a run its executor may execute; Recovering when a restart or
// an interruption stopped its previous execution, Batch the tool batch that
// execution last checkpointed.
type claimedRun struct {
	Run        Run
	Recovering bool
	Batch      *agent.ToolBatch
}

// claim readies a run for execution under its session gate and reports whether
// to execute it: an ended run, and one still waiting for approval, stay as they
// are; a run found running has no executor, so it was interrupted and is
// recovered, at most maxRunRecoveryAttempts times. Others become running.
func (c *Core) claim(ctx context.Context, runID string) (claimedRun, bool, error) {
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return claimedRun{}, false, err
	}
	gate := c.sessionGate(run.SessionID)
	gate.Lock()
	defer gate.Unlock()
	if run, err = c.store.GetRun(ctx, runID); err != nil || run.Status.Terminal() {
		return claimedRun{}, false, err
	}
	if run.Status == RunStatusWaitingApproval {
		if pending, err := c.runHasPendingApprovals(ctx, run.SessionID, run.ID); err != nil || pending {
			return claimedRun{}, false, err
		}
	}
	if run.Status != RunStatusRunning {
		err := c.transition(ctx, &run, runChange{To: RunStatusRunning})
		if errors.Is(err, ErrRunEnded) {
			return claimedRun{}, false, nil
		}
		return claimedRun{Run: run}, err == nil, err
	}
	checkpoint, err := c.markRunRecovery(ctx, run.ID)
	if err != nil {
		return claimedRun{}, false, err
	}
	if checkpoint.RecoveryCount > maxRunRecoveryAttempts {
		return claimedRun{}, false, c.transition(ctx, &run, runChange{To: RunStatusFailed, Err: fmt.Sprintf("run stopped after %d daemon-restart recovery attempts", checkpoint.RecoveryCount-1)})
	}
	return claimedRun{Run: run, Recovering: true, Batch: checkpoint.Batch}, true, nil
}

// interruptedCalls says how a recovered run settles each of its calls without
// a result: a call waiting for a decision waits on; a call never started (its
// batch checkpoint says so), a read-only one and an agent call that started its
// child run again; an unknown tool's call is answered; any other call changes
// things and is asked about first, as it may have run.
func (c *Core) interruptedCalls(ctx context.Context, run Run, batch *agent.ToolBatch) ([]agent.InterruptedCall, error) {
	messages, err := c.store.ListRunMessages(ctx, run.SessionID, run.ID)
	if err != nil {
		return nil, err
	}
	approvals, err := c.store.ListRunApprovals(ctx, run.SessionID, run.ID)
	if err != nil {
		return nil, err
	}
	var out []agent.InterruptedCall
	for _, call := range incompleteToolCalls(messages, run.ID) {
		if latest, ok := latestApprovalForCall(approvals, call.ID); ok && latest.State != ApprovalStateApproved {
			continue
		}
		settle, err := c.settleInterrupted(ctx, run, call, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, settle)
	}
	return out, nil
}

func (c *Core) settleInterrupted(ctx context.Context, run Run, call transcript.ToolCallPart, batch *agent.ToolBatch) (agent.InterruptedCall, error) {
	rerun := agent.InterruptedCall{ToolCallID: call.ID, Settle: agent.SettleRerun}
	if batch != nil && slices.Contains(batch.DeferredIDs, call.ID) {
		return rerun, nil
	}
	if _, err := c.subagentTaskOfCall(ctx, run.SessionID, run.ID, call.ID); err == nil {
		return rerun, nil
	} else if !errors.Is(err, ErrNotFound) {
		return agent.InterruptedCall{}, err
	}
	var spec tools.Spec
	known := false
	if c.tools != nil {
		spec, known = c.tools.Spec(call.Name)
	}
	switch {
	case !known:
		return agent.InterruptedCall{ToolCallID: call.ID, Settle: agent.SettleAnswer, Result: tools.Result{
			Content: "The daemon restarted while this tool was active. Its completion state is unknown, and MatrixClaw did not replay the unavailable tool.",
			Status:  tools.ResultStatusError,
			IsError: true,
		}}, nil
	case !spec.Mutates():
		return rerun, nil
	}
	return agent.InterruptedCall{ToolCallID: call.ID, Settle: agent.SettleAsk, Request: tools.ApprovalRequest{
		ToolID:      call.Name,
		ToolCallID:  call.ID,
		Action:      "retry_after_daemon_restart",
		Description: fmt.Sprintf("The daemon restarted while %s may have been executing. Retry this mutating tool? MatrixClaw will not replay it without confirmation.", firstNonEmpty(call.Name, "a tool")),
		Suggestion:  c.suggestRule(ctx, run.SessionID, call.Name, []byte(call.Input)),
	}}, nil
}

// latestApprovalForCall is the newest approval the call asked for.
func latestApprovalForCall(approvals []Approval, callID string) (Approval, bool) {
	var latest Approval
	found := false
	for _, approval := range approvals {
		if approval.ToolCallRef == callID && (!found || approval.RequestedAt.After(latest.RequestedAt)) {
			latest, found = approval, true
		}
	}
	return latest, found
}

// incompleteToolCalls lists the run's calls that are neither deferred nor answered.
func incompleteToolCalls(messages []transcript.Message, runID string) []transcript.ToolCallPart {
	answered := toolResultCallIDs(messages)
	seen := map[string]bool{}
	var calls []transcript.ToolCallPart
	for _, message := range messages {
		if message.RunID != runID {
			continue
		}
		for _, part := range message.Parts {
			call := part.ToolCall
			if call == nil || call.Deferred || call.ID == "" || seen[call.ID] {
				continue
			}
			if _, ok := answered[call.ID]; ok {
				continue
			}
			seen[call.ID] = true
			calls = append(calls, *call)
		}
	}
	return calls
}

// sealExternalReply marks the newest reply of an external agent's run, cut off
// by a restart, interrupted.
func (c *Core) sealExternalReply(ctx context.Context, runID string, messages []transcript.Message) error {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.RunID != runID {
			continue
		}
		if message.Role != transcript.MessageRoleAssistant || transcript.HasFinishReason(message, "") || strings.TrimSpace(message.Content) == "" && len(message.Parts) == 0 {
			return nil
		}
		return c.sealInterruptedReply(ctx, &message, true)
	}
	return nil
}

// sealInterruptedReply marks a reply the run's interruption cut off, so a
// resumed run does not read it as its own.
func (c *Core) sealInterruptedReply(ctx context.Context, reply *transcript.Message, saved bool) error {
	if reply == nil || !saved && strings.TrimSpace(reply.Content) == "" && len(reply.Parts) == 0 || transcript.HasFinishReason(*reply, transcript.FinishReasonDaemonRestart) {
		return nil
	}
	reply.Parts = append(transcript.NormalizeMessageParts(reply.Content, reply.Parts), transcript.MessagePart{
		Kind:   transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{Reason: transcript.FinishReasonDaemonRestart, Message: "Generation was interrupted by a daemon restart; a recovered continuation follows."},
	})
	return c.sealReply(ctx, reply, saved)
}

// recoverRuns resumes each run the previous daemon left active: a parked
// run once what it waits for is there, any other one through claim.
func (c *Core) recoverRuns(ctx context.Context) error {
	runs, err := c.store.ListActiveRuns(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, run := range runs {
		switch run.Status {
		case RunStatusWaitingEvents:
			err = c.wakeWaitingRun(ctx, run.SessionID, run.ID)
		case RunStatusWaitingApproval:
			err = c.resumeDecidedRun(ctx, run.SessionID, run.ID)
		default:
			err = c.startRun(ctx, run.ID)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("recover run %s: %w", run.ID, err))
		}
	}
	return errors.Join(errs...)
}

// Recover resumes what the previous daemon left, once, at daemon start: its
// shell tasks are lost; each run it left active resumes; a subagent task whose
// child run ended is told; an idle session starts its next queued message, or
// a run for background work that finished meanwhile.
func (c *Core) Recover(ctx context.Context) error {
	return errors.Join(c.recoverShellTasks(ctx), c.recoverRuns(ctx), c.recoverSubagentTasks(ctx), c.settleIdleSessions(ctx))
}

// settleIdleSessions starts what the sessions with queued messages or unseen
// finished tasks owe them; sessions busy with a run are left to it.
func (c *Core) settleIdleSessions(ctx context.Context) error {
	inputs, err := c.store.ListPendingSessionInputs(ctx, "")
	if err != nil {
		return err
	}
	events, err := c.store.ListTasks(ctx, TaskFilter{Undelivered: true})
	if err != nil {
		return err
	}
	var sessions []string
	for _, input := range inputs {
		sessions = append(sessions, input.SessionID)
	}
	for _, task := range events {
		sessions = append(sessions, task.SessionID)
	}
	var errs []error
	seen := map[string]bool{}
	for _, sessionID := range sessions {
		if seen[sessionID] {
			continue
		}
		seen[sessionID] = true
		started, err := c.startNextPendingSessionInput(ctx, sessionID)
		if err == nil && !started {
			err = c.wakeSession(ctx, sessionID, nil)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", sessionID, err))
		}
	}
	return errors.Join(errs...)
}
