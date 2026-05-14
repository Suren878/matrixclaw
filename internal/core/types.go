package core

import (
	"encoding/json"
	"time"
)

type ServerStatus struct {
	StartedAt            time.Time `json:"started_at"`
	UptimeSeconds        int64     `json:"uptime_seconds"`
	GoAllocBytes         uint64    `json:"go_alloc_bytes"`
	GoSysBytes           uint64    `json:"go_sys_bytes"`
	ProcessRSSBytes      uint64    `json:"process_rss_bytes"`
	MemoryTotalBytes     uint64    `json:"memory_total_bytes"`
	MemoryAvailableBytes uint64    `json:"memory_available_bytes"`
	MemoryUsedBytes      uint64    `json:"memory_used_bytes"`
	CPUUsedPercent       float64   `json:"cpu_used_percent"`
	CPUKnown             bool      `json:"cpu_known"`
}

type SessionStatus string

const (
	SessionStatusActive   SessionStatus = "active"
	SessionStatusArchived SessionStatus = "archived"
)

type SessionKind string

const (
	SessionKindAssistant     SessionKind = "assistant"
	SessionKindExternalAgent SessionKind = "external_agent"
)

type SessionRuntime string

const (
	SessionRuntimeMatrixClaw    SessionRuntime = "matrixclaw"
	SessionRuntimeExternalAgent SessionRuntime = "external_agent"
	SessionRuntimeCodex         SessionRuntime = "codex"
)

type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

type RunStatus string

const (
	RunStatusAccepted        RunStatus = "accepted"
	RunStatusRunning         RunStatus = "running"
	RunStatusWaitingApproval RunStatus = "waiting_approval"
	RunStatusCompleted       RunStatus = "completed"
	RunStatusCanceled        RunStatus = "canceled"
	RunStatusFailed          RunStatus = "failed"
)

type Session struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Kind           SessionKind    `json:"kind"`
	RuntimeID      SessionRuntime `json:"runtime_id,omitempty"`
	WorkingDir     string         `json:"working_dir,omitempty"`
	ProviderID     string         `json:"provider_id,omitempty"`
	ModelID        string         `json:"model_id,omitempty"`
	PermissionMode PermissionMode `json:"permission_mode,omitempty"`
	Status         SessionStatus  `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type SessionListFilter struct {
	IncludeArchived bool
}

type ClientBinding struct {
	Client      string    `json:"client"`
	ExternalKey string    `json:"external_key"`
	SessionID   string    `json:"session_id"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ClientDeliveryStatus string

const (
	ClientDeliveryStatusPending ClientDeliveryStatus = "pending"
	ClientDeliveryStatusReady   ClientDeliveryStatus = "ready"
	ClientDeliveryStatusSent    ClientDeliveryStatus = "sent"
	ClientDeliveryStatusFailed  ClientDeliveryStatus = "failed"
)

type ClientDeliveryTarget struct {
	Client      string          `json:"client,omitempty"`
	ExternalKey string          `json:"external_key,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	RunID       string          `json:"run_id,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Address     json.RawMessage `json:"address,omitempty"`
}

type ClientDelivery struct {
	ID          string               `json:"id"`
	Type        string               `json:"type"`
	Client      string               `json:"client"`
	ExternalKey string               `json:"external_key,omitempty"`
	SessionID   string               `json:"session_id,omitempty"`
	RunID       string               `json:"run_id,omitempty"`
	TaskID      string               `json:"task_id,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Address     json.RawMessage      `json:"address,omitempty"`
	Status      ClientDeliveryStatus `json:"status"`
	Error       string               `json:"error,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	FinishedAt  *time.Time           `json:"finished_at,omitempty"`
}

type ClientDeliveryFilter struct {
	Client       string
	ExternalKey  string
	SessionID    string
	RunID        string
	TaskID       string
	Type         string
	Status       ClientDeliveryStatus
	CreatedAfter time.Time
	Limit        int
}

type RunTiming struct {
	TotalMillis    int64     `json:"total_ms,omitempty"`
	ModelMillis    int64     `json:"model_ms,omitempty"`
	ToolMillis     int64     `json:"tool_ms,omitempty"`
	ApprovalMillis int64     `json:"approval_ms,omitempty"`
	LastEventAt    time.Time `json:"last_event_at,omitempty"`
}

