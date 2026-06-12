package core

import (
	"encoding/json"
	"time"
)

type ApprovalState string

const (
	ApprovalStatePending  ApprovalState = "pending"
	ApprovalStateApproved ApprovalState = "approved"
	ApprovalStateRejected ApprovalState = "rejected"
)

type Approval struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"session_id"`
	RunID       string          `json:"run_id,omitempty"`
	ToolCallRef string          `json:"tool_call_id,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	Description string          `json:"description,omitempty"`
	Action      string          `json:"action,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Path        string          `json:"path,omitempty"`
	State       ApprovalState   `json:"state"`
	RequestedAt time.Time       `json:"requested_at"`
	DecidedAt   *time.Time      `json:"decided_at,omitempty"`
}

type PermissionRequest struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"session_id"`
	ToolCallID  string          `json:"tool_call_id"`
	ToolName    string          `json:"tool_name"`
	Description string          `json:"description"`
	Action      string          `json:"action"`
	Params      json.RawMessage `json:"params,omitempty"`
	Path        string          `json:"path"`
}

type PermissionNotification struct {
	ApprovalID string `json:"approval_id,omitempty"`
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted,omitempty"`
	Denied     bool   `json:"denied,omitempty"`
}

type FileSnapshot struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ToolLifecycleState string

const (
	ToolLifecycleRequested       ToolLifecycleState = "requested"
	ToolLifecycleWaitingApproval ToolLifecycleState = "waiting_approval"
	ToolLifecycleCompleted       ToolLifecycleState = "completed"
	ToolLifecycleFailed          ToolLifecycleState = "failed"
)

type ToolUpdate struct {
	ToolCallID      string             `json:"tool_call_id"`
	ToolName        string             `json:"tool_name"`
	State           ToolLifecycleState `json:"state"`
	ResultStatus    string             `json:"result_status,omitempty"`
	RunID           string             `json:"run_id,omitempty"`
	SessionID       string             `json:"session_id,omitempty"`
	ApprovalID      string             `json:"approval_id,omitempty"`
	ResultMessageID string             `json:"result_message_id,omitempty"`
	Error           string             `json:"error,omitempty"`
}

type ExecuteToolInput struct {
	SessionID  string          `json:"session_id"`
	RunID      string          `json:"run_id,omitempty"`
	ToolName   string          `json:"tool_name"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	WorkingDir string          `json:"working_dir,omitempty"`
	Approved   bool            `json:"approved,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
}

type ExecuteToolResult struct {
	ToolCallMessage   Message   `json:"tool_call_message"`
	ToolResultMessage *Message  `json:"tool_result_message,omitempty"`
	Approval          *Approval `json:"approval,omitempty"`
}
