package core

import (
	"context"
	"sort"
	"strings"
)

const defaultClientSnapshotMessageLimit = 50

type ClientSnapshot struct {
	SessionID             string                   `json:"session_id"`
	Session               *Session                 `json:"session,omitempty"`
	Capabilities          *SessionCapabilities     `json:"capabilities,omitempty"`
	Context               *ContextReport           `json:"context,omitempty"`
	Plan                  *SessionPlan             `json:"plan,omitempty"`
	Messages              []Message                `json:"messages"`
	Run                   *Run                     `json:"run,omitempty"`
	Timing                *RunTiming               `json:"timing,omitempty"`
	ToolUpdates           []ToolUpdate             `json:"tool_updates,omitempty"`
	Approvals             []Approval               `json:"approvals,omitempty"`
	ApprovalNotifications []PermissionNotification `json:"approval_notifications,omitempty"`
	Files                 []FileSnapshot           `json:"files,omitempty"`
}

func (c *Core) ClientSnapshot(ctx context.Context, client string, externalKey string) (ClientSnapshot, error) {
	binding, err := c.CurrentBinding(ctx, client, externalKey)
	if err != nil {
		return ClientSnapshot{}, err
	}

	snapshot := ClientSnapshot{SessionID: binding.SessionID}
	if strings.TrimSpace(binding.SessionID) == "" {
		return snapshot, nil
	}

	session, err := c.store.GetSession(ctx, binding.SessionID)
	if err != nil {
		return ClientSnapshot{}, err
	}
	session = c.decorateSessionLLM(session)
	snapshot.Session = &session
	capabilities := CapabilitiesForSession(session)
	snapshot.Capabilities = &capabilities
	if plan, err := c.store.GetSessionPlan(ctx, binding.SessionID); err != nil {
		return ClientSnapshot{}, err
	} else {
		snapshot.Plan = &plan
	}

	approvals, err := c.store.ListApprovals(ctx, binding.SessionID, "")
	if err != nil {
		return ClientSnapshot{}, err
	}
	files, err := c.store.ListFileSnapshots(ctx, binding.SessionID)
	if err != nil {
		return ClientSnapshot{}, err
	}
	messages, err := c.store.ListMessages(ctx, binding.SessionID, c.clientSnapshotMessageLimit())
	if err != nil {
		return ClientSnapshot{}, err
	}

	snapshot.Files = files
	snapshot.Messages = messages
	context := c.contextReport(binding.SessionID, messages)
	snapshot.Context = &context
	snapshot.Approvals, snapshot.ToolUpdates, snapshot.ApprovalNotifications = deriveClientSnapshotToolState(approvals, messages)
	if len(messages) == 0 {
		return snapshot, nil
	}

	lastRunID := strings.TrimSpace(messages[len(messages)-1].RunID)
	if lastRunID == "" {
		return snapshot, nil
	}
	run, err := c.store.GetRun(ctx, lastRunID)
	if err != nil {
		return ClientSnapshot{}, err
	}
	snapshot.Run = &run
	timing := deriveRunTiming(run, approvals, messages, c.now().UTC())
	snapshot.Timing = &timing
	return snapshot, nil
}

func (c *Core) clientSnapshotMessageLimit() int {
	if c == nil || c.historyLimit <= 0 {
		return defaultClientSnapshotMessageLimit
	}
	return c.historyLimit
}

