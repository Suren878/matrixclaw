package permission

import permissionrules "github.com/Suren878/matrixclaw/internal/permission"

type PermissionRequest struct {
	ID          string                      `json:"id"`
	SessionID   string                      `json:"session_id"`
	AgentName   string                      `json:"agent_name,omitempty"`
	ToolCallID  string                      `json:"tool_call_id"`
	ToolName    string                      `json:"tool_name"`
	Description string                      `json:"description"`
	Params      any                         `json:"params,omitempty"`
	Path        string                      `json:"path"`
	Suggestion  *permissionrules.Suggestion `json:"suggestion,omitempty"`
}

type PermissionNotification struct {
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted,omitempty"`
	Denied     bool   `json:"denied,omitempty"`
}
