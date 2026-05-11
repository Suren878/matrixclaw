package core

import (
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/version"
)

type CreateSessionRequest struct {
	Title      string `json:"title"`
	WorkingDir string `json:"working_dir"`
}

type RenameSessionRequest struct {
	Title string `json:"title"`
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

type ApprovalResolveRequest struct {
	Approved bool `json:"approved"`
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
	Error       string  `json:"error"`
	SessionID   string  `json:"session_id"`
	UserMessage Message `json:"user_message"`
	Run         Run     `json:"run"`
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

type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

type MessageResponse struct {
	Message Message `json:"message"`
}

type RunResponse struct {
	Run Run `json:"run"`
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

type ServerStatusResponse struct {
	Status ServerStatus `json:"status"`
}

type ToolsResponse struct {
	Tools []tools.Spec `json:"tools"`
}

type ToolExecuteResponse struct {
	Result ExecuteToolResult `json:"result"`
}
