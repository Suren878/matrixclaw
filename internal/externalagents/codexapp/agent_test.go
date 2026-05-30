package codexapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestAppServerProcessExitSetsClientError(t *testing.T) {
	script := writeExecutableScript(t, "exit-now", "#!/bin/sh\nexit 42\n")

	client, err := Start(context.Background(), ProcessOptions{Path: script, Args: []string{"app-server"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close()

	waitForClientDone(t, client)
	err = client.Err()
	if err == nil || !strings.Contains(err.Error(), "codex app-server exited") {
		t.Fatalf("Client.Err() = %v, want app-server exit error", err)
	}
}

func TestAppServerProcessCloseIsIdempotent(t *testing.T) {
	script := writeExecutableScript(t, "sleep-now", "#!/bin/sh\nsleep 10\n")

	client, err := Start(context.Background(), ProcessOptions{Path: script, Args: []string{"app-server"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	firstErr := client.Close()
	secondErr := client.Close()

	if secondErr != nil && strings.Contains(secondErr.Error(), "Wait was already called") {
		t.Fatalf("second Close error = %v, want idempotent close without second Wait", secondErr)
	}
	if firstErr != nil && secondErr != nil && firstErr.Error() != secondErr.Error() {
		t.Fatalf("Close errors differ: first %v, second %v", firstErr, secondErr)
	}

	select {
	case <-client.done:
	case <-time.After(time.Second):
		t.Fatalf("client done did not close after process Close")
	}
}

func writeExecutableScript(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
