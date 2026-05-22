package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) GetWebSearchConfig(ctx context.Context) (setup.WebSearchConfigResponse, error) {
	var response setup.WebSearchConfigResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/web-search", nil, &response); err != nil {
		return setup.WebSearchConfigResponse{}, err
	}
	return response, nil
}

func (c *Client) UpdateWebSearchConfig(ctx context.Context, update setup.WebSearchConfig) (setup.WebSearchConfigResponse, error) {
	var response setup.WebSearchConfigResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v1/modules/web-search", update, &response); err != nil {
		return setup.WebSearchConfigResponse{}, err
	}
	return response, nil
}
