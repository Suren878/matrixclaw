package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/tools"
)

const (
	sessionSearchToolName = "session_search"
	memoryToolName        = "memory"
)

// MemoryToolExecutors exposes session recall and persistent memory to models.
func MemoryToolExecutors(app *Core) []tools.Executor {
	if app == nil {
		return nil
	}
	return []tools.Executor{
		&sessionSearchTool{app: app},
		&memoryTool{app: app},
	}
}

type sessionSearchTool struct {
	app *Core
}

func (t *sessionSearchTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              sessionSearchToolName,
		Name:            "SessionSearch",
		Description:     "Search previous Matrixclaw session messages and return matches grouped by session.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "core.memory",
		Category:        tools.CategoryAutomation,
		Profiles:        []tools.Profile{tools.ProfileReadOnly, tools.ProfileCoding, tools.ProfileAutomation},
		OutputKind:      tools.OutputSearchResults,
		InputJSONSchema: sessionSearchToolSchema,
	}
}

func (t *sessionSearchTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Query     string `json:"query"`
		SessionID string `json:"session_id"`
		Limit     int    `json:"limit"`
	}
	if err := decodeMemoryToolArgs(sessionSearchToolName, call.Args, &input); err != nil {
		return tools.Result{}, err
	}
	report, err := t.app.SessionSearch(ctx, SearchFilter{
		Query:     input.Query,
		SessionID: input.SessionID,
		Limit:     input.Limit,
	})
	if err != nil {
		return memoryErrorResult(err), nil
	}
	return tools.Result{
		Content:  formatSessionSearchReport(report),
		Metadata: report,
		Status:   tools.ResultStatusSuccess,
	}, nil
}

type memoryTool struct {
	app *Core
}

func (t *memoryTool) Spec() tools.Spec {
	return tools.Spec{
		ID:               memoryToolName,
		Name:             "Memory",
		Description:      "List, add, replace, or remove persistent Matrixclaw memory. Mutations require approval.",
		Risk:             tools.RiskApproval,
		Effect:           tools.EffectMutation,
		ApprovalMode:     tools.ApprovalOnRequest,
		PermissionParams: "memory_permissions",
		Namespace:        "core.memory",
		Category:         tools.CategoryAutomation,
		Profiles:         []tools.Profile{tools.ProfileCoding, tools.ProfileAutomation},
		OutputKind:       tools.OutputText,
		InputJSONSchema:  memoryToolSchema,
	}
}

