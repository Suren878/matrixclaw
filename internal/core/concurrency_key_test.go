package core_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestOnlyChildrenWritingTheParentsDirectoryHoldItsKey(t *testing.T) {
	t.Parallel()
	app, _, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	registry := tools.NewRegistry(core.AgentToolExecutors(app)...)
	for _, tc := range []struct {
		args, want string
	}{
		{`{"description":"Count","prompt":"count files"}`, "subagents:/work"},
		{`{"description":"Count","prompt":"count files","isolation":"shared"}`, "subagents:/work"},
		{`{"description":"Review","prompt":"review the diff","readonly":true}`, ""},
		{`{"description":"Fix","prompt":"fix the bug","isolation":"worktree"}`, ""},
		{`{"description":"Build","prompt":"build it","background":true}`, ""},
	} {
		if got := registry.ConcurrencyKey("agent", tools.Call{WorkingDir: "/work/", Args: []byte(tc.args)}); got != tc.want {
			t.Errorf("key for %s = %q, want %q", tc.args, got, tc.want)
		}
	}
}
