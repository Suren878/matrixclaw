package daemonclient

import (
	"context"
	"net/http"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func (c *Client) Tools(ctx context.Context) ([]tools.Spec, error) {
	var response core.ToolsResponse
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tools", nil, &response); err != nil {
		return nil, err
	}
	return response.Tools, nil
}

func (c *Client) ExecuteTool(ctx context.Context, input core.ExecuteToolInput) (core.ExecuteToolResult, error) {
	var response core.ToolExecuteResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/tools/execute", input, &response); err != nil {
		return core.ExecuteToolResult{}, err
	}
	return response.Result, nil
}
