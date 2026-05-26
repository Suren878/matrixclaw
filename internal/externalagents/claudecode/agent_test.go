package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentDescriptorUsesClaudeCodeDefaults(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '2.1.146 (Claude Code)\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	agent := Agent{Path: bin, Enabled: true}
	if got := agent.ID(); got != "claude-code" {
		t.Fatalf("ID() = %q", got)
	}
	if got := agent.DisplayName(); got != "Claude Code" {
		t.Fatalf("DisplayName() = %q", got)
	}
	availability := agent.Available(context.Background())
	if !availability.Installed || !availability.Enabled {
		t.Fatalf("availability = %#v", availability)
	}
	if availability.Mode != "cli" {
		t.Fatalf("mode = %q, want cli", availability.Mode)
	}
	if availability.Version != "2.1.146 (Claude Code)" {
		t.Fatalf("version = %q", availability.Version)
	}
}
