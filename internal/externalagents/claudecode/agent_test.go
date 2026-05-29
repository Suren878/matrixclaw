package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestAgentAvailableRejectsMacOSAppBundlePathWithoutExecutingIt(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	appBin := filepath.Join(dir, "Claude.app", "Contents", "MacOS", "Claude")
	if err := os.MkdirAll(filepath.Dir(appBin), 0o755); err != nil {
		t.Fatalf("mkdir app bundle: %v", err)
	}
	if err := os.WriteFile(appBin, []byte("#!/bin/sh\nprintf opened > "+shellQuote(marker)+"\n"), 0o755); err != nil {
		t.Fatalf("write fake app binary: %v", err)
	}

	availability := Agent{Path: appBin, Enabled: true}.Available(context.Background())
	if availability.Installed {
		t.Fatalf("Installed = true for app bundle path: %#v", availability)
	}
	if !strings.Contains(strings.ToLower(availability.Detail), "cli") {
		t.Fatalf("Detail = %q, want CLI guidance", availability.Detail)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("app bundle path was executed, marker err = %v", err)
	}
}
