package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func (c *Core) ResolveApproval(ctx context.Context, approvalID string, approved bool) (Approval, error) {
	approval, err := c.store.GetApproval(ctx, normalizeText(approvalID))
	if err != nil {
		return Approval{}, err
	}
	if approval.State != ApprovalStatePending {
		switch {
		case approved && approval.State == ApprovalStateApproved:
			return approval, nil
		case !approved && approval.State == ApprovalStateRejected:
			return approval, nil
		default:
			return Approval{}, fmt.Errorf("%w: approval already resolved", ErrInvalidInput)
		}
	}
	if bridge, ok := decodeSubagentApprovalBridge(approval); ok {
		return c.resolveSubagentApprovalBridge(ctx, approval, bridge, approved)
	}
	decidedAt := c.now().UTC()
	if approved {
		approval.State = ApprovalStateApproved
	} else {
		approval.State = ApprovalStateRejected
	}
	approval.DecidedAt = &decidedAt
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
			Granted:    approved,
			Denied:     !approved,
		},
	})
	if approved {
		c.publishEvent(Event{
			Type:      EventToolUpdated,
			SessionID: approval.SessionID,
			RunID:     approval.RunID,
			Payload: ToolUpdate{
				ToolCallID: approval.ToolCallRef,
				ToolName:   approval.ToolName,
				State:      ToolLifecycleRequested,
				RunID:      approval.RunID,
				SessionID:  approval.SessionID,
				ApprovalID: approval.ID,
			},
		})
		if strings.TrimSpace(approval.RunID) == "" {
			if _, err := c.replayApprovedTool(ctx, approval); err != nil {
				return Approval{}, err
			}
		} else {
			if err := c.startRun(ctx, approval.RunID); err != nil {
				return Approval{}, err
			}
		}
	} else {
		c.publishEvent(Event{
			Type:      EventToolUpdated,
			SessionID: approval.SessionID,
			RunID:     approval.RunID,
			Payload: ToolUpdate{
				ToolCallID: approval.ToolCallRef,
				ToolName:   approval.ToolName,
				State:      ToolLifecycleFailed,
				RunID:      approval.RunID,
				SessionID:  approval.SessionID,
				ApprovalID: approval.ID,
				Error:      "approval denied",
			},
		})
		if strings.TrimSpace(approval.RunID) != "" {
			run, runErr := c.store.GetRun(ctx, approval.RunID)
			if runErr == nil {
				if failErr := c.failRunByID(ctx, run, errors.New("approval denied")); failErr != nil {
					return Approval{}, failErr
				}
			}
		}
	}
	return approval, nil
}

func (c *Core) resolveSubagentApprovalBridge(ctx context.Context, approval Approval, bridge subagentApprovalBridgeParams, approved bool) (Approval, error) {
	decidedAt := c.now().UTC()
	if approved {
		approval.State = ApprovalStateApproved
	} else {
		approval.State = ApprovalStateRejected
	}
	approval.DecidedAt = &decidedAt
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
			Granted:    approved,
			Denied:     !approved,
		},
	})

	task, err := c.store.GetSubagentTask(ctx, bridge.TaskID)
	if err != nil {
		task, err = c.store.GetSubagentTaskByChildRun(ctx, bridge.ChildRunID)
	}
	if err != nil {
		return Approval{}, err
	}

	if _, err := c.ResolveApproval(ctx, bridge.ChildApprovalID, approved); err != nil {
		terminal, terminalErr := c.subagentTaskTerminal(ctx, task)
		if terminalErr != nil || !terminal {
			return Approval{}, err
		}
	}

	if approved {
		if latest, err := c.store.GetSubagentTask(ctx, task.ID); err == nil {
			task = latest
		} else if !errors.Is(err, ErrNotFound) {
			return Approval{}, err
		}
		if !subagentTaskTerminalStatus(task.Status) {
			task, err = c.markSubagentTaskRunning(ctx, task)
			if err != nil {
				return Approval{}, err
			}
		}
		c.publishEvent(Event{
			Type:      EventToolUpdated,
			SessionID: approval.SessionID,
			RunID:     approval.RunID,
			Payload: ToolUpdate{
				ToolCallID: approval.ToolCallRef,
				ToolName:   approval.ToolName,
				State:      ToolLifecycleRequested,
				RunID:      approval.RunID,
				SessionID:  approval.SessionID,
				ApprovalID: approval.ID,
			},
		})
		if task.Mode == SubagentTaskModeAsync {
			return approval, nil
		}
		if err := c.resumeParentAfterSubagentTerminal(ctx, task); err != nil {
			return Approval{}, err
		}
		return approval, nil
	}

	summary := "Subagent approval denied"
	if bridge.ChildToolName != "" {
		summary += " for " + bridge.ChildToolName
	}
	task, err = c.finishSubagentTaskRecord(ctx, task, SubagentTaskStatusFailed, summary, summary, false)
	if err != nil {
		return Approval{}, err
	}
	if task.Mode == SubagentTaskModeAsync {
		c.publishSubagentToolUpdate(task)
		task, err = c.queueSubagentCompletionRecord(ctx, task)
		if err != nil {
			return Approval{}, err
		}
		if err := c.deliverPendingSubagentCompletionsForParent(ctx, task.ParentSessionID); err != nil {
			return Approval{}, err
		}
		return approval, nil
	}
	if err := c.finishRejectedSubagentDelegateTool(ctx, approval, task, summary); err != nil {
		return Approval{}, err
	}
	if strings.TrimSpace(approval.RunID) != "" {
		if err := c.startRun(ctx, approval.RunID); err != nil {
			return Approval{}, err
		}
	}
	return approval, nil
}

func (c *Core) finishRejectedSubagentDelegateTool(ctx context.Context, approval Approval, task SubagentTask, summary string) error {
	toolCallID := strings.TrimSpace(approval.ToolCallRef)
	done, err := c.store.HasToolResult(ctx, approval.SessionID, toolCallID)
	if err != nil || done {
		return err
	}
	toolCall, err := c.sessionToolCallMessage(ctx, approval.SessionID, toolCallID)
	if err != nil {
		return fmt.Errorf("parent delegate tool call: %w", err)
	}
	args, _ := toolCallArgs(toolCall)
	prepared := preparedToolCall{
		SessionID:  approval.SessionID,
		RunID:      approval.RunID,
		ToolName:   approval.ToolName,
		ToolCallID: approval.ToolCallRef,
		Message:    toolCall,
	}
	_, _, err = c.finishToolCall(ctx, prepared, ExecuteToolInput{Args: args}, tools.Result{
		Content:  summary,
		Metadata: task,
		Status:   tools.ResultStatusError,
		IsError:  true,
	})
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
