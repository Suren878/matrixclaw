package core_test

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestDelegatedChildrenHoldTheirOwnKeyPerDirectory(t *testing.T) {
	app, _, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	registry := tools.NewRegistry(core.SubagentToolExecutors(app)...)
	for _, tc := range []struct {
		args, want string
	}{
		{`{"goal":"count files"}`, "subagents:/work"},
		{`{"goal":"count files","working_dir":"/other/"}`, "subagents:/other"},
	} {
		if got := registry.ConcurrencyKey("delegate_task", tools.Call{WorkingDir: "/work", Args: []byte(tc.args)}); got != tc.want {
			t.Errorf("key for %s = %q, want %q", tc.args, got, tc.want)
		}
	}
}
