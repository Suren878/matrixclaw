package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const (
	maxRunRecoveryAttempts            = 8
	runRecoveryApprovalSource         = "daemon_restart_recovery"
	runInterruptionPersistenceTimeout = 5 * time.Second
)

type interruptedToolCall struct {
	Message transcript.Message
	Call    transcript.ToolCallPart
}

type runRecoveryApprovalParams struct {
	Source     string `json:"source"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
}

func (c *Core) RecoverActiveRuns(ctx context.Context) error {
	if c == nil || c.store == nil {
		return nil
	}
	runs, err := c.store.ListActiveRuns(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, run := range runs {
		if run.Status == RunStatusWaitingEvents {
			if err := c.wakeWaitingRun(ctx, run.SessionID, run.ID); err != nil {
				errs = append(errs, fmt.Errorf("wake waiting run %s: %w", run.ID, err))
			}
			continue
		}
		start, err := c.prepareInactiveRunForRecovery(ctx, run.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("recover interrupted run %s: %w", run.ID, err))
			continue
		}
		if start {
			if err := c.startRun(ctx, run.ID); err != nil {
				errs = append(errs, fmt.Errorf("restart recovered run %s: %w", run.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

// prepareInactiveRunForRecovery readies a run the previous daemon left active
// and reports whether to start it. It holds the session gate, as the API may
// already be starting runs of the session.
func (c *Core) prepareInactiveRunForRecovery(ctx context.Context, runID string) (bool, error) {
	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return false, err
	}
	gate := c.sessionGate(run.SessionID)
	gate.Lock()
	defer gate.Unlock()

	if c.runIsActive(run.ID) {
		return false, nil
	}
	run, err = c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	shouldRecover, err := c.runNeedsCrashRecovery(ctx, run)
	if err != nil {
		return false, err
	}
	if !shouldRecover {
		return run.Status == RunStatusAccepted, nil
	}
	return c.prepareRunAfterCrash(ctx, &run)
}

// prepareClaimedRun reports whether a run about to execute should: a run kept
// running for recovery after an interruption is recovered first, a run waiting
// for approvals executes once none is pending.
func (c *Core) prepareClaimedRun(ctx context.Context, runID string) (bool, error) {
	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return false, err
	}
	gate := c.sessionGate(run.SessionID)
	gate.Lock()
	defer gate.Unlock()

	run, err = c.store.GetRun(ctx, run.ID)
	if err != nil {
		return false, err
	}
	switch run.Status {
	case RunStatusAccepted:
		return true, nil
	case RunStatusRunning:
		return c.prepareRunAfterCrash(ctx, &run)
	case RunStatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, run.SessionID, run.ID)
		if err != nil {
			return false, err
		}
		return !pending, nil
	case RunStatusWaitingEvents:
		// A woken run parks again at once when nothing it waits for arrived.
		return true, nil
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return false, nil
	default:
		return false, fmt.Errorf("core: unsupported run status %q", run.Status)
	}
}

func (c *Core) runNeedsCrashRecovery(ctx context.Context, run Run) (bool, error) {
	switch run.Status {
	case RunStatusRunning:
		return true, nil
	case RunStatusWaitingApproval:
		pending, err := c.runHasPendingApprovals(ctx, run.SessionID, run.ID)
		return !pending, err
	case RunStatusAccepted:
		if _, ok, err := c.runCheckpoint(ctx, run.ID); err != nil || ok {
			return ok, err
		}
		calls, err := c.incompleteToolCalls(ctx, run.SessionID, run.ID)
		return len(calls) > 0, err
	default:
		return false, nil
	}
}

func (c *Core) prepareRunAfterCrash(ctx context.Context, run *Run) (bool, error) {
	if run == nil {
		return false, nil
	}
	previous, _, err := c.runCheckpoint(ctx, run.ID)
	if err != nil {
		return false, err
	}
	checkpoint, err := c.markRunRecovery(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if checkpoint.RecoveryCount > maxRunRecoveryAttempts {
		return false, c.transition(ctx, run, runChange{To: RunStatusFailed, Err: fmt.Sprintf("run stopped after %d daemon-restart recovery attempts", checkpoint.RecoveryCount-1)})
	}

	messages, err := c.store.ListRunMessages(ctx, run.SessionID, run.ID)
	if err != nil {
		return false, err
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return false, err
	}
	if CoreSessionIsExternalAgent(session) {
		// Tool parts emitted by Codex/Claude belong to the external runtime. They
		// must never be replayed through MatrixClaw's native tool registry.
		if err := c.markLatestAssistantInterrupted(ctx, run.ID, messages, true); err != nil {
			return false, err
		}
		if err := c.handBack(ctx, run); err != nil {
			return false, err
		}
		return true, nil
	}
	approvals, err := c.store.ListRunApprovals(ctx, run.SessionID, run.ID)
	if err != nil {
		return false, err
	}

	// Every call of a batch in flight is settled: read-only ones and those the last
	// batch checkpoint names not started are deferred for the resumed run, mutating
	// ones ask again, asked ones keep waiting; a running child keeps the parent waiting.
	notStarted := map[string]bool{}
	if previous.Batch != nil {
		for _, id := range previous.Batch.DeferredIDs {
			notStarted[id] = true
		}
	}
	waitApproval, waitSubagent := false, false
	for _, interrupted := range incompleteToolCallsForRun(messages, run.ID) {
		if notStarted[interrupted.Call.ID] {
			if err := c.deferInterruptedCall(ctx, interrupted); err != nil {
				return false, err
			}
			continue
		}
		disposition, err := c.recoverInterruptedTool(ctx, *run, interrupted, approvals)
		if err != nil {
			return false, err
		}
		switch disposition {
		case recoveryToolWaitApproval:
			waitApproval = true
		case recoveryToolWaitSubagent:
			waitSubagent = true
		}
	}
	if waitSubagent {
		return false, nil
	}
	if waitApproval {
		if run.Status == RunStatusWaitingApproval {
			return false, nil
		}
		return false, c.transition(ctx, run, runChange{To: RunStatusWaitingApproval})
	}

	if err := c.markLatestPartialAssistantInterrupted(ctx, run.ID, messages); err != nil {
		return false, err
	}
	if err := c.handBack(ctx, run); err != nil {
		return false, err
	}
	return true, nil
}

// handBack leaves a recovered run accepted, ready to execute again.
func (c *Core) handBack(ctx context.Context, run *Run) error {
	if run.Status == RunStatusAccepted {
		return nil
	}
	return c.transition(ctx, run, runChange{To: RunStatusAccepted})
}

type recoveryToolDisposition int

const (
	recoveryToolContinue recoveryToolDisposition = iota
	recoveryToolWaitApproval
	recoveryToolWaitSubagent
)

func (c *Core) recoverInterruptedTool(ctx context.Context, run Run, interrupted interruptedToolCall, approvals []Approval) (recoveryToolDisposition, error) {
	call := interrupted.Call
	if latest, ok := latestApprovalForToolCall(approvals, run.ID, call.ID); ok {
		_, bridged := decodeSubagentApprovalBridge(latest)
		switch {
		case latest.State == ApprovalStatePending:
			return recoveryToolWaitApproval, nil
		case latest.State == ApprovalStateRejected && !bridged:
			// The resumed run reads the denial as the call's result.
			return recoveryToolContinue, nil
		}
	}

	if task, err := c.store.GetSubagentTaskByParentToolCall(ctx, run.SessionID, run.ID, call.ID); err == nil {
		if task.Mode == SubagentTaskModeBlocking {
			childRun, childErr := c.store.GetRun(ctx, task.ChildRunID)
			if childErr != nil && !errors.Is(childErr, ErrNotFound) {
				return recoveryToolContinue, childErr
			}
			if childErr == nil && childRun.Status == RunStatusWaitingApproval {
				mirrored, err := c.mirrorPendingSubagentApproval(ctx, task)
				switch {
				case err != nil:
					return recoveryToolContinue, err
				case mirrored:
					return recoveryToolWaitApproval, nil
				}
				return recoveryToolWaitSubagent, nil
			}
			if childErr == nil && !childRun.Status.Terminal() {
				return recoveryToolWaitSubagent, nil
			}
		}
		return recoveryToolContinue, c.replayInterruptedTool(ctx, run, interrupted)
	} else if !errors.Is(err, ErrNotFound) {
		return recoveryToolContinue, err
	}

	var spec tools.Spec
	known := false
	if c.tools != nil {
		spec, known = c.tools.Spec(call.Name)
	}
	if !known {
		return recoveryToolContinue, c.finishUnknownInterruptedTool(ctx, run, interrupted)
	}
	if !spec.Mutates() {
		return recoveryToolContinue, c.deferInterruptedCall(ctx, interrupted)
	}
	if _, err := c.ensureRunRecoveryApproval(ctx, run, interrupted, approvals); err != nil {
		return recoveryToolContinue, err
	}
	return recoveryToolWaitApproval, nil
}

func (c *Core) replayInterruptedTool(ctx context.Context, run Run, interrupted interruptedToolCall) error {
	result, err := c.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:   run.SessionID,
		RunID:       run.ID,
		ToolName:    interrupted.Call.Name,
		Client:      run.Client,
		ExternalKey: run.ExternalKey,
		ToolCallID:  interrupted.Call.ID,
		Approved:    true,
		Args:        json.RawMessage(interrupted.Call.Input),
	})
	if err != nil && result.ToolResultMessage == nil {
		return err
	}
	return nil
}

// deferInterruptedCall marks a call deferred, so the resumed run starts it
// through its permission check.
func (c *Core) deferInterruptedCall(ctx context.Context, interrupted interruptedToolCall) error {
	message := interrupted.Message
	message.Parts = slices.Clone(message.Parts)
	for i, part := range message.Parts {
		if part.ToolCall != nil && part.ToolCall.ID == interrupted.Call.ID {
			call := *part.ToolCall
			call.Deferred = true
			message.Parts[i].ToolCall = &call
		}
	}
	message.UpdatedAt = c.now().UTC()
	if err := c.store.UpdateMessage(ctx, message); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: message.SessionID, RunID: message.RunID, Payload: message})
	return nil
}

func (c *Core) finishUnknownInterruptedTool(ctx context.Context, run Run, interrupted interruptedToolCall) error {
	prepared := preparedToolCall{
		SessionID:  run.SessionID,
		RunID:      run.ID,
		ToolName:   interrupted.Call.Name,
		ToolCallID: interrupted.Call.ID,
		Message:    interrupted.Message,
	}
	_, _, err := c.finishToolCall(ctx, prepared, ExecuteToolInput{Args: json.RawMessage(interrupted.Call.Input)}, tools.Result{
		Content: "The daemon restarted while this tool was active. Its completion state is unknown, and MatrixClaw did not replay the unavailable tool.",
		Status:  tools.ResultStatusError,
		IsError: true,
	})
	return err
}

func (c *Core) ensureRunRecoveryApproval(ctx context.Context, run Run, interrupted interruptedToolCall, approvals []Approval) (Approval, error) {
	for _, approval := range approvals {
		if approval.ToolCallRef != interrupted.Call.ID || approval.State != ApprovalStatePending {
			continue
		}
		return approval, nil
	}
	params := runRecoveryApprovalParams{
		Source:     runRecoveryApprovalSource,
		ToolCallID: interrupted.Call.ID,
		ToolName:   interrupted.Call.Name,
	}
	request := &tools.ApprovalRequest{
		ToolID:      interrupted.Call.Name,
		ToolCallID:  interrupted.Call.ID,
		Action:      "retry_after_daemon_restart",
		Description: fmt.Sprintf("The daemon restarted while %s may have been executing. Retry this mutating tool? MatrixClaw will not replay it without confirmation.", firstNonEmpty(interrupted.Call.Name, "a tool")),
		Params:      params,
		Suggestion:  c.suggestRule(ctx, run.SessionID, interrupted.Call.Name, json.RawMessage(interrupted.Call.Input)),
	}
	prepared := preparedToolCall{
		SessionID:  run.SessionID,
		RunID:      run.ID,
		ToolName:   interrupted.Call.Name,
		ToolCallID: interrupted.Call.ID,
		Message:    interrupted.Message,
	}
	_, approval, pending, err := c.createPendingApproval(ctx, prepared, ExecuteToolInput{}, tools.Result{Approval: request}, nil)
	if err != nil {
		return Approval{}, err
	}
	if !pending || approval == nil {
		return Approval{}, errors.New("core: recovery approval was not created")
	}
	return *approval, nil
}

func latestApprovalForToolCall(approvals []Approval, runID string, toolCallID string) (Approval, bool) {
	runID = normalizeText(runID)
	toolCallID = normalizeText(toolCallID)
	var latest Approval
	found := false
	for _, approval := range approvals {
		if normalizeText(approval.RunID) != runID || normalizeText(approval.ToolCallRef) != toolCallID {
			continue
		}
		if !found || approval.RequestedAt.After(latest.RequestedAt) {
			latest = approval
			found = true
		}
	}
	return latest, found
}

// incompleteToolCalls lists the run's tool calls that have no result yet.
func (c *Core) incompleteToolCalls(ctx context.Context, sessionID string, runID string) ([]interruptedToolCall, error) {
	messages, err := c.store.ListRunMessages(ctx, sessionID, runID)
	if err != nil {
		return nil, err
	}
	return incompleteToolCallsForRun(messages, runID), nil
}

func incompleteToolCallsForRun(messages []transcript.Message, runID string) []interruptedToolCall {
	runID = normalizeText(runID)
	completed := toolResultCallIDs(messages)
	seen := map[string]struct{}{}
	var calls []interruptedToolCall
	for _, message := range messages {
		if normalizeText(message.RunID) != runID {
			continue
		}
		for _, part := range message.Parts {
			if part.ToolCall == nil || part.ToolCall.Deferred {
				continue
			}
			id := normalizeText(part.ToolCall.ID)
			if id == "" {
				continue
			}
			if _, ok := completed[id]; ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			calls = append(calls, interruptedToolCall{Message: message, Call: *part.ToolCall})
		}
	}
	return calls
}

func (c *Core) markLatestPartialAssistantInterrupted(ctx context.Context, runID string, messages []transcript.Message) error {
	return c.markLatestAssistantInterrupted(ctx, runID, messages, false)
}

func (c *Core) markLatestAssistantInterrupted(ctx context.Context, runID string, messages []transcript.Message, allowToolParts bool) error {
	runID = normalizeText(runID)
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if normalizeText(message.RunID) != runID {
			continue
		}
		if message.Role != transcript.MessageRoleAssistant {
			return nil
		}
		if (!allowToolParts && messageHasToolPart(message)) || transcript.HasFinishReason(message, "") {
			return nil
		}
		if strings.TrimSpace(message.Content) == "" && len(message.Parts) == 0 {
			return nil
		}
		appendDaemonRestartFinish(&message)
		message.UpdatedAt = c.now().UTC()
		if err := c.store.UpdateMessage(ctx, message); err != nil {
			return err
		}
		c.publishEvent(Event{Type: EventMessageUpdated, SessionID: message.SessionID, RunID: message.RunID, Payload: message})
		return nil
	}
	return nil
}

func appendDaemonRestartFinish(message *transcript.Message) {
	if message == nil || transcript.HasFinishReason(*message, transcript.FinishReasonDaemonRestart) {
		return
	}
	message.Parts = transcript.NormalizeMessageParts(message.Content, message.Parts)
	message.Parts = append(message.Parts, transcript.MessagePart{
		Kind: transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{
			Reason:  transcript.FinishReasonDaemonRestart,
			Message: "Generation was interrupted by a daemon restart; a recovered continuation follows.",
		},
	})
}

func (c *Core) preserveRunForRecovery(ctx context.Context, run Run, assistant *transcript.Message, assistantSaved bool) error {
	if assistant != nil && (assistantSaved || strings.TrimSpace(assistant.Content) != "" || len(assistant.Parts) > 0) {
		appendDaemonRestartFinish(assistant)
		if err := c.sealReply(ctx, assistant, assistantSaved); err != nil {
			return err
		}
	}

	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if latest.Status.Terminal() {
		c.clearRunCheckpoint(ctx, run.ID)
		return nil
	}
	return c.saveRunCheckpoint(ctx, run.ID)
}

func messageHasToolPart(message transcript.Message) bool {
	for _, part := range message.Parts {
		if part.ToolCall != nil || part.ToolResult != nil {
			return true
		}
	}
	return false
}
