package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

func (c *Client) ListSkills(ctx context.Context, opts skills.SearchOptions) ([]skills.Skill, error) {
	return c.skills(ctx, skillsQuery("", opts))
}

func (c *Client) SearchSkills(ctx context.Context, query string, opts skills.SearchOptions) ([]skills.Skill, error) {
	return c.skills(ctx, skillsQuery(query, opts))
}

// SkillUsage lists the skills by how often they were used.
func (c *Client) SkillUsage(ctx context.Context) ([]skills.Skill, error) {
	return c.skills(ctx, url.Values{"order": {"usage"}})
}

func skillsQuery(query string, opts skills.SearchOptions) url.Values {
	values := url.Values{}
	if query = strings.TrimSpace(query); query != "" {
		values.Set("query", query)
	}
	if opts.Limit > 0 {
		values.Set("limit", strconv.Itoa(opts.Limit))
	}
	for name, on := range map[string]bool{
		"include_quarantined": opts.IncludeQuarantined,
		"include_archived":    opts.IncludeArchived,
		"include_disabled":    opts.IncludeDisabled,
	} {
		if on {
			values.Set(name, "1")
		}
	}
	return values
}

func (c *Client) skills(ctx context.Context, values url.Values) ([]skills.Skill, error) {
	path := "/v1/modules/skills"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response skills.SkillsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) GetSkill(ctx context.Context, id string) (skills.SkillDetail, error) {
	var response skills.SkillDetail
	if err := c.doJSON(ctx, http.MethodGet, skillPath(id), nil, &response); err != nil {
		return skills.SkillDetail{}, err
	}
	return response, nil
}

func (c *Client) InstallSkill(ctx context.Context, path string) ([]skills.Skill, error) {
	var response skills.SkillsResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/skills", skills.InstallRequest{Path: path}, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) CreateSkillDraft(ctx context.Context, request skills.DraftRequest) (skills.Skill, error) {
	var response skills.Skill
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/skills/drafts", request, &response); err != nil {
		return skills.Skill{}, err
	}
	return response, nil
}

func (c *Client) UpdateSkillMetadata(ctx context.Context, id string, update skills.MetadataUpdate) (skills.Skill, error) {
	var response skills.Skill
	if err := c.doJSON(ctx, http.MethodPatch, skillPath(id), update, &response); err != nil {
		return skills.Skill{}, err
	}
	return response, nil
}

func (c *Client) UpdateSkillBody(ctx context.Context, id string, body string) error {
	return c.doJSON(ctx, http.MethodPut, skillPath(id)+"/body", skills.BodyRequest{Body: body}, nil)
}

// SkillAction runs trust, quarantine, enable, disable, archive, restore, pin
// or unpin on a skill.
func (c *Client) SkillAction(ctx context.Context, id string, action string) error {
	return c.doJSON(ctx, http.MethodPost, skillPath(id)+"/"+escapedPath(action), nil, nil)
}

func (c *Client) RemoveSkill(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, skillPath(id), nil, nil)
}

func (c *Client) SessionSkills(ctx context.Context, sessionID string) ([]skills.Skill, error) {
	var response skills.SkillsResponse
	if err := c.doJSON(ctx, http.MethodGet, sessionSkillsPath(sessionID), nil, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) UseSkill(ctx context.Context, sessionID string, skillID string) (skills.SkillDetail, error) {
	var response skills.SkillDetail
	if err := c.doJSON(ctx, http.MethodPost, sessionSkillsPath(sessionID)+"/"+escapedPath(skillID), nil, &response); err != nil {
		return skills.SkillDetail{}, err
	}
	return response, nil
}

func (c *Client) UnloadSkill(ctx context.Context, sessionID string, skillID string) error {
	return c.doJSON(ctx, http.MethodDelete, sessionSkillsPath(sessionID)+"/"+escapedPath(skillID), nil, nil)
}

func skillPath(id string) string {
	return "/v1/modules/skills/" + escapedPath(id)
}

func sessionSkillsPath(sessionID string) string {
	return "/v1/sessions/" + escapedPath(sessionID) + "/skills"
}
