package builtins

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents/codexapp"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestBuildRegistryUsesAliasConfigKeys(t *testing.T) {
	registry, runtimes, err := BuildRegistry(setup.ModulesConfig{
		ExternalAgents: map[string]setup.ExternalAgentConfig{
			"codex": {Enabled: true, Path: "/custom/bin/codex"},
		},
	})
	if err != nil {
		t.Fatalf("BuildRegistry() error = %v", err)
	}
	defer closeRuntimes(runtimes)

	if _, ok := registry.Get("codex"); !ok {
		t.Fatal("registry should resolve codex alias")
	}
	if len(runtimes) != 1 {
		t.Fatalf("runtimes len = %d, want 1", len(runtimes))
	}
	runtime, ok := runtimes[0].(*codexapp.Runtime)
	if !ok {
		t.Fatalf("runtime = %T, want *codexapp.Runtime", runtimes[0])
	}
	if !runtime.Agent.Enabled || runtime.Agent.Path != "/custom/bin/codex" {
		t.Fatalf("codex runtime config = enabled:%v path:%q, want enabled custom path", runtime.Agent.Enabled, runtime.Agent.Path)
	}
}
