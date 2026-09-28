package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	for _, run := range runs {
		start, err := c.prepareInactiveRunForRecovery(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("recover interrupted run %s: %w", run.ID, err)
		}
		if start {
			if err := c.startRun(ctx, run.ID); err != nil {
				return fmt.Errorf("restart recovered run %s: %w", run.ID, err)
			}
		}
	}
	return nil
}

// prepareInactiveRunForRecovery serializes startup recovery with workflow
// activities for the same session. A persisted go-workflows activity may wake
// before the daemon's startup recovery goroutine; whichever path arrives first
// owns recovery and the other observes the freshly reloaded state.
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

// prepareClaimedRun lets a persisted workflow activity recover its own orphan
// inline. This closes the startup race where the workflow worker can poll an
// old activity before RecoverActiveRuns has reset the durable run to accepted.
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
		messages, err := c.store.ListMessages(ctx, run.SessionID, 0)
		if err != nil {
			return false, err
		}
		return len(incompleteToolCallsForRun(messages, run.ID)) > 0, nil
	default:
		return false, nil
	}
}

func (c *Core) prepareRunAfterCrash(ctx context.Context, run *Run) (bool, error) {
	if run == nil {
		return false, nil
	}
	checkpoint, err := c.markRunRecovery(ctx, run.ID)
	if err != nil {
		return false, err
	}
	if checkpoint.RecoveryCount > maxRunRecoveryAttempts {
		return false, c.failRecoveredRun(ctx, run, fmt.Sprintf("run stopped after %d daemon-restart recovery attempts", checkpoint.RecoveryCount-1))
	}

	messages, err := c.store.ListMessages(ctx, run.SessionID, 0)
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
		if err := c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseRecovering, "", ""); err != nil {
			return false, err
		}
		if err := c.setRunStatus(ctx, run, RunStatusAccepted, ""); err != nil {
			return false, err
		}
		return true, nil
	}
	approvals, err := c.store.ListApprovals(ctx, run.SessionID, "")
	if err != nil {
		return false, err
	}
	for _, approval := range approvalsForRun(approvals, run.ID) {
		if approval.State == ApprovalStatePending {
			if err := c.setRunStatus(ctx, run, RunStatusWaitingApproval, ""); err != nil {
				return false, err
			}
			if err := c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseWaitingApproval, approval.ToolCallRef, approval.ToolName); err != nil {
				return false, err
			}
			return false, nil
		}
	}

	for _, interrupted := range incompleteToolCallsForRun(messages, run.ID) {
		disposition, err := c.recoverInterruptedTool(ctx, *run, interrupted, approvals)
		if err != nil {
			return false, err
		}
		switch disposition {
		case recoveryToolWaitApproval:
			if err := c.setRunStatus(ctx, run, RunStatusWaitingApproval, ""); err != nil {
				return false, err
			}
			return false, nil
		case recoveryToolWaitSubagent:
			if err := c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseWaitingSubagent, interrupted.Call.ID, interrupted.Call.Name); err != nil {
				return false, err
			}
			return false, nil
		}
	}

	if err := c.markLatestPartialAssistantInterrupted(ctx, run.ID, messages); err != nil {
		return false, err
	}
	if err := c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseRecovering, "", ""); err != nil {
		return false, err
	}
	if err := c.setRunStatus(ctx, run, RunStatusAccepted, ""); err != nil {
		return false, err
	}
	return true, nil
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
			if childErr == nil && !subagentRunStatusTerminal(childRun.Status) {
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
		return recoveryToolContinue, c.replayInterruptedTool(ctx, run, interrupted)
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
	for _, approval := range approvalsForRun(approvals, run.ID) {
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
		now := c.now().UTC()
		if assistant.CreatedAt.IsZero() {
			assistant.CreatedAt = now
		}
		assistant.UpdatedAt = now
		if assistantSaved {
			if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
				return err
			}
			c.publishEvent(Event{Type: EventMessageUpdated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
		} else {
			if err := c.store.SaveMessage(ctx, *assistant); err != nil {
				return err
			}
			c.publishEvent(Event{Type: EventMessageCreated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
		}
	}

	latest, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if subagentRunStatusTerminal(latest.Status) {
		c.clearRunCheckpoint(ctx, run.ID)
		return nil
	}
	return c.saveRunCheckpoint(ctx, run.ID, RunCheckpointPhaseRecovering, "", "")
}

func messageHasToolPart(message transcript.Message) bool {
	for _, part := range message.Parts {
		if part.ToolCall != nil || part.ToolResult != nil {
			return true
		}
	}
	return false
}

func (c *Core) failRecoveredRun(ctx context.Context, run *Run, message string) error {
	if run == nil {
		return nil
	}
	finishedAt := c.now().UTC()
	run.Status = RunStatusFailed
	run.Error = strings.TrimSpace(message)
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	c.clearRunCheckpoint(ctx, run.ID)
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *run})
	return nil
}
