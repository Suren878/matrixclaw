package mcp

import (
	"context"
	"time"

	mcpbridge "github.com/Suren878/matrixclaw/internal/mcp"
	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/tools"
)

type Module struct {
	client *mcpbridge.ClientModule
}

func New(ctx context.Context, cfg setup.MCPConfig) (*Module, error) {
	client, err := mcpbridge.NewClientModule(ctx, bridgeConfig(cfg))
	if err != nil {
		return nil, err
	}
	return &Module{client: client}, nil
}

func (m *Module) ID() string {
	return "mcp"
}

func (m *Module) Name() string {
	return "MCP"
}

func (m *Module) RegisterTools(registry *tools.Registry) error {
	if m == nil || m.client == nil {
		return nil
	}
	return m.client.RegisterTools(registry)
}

func (m *Module) Context() string {
	if m == nil || m.client == nil {
		return ""
	}
	return m.client.Context()
}

func (m *Module) Close() error {
	if m == nil || m.client == nil {
		return nil
	}
	return m.client.Close()
}

func bridgeConfig(cfg setup.MCPConfig) mcpbridge.Config {
	out := mcpbridge.Config{Enabled: cfg.Enabled}
	out.Servers = make([]mcpbridge.ServerConfig, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		out.Servers = append(out.Servers, mcpbridge.ServerConfig{
			ID:              server.ID,
			Name:            server.Name,
			Enabled:         server.Enabled,
			Transport:       server.Transport,
			Command:         server.Command,
			Args:            append([]string(nil), server.Args...),
			Env:             copyMap(server.Env),
			Endpoint:        server.Endpoint,
			ToolPrefix:      server.ToolPrefix,
			ReadOnly:        server.ReadOnly,
			RequireApproval: server.RequireApproval,
			Timeout:         time.Duration(server.TimeoutSeconds) * time.Second,
		})
	}
	return out
}

func copyMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
