package codexapp

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAgentAvailableRequiresInstalledBinaryForEnabled(t *testing.T) {
	agent := Agent{
		Path:    filepath.Join(t.TempDir(), "missing-codex"),
		Enabled: true,
	}

	availability := agent.Available(context.Background())
	if availability.Installed {
		t.Fatalf("Installed = true, want false for missing binary")
	}
	if availability.Enabled {
		t.Fatalf("Enabled = true, want false when binary is missing")
	}
	if availability.Detail == "" {
		t.Fatal("Detail should explain missing binary")
	}
}