func deriveClientSnapshotToolState(approvals []Approval, messages []Message) ([]Approval, []ToolUpdate, []PermissionNotification) {
	updates := make(map[string]ToolUpdate)
	pendingApprovals := make([]Approval, 0, len(approvals))
	notifications := make([]PermissionNotification, 0, len(approvals))

	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolCall == nil {
				continue
			}
			toolCallID := strings.TrimSpace(part.ToolCall.ID)
			if toolCallID == "" {
				continue
			}
			update := updates[toolCallID]
			update.ToolCallID = toolCallID
			update.ToolName = firstNonEmpty(update.ToolName, strings.TrimSpace(part.ToolCall.Name))
			update.RunID = firstNonEmpty(update.RunID, strings.TrimSpace(message.RunID))
			update.SessionID = firstNonEmpty(update.SessionID, strings.TrimSpace(message.SessionID))
			if update.State == "" {
				update.State = ToolLifecycleRequested
			}
			updates[toolCallID] = update
		}
	}

	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			toolCallID := strings.TrimSpace(part.ToolResult.ToolCallID)
			if toolCallID == "" {
				continue
			}
			update := updates[toolCallID]
			update.ToolCallID = toolCallID
			update.ToolName = firstNonEmpty(update.ToolName, strings.TrimSpace(part.ToolResult.Name))
			update.RunID = firstNonEmpty(update.RunID, strings.TrimSpace(message.RunID))
			update.SessionID = firstNonEmpty(update.SessionID, strings.TrimSpace(message.SessionID))
			update.ResultMessageID = strings.TrimSpace(message.ID)
			update.ResultStatus = normalizeToolResultStatus(part.ToolResult.Status, part.ToolResult.IsError)
			if update.ResultStatus == "error" {
				update.State = ToolLifecycleFailed
				update.Error = strings.TrimSpace(part.ToolResult.Content)
			} else {
				update.State = ToolLifecycleCompleted
				update.Error = ""
			}
			updates[toolCallID] = update
		}
	}

	for _, approval := range approvals {
		toolCallID := strings.TrimSpace(approval.ToolCallRef)
		switch approval.State {
		case ApprovalStatePending:
			pendingApprovals = append(pendingApprovals, approval)
			if toolCallID != "" {
				update := updates[toolCallID]
				update.ToolCallID = toolCallID
				update.ToolName = firstNonEmpty(update.ToolName, strings.TrimSpace(approval.ToolName))
				update.RunID = firstNonEmpty(update.RunID, strings.TrimSpace(approval.RunID))
				update.SessionID = firstNonEmpty(update.SessionID, strings.TrimSpace(approval.SessionID))
				update.ApprovalID = strings.TrimSpace(approval.ID)
				update.State = ToolLifecycleWaitingApproval
				updates[toolCallID] = update
			}
		case ApprovalStateApproved:
			if toolCallID == "" {
				continue
			}
			notifications = append(notifications, PermissionNotification{
				ApprovalID: strings.TrimSpace(approval.ID),
				ToolCallID: toolCallID,
				Granted:    true,
			})
			update := updates[toolCallID]
			update.ToolCallID = toolCallID
			update.ToolName = firstNonEmpty(update.ToolName, strings.TrimSpace(approval.ToolName))
			update.RunID = firstNonEmpty(update.RunID, strings.TrimSpace(approval.RunID))
			update.SessionID = firstNonEmpty(update.SessionID, strings.TrimSpace(approval.SessionID))
			update.ApprovalID = strings.TrimSpace(approval.ID)
			if update.State == "" || update.State == ToolLifecycleRequested || update.State == ToolLifecycleWaitingApproval {
				update.State = ToolLifecycleRequested
				update.Error = ""
			}
			updates[toolCallID] = update
		case ApprovalStateRejected:
			if toolCallID == "" {
				continue
			}
			notifications = append(notifications, PermissionNotification{
				ApprovalID: strings.TrimSpace(approval.ID),
				ToolCallID: toolCallID,
				Denied:     true,
			})
			update := updates[toolCallID]
			update.ToolCallID = toolCallID
			update.ToolName = firstNonEmpty(update.ToolName, strings.TrimSpace(approval.ToolName))
			update.RunID = firstNonEmpty(update.RunID, strings.TrimSpace(approval.RunID))
			update.SessionID = firstNonEmpty(update.SessionID, strings.TrimSpace(approval.SessionID))
			update.ApprovalID = strings.TrimSpace(approval.ID)
			if update.State == "" || update.State == ToolLifecycleRequested || update.State == ToolLifecycleWaitingApproval {
				update.State = ToolLifecycleFailed
				update.Error = "approval denied"
			}
			updates[toolCallID] = update
		}
	}

	toolUpdates := make([]ToolUpdate, 0, len(updates))
	for _, update := range updates {
		toolUpdates = append(toolUpdates, update)
	}

	sort.Slice(pendingApprovals, func(i, j int) bool {
		if pendingApprovals[i].Path == pendingApprovals[j].Path {
			return pendingApprovals[i].ID < pendingApprovals[j].ID
		}
		return pendingApprovals[i].Path < pendingApprovals[j].Path
	})
	sort.Slice(toolUpdates, func(i, j int) bool {
		if toolUpdates[i].ToolCallID == toolUpdates[j].ToolCallID {
			return toolUpdates[i].ToolName < toolUpdates[j].ToolName
		}
		return toolUpdates[i].ToolCallID < toolUpdates[j].ToolCallID
	})
	sort.Slice(notifications, func(i, j int) bool {
		if notifications[i].ToolCallID == notifications[j].ToolCallID {
			return notifications[i].ApprovalID < notifications[j].ApprovalID
		}
		return notifications[i].ToolCallID < notifications[j].ToolCallID
	})

	return pendingApprovals, toolUpdates, notifications
}

func normalizeToolResultStatus(status string, isError bool) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "success", "error", "neutral":
		return status
	}
	if isError {
		return "error"
	}
	return "success"
}

func firstNonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
