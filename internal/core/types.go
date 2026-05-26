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
	ID                string         `json:"id"`
	Title             string         `json:"title"`
	Kind              SessionKind    `json:"kind"`
	RuntimeID         SessionRuntime `json:"runtime_id,omitempty"`
	ParentSessionID   string         `json:"parent_session_id,omitempty"`
	Hidden            bool           `json:"hidden,omitempty"`
	WorkingDir        string         `json:"working_dir,omitempty"`
	ProviderID        string         `json:"provider_id,omitempty"`
	ModelID           string         `json:"model_id,omitempty"`
	ExternalAgentID   string         `json:"external_agent_id,omitempty"`
	ExternalAgentName string         `json:"external_agent_name,omitempty"`
	PermissionMode    PermissionMode `json:"permission_mode,omitempty"`
	Status            SessionStatus  `json:"status"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type SessionCapabilities struct {
	ProviderSelection bool `json:"provider_selection"`
	PermissionMode    bool `json:"permission_mode"`
	PlanningMode      bool `json:"planning_mode"`
	NativeTools       bool `json:"native_tools"`
	ExternalAgent     bool `json:"external_agent"`
}

func CapabilitiesForSession(session Session) SessionCapabilities {
	if NormalizeSessionRuntime(session.RuntimeID) == SessionRuntimeExternalAgent || NormalizeSessionKind(session.Kind) == SessionKindExternalAgent {
		return SessionCapabilities{ExternalAgent: true}
	}
	return SessionCapabilities{
		ProviderSelection: true,
		PermissionMode:    true,
		PlanningMode:      true,
		NativeTools:       true,
	}
}

type SessionListFilter struct {
	IncludeArchived bool
	IncludeHidden   bool
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

type SubagentTaskStatus string

const (
	SubagentTaskStatusRunning   SubagentTaskStatus = "running"
	SubagentTaskStatusCompleted SubagentTaskStatus = "completed"
	SubagentTaskStatusFailed    SubagentTaskStatus = "failed"
)

type SubagentRuntime string

const (
	SubagentRuntimeMatrixClaw SubagentRuntime = "matrixclaw"
	SubagentRuntimeCodex      SubagentRuntime = "codex"
	SubagentRuntimeClaude     SubagentRuntime = "claude"
	SubagentRuntimeAuto       SubagentRuntime = "auto"
)

type SubagentTask struct {
	ID               string             `json:"id"`
	ParentSessionID  string             `json:"parent_session_id"`
	ParentRunID      string             `json:"parent_run_id,omitempty"`
	ParentToolCallID string             `json:"parent_tool_call_id,omitempty"`
	ChildSessionID   string             `json:"child_session_id,omitempty"`
	ChildRunID       string             `json:"child_run_id,omitempty"`
	Runtime          string             `json:"runtime"`
	Goal             string             `json:"goal"`
	Status           SubagentTaskStatus `json:"status"`
	Summary          string             `json:"summary,omitempty"`
	Error            string             `json:"error,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	FinishedAt       *time.Time         `json:"finished_at,omitempty"`
}

type UsageRecord struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id"`
	RunID           string    `json:"run_id"`
	MessageID       string    `json:"message_id,omitempty"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	InputTokens     int64     `json:"input_tokens,omitempty"`
	OutputTokens    int64     `json:"output_tokens,omitempty"`
	TotalTokens     int64     `json:"total_tokens,omitempty"`
	CachedTokens    int64     `json:"cached_tokens,omitempty"`
	ReasoningTokens int64     `json:"reasoning_tokens,omitempty"`
	Estimated       bool      `json:"estimated,omitempty"`
	ProviderRaw     string    `json:"provider_raw,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type UsageFilter struct {
	SessionID string
	RunID     string
	Limit     int
}

type UsageSummary struct {
	SessionID       string `json:"session_id,omitempty"`
	Runs            int    `json:"runs"`
	InputTokens     int64  `json:"input_tokens,omitempty"`
	OutputTokens    int64  `json:"output_tokens,omitempty"`
	TotalTokens     int64  `json:"total_tokens,omitempty"`
	CachedTokens    int64  `json:"cached_tokens,omitempty"`
	ReasoningTokens int64  `json:"reasoning_tokens,omitempty"`
}

type UsageReport struct {
	Summary UsageSummary  `json:"summary"`
	Records []UsageRecord `json:"records,omitempty"`
}

type PlanItemStatus string

const (
	PlanItemPending PlanItemStatus = "pending"
	PlanItemActive  PlanItemStatus = "active"
	PlanItemDone    PlanItemStatus = "done"
	PlanItemSkipped PlanItemStatus = "skipped"
)

type SessionPlan struct {
	SessionID string     `json:"session_id"`
	Goal      string     `json:"goal,omitempty"`
	Items     []PlanItem `json:"items,omitempty"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
}

type PlanItem struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Text      string         `json:"text"`
	Status    PlanItemStatus `json:"status"`
	Position  int            `json:"position"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type PlanRunStatus string

const (
	PlanRunIdle      PlanRunStatus = "idle"
	PlanRunRunning   PlanRunStatus = "running"
	PlanRunBlocked   PlanRunStatus = "blocked"
	PlanRunFailed    PlanRunStatus = "failed"
	PlanRunCompleted PlanRunStatus = "completed"
	PlanRunCanceled  PlanRunStatus = "canceled"
)

type PlanRun struct {
	SessionID     string        `json:"session_id"`
	Status        PlanRunStatus `json:"status"`
	CurrentItemID string        `json:"current_item_id,omitempty"`
	LastRunID     string        `json:"last_run_id,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	StepNo        int           `json:"step_no,omitempty"`
	Attempt       int           `json:"attempt,omitempty"`
	CreatedAt     time.Time     `json:"created_at,omitempty"`
	UpdatedAt     time.Time     `json:"updated_at,omitempty"`
}

type SearchFilter struct {
	Query     string
	SessionID string
	Limit     int
}

type SearchResult struct {
	MessageID string    `json:"message_id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role,omitempty"`
	Snippet   string    `json:"snippet,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	Rank      float64   `json:"rank,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type SearchReport struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

type SessionSearchResult struct {
	Session Session        `json:"session"`
	Matches []SearchResult `json:"matches"`
}

type SessionSearchReport struct {
	Query    string                `json:"query"`
	Sessions []SessionSearchResult `json:"sessions"`
}

type MemoryScope string

const (
	MemoryScopeGlobal  MemoryScope = "global"
	MemoryScopeUser    MemoryScope = "user"
	MemoryScopeProject MemoryScope = "project"
)

type MemoryEntry struct {
	ID         string      `json:"id"`
	Scope      MemoryScope `json:"scope"`
	Key        string      `json:"key,omitempty"`
	Content    string      `json:"content"`
	WorkingDir string      `json:"working_dir,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type MemoryFilter struct {
	Scope      MemoryScope
	WorkingDir string
	Limit      int
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
	ParentSessionID string
	Hidden          bool
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
