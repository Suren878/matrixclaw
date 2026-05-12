package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) ListSetupProviders(ctx context.Context) ([]setup.ProviderSetupItem, error) {
	var response setup.ProviderSetupListResponse
	path := "/v1/setup/providers?" + c.clientQuery()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Providers, nil
}

func (c *Client) ConfigureSetupProvider(ctx context.Context, providerID string, update setup.ProviderSetupUpdate) (setup.ProviderSetupItem, error) {
	var response setup.ProviderSetupResponse
	path := "/v1/setup/providers/" + escapedPath(providerID) + "?" + c.clientQuery()
	if err := c.doJSON(ctx, http.MethodPatch, path, update, &response); err != nil {
		return setup.ProviderSetupItem{}, err
	}
	return response.Provider, nil
}

func (c *Client) ProviderModels(ctx context.Context, providerID string, update setup.ProviderSetupUpdate) ([]string, error) {
	var response setup.ProviderModelsResponse
	path := "/v1/setup/providers/" + escapedPath(providerID) + "/models?" + c.clientQuery()
	if err := c.doJSON(ctx, http.MethodPost, path, update, &response); err != nil {
		return nil, err
	}
	return response.Models, nil
}

func (c *Client) DeleteSetupProvider(ctx context.Context, providerID string) error {
	var response setup.ProviderSetupOKResponse
	path := "/v1/setup/providers/" + escapedPath(providerID) + "?" + c.clientQuery()
	return c.doJSON(ctx, http.MethodDelete, path, nil, &response)
}
