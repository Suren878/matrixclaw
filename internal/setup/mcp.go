package setup

import "fmt"

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

func (s *Service) UpdateMCPServer(serverID string, update MCPServerUpdate) (MCPConfig, error) {
	cfg, err := s.Load()
	if err != nil {
		return MCPConfig{}, err
	}
	id := normalizeMCPID(serverID)
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
