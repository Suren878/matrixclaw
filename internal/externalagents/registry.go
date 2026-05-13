package externalagents

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	agents map[string]Agent
}

func NewRegistry(agents ...Agent) (*Registry, error) {
	registry := &Registry{agents: map[string]Agent{}}
	for _, agent := range agents {
		if err := registry.Register(agent); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(agent Agent) error {
	if agent == nil {
		return fmt.Errorf("externalagents: register nil agent")
	}
	id := normalizeID(agent.ID())
	if id == "" {
		return fmt.Errorf("externalagents: register agent with empty id")
	}
	if _, exists := r.agents[id]; exists {
		return fmt.Errorf("externalagents: duplicate agent id %q", id)
	}
	r.agents[id] = agent
	return nil
}

func (r *Registry) Get(id string) (Agent, bool) {
	agent, ok := r.agents[normalizeID(id)]
	return agent, ok
}

func (r *Registry) List(ctx context.Context) []Descriptor {
	out := make([]Descriptor, 0, len(r.agents))
	for id, agent := range r.agents {
		availability := agent.Available(ctx)
		out = append(out, Descriptor{
			ID:          id,
			DisplayName: agent.DisplayName(),
			Installed:   availability.Installed,
			Enabled:     availability.Enabled,
			AuthState:   availability.AuthState,
			Mode:        availability.Mode,
			Detail:      availability.Detail,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func normalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
