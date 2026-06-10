package setup

import (
	"fmt"
	"strings"
)

func (s *Service) GetMCPConfig() (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	return cfg.Modules.MCP, nil
}

func (s *Service) UpdateMCPConfig(update MCPConfigUpdate) (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	if update.Enabled != nil {
		cfg.Modules.MCP.Enabled = *update.Enabled
	}
	if err := s.store.Save(cfg); err != nil {
		return MCPConfig{}, err
	}
	return s.GetMCPConfig()
}

func (s *Service) CreateMCPServer(server MCPServerConfig) (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	server = normalizeMCPServerForCreate(server)
	if server.ID == "" {
		return MCPConfig{}, fmt.Errorf("mcp server id is required")
	}
	if reservedExternalMCPServerID(server.ID) {
		return MCPConfig{}, fmt.Errorf("mcp server id %q is reserved for the Browser module", server.ID)
	}
	cfg.Modules.MCP = normalizeMCPConfig(cfg.Modules.MCP)
	if mcpServerConfigExists(cfg.Modules.MCP.Servers, server.ID) {
		return MCPConfig{}, fmt.Errorf("mcp server already exists: %s", server.ID)
	}
	cfg.Modules.MCP.Servers = append(cfg.Modules.MCP.Servers, server)
	cfg.Modules.MCP = normalizeMCPConfig(cfg.Modules.MCP)
	if !mcpServerConfigExists(cfg.Modules.MCP.Servers, server.ID) {
		return MCPConfig{}, fmt.Errorf("mcp server %s is incomplete", server.ID)
	}
	if err := s.store.Save(cfg); err != nil {
		return MCPConfig{}, err
	}
	return s.GetMCPConfig()
}

func normalizeMCPServerForCreate(server MCPServerConfig) MCPServerConfig {
	server.ID = normalizeMCPID(server.ID)
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		server.Name = server.ID
	}
	server.Transport = normalizeMCPTransport(server.Transport)
	server.Command = strings.TrimSpace(server.Command)
	server.Endpoint = strings.TrimRight(strings.TrimSpace(server.Endpoint), "/")
	server.ToolPrefix = normalizeMCPID(server.ToolPrefix)
	if server.ToolPrefix == "" {
		server.ToolPrefix = server.ID
	}
	if server.Transport != "http" && server.Command == "" {
		server.Command = server.ID
	}
	if server.TimeoutSeconds < 0 {
		server.TimeoutSeconds = 0
	}
	server.Args = trimStringSlice(server.Args)
	server.Env = trimStringMap(server.Env)
	return server
}

func (s *Service) UpdateMCPServer(serverID string, update MCPServerUpdate) (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	id := normalizeMCPID(serverID)
	if reservedExternalMCPServerID(id) {
		return MCPConfig{}, fmt.Errorf("mcp server id %q is reserved for the Browser module", id)
	}
	for i := range cfg.Modules.MCP.Servers {
		if cfg.Modules.MCP.Servers[i].ID != id {
			continue
		}
		if update.Enabled != nil {
			cfg.Modules.MCP.Servers[i].Enabled = *update.Enabled
		}
		if update.Name != nil {
			cfg.Modules.MCP.Servers[i].Name = *update.Name
		}
		if update.Transport != nil {
			cfg.Modules.MCP.Servers[i].Transport = *update.Transport
		}
		if update.Command != nil {
			cfg.Modules.MCP.Servers[i].Command = *update.Command
		}
		if update.Args != nil {
			cfg.Modules.MCP.Servers[i].Args = update.Args
		}
		if update.Endpoint != nil {
			cfg.Modules.MCP.Servers[i].Endpoint = *update.Endpoint
		}
		if update.ToolPrefix != nil {
			cfg.Modules.MCP.Servers[i].ToolPrefix = *update.ToolPrefix
		}
		if update.ReadOnly != nil {
			cfg.Modules.MCP.Servers[i].ReadOnly = *update.ReadOnly
		}
		if update.RequireApproval != nil {
			cfg.Modules.MCP.Servers[i].RequireApproval = *update.RequireApproval
		}
		if update.TimeoutSeconds != nil {
			cfg.Modules.MCP.Servers[i].TimeoutSeconds = *update.TimeoutSeconds
		}
		cfg.Modules.MCP = normalizeMCPConfig(cfg.Modules.MCP)
		if err := s.store.Save(cfg); err != nil {
			return MCPConfig{}, err
		}
		return s.GetMCPConfig()
	}
	return MCPConfig{}, fmt.Errorf("mcp server not found: %s", id)
}

func (s *Service) DeleteMCPServer(serverID string) (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	id := normalizeMCPID(serverID)
	if id == "" {
		return MCPConfig{}, fmt.Errorf("mcp server id is required")
	}
	if reservedExternalMCPServerID(id) {
		return MCPConfig{}, fmt.Errorf("mcp server id %q is reserved for the Browser module", id)
	}
	servers := make([]MCPServerConfig, 0, len(cfg.Modules.MCP.Servers))
	deleted := false
	for _, server := range cfg.Modules.MCP.Servers {
		if normalizeMCPID(server.ID) == id {
			deleted = true
			continue
		}
		servers = append(servers, server)
	}
	if !deleted {
		return MCPConfig{}, fmt.Errorf("mcp server not found: %s", id)
	}
	cfg.Modules.MCP.Servers = servers
	cfg.Modules.MCP = normalizeMCPConfig(cfg.Modules.MCP)
	if err := s.store.Save(cfg); err != nil {
		return MCPConfig{}, err
	}
	return s.GetMCPConfig()
}

func mcpServerConfigExists(servers []MCPServerConfig, id string) bool {
	id = normalizeMCPID(id)
	for _, server := range servers {
		if normalizeMCPID(server.ID) == id {
			return true
		}
	}
	return false
}

func reservedExternalMCPServerID(id string) bool {
	return normalizeMCPID(id) == BrowserModuleBrowser
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
