package core

import (
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type ApprovalState string

const (
	ApprovalStatePending  ApprovalState = "pending"
	ApprovalStateApproved ApprovalState = "approved"
	ApprovalStateRejected ApprovalState = "rejected"
)

type Approval struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	RunID     string `json:"run_id,omitempty"`
	// TaskID names the subagent task whose child asked, AgentName its name;
	// the approval is listed and announced in the parent's session.
	TaskID      string          `json:"task_id,omitempty"`
	AgentName   string          `json:"agent_name,omitempty"`
	ToolCallRef string          `json:"tool_call_id,omitempty"`
	ToolName    string          `json:"tool_name,omitempty"`
	Description string          `json:"description,omitempty"`
	Action      string          `json:"action,omitempty"`
	Params      json.RawMessage `json:"params,omitempty"`
	Path        string          `json:"path,omitempty"`
	State       ApprovalState   `json:"state"`
	// Reason is why the user denied the call; the model reads it.
	Reason string `json:"reason,omitempty"`
	// Suggestion is the rule an "Always allow" answer keeps.
	Suggestion  *permission.Suggestion `json:"suggestion,omitempty"`
	RequestedAt time.Time              `json:"requested_at"`
	DecidedAt   *time.Time             `json:"decided_at,omitempty"`
}

type PermissionRequest struct {
	ID          string                 `json:"id"`
	SessionID   string                 `json:"session_id"`
	TaskID      string                 `json:"task_id,omitempty"`
	AgentName   string                 `json:"agent_name,omitempty"`
	ToolCallID  string                 `json:"tool_call_id"`
	ToolName    string                 `json:"tool_name"`
	Description string                 `json:"description"`
	Action      string                 `json:"action"`
	Params      json.RawMessage        `json:"params,omitempty"`
	Path        string                 `json:"path"`
	Suggestion  *permission.Suggestion `json:"suggestion,omitempty"`
}

type PermissionNotification struct {
	ApprovalID string `json:"approval_id,omitempty"`
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted,omitempty"`
	Denied     bool   `json:"denied,omitempty"`
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

// ExecuteToolInput is a tool call made outside any run (the API, voice, the
// MCP server); a run's calls go through its engine.
type ExecuteToolInput struct {
	SessionID   string          `json:"session_id"`
	ToolName    string          `json:"tool_name"`
	Client      string          `json:"client,omitempty"`
	ExternalKey string          `json:"external_key,omitempty"`
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	WorkingDir  string          `json:"working_dir,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
}

type ExecuteToolResult struct {
	ToolCallMessage   transcript.Message  `json:"tool_call_message"`
	ToolResultMessage *transcript.Message `json:"tool_result_message,omitempty"`
	Approval          *Approval           `json:"approval,omitempty"`
}
