package externalagents

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	agents  map[string]Agent
	aliases map[string]string
}

func NewRegistry(agents ...Agent) (*Registry, error) {
	registry := &Registry{
		agents:  map[string]Agent{},
		aliases: map[string]string{},
	}
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
	if canonical, exists := r.aliases[id]; exists {
		return fmt.Errorf("externalagents: agent id %q conflicts with alias for %q", id, canonical)
	}
	r.agents[id] = agent
	for _, alias := range agentAliases(agent) {
		alias = normalizeID(alias)
		if alias == "" || alias == id {
			continue
		}
		if _, exists := r.agents[alias]; exists {
			return fmt.Errorf("externalagents: alias %q conflicts with registered agent id", alias)
		}
		if canonical, exists := r.aliases[alias]; exists {
			return fmt.Errorf("externalagents: duplicate alias %q for %q and %q", alias, canonical, id)
		}
		r.aliases[alias] = id
	}
	return nil
}

func (r *Registry) Get(id string) (Agent, bool) {
	id = normalizeID(id)
	if canonical, ok := r.aliases[id]; ok {
		id = canonical
	}
	agent, ok := r.agents[id]
	return agent, ok
}

func (r *Registry) List(ctx context.Context) []Descriptor {
	out := make([]Descriptor, 0, len(r.agents))
	for id, agent := range r.agents {
		availability := agent.Available(ctx)
		out = append(out, Descriptor{
			ID:          id,
			Aliases:     agentAliases(agent),
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

func agentAliases(agent Agent) []string {
	aliased, ok := agent.(AliasProvider)
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, alias := range aliased.Aliases() {
		alias = normalizeID(alias)
		if alias == "" {
			continue
		}
		if _, exists := seen[alias]; exists {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

func normalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
