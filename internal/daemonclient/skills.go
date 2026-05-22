package daemonclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/skills"
)

type skillsListResponse struct {
	Skills []skills.Skill `json:"skills"`
}

func (c *Client) ListSkills(ctx context.Context, opts skills.SearchOptions) ([]skills.Skill, error) {
	values := url.Values{}
	if opts.Limit > 0 {
		values.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.IncludeQuarantined {
		values.Set("include_quarantined", "1")
	}
	if opts.IncludeArchived {
		values.Set("include_archived", "1")
	}
	if opts.IncludeDisabled {
		values.Set("include_disabled", "1")
	}
	path := "/v1/modules/skills"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response skillsListResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) SearchSkills(ctx context.Context, query string, opts skills.SearchOptions) ([]skills.Skill, error) {
	values := url.Values{}
	values.Set("query", query)
	if opts.Limit > 0 {
		values.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.IncludeQuarantined {
		values.Set("include_quarantined", "1")
	}
	if opts.IncludeArchived {
		values.Set("include_archived", "1")
	}
	if opts.IncludeDisabled {
		values.Set("include_disabled", "1")
	}
	var response skillsListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/skills?"+values.Encode(), nil, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) GetSkill(ctx context.Context, id string) (skills.SkillDetail, error) {
	var response skills.SkillDetail
	path := "/v1/modules/skills/" + escapedPath(id)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return skills.SkillDetail{}, err
	}
	return response, nil
}

func (c *Client) InstallSkill(ctx context.Context, path string) ([]skills.Skill, error) {
	var response skillsListResponse
	request := struct {
		Path string `json:"path"`
	}{Path: path}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/skills", request, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) SkillAction(ctx context.Context, id string, action string) error {
	path := "/v1/modules/skills/" + escapedPath(id) + "/" + escapedPath(action)
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}

func (c *Client) SessionSkills(ctx context.Context, sessionID string) ([]skills.Skill, error) {
	var response skillsListResponse
	path := "/v1/modules/skills/sessions/" + escapedPath(sessionID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Skills, nil
}

func (c *Client) UseSkill(ctx context.Context, sessionID string, skillID string) (skills.SkillDetail, error) {
	var response skills.SkillDetail
	path := "/v1/modules/skills/sessions/" + escapedPath(sessionID) + "/" + escapedPath(skillID) + "/use"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &response); err != nil {
		return skills.SkillDetail{}, err
	}
	return response, nil
}

func (c *Client) UnloadSkill(ctx context.Context, sessionID string, skillID string) error {
	path := "/v1/modules/skills/sessions/" + escapedPath(sessionID) + "/" + escapedPath(skillID) + "/unload"
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}

func (c *Client) CreateSkillDraft(ctx context.Context, name string, description string, tags []string, body string) (skills.Skill, error) {
	var response skills.Skill
	request := struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags,omitempty"`
		Body        string   `json:"body,omitempty"`
	}{Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), Tags: tags, Body: body}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/skills", request, &response); err != nil {
		return skills.Skill{}, err
	}
	return response, nil
}

func (c *Client) UpdateSkillMetadata(ctx context.Context, id string, update skills.MetadataUpdate) (skills.Skill, error) {
	var response skills.Skill
	path := "/v1/modules/skills/" + escapedPath(id)
	if err := c.doJSON(ctx, http.MethodPatch, path, update, &response); err != nil {
		return skills.Skill{}, err
	}
	return response, nil
}

func (c *Client) UpdateSkillBody(ctx context.Context, id string, body string) error {
	request := struct {
		Body string `json:"body"`
	}{Body: body}
	path := "/v1/modules/skills/" + escapedPath(id) + "/body"
	return c.doJSON(ctx, http.MethodPatch, path, request, nil)
}

func (c *Client) SetSkillEnabled(ctx context.Context, id string, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	return c.SkillAction(ctx, id, action)
}
