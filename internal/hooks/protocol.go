package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
)

// input is what a hook reads on stdin, the fields of the Claude Code protocol
// its event uses.
type input struct {
	SessionID            string          `json:"session_id"`
	RunID                string          `json:"run_id,omitempty"`
	HookEventName        Event           `json:"hook_event_name"`
	Cwd                  string          `json:"cwd"`
	PermissionMode       string          `json:"permission_mode,omitempty"`
	ToolName             string          `json:"tool_name,omitempty"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse         *ToolResponse   `json:"tool_response,omitempty"`
	StopHookActive       *bool           `json:"stop_hook_active,omitempty"`
	LastAssistantMessage string          `json:"last_assistant_message,omitempty"`
	Source               string          `json:"source,omitempty"`
	Trigger              string          `json:"trigger,omitempty"`
	Message              string          `json:"message,omitempty"`
	NotificationType     string          `json:"notification_type,omitempty"`
}

// ToolResponse is a finished call's result as a PostToolUse hook reads it.
type ToolResponse struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// output is what a hook that exits 0 may print as JSON.
type output struct {
	Continue           *bool  `json:"continue"`
	StopReason         string `json:"stopReason"`
	SystemMessage      string `json:"systemMessage"`
	Decision           string `json:"decision"`
	Reason             string `json:"reason"`
	HookSpecificOutput struct {
		PermissionDecision       string          `json:"permissionDecision"`
		PermissionDecisionReason string          `json:"permissionDecisionReason"`
		UpdatedInput             json.RawMessage `json:"updatedInput"`
		AdditionalContext        string          `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// maxContext bounds the text one hook adds for the model.
const maxContext = 10_000

// parseOutput reads stdout as JSON when it is a JSON object.
func parseOutput(stdout []byte) (output, bool) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return output{}, false
	}
	var out output
	if err := json.Unmarshal(trimmed, &out); err != nil {
		return output{}, false
	}
	return out, true
}

// stopped reports whether the hook asked to stop, with continue false.
func (o output) stopped() bool {
	return o.Continue != nil && !*o.Continue
}

// clip cuts text a hook adds for the model to maxContext characters.
func clip(text string) string {
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > maxContext {
		return string(runes[:maxContext]) + "\n[cut at 10000 characters]"
	}
	return text
}
