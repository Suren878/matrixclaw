package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		Name:            "Skill Search",
		Description:     "Search trusted Matrixclaw skills by name, description, tags, category, and snippets.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		Profiles:        []tools.Profile{tools.ProfileReadOnly, tools.ProfileCoding, tools.ProfileSkills},
		OutputKind:      tools.OutputSearchResults,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`),
	}
}

func (t *searchTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(call.Args, &input)
	results, err := t.service.Search(input.Query, SearchOptions{Limit: input.Limit})
	if err != nil {
		return tools.Result{Content: "Skill search failed: " + err.Error(), IsError: true}, nil
	}
	raw, _ := json.MarshalIndent(results, "", "  ")
	return tools.Result{Content: string(raw), Metadata: results, Status: tools.ResultStatusSuccess}, nil
}

func (t *viewTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              "skill_view",
		Name:            "Skill View",
		Description:     "View a trusted Matrixclaw skill's metadata and full SKILL.md body without activating it.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectReadOnly,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		Profiles:        []tools.Profile{tools.ProfileReadOnly, tools.ProfileCoding, tools.ProfileSkills},
		OutputKind:      tools.OutputText,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
	}
}

func (t *viewTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid skill_view arguments.", IsError: true}, nil
	}
	detail, err := t.service.View(input.ID)
	if err != nil {
		return tools.Result{Content: "Skill view failed: " + err.Error(), IsError: true}, nil
	}
	if detail.Skill.TrustState != TrustTrusted || detail.Skill.State != StateActive || !detail.Skill.Enabled {
		return tools.Result{Content: "Skill is not trusted, enabled, and active.", IsError: true}, nil
	}
	return tools.Result{Content: formatSkillDetail(detail), Metadata: detail, Status: tools.ResultStatusSuccess}, nil
}

func (t *useTool) Spec() tools.Spec {
	return tools.Spec{
		ID:              "skill_use",
		Name:            "Skill Use",
		Description:     "Activate a trusted Matrixclaw skill for this session and return its full instructions.",
		Risk:            tools.RiskSafe,
		Effect:          tools.EffectMutation,
		ApprovalMode:    tools.ApprovalNever,
		Namespace:       "module.skills",
		Category:        tools.CategorySkills,
		Profiles:        []tools.Profile{tools.ProfileCoding, tools.ProfileSkills},
		OutputKind:      tools.OutputText,
		InputJSONSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"],"additionalProperties":false}`),
	}
}

func (t *useTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid skill_use arguments.", IsError: true}, nil
	}
	detail, err := t.service.Use(call.SessionID, input.ID)
	if err != nil {
		return tools.Result{Content: "Skill use failed: " + err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: formatSkillDetail(detail), Metadata: detail, Status: tools.ResultStatusSuccess}, nil
}

func (t *manageTool) Spec() tools.Spec {
	return tools.Spec{
		ID:               "skill_manage",
		Name:             "Skill Manage",
		Description:      "Create, edit, patch, write files for, archive, restore, pin, unpin, trust, quarantine, disable, or remove Matrixclaw skills. Mutations require approval.",
		Risk:             tools.RiskApproval,
		Effect:           tools.EffectMutation,
		ApprovalMode:     tools.ApprovalOnRequest,
		PermissionParams: "skill_manage_permissions",
		Namespace:        "module.skills",
		Category:         tools.CategorySkills,
		Profiles:         []tools.Profile{tools.ProfileCoding, tools.ProfileSkills},
		OutputKind:       tools.OutputText,
		InputJSONSchema:  json.RawMessage(`{"type":"object","properties":{"action":{"type":"string"},"id":{"type":"string"},"name":{"type":"string"},"description":{"type":"string"},"path":{"type":"string"},"content":{"type":"string"}},"required":["action"],"additionalProperties":false}`),
	}
}

