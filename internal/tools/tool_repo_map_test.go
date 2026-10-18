package tools

import (
	"os/exec"
	"strings"
	"testing"
)

func TestRepoMapFollowsGitignoreAndFocus(t *testing.T) {
	tree := []string{
		".gitignore", "gen/\n",
		"store/store.go", "package store\n\ntype Store struct{}\n\nfunc OpenStore() *Store { return nil }\n",
		"store/cache.go", "package store\n\nfunc WarmCacheEntries() {}\n",
		"api/handler.go", "package api\n\nimport \"x.io/store\"\n\nfunc Handle() { store.WarmCacheEntries() }\n",
		"gen/models.go", "package gen\n\nfunc GeneratedModel() {}\n",
		".hidden/tool.go", "package hidden\n\nfunc HiddenTool() {}\n",
		"vendor/lib/lib.go", "package lib\n\nfunc VendoredLib() {}\n",
	}
	check := func(t *testing.T, dir string) {
		t.Helper()
		result := runSearchTool(t, NewRepoMapExecutor(), dir, RepoMapParams{})
		for _, want := range []string{"store/store.go:", "func OpenStore() *Store", "store/cache.go:"} {
			if !strings.Contains(result.Content, want) {
				t.Errorf("map lacks %q:\n%s", want, result.Content)
			}
		}
		for _, left := range []string{"GeneratedModel", "HiddenTool", "VendoredLib"} {
			if strings.Contains(result.Content, left) {
				t.Errorf("map shows %s:\n%s", left, result.Content)
			}
		}
		focused := runSearchTool(t, NewRepoMapExecutor(), dir, RepoMapParams{FocusFiles: []string{"api/handler.go"}})
		first, _, _ := strings.Cut(strings.SplitN(focused.Content, "\n\n", 2)[1], "\n")
		if first != "store/cache.go:" || strings.Contains(focused.Content, "api/handler.go") {
			t.Errorf("focused map:\n%s", focused.Content)
		}
	}
	t.Run("walk", func(t *testing.T) {
		dir := t.TempDir()
		writeTree(t, dir, tree...)
		check(t, dir)
	})
	t.Run("git", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git")
		}
		dir := t.TempDir()
		writeTree(t, dir, tree...)
		if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		check(t, dir)
	})
}
