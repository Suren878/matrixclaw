package tools

import (
	"context"
	"encoding/json"
	"time"
)

type RiskLevel string

const (
	RiskSafe     RiskLevel = "safe"
	RiskApproval RiskLevel = "approval"
)

type Spec struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description,omitempty"`
	Risk            RiskLevel       `json:"risk"`
	InputJSONSchema json.RawMessage `json:"input_json_schema,omitempty"`
}

type Call struct {
	SessionID  string          `json:"session_id,omitempty"`
	RunID      string          `json:"run_id,omitempty"`
	WorkingDir string          `json:"working_dir,omitempty"`
	Approved   bool            `json:"approved,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
}

type ApprovalRequest struct {
	ID          string `json:"id"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	ToolID      string `json:"tool_id"`
	Action      string `json:"action"`
	Path        string `json:"path,omitempty"`
	Description string `json:"description,omitempty"`
	Params      any    `json:"params,omitempty"`
}

type FileVersion struct {
	Path       string `json:"path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
	Diff       string `json:"diff,omitempty"`
	Additions  int    `json:"additions,omitempty"`
	Removals   int    `json:"removals,omitempty"`
}

type BackgroundJob struct {
	ID          string    `json:"shell_id"`
	Command     string    `json:"command"`
	WorkingDir  string    `json:"working_dir"`
	Description string    `json:"description,omitempty"`
	StartedAt   time.Time `json:"started_at"`
}

type ResultStatus string

const (
	ResultStatusSuccess ResultStatus = "success"
	ResultStatusError   ResultStatus = "error"
	ResultStatusNeutral ResultStatus = "neutral"
)

type Result struct {
	Content     string           `json:"content"`
	Metadata    any              `json:"metadata,omitempty"`
	MIMEType    string           `json:"mime_type,omitempty"`
	Status      ResultStatus     `json:"status,omitempty"`
	IsError     bool             `json:"is_error,omitempty"`
	Approval    *ApprovalRequest `json:"approval,omitempty"`
	FileVersion *FileVersion     `json:"file_version,omitempty"`
	Background  *BackgroundJob   `json:"background,omitempty"`
}

type Executor interface {
	Spec() Spec
	Execute(ctx context.Context, call Call) (Result, error)
}
