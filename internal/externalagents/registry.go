package externalagents

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	agents  map[string]RuntimeAgent
	aliases map[string]string
}

func NewRegistry(agents ...RuntimeAgent) (*Registry, error) {
	registry := &Registry{
		agents:  map[string]RuntimeAgent{},
		aliases: map[string]string{},
	}
	for _, agent := range agents {
		if err := registry.Register(agent); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

// Register adds agent under its ID and any extra names it answers to.
func (r *Registry) Register(agent RuntimeAgent, aliases ...string) error {
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
	for _, alias := range aliases {
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
	r.agents[id] = agent
	return nil
}

func (r *Registry) Get(id string) (RuntimeAgent, bool) {
	id, _ = r.CanonicalID(id)
	agent, ok := r.agents[id]
	return agent, ok
}

func (r *Registry) CanonicalID(id string) (string, bool) {
	id = normalizeID(id)
	if id == "" {
		return "", false
	}
	if _, ok := r.agents[id]; ok {
		return id, true
	}
	if canonical, ok := r.aliases[id]; ok {
		return canonical, true
	}
	return "", false
}

func (r *Registry) List(ctx context.Context) []Descriptor {
	out := make([]Descriptor, 0, len(r.agents))
	for id, agent := range r.agents {
		availability := agent.Available(ctx)
		out = append(out, Descriptor{
			ID:          id,
			Aliases:     r.aliasesOf(id),
			DisplayName: agent.DisplayName(),
			Installed:   availability.Installed,
			Enabled:     availability.Enabled,
			Mode:        availability.Mode,
			Path:        availability.Path,
			Version:     availability.Version,
			Detail:      availability.Detail,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Registry) aliasesOf(id string) []string {
	var out []string
	for alias, canonical := range r.aliases {
		if canonical == id {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
