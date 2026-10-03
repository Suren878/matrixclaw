package setup

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/textutil"
)

func (s *Service) GetMCPConfig() (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	return cfg.Modules.MCP, nil
}

func (s *Service) UpdateMCPConfig(update MCPConfigUpdate) (MCPConfig, error) {
	return s.updateMCP(func(cfg *MCPConfig) error {
		if update.Enabled != nil {
			cfg.Enabled = *update.Enabled
		}
		return nil
	})
}

func (s *Service) updateMCP(change func(*MCPConfig) error) (MCPConfig, error) {
	cfg, err := s.Update(func(cfg *Config) error { return change(&cfg.Modules.MCP) })
	if err != nil {
		return MCPConfig{}, err
	}
	return cfg.Modules.MCP, nil
}

func (s *Service) CreateMCPServer(server MCPServerConfig) (MCPConfig, error) {
	server = normalizeMCPServerForCreate(server)
	if server.ID == "" {
		return MCPConfig{}, fmt.Errorf("mcp server id is required")
	}
	if reservedExternalMCPServerID(server.ID) {
		return MCPConfig{}, fmt.Errorf("mcp server id %q is reserved for the Browser module", server.ID)
	}
	return s.updateMCP(func(cfg *MCPConfig) error {
		*cfg = normalizeMCPConfig(*cfg)
		if mcpServerConfigExists(cfg.Servers, server.ID) {
			return fmt.Errorf("mcp server already exists: %s", server.ID)
		}
		cfg.Servers = append(cfg.Servers, server)
		*cfg = normalizeMCPConfig(*cfg)
		if !mcpServerConfigExists(cfg.Servers, server.ID) {
			return fmt.Errorf("mcp server %s is incomplete", server.ID)
		}
		return nil
	})
}

func normalizeMCPServerForCreate(server MCPServerConfig) MCPServerConfig {
	server.ID = slugID(server.ID)
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		server.Name = server.ID
	}
	server.Transport = normalizeMCPTransport(server.Transport)
	server.Command = strings.TrimSpace(server.Command)
	server.Endpoint = strings.TrimRight(strings.TrimSpace(server.Endpoint), "/")
	server.ToolPrefix = slugID(server.ToolPrefix)
	if server.ToolPrefix == "" {
		server.ToolPrefix = server.ID
	}
	if server.Transport != "http" && server.Command == "" {
		server.Command = server.ID
	}
	if server.TimeoutSeconds < 0 {
		server.TimeoutSeconds = 0
	}
	server.Args = textutil.NonBlank(server.Args...)
	server.Env = trimStringMap(server.Env)
	return server
}

func (s *Service) UpdateMCPServer(serverID string, update MCPServerUpdate) (MCPConfig, error) {
	id := slugID(serverID)
	if reservedExternalMCPServerID(id) {
		return MCPConfig{}, fmt.Errorf("mcp server id %q is reserved for the Browser module", id)
	}
	return s.updateMCP(func(cfg *MCPConfig) error {
		for i := range cfg.Servers {
			if cfg.Servers[i].ID != id {
				continue
			}
			if update.Enabled != nil {
				cfg.Servers[i].Enabled = *update.Enabled
			}
			if update.Name != nil {
				cfg.Servers[i].Name = *update.Name
			}
			if update.Transport != nil {
				cfg.Servers[i].Transport = *update.Transport
			}
			if update.Command != nil {
				cfg.Servers[i].Command = *update.Command
			}
			if update.Args != nil {
				cfg.Servers[i].Args = update.Args
			}
			if update.Endpoint != nil {
				cfg.Servers[i].Endpoint = *update.Endpoint
			}
			if update.ToolPrefix != nil {
				cfg.Servers[i].ToolPrefix = *update.ToolPrefix
			}
			if update.ReadOnly != nil {
				cfg.Servers[i].ReadOnly = *update.ReadOnly
			}
			if update.TimeoutSeconds != nil {
				cfg.Servers[i].TimeoutSeconds = *update.TimeoutSeconds
			}
			*cfg = normalizeMCPConfig(*cfg)
			return nil
		}
		return fmt.Errorf("mcp server not found: %s", id)
	})
}

func (s *Service) DeleteMCPServer(serverID string) (MCPConfig, error) {
	id := slugID(serverID)
	if id == "" {
		return MCPConfig{}, fmt.Errorf("mcp server id is required")
	}
	if reservedExternalMCPServerID(id) {
		return MCPConfig{}, fmt.Errorf("mcp server id %q is reserved for the Browser module", id)
	}
	return s.updateMCP(func(cfg *MCPConfig) error {
		servers := make([]MCPServerConfig, 0, len(cfg.Servers))
		for _, server := range cfg.Servers {
			if slugID(server.ID) != id {
				servers = append(servers, server)
			}
		}
		if len(servers) == len(cfg.Servers) {
			return fmt.Errorf("mcp server not found: %s", id)
		}
		cfg.Servers = servers
		*cfg = normalizeMCPConfig(*cfg)
		return nil
	})
}

func mcpServerConfigExists(servers []MCPServerConfig, id string) bool {
	id = slugID(id)
	for _, server := range servers {
		if slugID(server.ID) == id {
			return true
		}
	}
	return false
}

func reservedExternalMCPServerID(id string) bool {
	return slugID(id) == BrowserModuleBrowser
}

func MCPConfigStatus(cfg MCPConfig) string {
	total := len(cfg.Servers)
	enabled := 0
	for _, server := range cfg.Servers {
		if server.Enabled {
			enabled++
		}
	}
	if !cfg.Enabled {
		if total == 0 {
			return "Disabled"
		}
		return fmt.Sprintf("Disabled · %d servers", total)
	}
	if total == 0 {
		return "Enabled · no servers"
	}
	return fmt.Sprintf("%d/%d enabled", enabled, total)
}
