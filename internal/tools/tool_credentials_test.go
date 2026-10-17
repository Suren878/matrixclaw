package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const credentialSecret = "private-provider-key"

// protectForTest registers places as credentials for the test only.
func protectForTest(t *testing.T, places ...string) {
	t.Helper()
	credentials.mu.Lock()
	saved := credentials.sources
	credentials.mu.Unlock()
	ProtectCredentials(func() []string { return places })
	t.Cleanup(func() {
		credentials.mu.Lock()
		credentials.sources = saved
		credentials.mu.Unlock()
	})
}

func writeSecret(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"api_key":"`+credentialSecret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func executeReadForTest(t *testing.T, path string, workingDir string) Result {
	t.Helper()
	args, err := json.Marshal(ReadParams{FilePath: path})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewRegistry(CoreExecutors()...).Execute(context.Background(), readToolName, Call{WorkingDir: workingDir, Args: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestReadRefusesRegisteredCredentials(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "config", "setup.json")
	authDir := filepath.Join(dir, "auth")
	protectForTest(t, setupPath, authDir)
	link := filepath.Join(dir, "provider.json")
	writeSecret(t, setupPath)
	if err := os.Symlink(setupPath, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		setupPath,
		setupPath + ".before-upgrade",
		link,
		filepath.Join(authDir, "openai-codex.json"),
	} {
		writeSecret(t, path)
		if result := executeReadForTest(t, path, dir); !result.IsError() || strings.Contains(result.Content, credentialSecret) {
			t.Fatalf("%s: result = %#v, want a refusal", path, result)
		}
	}
	if result := runMutation(t, dir, "write", WriteParams{FilePath: filepath.Join(authDir, "new.json"), Content: "{}"}); !result.IsError() {
		t.Fatalf("write into credentials: %#v", result)
	}
}

func TestReadAllowsAFileNamedLikeACredential(t *testing.T) {
	dir := t.TempDir()
	protectForTest(t, filepath.Join(dir, "private", "setup.json"))
	projectPath := filepath.Join(dir, "project", "setup.json")
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte(`{"example":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := executeReadForTest(t, projectPath, dir); result.IsError() || !strings.Contains(result.Content, `"example":true`) {
		t.Fatalf("result = %#v, want the file", result)
	}
}

func TestGrepSkipsAndRefusesCredentials(t *testing.T) {
	dir := t.TempDir()
	setupPath := filepath.Join(dir, "setup.json")
	writeSecret(t, setupPath)
	protectForTest(t, setupPath)
	if err := os.WriteFile(filepath.Join(dir, "public.txt"), []byte("public-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: credentialSecret + "|public-marker", Path: dir})
	if result.IsError() || strings.Contains(result.Content, credentialSecret) || !strings.Contains(result.Content, "public-marker") {
		t.Fatalf("search = %#v, want the public match only", result)
	}
	if result := runSearchTool(t, NewGrepExecutor(), dir, GrepParams{Pattern: "api_key", Path: setupPath}); !result.IsError() {
		t.Fatalf("direct search = %#v, want a refusal", result)
	}
}
