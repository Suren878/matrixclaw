package builtins

import (
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/externalagents/codexapp"
	"github.com/Suren878/matrixclaw/internal/setup"
)

type Factory struct {
	ID  string
	New func(setup.ExternalAgentConfig) externalagents.RuntimeAgent
}

func Factories() []Factory {
	return []Factory{
		{
			ID: codexapp.AgentID,
			New: func(cfg setup.ExternalAgentConfig) externalagents.RuntimeAgent {
				return codexapp.NewRuntime(codexapp.RuntimeOptions{
					Path:    cfg.Path,
					Enabled: cfg.Enabled,
				})
			},
		},
	}
}

func BuildRegistry(modules setup.ModulesConfig) (*externalagents.Registry, []externalagents.RuntimeAgent, error) {
	factories := Factories()
	runtimes := make([]externalagents.RuntimeAgent, 0, len(factories))
	for _, factory := range factories {
		if factory.New == nil {
			continue
		}
		runtimes = append(runtimes, factory.New(modules.ExternalAgents[factory.ID]))
	}
	registry, err := externalagents.NewRegistry(runtimeAgentsAsAgents(runtimes)...)
	if err != nil {
		closeRuntimes(runtimes)
		return nil, nil, err
	}
	return registry, runtimes, nil
}

func runtimeAgentsAsAgents(runtimes []externalagents.RuntimeAgent) []externalagents.Agent {
	agents := make([]externalagents.Agent, 0, len(runtimes))
	for _, runtime := range runtimes {
		agents = append(agents, runtime)
	}
	return agents
}

func closeRuntimes(runtimes []externalagents.RuntimeAgent) {
	for _, runtime := range runtimes {
		if runtime != nil {
			_ = runtime.Close()
		}
	}
}
