package core

import (
	"path/filepath"
	"testing"
)

func TestSubagentWorktreeRepoIDDistinguishesSameBasename(t *testing.T) {
	first := subagentWorktreeRepoID(filepath.Join(string(filepath.Separator), "srv", "one", "project"))
	second := subagentWorktreeRepoID(filepath.Join(string(filepath.Separator), "srv", "two", "project"))
	if first == second {
		t.Fatalf("repo IDs collide for different roots: %q", first)
	}
	if first != subagentWorktreeRepoID(filepath.Join(string(filepath.Separator), "srv", "one", "project")) {
		t.Fatal("repo ID is not stable")
	}
}
