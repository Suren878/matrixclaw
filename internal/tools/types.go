package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
)

type Effect string

const (
	EffectReadOnly Effect = "readonly"
	EffectMutation Effect = "mutation"
)

type Category string

const (
	CategoryFilesystem Category = "filesystem"
	CategoryShell      Category = "shell"
	CategoryAutomation Category = "automation"
	CategoryStorage    Category = "storage"
	CategoryWeb        Category = "web"
	CategorySkills     Category = "skills"
)

// Spec describes a tool to the model and to the engine: Effect and Category
// drive concurrency, Asks approvals, Namespace groups a module's tools.
type Spec struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Effect      Effect `json:"effect,omitempty"`
	// Asks makes a call no permission rule decides wait for the user's approval.
	Asks            bool            `json:"asks,omitempty"`
	Namespace       string          `json:"namespace,omitempty"`
	Category        Category        `json:"category,omitempty"`
	InputJSONSchema json.RawMessage `json:"input_json_schema,omitempty"`
}

type Call struct {
	SessionID   string          `json:"session_id,omitempty"`
	RunID       string          `json:"run_id,omitempty"`
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	Client      string          `json:"client,omitempty"`
	ExternalKey string          `json:"external_key,omitempty"`
	WorkingDir  string          `json:"working_dir,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
	// Recheck applies the permission rules to another subject the call reaches,
	// such as a redirect's host; nil when no rules apply.
	Recheck func(context.Context, permission.Subject) error `json:"-"`
}

// ApprovalRequest is what the user sees before a call runs: what it does, the
// file or directory it changes and tool-specific details such as a diff.
type ApprovalRequest struct {
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Params      any    `json:"params,omitempty"`
	// Suggestion is the rule an "Always allow" answer keeps; core sets it.
	Suggestion *permission.Suggestion `json:"suggestion,omitempty"`
}

// Previewer is implemented by tools whose approval shows more than the call's
// arguments. Preview has no side effects; its error answers the call instead.
type Previewer interface {
	Preview(ctx context.Context, call Call) (ApprovalRequest, error)
}

type SkillManagePermissionsParams struct {
	Action      string `json:"action"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Content     string `json:"content,omitempty"`
}

type ResultStatus string

const (
	ResultStatusSuccess ResultStatus = "success"
	ResultStatusError   ResultStatus = "error"
	ResultStatusNeutral ResultStatus = "neutral"
)

type Result struct {
	Content  string `json:"content"`
	Metadata any    `json:"metadata,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	// Status is how the call went; empty is success.
	Status ResultStatus `json:"status,omitempty"`
	// OutputPath is the file holding the full output when Content was cut.
	OutputPath string `json:"output_path,omitempty"`
	// Await parks the run once its batch is done, until one of the tasks
	// finishes, the user writes or Until passes.
	Await *Await `json:"await,omitempty"`
	// Waiting marks a result that only reports a task still running after the
	// call waited for it; the no-progress guard skips it.
	Waiting bool `json:"waiting,omitempty"`
}

// Await is what a run waits for: any of TaskIDs, or any background task of its
// session when there are none, until Until.
type Await struct {
	TaskIDs []string  `json:"task_ids,omitempty"`
	Until   time.Time `json:"until"`
}

// IsError reports whether the call failed.
func (r Result) IsError() bool {
	return r.Status == ResultStatusError
}

type Executor interface {
	Spec() Spec
	Execute(ctx context.Context, call Call) (Result, error)
}

func (s Spec) Mutates() bool {
	return normalizeEffect(s.Effect) == EffectMutation
}

func (s Spec) IsFilesystemMutation() bool {
	return s.Mutates() && normalizeCategory(s.Category) == CategoryFilesystem
}
