package controlplane

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

func skillsLibrarySearchOptions() skills.SearchOptions {
	return skills.SearchOptions{IncludeQuarantined: true, IncludeArchived: true, IncludeDisabled: true, Limit: 200}
}

func splitSkillLibrary(items []skills.Skill) (review []skills.Skill, installed []skills.Skill, drafts []skills.Skill) {
	for _, item := range items {
		if item.TrustState == skills.TrustQuarantine {
			review = append(review, item)
			continue
		}
		if item.Source == "draft" {
			drafts = append(drafts, item)
			continue
		}
		if item.TrustState == skills.TrustTrusted {
			installed = append(installed, item)
		}
	}
	return review, installed, drafts
}

func filterSkillsSection(items []skills.Skill, section string) []skills.Skill {
	review, installed, drafts := splitSkillLibrary(items)
	switch section {
	case "review":
		return review
	case "installed":
		return installed
	case "usage", "search":
		if section == "search" {
			return items
		}
		return items
	case "drafts":
		return drafts
	case "library":
		return items
	default:
		return items
	}
}

func skillSectionForItem(item skills.Skill) string {
	if item.TrustState == skills.TrustQuarantine {
		return "review"
	}
	return "library"
}

func skillsSectionTitle(section string) string {
	switch section {
	case "review":
		return "Review Queue"
	case "library":
		return "Skill Library"
	case "installed":
		return "Installed Skills"
	case "drafts":
		return "Created Skills"
	case "usage":
		return "Usage Status"
	case "search":
		return "Skill Search"
	default:
		return "Skills"
	}
}

func skillsSectionEmptyInfo(section string) string {
	switch section {
	case "review":
		return "Imported skills that need review will appear here."
	case "drafts":
		return "Created skills will appear here."
	case "library":
		return "Add a SKILL.md directory, plugin bundle, or create a skill."
	default:
		return "Install a SKILL.md directory or plugin bundle."
	}
}

func skillsModuleInfo(items []skills.Skill) string {
	if len(items) == 0 {
		return ""
	}
	trusted := 0
	quarantined := 0
	for _, item := range items {
		if item.TrustState == skills.TrustTrusted && item.State == skills.StateActive && item.Enabled {
			trusted++
		}
		if item.TrustState == skills.TrustQuarantine {
			quarantined++
		}
	}
	if quarantined > 0 {
		return fmt.Sprintf("%d enabled · %d review", trusted, quarantined)
	}
	return fmt.Sprintf("%d enabled", trusted)
}

func skillCountInfo(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d", count)
}

func skillTitle(skill skills.Skill) string {
	if strings.TrimSpace(skill.Name) != "" {
		return strings.TrimSpace(skill.Name)
	}
	return strings.TrimSpace(skill.ID)
}

func skillInfo(skill skills.Skill) string {
	enabled := "Disabled"
	if skill.Enabled {
		enabled = "Enabled"
	}
	parts := []string{skillOriginInfo(skill)}
	if skill.TrustState == skills.TrustQuarantine {
		parts = append(parts, skillTrustInfo(skill))
	}
	parts = append(parts, enabled)
	if skill.State == skills.StateArchived {
		parts = append(parts, "Archived")
	} else if skill.State != "" && skill.State != skills.StateActive {
		parts = append(parts, skill.State)
	}
	return strings.Join(nonEmptyStrings(parts...), " · ")
}

func skillSearchText(skill skills.Skill) string {
	parts := []string{
		skill.ID,
		skill.Name,
		skill.Description,
		skill.Category,
		skill.Source,
		skillOriginInfo(skill),
		skillTrustInfo(skill),
	}
	parts = append(parts, skill.Tags...)
	parts = append(parts, skill.Platforms...)
	return strings.Join(nonEmptyStrings(parts...), " ")
}

func sessionSkillsMeta(active []skills.Skill) string {
	if len(active) == 0 {
		return "No active skills in this chat"
	}
	return fmt.Sprintf("%d active in this chat", len(active))
}

func sessionSkillInfo(skill skills.Skill, active bool) string {
	info := skillInfo(skill)
	if active {
		return "In this chat · " + info
	}
	return info
}

func skillOriginInfo(skill skills.Skill) string {
	switch strings.ToLower(strings.TrimSpace(skill.Source)) {
	case "draft":
		return "Created"
	case "":
		return "Installed"
	default:
		return "Imported"
	}
}

func skillTrustInfo(skill skills.Skill) string {
	switch skill.TrustState {
	case skills.TrustTrusted:
		return "Trusted"
	case skills.TrustQuarantine:
		return "Needs Review"
	default:
		return strings.TrimSpace(skill.TrustState)
	}
}

func skillAvailableInSession(skill skills.Skill) bool {
	return skill.TrustState == skills.TrustTrusted && skill.State == skills.StateActive && skill.Enabled
}

func skillIDSet(items []skills.Skill) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		out[item.ID] = struct{}{}
	}
	return out
}

func hasSkillID(items []skills.Skill, id string) bool {
	id = strings.TrimSpace(id)
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func parseSkillMetadataInput(value string) (string, string, []string, string) {
	parts := splitSkillInput(value, 4)
	return partAt(parts, 0), partAt(parts, 1), parseTags(partAt(parts, 2)), partAt(parts, 3)
}

func splitSkillInput(value string, limit int) []string {
	raw := strings.SplitN(value, "|", limit)
	out := make([]string, len(raw))
	for i, part := range raw {
		out[i] = strings.TrimSpace(part)
	}
	return out
}

func partAt(parts []string, index int) string {
	if index < 0 || index >= len(parts) {
		return ""
	}
	return strings.TrimSpace(parts[index])
}

func parseTags(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func splitSkillPickerContext(value string) (string, string) {
	section, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return "installed", strings.TrimSpace(value)
	}
	if strings.TrimSpace(section) == "" {
		section = "installed"
	}
	return strings.TrimSpace(section), strings.TrimSpace(id)
}

type skillDraftState struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

func encodeSkillDraftState(state skillDraftState) string {
	raw, _ := json.Marshal(state)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeSkillDraftState(value string) (skillDraftState, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return skillDraftState{}, false
	}
	var state skillDraftState
	if err := json.Unmarshal(raw, &state); err != nil {
		return skillDraftState{}, false
	}
	return state, true
}

func defaultSkillDraftBody(state skillDraftState) string {
	description := strings.TrimSpace(state.Description)
	if description == "" {
		description = "Use this skill when the task matches its description."
	}
	return "Use this skill when: " + description + "\n\nSteps:\n1. Identify whether the current task matches this skill.\n2. Follow the project conventions before making changes.\n3. Verify the result before finishing."
}
