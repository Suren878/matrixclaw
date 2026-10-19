package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
)

const toolName = "memory"

// NewTool is the memory tool: models list entries and suggest changes,
// which ask the user.
func NewTool(service *Service) tools.Executor {
	return &tool{service: service}
}

type tool struct {
	service *Service
}

func (t *tool) Spec() tools.Spec {
	return tools.Spec{
		ID:              toolName,
		Description:     "Persistent notes shown to you at the start of every later run. Save durable facts that will matter again: the user's preferences, project conventions, build and test commands, decisions and their reasons. Do not save task progress, secrets, or what the repository's own docs already say. action is list (the default; never asks), add, replace or remove; replace and remove need the id from list. Changes wait for the user's approval.",
		Effect:          tools.EffectMutation,
		Asks:            true,
		Namespace:       "core.memory",
		Category:        tools.CategoryAutomation,
		InputJSONSchema: toolSchema,
	}
}

type toolInput struct {
	Action     string `json:"action"`
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Key        string `json:"key"`
	Content    string `json:"content"`
	WorkingDir string `json:"working_dir"`
	Limit      int    `json:"limit"`
}

// readInput reads a memory call; no action is list and no directory the call's.
func readInput(call tools.Call) (toolInput, error) {
	var input toolInput
	args := call.Args
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return input, tools.InvalidArgs(toolName, err)
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	if input.Action == "" {
		input.Action = "list"
	}
	if input.WorkingDir == "" {
		input.WorkingDir = call.WorkingDir
	}
	return input, nil
}

// PermissionSubject is the call's action, so rules (and the built-in
// "memory: list") can let reading run while changes ask.
func (t *tool) PermissionSubject(call tools.Call) permission.Subject {
	input, err := readInput(call)
	if err != nil {
		return permission.Subject{}
	}
	return permission.Subject{Kind: permission.KindName, Value: input.Action}
}

// ReadsOnly reports whether the call only lists memory.
func (t *tool) ReadsOnly(call tools.Call) bool {
	input, err := readInput(call)
	return err == nil && input.Action == "list"
}

func (t *tool) Preview(_ context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	input, err := readInput(call)
	description := "Update Matrixclaw memory"
	switch input.Action {
	case "list":
		description = "List Matrixclaw memory"
	case "add":
		description = "Add Matrixclaw memory"
	case "replace":
		description = "Replace Matrixclaw memory " + strings.TrimSpace(input.ID)
	case "remove":
		description = "Remove Matrixclaw memory " + strings.TrimSpace(input.ID)
	}
	return tools.ApprovalRequest{Description: description, Params: input}, err
}

func (t *tool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	input, err := readInput(call)
	if err != nil {
		return tools.Result{}, err
	}
	switch input.Action {
	case "list":
		entries, err := t.service.List(ctx, Filter{Scope: Scope(input.Scope), WorkingDir: input.WorkingDir, Limit: input.Limit})
		if err != nil {
			return errorResult(err), nil
		}
		return tools.Result{Content: formatEntries(entries), Metadata: entries, Status: tools.ResultStatusSuccess}, nil
	case "add":
		entry, err := t.service.Create(ctx, Entry{Scope: Scope(input.Scope), Key: input.Key, Content: input.Content, WorkingDir: input.WorkingDir})
		if err != nil {
			return errorResult(err), nil
		}
		return tools.Result{Content: "Memory added: " + entry.ID, Metadata: entry, Status: tools.ResultStatusSuccess}, nil
	case "replace":
		entry, err := t.service.Update(ctx, Entry{ID: input.ID, Scope: Scope(input.Scope), Key: input.Key, Content: input.Content, WorkingDir: input.WorkingDir})
		if err != nil {
			return errorResult(err), nil
		}
		return tools.Result{Content: "Memory replaced: " + entry.ID, Metadata: entry, Status: tools.ResultStatusSuccess}, nil
	case "remove":
		if err := t.service.Delete(ctx, input.ID); err != nil {
			return errorResult(err), nil
		}
		return tools.Result{Content: "Memory removed: " + strings.TrimSpace(input.ID), Status: tools.ResultStatusSuccess}, nil
	default:
		return tools.Result{
			Content: "Unsupported memory action. Use list, add, replace, or remove.",
			Status:  tools.ResultStatusError,
		}, nil
	}
}

func errorResult(err error) tools.Result {
	return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}
}

func formatEntries(entries []Entry) string {
	if len(entries) == 0 {
		return "No memory entries."
	}
	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "Memory entries:")
	for _, entry := range entries {
		label := strings.TrimSpace(string(entry.Scope))
		if label == "" {
			label = string(ScopeGlobal)
		}
		if strings.TrimSpace(entry.Key) != "" {
			label += "/" + strings.TrimSpace(entry.Key)
		}
		lines = append(lines, fmt.Sprintf("- %s [%s]: %s", entry.ID, label, textutil.Clip(strings.Join(strings.Fields(entry.Content), " "), 240, "...")))
	}
	return strings.Join(lines, "\n")
}

var toolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "add", "replace", "remove"], "description": "What to do; list when omitted."},
    "id": {"type": "string", "description": "The entry to replace or remove, as list shows it."},
    "scope": {"type": "string", "enum": ["global", "user", "project"], "description": "user: about the user; project: only for runs in one working directory; global: everything else."},
    "key": {"type": "string", "description": "A short label for the entry."},
    "content": {"type": "string", "description": "The note itself, for add and replace: one self-contained fact."},
    "working_dir": {"type": "string", "description": "The project directory of a project entry; defaults to the current working directory."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50, "description": "Maximum entries list returns."}
  },
  "additionalProperties": false
}`)
