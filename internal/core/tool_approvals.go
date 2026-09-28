package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// ResolveApproval records the user's decision on an approval. A run resumes once
// none of its approvals is pending and reads a denial as the call's result; a
// call made outside a run is replayed or answered here.
func (c *Core) ResolveApproval(ctx context.Context, approvalID string, decision ApprovalResolveRequest) (Approval, error) {
	approval, err := c.store.GetApproval(ctx, normalizeText(approvalID))
	if err != nil {
		return Approval{}, err
	}
	if approval.State != ApprovalStatePending {
		if approval.State == approvalState(decision.Approved) {
			return approval, nil
		}
		return Approval{}, fmt.Errorf("%w: approval already resolved", ErrInvalidInput)
	}
	bridge, bridged := decodeSubagentApprovalBridge(approval)
	approval, err = c.recordApprovalDecision(ctx, approval, decision, bridged)
	if err != nil {
		return Approval{}, err
	}
	switch {
	case bridged:
		return approval, c.passDecisionToSubagent(ctx, bridge, decision)
	case strings.TrimSpace(approval.RunID) == "":
		return approval, c.finishRunlessApproval(ctx, approval)
	default:
		return approval, c.resumeDecidedRun(ctx, approval.SessionID, approval.RunID)
	}
}

func approvalState(approved bool) ApprovalState {
	if approved {
		return ApprovalStateApproved
	}
	return ApprovalStateRejected
}

// recordApprovalDecision stores the decision and tells clients where the call
// stands; a bridged call keeps going whichever way its child's call was decided.
func (c *Core) recordApprovalDecision(ctx context.Context, approval Approval, decision ApprovalResolveRequest, bridged bool) (Approval, error) {
	decidedAt := c.now().UTC()
	approval.State = approvalState(decision.Approved)
	approval.DecidedAt = &decidedAt
	if !decision.Approved {
		approval.Reason = normalizeText(decision.Reason)
	}
	if err := c.store.UpdateApproval(ctx, approval); err != nil {
		return Approval{}, err
	}
	c.publishEvent(Event{
		Type:      EventApprovalResult,
		SessionID: approval.SessionID,
		RunID:     approval.RunID,
		Payload: PermissionNotification{
			ApprovalID: approval.ID,
			ToolCallID: approval.ToolCallRef,
			Granted:    decision.Approved,
			Denied:     !decision.Approved,
		},
	})
	update := ToolUpdate{
		ToolCallID: approval.ToolCallRef,
		ToolName:   approval.ToolName,
		State:      ToolLifecycleRequested,
		RunID:      approval.RunID,
		SessionID:  approval.SessionID,
		ApprovalID: approval.ID,
	}
	if !decision.Approved && !bridged {
		update.State = ToolLifecycleFailed
		update.Error = agent.DenialResult(approval.Reason).Content
	}
	c.publishToolUpdate(approval.SessionID, approval.RunID, update)
	return approval, nil
}

// resumeDecidedRun starts a parked run once none of its approvals is pending. A run
// still finishing its step is left to its own check after parking; the session
// gate makes that check and a decision start the run once.
func (c *Core) resumeDecidedRun(ctx context.Context, sessionID string, runID string) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	run, err := c.store.GetRun(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil || run.Status != RunStatusWaitingApproval {
		return err
	}
	pending, err := c.runHasPendingApprovals(ctx, sessionID, runID)
	if err != nil || pending {
		return err
	}
	return c.startRun(ctx, runID)
}

// finishRunlessApproval completes a call made outside a run (API, voice, MCP
// server): a grant replays it, a denial becomes its result.
func (c *Core) finishRunlessApproval(ctx context.Context, approval Approval) error {
	if approval.State == ApprovalStateApproved {
		_, err := c.replayApprovedTool(ctx, approval)
		return err
	}
	return c.finishApprovalCall(ctx, approval, agent.DenialResult(approval.Reason))
}

