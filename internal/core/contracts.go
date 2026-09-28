package core

import (
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
	"github.com/Suren878/matrixclaw/internal/version"
)

type CreateSessionRequest struct {
	Title           string `json:"title"`
	Kind            string `json:"kind,omitempty"`
	RuntimeID       string `json:"runtime_id,omitempty"`
	WorkingDir      string `json:"working_dir"`
	ProviderID      string `json:"provider_id,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
	PermissionMode  string `json:"permission_mode,omitempty"`
	ExternalAgentID string `json:"external_agent_id,omitempty"`
}

type RenameSessionRequest struct {
	Title string `json:"title"`
}

type UpdateSessionPlanRequest struct {
	Goal  *string `json:"goal,omitempty"`
	Clear bool    `json:"clear,omitempty"`
}

type AddPlanItemRequest struct {
	Text     string `json:"text"`
	ParentID string `json:"parent_id,omitempty"`
}

type UpdatePlanItemRequest struct {
	ItemID string `json:"item_id"`
	Status string `json:"status,omitempty"`
	Text   string `json:"text,omitempty"`
}

type PlanRunStartRequest struct {
	Reset bool `json:"reset,omitempty"`
}

type PlanRunBindRequest struct {
	RunID string `json:"run_id"`
}

type SearchResponse struct {
	Search SearchReport `json:"search"`
}

type MemoryResponse struct {
	Memories []MemoryEntry `json:"memories"`
}

type UpdateSessionPermissionModeRequest struct {
	PermissionMode string `json:"permission_mode"`
}

type UpdateSessionLLMRequest struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

type CreateSystemMessageRequest struct {
	Content string `json:"content"`
}

// ApprovalResolveRequest is a user's decision on an approval; Reason explains a
// denial to the model, and Always keeps the approval's suggested rule in that scope.
type ApprovalResolveRequest struct {
	Approved bool             `json:"approved"`
	Reason   string           `json:"reason,omitempty"`
	Always   permission.Scope `json:"always,omitempty"`
}

// PermissionRuleRequest adds a rule. Pattern is a path glob, a command prefix
// such as "go test:*", a domain or an MCP "server__tool" name; empty is the whole tool.
type PermissionRuleRequest struct {
	Tool    string            `json:"tool"`
	Pattern string            `json:"pattern,omitempty"`
	Effect  permission.Effect `json:"effect"`
	Scope   permission.Scope  `json:"scope"`
}

type PermissionRulesResponse struct {
	Rules []permission.Rule `json:"rules"`
}

type PermissionRuleResponse struct {
	Rule permission.Rule `json:"rule"`
}

type AdminRestartRequest struct {
	Notification *ClientDeliveryTarget `json:"notification,omitempty"`
}

type OKResponse struct {
	OK bool `json:"ok"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type AcceptRunErrorResponse struct {
	Error       string             `json:"error"`
	SessionID   string             `json:"session_id"`
	UserMessage transcript.Message `json:"user_message"`
	Run         Run                `json:"run"`
}

type HealthResponse struct {
	OK      bool         `json:"ok"`
	Version version.Info `json:"version"`
}

type SessionsResponse struct {
	Sessions []Session `json:"sessions"`
}

type SessionResponse struct {
	Session Session `json:"session"`
}

type ExternalAgentsResponse struct {
	Agents []ExternalAgentDescriptor `json:"agents"`
}

type UpdateExternalAgentRequest struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Path    string `json:"path,omitempty"`
}

type MessagesResponse struct {
	Messages []transcript.Message `json:"messages"`
}

type MessageResponse struct {
	Message transcript.Message `json:"message"`
}

type RunResponse struct {
	Run Run `json:"run"`
}

type RunStepsResponse struct {
	Steps []RunStep `json:"steps"`
}

type ClientBindingResponse struct {
	Binding ClientBinding `json:"binding"`
}

type ClientSnapshotResponse struct {
	Snapshot ClientSnapshot `json:"snapshot"`
}

type SessionContextResponse struct {
	Context ContextReport `json:"context"`
}

type UsageResponse struct {
	Usage UsageReport `json:"usage"`
}

type SessionBudgetResponse struct {
	Budget SessionBudgetReport `json:"budget"`
}

type SessionPlanResponse struct {
	Plan SessionPlan `json:"plan"`
}

type PlanRunResponse struct {
	PlanRun PlanRun     `json:"plan_run"`
	Plan    SessionPlan `json:"plan"`
}

type SessionCompactResponse struct {
	Compact CompactSessionResult `json:"compact"`
}

type SessionProvidersResponse struct {
	Providers []SessionProviderOption `json:"providers"`
}

type SessionModelsResponse struct {
	ProviderID string   `json:"provider_id"`
	ModelID    string   `json:"model_id"`
	Models     []string `json:"models"`
}

type ApprovalsResponse struct {
	Approvals []Approval `json:"approvals"`
}

type ApprovalResponse struct {
	Approval Approval `json:"approval"`
}

type ClientDeliveriesResponse struct {
	Deliveries []ClientDelivery `json:"deliveries"`
}

type ClientDeliveryFailRequest struct {
	Error string `json:"error,omitempty"`
}

type ServerStatusResponse struct {
	Status ServerStatus `json:"status"`
}

type ToolsResponse struct {
	Tools []tools.Spec `json:"tools"`
}

type ToolExecuteResponse struct {
	Result ExecuteToolResult `json:"result"`
}
