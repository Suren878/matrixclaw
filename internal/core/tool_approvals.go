package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/permission"
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
	if decision.Approved && decision.Always == permission.ScopeGlobal && decision.Restricted {
		return Approval{}, fmt.Errorf("%w: keep a rule for every session", ErrOwnerOnly)
	}
	if decision.Approved && decision.Always != "" {
		if err := c.keepSuggestedRule(ctx, approval, decision.Always); err != nil {
			return Approval{}, err
		}
	}
	approval, err = c.recordApprovalDecision(ctx, approval, decision)
	if err != nil {
		return Approval{}, err
	}
	if strings.TrimSpace(approval.RunID) == "" {
		return approval, c.finishRunlessApproval(ctx, approval)
	}
	return approval, c.resumeDecidedRun(ctx, approval.SessionID, approval.RunID)
}

// keepSuggestedRule saves the approval's suggested rule as an allow rule; a
// session rule belongs to the approval's session, the parent's for a subagent's.
func (c *Core) keepSuggestedRule(ctx context.Context, approval Approval, scope permission.Scope) error {
	if !scope.Valid() {
		return fmt.Errorf("%w: unknown rule scope %q", ErrInvalidInput, scope)
	}
	if approval.Suggestion == nil {
		return fmt.Errorf("%w: approval %s suggests no rule", ErrInvalidInput, approval.ID)
	}
	rule := permission.Rule{
		ID:        c.newID("rule"),
		Tool:      approval.Suggestion.Tool,
		Pattern:   approval.Suggestion.Pattern,
		Effect:    permission.Allow,
		Scope:     scope,
		CreatedAt: c.now().UTC(),
	}
	if scope == permission.ScopeSession {
		rule.SessionID, _ = c.approvalAudience(ctx, approval)
	}
	return c.store.CreatePermissionRule(ctx, rule)
}

func approvalState(approved bool) ApprovalState {
	if approved {
		return ApprovalStateApproved
	}
	return ApprovalStateRejected
}

// recordApprovalDecision stores the decision and tells clients where the call
// stands.
func (c *Core) recordApprovalDecision(ctx context.Context, approval Approval, decision ApprovalResolveRequest) (Approval, error) {
	decidedAt := c.now().UTC()
	approval.State = approvalState(decision.Approved)
	approval.DecidedAt = &decidedAt
	if !decision.Approved {
		approval.Reason = approvalReason(decision.Reason)
	}
	if err := c.store.UpdateApproval(ctx, approval); err != nil {
		return Approval{}, err
	}
	audience, audienceRun := c.approvalAudience(ctx, approval)
	c.publishEvent(Event{
		Type:      EventApprovalResult,
		SessionID: audience,
		RunID:     audienceRun,
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
	if !decision.Approved {
		update.State = ToolLifecycleFailed
		update.Error = agent.DenialResult(approval.Reason).Content
	}
	c.publishToolUpdate(approval.SessionID, approval.RunID, update)
	return approval, nil
}

// maxApprovalReasonRunes caps the denial reason the model reads as a result.
const maxApprovalReasonRunes = 2000

func approvalReason(reason string) string {
	reason = normalizeText(reason)
	runes := []rune(reason)
	if len(runes) <= maxApprovalReasonRunes {
		return reason
	}
	return strings.TrimSpace(string(runes[:maxApprovalReasonRunes-1])) + "…"
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
	return c.startDecidedRun(ctx, run)
}

// startDecidedRun starts a run waiting for approval once none is pending; the
// caller holds the session gate.
func (c *Core) startDecidedRun(ctx context.Context, run Run) error {
	pending, err := c.runHasPendingApprovals(ctx, run.SessionID, run.ID)
	if err != nil || pending {
		return err
	}
	return c.startRun(ctx, run.ID)
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

// finishApprovalCall writes result for the approval's call unless it has one.
func (c *Core) finishApprovalCall(ctx context.Context, approval Approval, result tools.Result) error {
	toolCallID := strings.TrimSpace(approval.ToolCallRef)
	done, err := c.store.HasToolResult(ctx, approval.SessionID, approval.RunID, toolCallID)
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

// replayApprovedTool runs a call made outside a run once it was granted.
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
	return c.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  approval.SessionID,
		ToolName:   approval.ToolName,
		ToolCallID: approval.ToolCallRef,
		WorkingDir: workingDirForApprovalResume(session.WorkingDir, spec, approval.Path),
		Approved:   true,
		Args:       args,
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

// runHasPendingApprovals reports whether one of the run's approvals is pending.
func (c *Core) runHasPendingApprovals(ctx context.Context, sessionID string, runID string) (bool, error) {
	approvals, err := c.store.ListRunApprovals(ctx, sessionID, runID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(approvals, func(approval Approval) bool { return approval.State == ApprovalStatePending }), nil
}
