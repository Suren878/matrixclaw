package openaicodex

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCredentialPathsCoverEveryTokenStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
	covered := func(path string) bool {
		return slices.ContainsFunc(CredentialPaths(), func(place string) bool {
			return path == place || strings.HasPrefix(path, place+string(filepath.Separator))
		})
	}
	for _, env := range []struct{ name, value string }{
		{"MATRIXCLAW_AUTH_DIR", ""},
		{"MATRIXCLAW_AUTH_DIR", filepath.Join(dir, "auth")},
		{"MATRIXCLAW_CODEX_AUTH_FILE", filepath.Join(dir, "elsewhere", "codex.json")},
	} {
		t.Setenv(env.name, env.value)
		if !covered(AuthStorePath()) || !covered(filepath.Join(dir, "codex", "auth.json")) {
			t.Fatalf("%s=%q: %v misses a token store", env.name, env.value, CredentialPaths())
		}
	}
}
