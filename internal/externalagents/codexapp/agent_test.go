package codexapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentAvailableRejectsMacOSAppBundlePathWithoutExecutingIt(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	appBin := filepath.Join(dir, "Codex.app", "Contents", "MacOS", "Codex")
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

func TestStartReturnsLookupErrorWithoutExecutingOriginalPath(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "opened")
	appBin := filepath.Join(dir, "Codex.app", "Contents", "MacOS", "Codex")
	if err := os.MkdirAll(filepath.Dir(appBin), 0o755); err != nil {
		t.Fatalf("mkdir app bundle: %v", err)
	}
	if err := os.WriteFile(appBin, []byte("#!/bin/sh\nprintf opened > "+shellQuote(marker)+"\n"), 0o755); err != nil {
		t.Fatalf("write fake app binary: %v", err)
	}

	client, err := Start(context.Background(), ProcessOptions{Path: appBin})
	if err == nil {
		_ = client.Close()
		t.Fatal("Start returned nil error for app bundle path")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "cli") {
		t.Fatalf("Start error = %q, want CLI guidance", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("app bundle path was executed, marker err = %v", statErr)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
