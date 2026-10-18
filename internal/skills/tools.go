package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type searchTool struct{ service *Service }
type viewTool struct{ service *Service }
type useTool struct{ service *Service }
type manageTool struct{ service *Service }

func ToolExecutors(service *Service) []tools.Executor {
	return []tools.Executor{
		&searchTool{service: service},
		&viewTool{service: service},
		&useTool{service: service},
		&manageTool{service: service},
	}
}

func (t *searchTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              "skill_search",
		Description:     "Search the trusted skills (reusable workflows the user keeps) by name, description, tags, category and content. Use it when a task looks like something a skill may cover; an empty query lists them.",
		Effect:          tools.EffectReadOnly,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Words to match; empty lists all trusted skills."},"limit":{"type":"integer","minimum":1,"maximum":50,"description":"Maximum skills to return."}},"additionalProperties":false}`),
	}
}

func (t *searchTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(call.Args, &input)
	results, err := t.service.Search(ctx, input.Query, SearchOptions{Limit: input.Limit})
	if err != nil {
		return tools.Result{Content: "Skill search failed: " + err.Error(), Status: tools.ResultStatusError}, nil
	}
	raw, _ := json.MarshalIndent(results, "", "  ")
	return tools.Result{Content: string(raw), Metadata: results, Status: tools.ResultStatusSuccess}, nil
}

func (t *viewTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              "skill_view",
		Description:     "Show a trusted skill's metadata and full SKILL.md without activating it, to decide whether it fits.",
		Effect:          tools.EffectReadOnly,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Skill id, as skill_search lists it."}},"required":["id"],"additionalProperties":false}`),
	}
}

func (t *viewTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid skill_view arguments.", Status: tools.ResultStatusError}, nil
	}
	detail, err := t.service.View(ctx, input.ID)
	if err != nil {
		return tools.Result{Content: "Skill view failed: " + err.Error(), Status: tools.ResultStatusError}, nil
	}
	if !usable(detail.Skill) {
		return tools.Result{Content: "Skill is not trusted, enabled, and active.", Status: tools.ResultStatusError}, nil
	}
	return tools.Result{Content: formatSkillDetail(detail), Metadata: detail, Status: tools.ResultStatusSuccess}, nil
}

func (t *useTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              "skill_use",
		Description:     "Activate a trusted skill for this session and return its full instructions; follow them for the rest of the task. Active skills are also loaded into later runs of the session.",
		Effect:          tools.EffectMutation,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Skill id, as skill_search lists it."}},"required":["id"],"additionalProperties":false}`),
	}
}

func (t *useTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid skill_use arguments.", Status: tools.ResultStatusError}, nil
	}
	detail, err := t.service.Use(ctx, call.SessionID, input.ID)
	if err != nil {
		return tools.Result{Content: "Skill use failed: " + err.Error(), Status: tools.ResultStatusError}, nil
	}
	return tools.Result{Content: formatSkillDetail(detail), Metadata: detail, Status: tools.ResultStatusSuccess}, nil
}

// skill_manage's verbs besides the library Actions.
const (
	verbCreate     = "create"
	verbWriteFile  = "write_file"
	verbEdit       = "edit"
	verbPatch      = "patch"
	verbRemoveFile = "remove_file"
)

// manageSchema is skill_manage's input schema; its action enum is the file
// verbs and every Action.
var manageSchema = func() json.RawMessage {
	verbs := []string{verbCreate, verbWriteFile, verbEdit, verbPatch, verbRemoveFile}
	for _, action := range Actions {
		verbs = append(verbs, string(action))
	}
	enum, _ := json.Marshal(verbs)
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":` + string(enum) + `},"id":{"type":"string","description":"The skill to act on."},"name":{"type":"string","description":"Name of a new skill (create)."},"description":{"type":"string","description":"One line saying when to use a new skill (create)."},"path":{"type":"string","description":"File inside the skill: SKILL.md, or under scripts/, references/ or assets/."},"content":{"type":"string","description":"SKILL.md body for create, or the whole new file content for write_file, edit and patch."}},"required":["action"],"additionalProperties":false}`)
}()

