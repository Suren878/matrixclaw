package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

func (c *Client) SessionPermissionRules(ctx context.Context, sessionID string) ([]permission.Rule, error) {
	var response core.PermissionRulesResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/permission-rules"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Rules, nil
}

func (c *Client) AddPermissionRule(ctx context.Context, sessionID string, request core.PermissionRuleRequest) (permission.Rule, error) {
	var response core.PermissionRuleResponse
	path := "/v1/sessions/" + escapedPath(sessionID) + "/permission-rules"
	if err := c.doJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return permission.Rule{}, err
	}
	return response.Rule, nil
}

func (c *Client) DeletePermissionRule(ctx context.Context, ruleID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/permission-rules/"+escapedPath(ruleID), nil, nil)
}