func (t *memoryTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input memoryToolInput
	if err := decodeMemoryToolArgs(memoryToolName, call.Args, &input); err != nil {
		return tools.Result{}, err
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	if input.Action == "" {
		input.Action = "list"
	}
	if input.WorkingDir == "" {
		input.WorkingDir = call.WorkingDir
	}
	switch input.Action {
	case "list":
		entries, err := t.app.ListMemories(ctx, MemoryFilter{
			Scope:      MemoryScope(input.Scope),
			WorkingDir: input.WorkingDir,
			Limit:      input.Limit,
		})
		if err != nil {
			return memoryErrorResult(err), nil
		}
		return tools.Result{Content: formatMemoryEntries(entries), Metadata: entries, Status: tools.ResultStatusSuccess}, nil
	case "add":
		if !call.Approved {
			return memoryApprovalResult(input), nil
		}
		entry, err := t.app.CreateMemory(ctx, MemoryEntry{
			Scope:      MemoryScope(input.Scope),
			Key:        input.Key,
			Content:    input.Content,
			WorkingDir: input.WorkingDir,
		})
		if err != nil {
			return memoryErrorResult(err), nil
		}
		return tools.Result{Content: "Memory added: " + entry.ID, Metadata: entry, Status: tools.ResultStatusSuccess}, nil
	case "replace":
		if !call.Approved {
			return memoryApprovalResult(input), nil
		}
		entry, err := t.app.UpdateMemory(ctx, MemoryEntry{
			ID:         input.ID,
			Scope:      MemoryScope(input.Scope),
			Key:        input.Key,
			Content:    input.Content,
			WorkingDir: input.WorkingDir,
		})
		if err != nil {
			return memoryErrorResult(err), nil
		}
		return tools.Result{Content: "Memory replaced: " + entry.ID, Metadata: entry, Status: tools.ResultStatusSuccess}, nil
	case "remove":
		if !call.Approved {
			return memoryApprovalResult(input), nil
		}
		if err := t.app.DeleteMemory(ctx, input.ID); err != nil {
			return memoryErrorResult(err), nil
		}
		return tools.Result{Content: "Memory removed: " + strings.TrimSpace(input.ID), Status: tools.ResultStatusSuccess}, nil
	default:
		return tools.Result{
			Content: "Unsupported memory action. Use list, add, replace, or remove.",
			IsError: true,
			Status:  tools.ResultStatusError,
		}, nil
	}
}

type memoryToolInput struct {
	Action     string `json:"action"`
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Key        string `json:"key"`
	Content    string `json:"content"`
	WorkingDir string `json:"working_dir"`
	Limit      int    `json:"limit"`
}

func decodeMemoryToolArgs(toolID string, args json.RawMessage, dest any) error {
	if len(args) == 0 {
		args = []byte(`{}`)
	}
	if err := json.Unmarshal(args, dest); err != nil {
		return tools.InvalidArgs(toolID, err)
	}
	return nil
}

func memoryApprovalResult(input memoryToolInput) tools.Result {
	action := strings.ToLower(strings.TrimSpace(input.Action))
	description := "Update Matrixclaw memory"
	switch action {
	case "add":
		description = "Add Matrixclaw memory"
	case "replace":
		description = "Replace Matrixclaw memory " + strings.TrimSpace(input.ID)
	case "remove":
		description = "Remove Matrixclaw memory " + strings.TrimSpace(input.ID)
	}
	return tools.Result{
		Content: "Approval required",
		Approval: &tools.ApprovalRequest{
			ToolID:      memoryToolName,
			Action:      "memory:" + action,
			Description: description,
			Params:      input,
		},
	}
}

func memoryErrorResult(err error) tools.Result {
	return tools.Result{
		Content: err.Error(),
		IsError: true,
		Status:  tools.ResultStatusError,
	}
}

func formatSessionSearchReport(report SessionSearchReport) string {
	if len(report.Sessions) == 0 {
		return "No previous session messages matched " + fmt.Sprintf("%q.", report.Query)
	}
	lines := []string{fmt.Sprintf("Found matches for %q:", report.Query)}
	for _, sessionResult := range report.Sessions {
		title := strings.TrimSpace(sessionResult.Session.Title)
		if title == "" {
			title = sessionResult.Session.ID
		}
		lines = append(lines, fmt.Sprintf("- %s (%s)", title, sessionResult.Session.ID))
		for _, match := range sessionResult.Matches {
			snippet := compactToolLine(match.Snippet)
			if snippet == "" {
				snippet = match.MessageID
			}
			lines = append(lines, fmt.Sprintf("  - %s: %s", match.Role, snippet))
		}
	}
	return strings.Join(lines, "\n")
}

func formatMemoryEntries(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return "No memory entries."
	}
	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "Memory entries:")
	for _, entry := range entries {
		scope := strings.TrimSpace(string(entry.Scope))
		if scope == "" {
			scope = string(MemoryScopeGlobal)
		}
		label := scope
		if strings.TrimSpace(entry.Key) != "" {
			label += "/" + strings.TrimSpace(entry.Key)
		}
		content := compactToolLine(entry.Content)
		lines = append(lines, fmt.Sprintf("- %s [%s]: %s", entry.ID, label, content))
	}
	return strings.Join(lines, "\n")
}

func compactToolLine(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= 240 {
		return value
	}
	return value[:237] + "..."
}

var (
	sessionSearchToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Text to search for in previous session messages."},
    "session_id": {"type": "string", "description": "Optional session id to restrict the search."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 20}
  },
  "required": ["query"],
  "additionalProperties": false
}`)

	memoryToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "add", "replace", "remove"]},
    "id": {"type": "string", "description": "Memory id for replace/remove."},
    "scope": {"type": "string", "enum": ["global", "user", "project"]},
    "key": {"type": "string", "description": "Optional short label for the memory."},
    "content": {"type": "string", "description": "Memory content for add/replace."},
    "working_dir": {"type": "string", "description": "Project root for project-scoped memory."},
    "limit": {"type": "integer", "minimum": 1, "maximum": 50}
  },
  "additionalProperties": false
}`)
)
