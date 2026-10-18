package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
)

// SessionGoal returns the session's goal, nil when it has none.
func (c *Client) SessionGoal(ctx context.Context, sessionID string) (*core.SessionGoal, error) {
	var response core.GoalResponse
	err := c.doJSON(ctx, http.MethodGet, goalPath(sessionID), nil, &response)
	return response.Goal, err
}

// SetSessionGoal gives the session a new goal, set by this client.
func (c *Client) SetSessionGoal(ctx context.Context, sessionID string, input core.GoalInput) (*core.SessionGoal, error) {
	input.Client, input.ExternalKey, input.ClientCapabilities = c.ClientName, c.ExternalKey, c.Capabilities
	var response core.GoalResponse
	err := c.doJSON(ctx, http.MethodPut, goalPath(sessionID), input, &response)
	return response.Goal, err
}

// ChangeSessionGoal pauses or resumes the session's goal.
func (c *Client) ChangeSessionGoal(ctx context.Context, sessionID string, action core.GoalAction) (*core.SessionGoal, error) {
	var response core.GoalResponse
	err := c.doJSON(ctx, http.MethodPost, goalPath(sessionID)+"/"+escapedPath(string(action)), nil, &response)
	return response.Goal, err
}

// ClearSessionGoal removes the session's goal.
func (c *Client) ClearSessionGoal(ctx context.Context, sessionID string) error {
	return c.doJSON(ctx, http.MethodDelete, goalPath(sessionID), nil, nil)
}

func goalPath(sessionID string) string {
	return "/v1/sessions/" + escapedPath(sessionID) + "/goal"
}
