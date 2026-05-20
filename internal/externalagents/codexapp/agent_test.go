package codexapp

import (
	"context"
	"os"
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

func TestLookupPathFindsUserNPMInstallOutsidePATH(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	binDir := filepath.Join(home, ".npm-global", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	binary := filepath.Join(binDir, "matrixclaw-test-codex")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	resolved, err := LookupPath("matrixclaw-test-codex")
	if err != nil {
		t.Fatalf("LookupPath() error = %v", err)
	}
	if resolved != binary {
		t.Fatalf("LookupPath() = %q, want %q", resolved, binary)
	}
}
