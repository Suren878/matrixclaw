package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var explicitSkillRef = regexp.MustCompile(`(?:^|\s)([$/])([A-Za-z0-9][A-Za-z0-9_-]*)\b`)

func (s *Service) PromptContext(req PromptRequest) string {
	if s == nil || !s.enabled {
		return ""
	}
	available, err := s.Search("", SearchOptions{Limit: 12})
	if err != nil || len(available) == 0 {
		return ""
	}
	lines := []string{"Available trusted skills:"}
	for _, skill := range available {
		lines = append(lines, fmt.Sprintf("- %s: %s", skill.ID, skill.Description))
	}

	fullIDs := s.promptSkillIDs(req, available)
	if len(fullIDs) > 0 {
		lines = append(lines, "", "Loaded skill instructions:")
		for _, id := range fullIDs {
			detail, err := s.Get(id)
			if err != nil || detail.Skill.TrustState != TrustTrusted || detail.Skill.State != StateActive {
				continue
			}
			lines = append(lines, fmt.Sprintf("<skill id=%q name=%q>", detail.Skill.ID, detail.Skill.Name))
			lines = append(lines, strings.TrimSpace(detail.Body))
			lines = append(lines, "</skill>")
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s *Service) promptSkillIDs(req PromptRequest, available []Skill) []string {
	seen := map[string]struct{}{}
	var ids []string
	add := func(id string) {
		id = NormalizeID(id)
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, skill := range s.sessionSkills(req.SessionID) {
		add(skill.ID)
	}
	latest := latestUserText(req.Messages)
	for _, match := range explicitSkillRef.FindAllStringSubmatch(latest, -1) {
		if len(match) >= 3 {
			add(match[2])
		}
	}
	if s.cfg.AutoInvoke && len(ids) == 0 {
		lower := strings.ToLower(latest)
		for _, skill := range available {
			if skill.ID != "" && strings.Contains(lower, strings.ToLower(skill.ID)) {
				add(skill.ID)
				break
			}
		}
	}
	return ids
}

func latestUserText(messages []PromptMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	if len(messages) == 0 {
		return ""
	}
	return strings.TrimSpace(messages[len(messages)-1].Content)
}

func (s *Service) sessionSkills(sessionID string) []Skill {
	skills, err := s.SessionSkills(sessionID)
	if err != nil {
		return nil
	}
	return skills
}