func (t *manageTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	var input tools.SkillManagePermissionsParams
	if err := json.Unmarshal(call.Args, &input); err != nil {
		return tools.Result{Content: "Invalid skill_manage arguments.", IsError: true}, nil
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if !call.Approved {
		return tools.Result{
			Content: "Approval required",
			Approval: &tools.ApprovalRequest{
				ToolID:      "skill_manage",
				Action:      action,
				Path:        skillManageApprovalPath(action, input),
				Description: skillManageApprovalDescription(action, input),
				Params:      input,
			},
		}, nil
	}
	switch action {
	case "create":
		return t.create(input)
	case "archive":
		return resultFromErr("Archived skill.", t.service.Archive(input.ID)), nil
	case "restore":
		return resultFromErr("Restored skill.", t.service.Restore(input.ID)), nil
	case "pin":
		return resultFromErr("Pinned skill.", t.service.Pin(input.ID, true)), nil
	case "unpin":
		return resultFromErr("Unpinned skill.", t.service.Pin(input.ID, false)), nil
	case "trust":
		return resultFromErr("Trusted skill.", t.service.Trust(input.ID)), nil
	case "quarantine":
		return resultFromErr("Quarantined skill.", t.service.Quarantine(input.ID)), nil
	case "disable":
		return resultFromErr("Disabled skill.", t.service.Disable(input.ID)), nil
	case "remove":
		return resultFromErr("Removed skill.", t.service.Remove(input.ID)), nil
	case "write_file", "edit", "patch", "remove_file":
		return t.writeSupportFile(action, input)
	default:
		return tools.Result{Content: "Unsupported skill_manage action: " + action, IsError: true}, nil
	}
}

func skillManageApprovalPath(action string, input tools.SkillManagePermissionsParams) string {
	switch action {
	case "create":
		name := firstNonEmpty(input.ID, input.Name)
		id := slugifySkillName(name)
		if id == "" {
			id = "new-skill"
		}
		return filepath.ToSlash(filepath.Join("skills", id, "SKILL.md"))
	default:
		return strings.TrimSpace(firstNonEmpty(input.ID, input.Path))
	}
}

func skillManageApprovalDescription(action string, input tools.SkillManagePermissionsParams) string {
	switch action {
	case "create":
		name := strings.TrimSpace(firstNonEmpty(input.Name, input.ID))
		if name == "" {
			name = "new skill"
		}
		return "Create quarantined skill: " + name
	default:
		return "Manage skill: " + action
	}
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

func (t *manageTool) create(input tools.SkillManagePermissionsParams) (tools.Result, error) {
	name := firstNonEmpty(input.Name, input.ID)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(input.Description) == "" {
		return tools.Result{Content: "Skill create requires name and description.", IsError: true}, nil
	}
	body := strings.TrimSpace(input.Content)
	if body == "" {
		body = "Describe the reusable workflow here."
	}
	skill, err := t.service.CreateDraft(name, input.Description, nil, body)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	return tools.Result{Content: "Created quarantined skill draft: " + skill.ID, Metadata: skill, Status: tools.ResultStatusSuccess}, nil
}

func (t *manageTool) writeSupportFile(action string, input tools.SkillManagePermissionsParams) (tools.Result, error) {
	detail, err := t.service.Get(input.ID)
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	rel := filepath.Clean(strings.TrimSpace(input.Path))
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return tools.Result{Content: "Invalid skill support path.", IsError: true}, nil
	}
	top := strings.Split(filepath.ToSlash(rel), "/")[0]
	if top != "scripts" && top != "references" && top != "assets" && rel != "SKILL.md" {
		return tools.Result{Content: "Skill files may only be under scripts/, references/, assets/, or SKILL.md.", IsError: true}, nil
	}
	target := filepath.Join(detail.Skill.Path, rel)
	if action == "remove_file" {
		if err := os.Remove(target); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
		if err := os.WriteFile(target, []byte(input.Content), 0o600); err != nil {
			return tools.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	_, _ = t.service.db.Exec(`UPDATE skills SET patch_count = patch_count + 1, updated_at = ? WHERE id = ?`, formatTime(t.service.now().UTC()), detail.Skill.ID)
	return tools.Result{Content: "Updated skill file: " + rel, Status: tools.ResultStatusSuccess}, nil
}

func formatSkillDetail(detail SkillDetail) string {
	return fmt.Sprintf("<skill id=%q name=%q>\n%s\n</skill>", detail.Skill.ID, detail.Skill.Name, strings.TrimSpace(detail.Body))
}

func resultFromErr(success string, err error) tools.Result {
	if err != nil {
		return tools.Result{Content: err.Error(), IsError: true}
	}
	return tools.Result{Content: success, Status: tools.ResultStatusSuccess}
}
