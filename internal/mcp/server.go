package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Suren878/matrixclaw/internal/tools"
)

type ToolRuntime interface {
	List() []tools.Spec
	Execute(ctx context.Context, toolID string, call tools.Call) (tools.Result, error)
}

func NewToolServer(runtime ToolRuntime) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{
		Name:    "matrixclaw",
		Version: "1.0.0",
	}, &sdk.ServerOptions{
		Instructions: "matrixclaw MCP server exposes the active matrixclaw daemon tool registry.",
	})
	if runtime == nil {
		return server
	}
	for _, spec := range runtime.List() {
		spec := spec
		server.AddTool(&sdk.Tool{
			Name:        spec.ID,
			Description: strings.TrimSpace(spec.Description),
			InputSchema: mcpInputSchema(spec.InputJSONSchema),
		}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			result, err := runtime.Execute(ctx, spec.ID, tools.Call{
				Args: req.Params.Arguments,
			})
			if err != nil {
				return nil, err
			}
			return toolResultToMCP(result), nil
		})
	}
	return server
}

func RunStdioServer(ctx context.Context, runtime ToolRuntime) error {
	return NewToolServer(runtime).Run(ctx, &sdk.StdioTransport{})
}

func mcpInputSchema(schema json.RawMessage) any {
	if len(schema) == 0 {
		return json.RawMessage(`{"type":"object","additionalProperties":true}`)
	}
	return schema
}

func toolResultToMCP(result tools.Result) *sdk.CallToolResult {
	content := strings.TrimSpace(result.Content)
	if content == "" {
		content = fmt.Sprintf("%s", result.Status)
	}
	out := &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: content}},
		IsError: result.IsError || result.Status == tools.ResultStatusError,
	}
	structured := map[string]any{
		"status": result.Status,
	}
	if result.Metadata != nil {
		structured["metadata"] = result.Metadata
	}
	if result.FileVersion != nil {
		structured["file_version"] = result.FileVersion
	}
	if result.Background != nil {
		structured["background"] = result.Background
	}
	if result.Approval != nil {
		structured["approval"] = result.Approval
		out.IsError = true
	}
	out.StructuredContent = structured
	return out
}
