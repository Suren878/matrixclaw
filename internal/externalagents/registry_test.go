package externalagents

import (
	"context"
	"testing"
)

func TestRegistryListsDescriptorsSorted(t *testing.T) {
	registry, err := NewRegistry(
		fakeAgent{id: "zeta", displayName: "Zeta", installed: true, enabled: true},
		fakeAgent{id: "alpha", displayName: "Alpha", installed: false},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	items := registry.List(context.Background())
	if len(items) != 2 {
		t.Fatalf("List() len = %d, want 2", len(items))
	}
	if items[0].ID != "alpha" || items[1].ID != "zeta" {
		t.Fatalf("List() order = %q, %q", items[0].ID, items[1].ID)
	}
	if !items[1].Enabled {
		t.Fatal("zeta should be enabled")
	}
}

func TestRegistryRejectsDuplicateIDs(t *testing.T) {
	_, err := NewRegistry(
		fakeAgent{id: "codex-app"},
		fakeAgent{id: " CODEX-APP "},
	)
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestRegistryResolvesAliases(t *testing.T) {
	registry, err := NewRegistry(fakeAgent{id: "codex-app", aliases: []string{"codex"}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	agent, ok := registry.Get(" CODEX ")
	if !ok {
		t.Fatal("Get(alias) ok = false, want true")
	}
	if agent.ID() != "codex-app" {
		t.Fatalf("Get(alias).ID() = %q, want codex-app", agent.ID())
	}

	items := registry.List(context.Background())
	if len(items) != 1 || len(items[0].Aliases) != 1 || items[0].Aliases[0] != "codex" {
		t.Fatalf("List aliases = %#v, want codex", items)
	}
}

func TestRegistryRejectsAliasConflicts(t *testing.T) {
	_, err := NewRegistry(
		fakeAgent{id: "codex-app", aliases: []string{"codex"}},
		fakeAgent{id: "codex"},
	)
	if err == nil {
		t.Fatal("expected alias conflict error")
	}
}

type fakeAgent struct {
	id          string
	aliases     []string
	displayName string
	installed   bool
	enabled     bool
}

func (a fakeAgent) ID() string {
	return a.id
}

func (a fakeAgent) DisplayName() string {
	return a.displayName
}

func (a fakeAgent) Aliases() []string {
	return a.aliases
}

func (a fakeAgent) Available(context.Context) Availability {
	return Availability{Installed: a.installed, Enabled: a.enabled}
}