type Message struct {
	ID        string        `json:"id"`
	SessionID string        `json:"session_id"`
	RunID     string        `json:"run_id"`
	Role      MessageRole   `json:"role"`
	Content   string        `json:"content"`
	Parts     []MessagePart `json:"parts,omitempty"`
	Model     string        `json:"model,omitempty"`
	Provider  string        `json:"provider,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type MessagePartKind string

const (
	MessagePartKindText       MessagePartKind = "text"
	MessagePartKindImage      MessagePartKind = "image"
	MessagePartKindReasoning  MessagePartKind = "reasoning"
	MessagePartKindToolCall   MessagePartKind = "tool_call"
	MessagePartKindToolResult MessagePartKind = "tool_result"
	MessagePartKindFinish     MessagePartKind = "finish"
)

type MessagePart struct {
	Kind       MessagePartKind `json:"kind"`
	Text       *TextPart       `json:"text,omitempty"`
	Image      *ImagePart      `json:"image,omitempty"`
	Reasoning  *ReasoningPart  `json:"reasoning,omitempty"`
	ToolCall   *ToolCallPart   `json:"tool_call,omitempty"`
	ToolResult *ToolResultPart `json:"tool_result,omitempty"`
	Finish     *FinishPart     `json:"finish,omitempty"`
}

type TextPart struct {
	Text string `json:"text"`
}

type ImagePart struct {
	MIMEType    string `json:"mime_type,omitempty"`
	DataBase64  string `json:"data_base64,omitempty"`
	Name        string `json:"name,omitempty"`
	StoragePath string `json:"storage_path,omitempty"`
	Temporary   bool   `json:"temporary,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

type ReasoningPart struct {
	Text             string          `json:"text"`
	Signature        string          `json:"signature,omitempty"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
	ToolID           string          `json:"tool_id,omitempty"`
	ResponsesData    json.RawMessage `json:"responses_data,omitempty"`
}

type ToolCallPart struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Input    string `json:"input"`
	Finished bool   `json:"finished,omitempty"`
}

type ToolResultPart struct {
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Content    string          `json:"content"`
	MIMEType   string          `json:"mime_type,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Status     string          `json:"status,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
}

type FinishPart struct {
	Reason  string          `json:"reason,omitempty"`
	Message string          `json:"message,omitempty"`
	Details json.RawMessage `json:"details,omitempty"`
}

type Run struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"session_id"`
	UserMessageID string     `json:"user_message_id"`
	Client        string     `json:"client,omitempty"`
	ExternalKey   string     `json:"external_key,omitempty"`
	Status        RunStatus  `json:"status"`
	Error         string     `json:"error,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

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

type CreateSessionInput struct {
	Title           string
	Kind            SessionKind
	RuntimeID       SessionRuntime
	WorkingDir      string
	ProviderID      string
	ModelID         string
	PermissionMode  PermissionMode
	ExternalAgentID string
}

type RenameSessionInput struct {
	SessionID string
	Title     string
}

type PermissionMode string

const (
	PermissionModeDefault     PermissionMode = "default"
	PermissionModeAcceptEdits PermissionMode = "accept_edits"
	PermissionModeFullAuto    PermissionMode = "full_auto"
)

type UpdateSessionPermissionModeInput struct {
	SessionID      string
	PermissionMode PermissionMode
}

type UseBindingInput struct {
	Client      string `json:"client"`
	ExternalKey string `json:"external_key"`
	SessionID   string `json:"session_id"`
}

type HandleMessageInput struct {
	Client           string        `json:"client"`
	ExternalKey      string        `json:"external_key"`
	SessionID        string        `json:"session_id"`
	Text             string        `json:"text"`
	Parts            []MessagePart `json:"parts,omitempty"`
	WorkingDir       string        `json:"working_dir"`
	AllowAutoBindOne bool          `json:"allow_auto_bind_one"`
}

type HandleTriggeredRunInput struct {
	TriggerID   string
	Client      string
	ExternalKey string
	SessionID   string
	Text        string
	WorkingDir  string
}

type AcceptRunResult struct {
	SessionID   string  `json:"session_id"`
	UserMessage Message `json:"user_message"`
	Run         Run     `json:"run"`
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

type SessionProviderOption struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Type         string `json:"type,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	Configured   bool   `json:"configured"`
}

type ExecuteToolResult struct {
	ToolCallMessage   Message   `json:"tool_call_message"`
	ToolResultMessage *Message  `json:"tool_result_message,omitempty"`
	Approval          *Approval `json:"approval,omitempty"`
}

func NormalizeMessageParts(content string, parts []MessagePart) []MessagePart {
	if len(parts) > 0 {
		return parts
	}
	if content == "" {
		return nil
	}
	return []MessagePart{{
		Kind: MessagePartKindText,
		Text: &TextPart{Text: content},
	}}
}
