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
	registry, err := NewRegistry(fakeAgent{id: "codex-app", aliases: []string{"codex-app", "codex", " CODEX "}})
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
	if canonical, ok := registry.CanonicalID(" CODEX "); !ok || canonical != "codex-app" {
		t.Fatalf("CanonicalID(alias) = %q/%v, want codex-app/true", canonical, ok)
	}
}

func TestRegistryRegisterDoesNotMutateOnAliasConflict(t *testing.T) {
	registry, err := NewRegistry(fakeAgent{id: "alpha"})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	err = registry.Register(fakeAgent{id: "beta", aliases: []string{"alpha"}})
	if err == nil {
		t.Fatal("expected alias conflict error")
	}
	if _, ok := registry.Get("beta"); ok {
		t.Fatal("conflicting agent should not be registered")
	}
	if _, ok := registry.Get("alpha"); !ok {
		t.Fatal("existing agent should remain registered")
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

func TestRegistryInfersRuntimeCapabilitiesConservatively(t *testing.T) {
	registry, err := NewRegistry(fakeRuntimeAgent{fakeAgent: fakeAgent{id: "runtime"}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	items := registry.List(context.Background())
	if len(items) != 1 {
		t.Fatalf("List() len = %d, want 1", len(items))
	}
	capabilities := items[0].Capabilities
	if !capabilities.StartSession || !capabilities.ResumeSession || !capabilities.StreamingEvents {
		t.Fatalf("runtime capabilities = %#v, want start/resume/streaming", capabilities)
	}
	if capabilities.ToolEvents || capabilities.Interrupt || capabilities.ConfigurablePath {
		t.Fatalf("runtime capabilities = %#v, want explicit opt-in for tool/interrupt/path", capabilities)
	}
}

func TestRegistryListsCapabilities(t *testing.T) {
	registry, err := NewRegistry(fakeCapableAgent{id: "codex-app"})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	items := registry.List(context.Background())
	if len(items) != 1 {
		t.Fatalf("List() len = %d, want 1", len(items))
	}
	if !items[0].Capabilities.StartSession || !items[0].Capabilities.ToolEvents || !items[0].Capabilities.ConfigurablePath {
		t.Fatalf("capabilities = %#v, want start/tool/path", items[0].Capabilities)
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

type fakeCapableAgent struct {
	fakeAgent
	id string
}

func (a fakeCapableAgent) ID() string {
	return a.id
}

func (a fakeCapableAgent) DisplayName() string {
	return a.id
}

func (a fakeCapableAgent) Available(context.Context) Availability {
	return Availability{Installed: true, Enabled: true}
}

func (a fakeCapableAgent) Capabilities() Capabilities {
	return Capabilities{StartSession: true, ToolEvents: true, ConfigurablePath: true}
}

type fakeRuntimeAgent struct {
	fakeAgent
}

func (a fakeRuntimeAgent) StartSession(context.Context, StartSessionRequest) (ExternalSession, error) {
	return ExternalSession{}, nil
}

func (a fakeRuntimeAgent) ResumeSession(context.Context, ExternalSession) (ExternalSession, error) {
	return ExternalSession{}, nil
}

func (a fakeRuntimeAgent) Send(context.Context, ExternalSession, Input) (<-chan Event, error) {
	return nil, nil
}

func (a fakeRuntimeAgent) Interrupt(context.Context, ExternalSession) error {
	return nil
}

func (a fakeRuntimeAgent) Close() error {
	return nil
}
