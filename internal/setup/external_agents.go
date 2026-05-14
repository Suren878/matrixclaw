package setup

import (
	"fmt"
	"strings"
)

func (s *Service) ExternalAgentConfig(id string) ExternalAgentConfig {
	cfg, err := s.Load()
	if err != nil {
		return ExternalAgentConfig{}
	}
	return cfg.ExternalAgentConfig(id)
}

func (s *Service) UpdateExternalAgent(id string, update ExternalAgentConfig) (Config, error) {
	id = normalizeExternalAgentID(id)
	if id == "" {
		return Config{}, fmt.Errorf("external agent id is required")
	}
	cfg, err := s.Load()
	if err != nil {
		return Config{}, err
	}
	if cfg.Modules.ExternalAgents == nil {
		cfg.Modules.ExternalAgents = map[string]ExternalAgentConfig{}
	}
	current := cfg.Modules.ExternalAgents[id]
	current.Enabled = update.Enabled
	if strings.TrimSpace(update.Path) != "" {
		current.Path = strings.TrimSpace(update.Path)
	}
	if !current.Enabled && strings.TrimSpace(current.Path) == "" {
		delete(cfg.Modules.ExternalAgents, id)
	} else {
		cfg.Modules.ExternalAgents[id] = current
	}
	if err := s.store.Save(cfg); err != nil {
		return Config{}, err
	}
	return s.Load()
}

func (cfg Config) ExternalAgentConfig(id string) ExternalAgentConfig {
	id = normalizeExternalAgentID(id)
	if id == "" || len(cfg.Modules.ExternalAgents) == 0 {
		return ExternalAgentConfig{}
	}
	return cfg.Modules.ExternalAgents[id]
}

func normalizeExternalAgentID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	switch id {
	case "codex":
		return "codex-app"
	default:
		return id
	}
}
