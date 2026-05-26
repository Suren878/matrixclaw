package builtins

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/externalagents/claudecode"
	"github.com/Suren878/matrixclaw/internal/setup"
)

func TestBuildRegistryIncludesClaudeCode(t *testing.T) {
	registry, runtimes, err := BuildRegistry(setup.ModulesConfig{
		ExternalAgents: map[string]setup.ExternalAgentConfig{
			"claude": {Enabled: true, Path: "/tmp/claude"},
		},
	})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	defer closeRuntimes(runtimes)

	canonical, ok := registry.CanonicalID("claude")
	if !ok || canonical != claudecode.AgentID {
		t.Fatalf("CanonicalID(claude) = %q, %v", canonical, ok)
	}
	agent, ok := registry.Get("claude-code")
	if !ok {
		t.Fatalf("claude-code not registered")
	}
	if agent.DisplayName() != "Claude Code" {
		t.Fatalf("DisplayName = %q", agent.DisplayName())
	}
}
