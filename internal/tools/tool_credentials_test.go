package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadExecutorBlocksMatrixclawSetupCredentials(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	secret := "private-provider-key"
	if err := os.WriteFile(setupPath, []byte(`{"api_key":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	result := executeReadForTest(t, setupPath, dir)
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
	if strings.Contains(result.Content, secret) {
		t.Fatalf("protected credential leaked in result: %q", result.Content)
	}
}

func TestReadExecutorBlocksSetupSymlink(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	if err := os.WriteFile(setupPath, []byte(`{"api_key":"private-provider-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(dir, "provider.json")
	if err := os.Symlink(setupPath, linkPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	result := executeReadForTest(t, linkPath, dir)
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
}

func TestReadExecutorBlocksDaemonEnvironmentCredentials(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	envPath := filepath.Join(dir, "daemon.env")
	secret := "private-provider-key"
	if err := os.WriteFile(envPath, []byte("PRIVATE_PROVIDER_API_KEY="+secret), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	result := executeReadForTest(t, envPath, dir)
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
	if strings.Contains(result.Content, secret) {
		t.Fatalf("protected credential leaked in result: %q", result.Content)
	}
}

func TestReadExecutorBlocksSetupCredentialBackup(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	backupPath := setupPath + ".before-upgrade"
	if err := os.WriteFile(backupPath, []byte(`{"api_key":"private-provider-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	result := executeReadForTest(t, backupPath, dir)
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
}

func TestReadExecutorBlocksLegacyMatrixclawCredentials(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "share")
	legacyPath := filepath.Join(dataDir, "matrixclaw", "matrixclaw.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"daemon":{"clients":{"telegram":{"bot_token":"private-bot-token"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataDir)

	result := executeReadForTest(t, legacyPath, dir)
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
}

func TestReadExecutorBlocksLegacyProviderCredentials(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "share")
	providersPath := filepath.Join(dataDir, "matrixclaw", "providers.json")
	if err := os.MkdirAll(filepath.Dir(providersPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providersPath, []byte(`[{"api_key":"private-provider-key"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataDir)

	result := executeReadForTest(t, providersPath, dir)
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
}

func TestReadExecutorAllowsUnrelatedSetupFile(t *testing.T) {
	dir := t.TempDir()
	protectedPath := filepath.Join(dir, "private", "setup.json")
	projectPath := filepath.Join(dir, "project", "setup.json")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`{"example":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", protectedPath)

	result := executeReadForTest(t, projectPath, dir)
	if result.IsError {
		t.Fatalf("result = %#v, want ordinary file content", result)
	}
	if !strings.Contains(result.Content, `"example":true`) {
		t.Fatalf("content = %q, want ordinary setup content", result.Content)
	}
}

func TestGrepExecutorSkipsMatrixclawSetupCredentials(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	secret := "private-provider-key"
	if err := os.WriteFile(setupPath, []byte(`{"api_key":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "public.txt"), []byte("public-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	args, err := json.Marshal(GrepParams{Pattern: "private-provider-key|public-marker", Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewGrepExecutor().Execute(context.Background(), Call{WorkingDir: dir, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result = %#v, want safe search results", result)
	}
	if strings.Contains(result.Content, secret) {
		t.Fatalf("protected credential leaked in grep result: %q", result.Content)
	}
	if !strings.Contains(result.Content, "public-marker") {
		t.Fatalf("content = %q, want public match", result.Content)
	}
}

func TestGrepExecutorBlocksDirectCredentialSearch(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	if err := os.WriteFile(setupPath, []byte(`{"api_key":"private-provider-key"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MATRIXCLAW_SETUP_PATH", setupPath)

	args, err := json.Marshal(GrepParams{Pattern: "api_key", Path: setupPath})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewGrepExecutor().Execute(context.Background(), Call{WorkingDir: dir, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("result = %#v, want protected-file error", result)
	}
}

func executeReadForTest(t *testing.T, path string, workingDir string) Result {
	t.Helper()
	args, err := json.Marshal(ReadParams{FilePath: path})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewReadExecutor().Execute(context.Background(), Call{WorkingDir: workingDir, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
