package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Suren878/matrixclaw/internal/permission"
)

type RiskLevel string

const (
	RiskSafe     RiskLevel = "safe"
	RiskApproval RiskLevel = "approval"
)

type Effect string

const (
	EffectReadOnly Effect = "readonly"
	EffectMutation Effect = "mutation"
)

type ApprovalMode string

const (
	ApprovalNever     ApprovalMode = "never"
	ApprovalOnRequest ApprovalMode = "on_request"
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

type Profile string

const (
	ProfileReadOnly   Profile = "readonly"
	ProfileCoding     Profile = "coding"
	ProfileAutomation Profile = "automation"
	ProfileStorage    Profile = "storage"
	ProfileWeb        Profile = "web"
	ProfileSkills     Profile = "skills"
)

type OutputKind string

const (
	OutputText          OutputKind = "text"
	OutputFileContent   OutputKind = "file_content"
	OutputFileTree      OutputKind = "file_tree"
	OutputSearchResults OutputKind = "search_results"
	OutputDiff          OutputKind = "diff"
	OutputJob           OutputKind = "job"
	OutputAudio         OutputKind = "audio"
	OutputStorageEntry  OutputKind = "storage_entry"
	OutputStorageList   OutputKind = "storage_list"
	OutputWebContent    OutputKind = "web_content"
)

type Spec struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description,omitempty"`
	Risk             RiskLevel       `json:"risk"`
	Effect           Effect          `json:"effect,omitempty"`
	ApprovalMode     ApprovalMode    `json:"approval_mode,omitempty"`
	PermissionParams string          `json:"permission_params,omitempty"`
	Namespace        string          `json:"namespace,omitempty"`
	Category         Category        `json:"category,omitempty"`
	Profiles         []Profile       `json:"profiles,omitempty"`
	OutputKind       OutputKind      `json:"output_kind,omitempty"`
	InputJSONSchema  json.RawMessage `json:"input_json_schema,omitempty"`
}

type Call struct {
	SessionID   string          `json:"session_id,omitempty"`
	RunID       string          `json:"run_id,omitempty"`
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	Client      string          `json:"client,omitempty"`
	ExternalKey string          `json:"external_key,omitempty"`
	WorkingDir  string          `json:"working_dir,omitempty"`
	Approved    bool            `json:"approved,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
	// Recheck applies the permission rules to another subject the call reaches,
	// such as a redirect's host; nil when no rules apply.
	Recheck func(context.Context, permission.Subject) error `json:"-"`
	// Guarded is set when a deny or ask rule names the tool, so it must not reach
	// subjects it cannot recheck, such as where a browser was redirected.
	Guarded bool `json:"-"`
}

type ApprovalRequest struct {
	ID          string `json:"id"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
	ToolID      string `json:"tool_id"`
	Action      string `json:"action"`
	Path        string `json:"path,omitempty"`
	Description string `json:"description,omitempty"`
	Params      any    `json:"params,omitempty"`
	// Suggestion is the rule an "Always allow" answer keeps.
	Suggestion *permission.Suggestion `json:"suggestion,omitempty"`
}

type SkillManagePermissionsParams struct {
	Action      string `json:"action"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Content     string `json:"content,omitempty"`
}

type FileVersion struct {
	Path       string `json:"path"`
	OldContent string `json:"old_content,omitempty"`
	NewContent string `json:"new_content,omitempty"`
	Diff       string `json:"diff,omitempty"`
	Additions  int    `json:"additions,omitempty"`
	Removals   int    `json:"removals,omitempty"`
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

type Executor interface {
	Spec() Spec
	Execute(ctx context.Context, call Call) (Result, error)
}

func (s Spec) Mutates() bool {
	return normalizeEffect(s.Effect) == EffectMutation
}

func (s Spec) RequiresApproval() bool {
	return normalizeApprovalMode(s.ApprovalMode) == ApprovalOnRequest || normalizeRiskLevel(s.Risk) == RiskApproval
}

func (s Spec) IsFilesystemMutation() bool {
	return s.Mutates() && normalizeCategory(s.Category) == CategoryFilesystem
}
