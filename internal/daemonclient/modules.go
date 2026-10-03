package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/modules"
)

// Modules is the status of every daemon module.
func (c *Client) Modules(ctx context.Context) ([]modules.Status, error) {
	var response modules.StatusResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules", nil, &response); err != nil {
		return nil, err
	}
	return response.Modules, nil
}

// ModuleSettings is a module's settings screen.
func (c *Client) ModuleSettings(ctx context.Context, moduleID string) (modules.Settings, error) {
	var response modules.Settings
	if err := c.doJSON(ctx, http.MethodGet, "/v1/settings/"+escapedPath(moduleID), nil, &response); err != nil {
		return modules.Settings{}, err
	}
	return response, nil
}

// ChangeModuleSetting sets a module's setting; it may install or download,
// so it waits as long as a voice runtime action.
func (c *Client) ChangeModuleSetting(ctx context.Context, moduleID string, request modules.ChangeRequest) (modules.ChangeResponse, error) {
	var response modules.ChangeResponse
	if err := c.doJSONWithClient(ctx, http.MethodPost, "/v1/settings/"+escapedPath(moduleID), request, &response, c.voiceRuntimeHTTPClient()); err != nil {
		return modules.ChangeResponse{}, err
	}
	return response, nil
}