// passDecisionToSubagent hands the decision to the child's own approval: the
// child runs its call or reads the denial and goes on, while the parent keeps
// waiting for the child.
func (c *Core) passDecisionToSubagent(ctx context.Context, bridge subagentApprovalBridgeParams, decision ApprovalResolveRequest) error {
	task, err := c.store.GetSubagentTask(ctx, bridge.TaskID)
	if err != nil {
		task, err = c.store.GetSubagentTaskByChildRun(ctx, bridge.ChildRunID)
	}
	if err != nil {
		return err
	}
	if _, err := c.ResolveApproval(ctx, bridge.ChildApprovalID, decision); err != nil {
		terminal, terminalErr := c.subagentTaskTerminal(ctx, task)
		if terminalErr != nil || !terminal {
			return err
		}
	}
	if latest, err := c.store.GetSubagentTask(ctx, task.ID); err == nil {
		task = latest
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if !subagentTaskTerminalStatus(task.Status) {
		if task, err = c.markSubagentTaskRunning(ctx, task); err != nil {
			return err
		}
	}
	if task.Mode == SubagentTaskModeAsync {
		return nil
	}
	return c.resumeParentAfterSubagentTerminal(ctx, task)
}

// finishApprovalCall writes result for the approval's call unless it has one.
func (c *Core) finishApprovalCall(ctx context.Context, approval Approval, result tools.Result) error {
	toolCallID := strings.TrimSpace(approval.ToolCallRef)
	done, err := c.store.HasToolResult(ctx, approval.SessionID, toolCallID)
	if err != nil || done {
		return err
	}
	toolCall, err := c.sessionToolCallMessage(ctx, approval.SessionID, toolCallID)
	if err != nil {
		return err
	}
	args, _ := toolCallArgs(toolCall)
	prepared := preparedToolCall{
		SessionID:  approval.SessionID,
		RunID:      approval.RunID,
		ToolName:   approval.ToolName,
		ToolCallID: toolCallID,
		Message:    toolCall,
	}
	_, _, err = c.finishToolCall(ctx, prepared, ExecuteToolInput{Args: args}, result)
	return err
}

func (c *Core) ListApprovals(ctx context.Context, sessionID string, state ApprovalState) ([]Approval, error) {
	return c.store.ListApprovals(ctx, normalizeText(sessionID), state)
}

func workingDirForApprovalResume(sessionWorkingDir string, spec tools.Spec, approvalPath string) string {
	if dir := normalizeWorkingDir(sessionWorkingDir); dir != "" {
		return dir
	}
	if spec.IsFilesystemMutation() {
		if path := normalizeWorkingDir(approvalPath); path != "" {
			return filepath.Dir(path)
		}
	}
	return normalizeWorkingDir(approvalPath)
}

func (c *Core) replayApprovedTool(ctx context.Context, approval Approval) (ExecuteToolResult, error) {
	session, err := c.store.GetSession(ctx, approval.SessionID)
	if err != nil {
		return ExecuteToolResult{}, err
	}

	toolCall, err := c.sessionToolCallMessage(ctx, approval.SessionID, approval.ToolCallRef)
	if err != nil {
		return ExecuteToolResult{}, err
	}
	args, found := toolCallArgs(toolCall)
	if !found {
		return ExecuteToolResult{}, fmt.Errorf("%w: tool call %s", ErrNotFound, toolCall.ID)
	}

	var spec tools.Spec
	if c.tools != nil {
		spec, _ = c.tools.Spec(approval.ToolName)
	}
	client := ""
	externalKey := ""
	if strings.TrimSpace(approval.RunID) != "" {
		if run, runErr := c.store.GetRun(ctx, approval.RunID); runErr == nil {
			client = run.Client
			externalKey = run.ExternalKey
		}
	}

	return c.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:   approval.SessionID,
		RunID:       approval.RunID,
		ToolName:    approval.ToolName,
		Client:      client,
		ExternalKey: externalKey,
		ToolCallID:  approval.ToolCallRef,
		WorkingDir:  workingDirForApprovalResume(session.WorkingDir, spec, approval.Path),
		Approved:    true,
		Args:        args,
	})
}

// sessionToolCallMessage loads the assistant message that carries toolCallID.
func (c *Core) sessionToolCallMessage(ctx context.Context, sessionID string, toolCallID string) (transcript.Message, error) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return transcript.Message{}, fmt.Errorf("%w: tool call id is required", ErrInvalidInput)
	}
	message, err := c.store.GetMessage(ctx, toolCallID)
	if err == nil && message.SessionID != sessionID {
		err = ErrNotFound
	}
	if errors.Is(err, ErrNotFound) {
		return transcript.Message{}, fmt.Errorf("%w: tool call %s", ErrNotFound, toolCallID)
	}
	return message, err
}

// toolCallArgs returns the input of the tool call part whose id is the message id.
func toolCallArgs(message transcript.Message) (json.RawMessage, bool) {
	for _, part := range message.Parts {
		if part.ToolCall == nil || strings.TrimSpace(part.ToolCall.ID) != message.ID {
			continue
		}
		if strings.TrimSpace(part.ToolCall.Input) == "" {
			return nil, true
		}
		return json.RawMessage(part.ToolCall.Input), true
	}
	return nil, false
}

func approvalsForRun(approvals []Approval, runID string) []Approval {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil
	}
	matched := make([]Approval, 0, len(approvals))
	for _, approval := range approvals {
		if approval.RunID != runID {
			continue
		}
		matched = append(matched, approval)
	}
	return matched
}

func (c *Core) runHasPendingApprovals(ctx context.Context, sessionID string, runID string) (bool, error) {
	approvals, err := c.store.ListApprovals(ctx, sessionID, ApprovalStatePending)
	if err != nil {
		return false, err
	}
	for _, approval := range approvals {
		if strings.TrimSpace(approval.RunID) == strings.TrimSpace(runID) {
			return true, nil
		}
	}
	return false, nil
}
