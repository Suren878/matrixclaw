package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) BrowserModule(ctx context.Context) (setup.BrowserModuleDescriptor, error) {
	var response setup.BrowserModuleResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/browser", nil, &response); err != nil {
		return setup.BrowserModuleDescriptor{}, err
	}
	return response.Module, nil
}

func (c *Client) UpdateBrowserModule(ctx context.Context, update setup.BrowserModuleUpdate) (setup.BrowserModuleDescriptor, error) {
	var response setup.BrowserModuleResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v1/modules/browser", update, &response); err != nil {
		return setup.BrowserModuleDescriptor{}, err
	}
	return response.Module, nil
}

func (c *Client) BrowserProviderAction(ctx context.Context, providerID string, request setup.BrowserProviderActionRequest) (setup.BrowserProviderOption, error) {
	var response setup.BrowserProviderActionResponse
	path := "/v1/modules/browser/providers/" + escapedPath(providerID) + "/action"
	if err := c.doJSONWithClient(ctx, http.MethodPost, path, request, &response, c.voiceRuntimeHTTPClient()); err != nil {
		return setup.BrowserProviderOption{}, err
	}
	return response.Provider, nil
}
