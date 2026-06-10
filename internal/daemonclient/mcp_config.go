package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (c *Client) MCPConfig(ctx context.Context) (setup.MCPConfigResponse, error) {
	var response setup.MCPConfigResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/modules/mcp", nil, &response); err != nil {
		return setup.MCPConfigResponse{}, err
	}
	return response, nil
}

func (c *Client) UpdateMCPConfig(ctx context.Context, update setup.MCPConfigUpdate) (setup.MCPConfigResponse, error) {
	var response setup.MCPConfigResponse
	if err := c.doJSON(ctx, http.MethodPatch, "/v1/modules/mcp", update, &response); err != nil {
		return setup.MCPConfigResponse{}, err
	}
	return response, nil
}

func (c *Client) CreateMCPServer(ctx context.Context, server setup.MCPServerConfig) (setup.MCPConfigResponse, error) {
	var response setup.MCPConfigResponse
	request := setup.MCPServerCreateRequest{Server: server}
	if err := c.doJSON(ctx, http.MethodPost, "/v1/modules/mcp/servers", request, &response); err != nil {
		return setup.MCPConfigResponse{}, err
	}
	return response, nil
}

func (c *Client) UpdateMCPServer(ctx context.Context, serverID string, update setup.MCPServerUpdate) (setup.MCPConfigResponse, error) {
	var response setup.MCPConfigResponse
	path := "/v1/modules/mcp/" + escapedPath(serverID) + "/server"
	if err := c.doJSON(ctx, http.MethodPatch, path, update, &response); err != nil {
		return setup.MCPConfigResponse{}, err
	}
	return response, nil
}

func (c *Client) DeleteMCPServer(ctx context.Context, serverID string) (setup.MCPConfigResponse, error) {
	var response setup.MCPConfigResponse
	path := "/v1/modules/mcp/" + escapedPath(serverID) + "/server"
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &response); err != nil {
		return setup.MCPConfigResponse{}, err
	}
	return response, nil
}