func (t *manageTool) Spec() tools.Spec {
	actions := make([]string, 0, len(Actions))
	for _, action := range Actions {
		actions = append(actions, string(action))
	}
	return tools.Spec{
		ID:              "skill_manage",
		Description:     "Create and maintain skills; every call waits for the user's approval. create makes a quarantined draft from name, description and content (the SKILL.md body); for a skill you propose yourself, show and revise the draft in chat first and call create only after the user confirms. write_file, edit and patch replace a file of skill id with content, and remove_file deletes one; path is SKILL.md or lies under scripts/, references/ or assets/. " + strings.Join(actions, ", ") + " act on skill id. A quarantined skill stays inactive until trusted.",
		Effect:          tools.EffectMutation,
		Asks:            true,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		InputJSONSchema: manageSchema,
	}
}

// manageInput reads a skill_manage call, its action lower-cased.
func manageInput(call tools.Call) (tools.SkillManagePermissionsParams, error) {
	var input tools.SkillManagePermissionsParams
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return input, errors.New("invalid skill_manage arguments")
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	return input, nil
}

func (t *manageTool) Preview(_ context.Context, call tools.Call) (tools.ApprovalRequest, error) {
	input, err := manageInput(call)
	return tools.ApprovalRequest{
		Path:        skillManageApprovalPath(input),
		Description: skillManageApprovalDescription(input),
		Params:      input,
	}, err
}

func (t *manageTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	input, err := manageInput(call)
	if err != nil {
		return errorResult(err), nil
	}
	switch input.Action {
	case verbCreate:
		return t.create(ctx, input), nil
	case verbWriteFile, verbEdit, verbPatch, verbRemoveFile:
		if err := t.service.WriteFile(ctx, input.ID, input.Path, input.Content, input.Action == verbRemoveFile); err != nil {
			return errorResult(err), nil
		}
		return tools.Result{Content: "Updated skill file: " + input.Path, Status: tools.ResultStatusSuccess}, nil
	}
	action, ok := ParseAction(input.Action)
	if !ok {
		return tools.Result{Content: "Unsupported skill_manage action: " + input.Action, Status: tools.ResultStatusError}, nil
	}
	if err := t.service.Apply(ctx, input.ID, action); err != nil {
		return errorResult(err), nil
	}
	return tools.Result{Content: fmt.Sprintf("Skill %s: %s done.", NormalizeID(input.ID), action), Status: tools.ResultStatusSuccess}, nil
}

func skillManageApprovalPath(input tools.SkillManagePermissionsParams) string {
	if input.Action != verbCreate {
		return textutil.FirstNonEmpty(input.ID, input.Path)
	}
	id := slugifySkillName(textutil.FirstNonEmpty(input.ID, input.Name))
	if id == "" {
		id = "new-skill"
	}
	return filepath.ToSlash(filepath.Join("skills", id, "SKILL.md"))
}

func skillManageApprovalDescription(input tools.SkillManagePermissionsParams) string {
	if input.Action != verbCreate {
		return "Manage skill: " + input.Action
	}
	return "Create quarantined skill: " + textutil.FirstNonEmpty(input.Name, input.ID, "new skill")
}

func slugifySkillName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func (t *manageTool) create(ctx context.Context, input tools.SkillManagePermissionsParams) tools.Result {
	name := textutil.FirstNonEmpty(input.Name, input.ID)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(input.Description) == "" {
		return tools.Result{Content: "Skill create requires name and description.", Status: tools.ResultStatusError}
	}
	skill, err := t.service.CreateDraft(ctx, name, input.Description, nil, input.Content)
	if err != nil {
		return errorResult(err)
	}
	return tools.Result{Content: "Created quarantined skill draft: " + skill.ID, Metadata: skill, Status: tools.ResultStatusSuccess}
}

func formatSkillDetail(detail SkillDetail) string {
	return fmt.Sprintf("<skill id=%q name=%q>\n%s\n</skill>", detail.Skill.ID, detail.Skill.Name, strings.TrimSpace(detail.Body))
}

func errorResult(err error) tools.Result {
	return tools.Result{Content: err.Error(), Status: tools.ResultStatusError}
}
