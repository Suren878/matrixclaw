package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/hooks"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/store"
)

// hookCore is a permission core running list.
func hookCore(t *testing.T, list ...hooks.Hook) (*core.Core, *store.SQLiteStore, *commandTool, string) {
	t.Helper()
	return permissionCore(t, core.Config{Hooks: hooks.NewRunner(nil, func() hooks.Config { return hooks.Config{Hooks: list} }, filepath.Join(t.TempDir(), "hooks-trust.json"), nil)})
}

func TestPreToolUseHookCannotOverrideADenyRule(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	app, db, bash, dir := hookCore(t, hooks.Hook{Event: hooks.PreToolUse, Matcher: "bash", Command: `touch ` + marker + `; echo '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`})
	session := permissionSession(t, db, "session_hook_deny", dir, core.PermissionModeDefault, "")
	saveRule(t, db, "rule_rm", permission.Rule{Tool: "bash", Pattern: "rm:*", Effect: permission.Deny, SessionID: session.ID})

	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"rm -rf build"}`)); got != "Blocked by rule bash: rm:*" {
		t.Fatalf("denied command result = %q", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the hook ran for a call a deny rule blocks")
	}
	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"make build"}`)); got != "ran make build" {
		t.Fatalf("a call the hook allows asked or failed: %q", got)
	}
	if len(bash.ran) != 1 {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestPreToolUseHookBlocksOrAsks(t *testing.T) {
	t.Parallel()
	app, db, bash, dir := hookCore(t,
		hooks.Hook{Event: hooks.PreToolUse, Matcher: "bash", Command: `case "$(cat)" in *'git push'*) echo "pushes are off" >&2; exit 2;; esac`},
		hooks.Hook{Event: hooks.PreToolUse, Matcher: "bash", Command: `case "$(cat)" in *deploy*) echo '{"hookSpecificOutput":{"permissionDecision":"ask","permissionDecisionReason":"deploys need a look"}}';; esac`},
	)
	session := permissionSession(t, db, "session_hook_block", dir, core.PermissionModeFullAuto, "")

	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"git push"}`)); got != "Blocked by hook: pushes are off" {
		t.Fatalf("blocked command result = %q", got)
	}
	pending := executeTool(t, app, session.ID, "bash", `{"command":"make deploy"}`)
	if pending.Approval == nil || !strings.HasSuffix(pending.Approval.Description, "Asked by hook: deploys need a look") {
		t.Fatalf("result = %+v", pending)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestPreToolUseHookRewrittenInputMeetsTheRulesAgain(t *testing.T) {
	t.Parallel()
	app, db, bash, dir := hookCore(t, hooks.Hook{Event: hooks.PreToolUse, Matcher: "bash", Command: `case "$(cat)" in
*'"command":"clean"'*) echo '{"hookSpecificOutput":{"updatedInput":{"command":"rm -rf build"}}}';;
*'"command":"ls"'*) echo '{"hookSpecificOutput":{"updatedInput":{"command":"ls -la"},"additionalContext":"long listing"}}';;
esac`})
	session := permissionSession(t, db, "session_hook_rewrite", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_rm", permission.Rule{Tool: "bash", Pattern: "rm:*", Effect: permission.Deny, SessionID: session.ID})

	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"clean"}`)); got != "Blocked by rule bash: rm:*" {
		t.Fatalf("rewritten command result = %q", got)
	}
	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"ls"}`)); got != "ran ls -la\n\nlong listing" {
		t.Fatalf("rewritten command result = %q", got)
	}
	if strings.Join(bash.ran, ",") != "ls -la" {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestPostToolUseHookAndDiagnosticsEndTheResult(t *testing.T) {
	t.Parallel()
	app, db, _, dir := hookCore(t, hooks.Hook{Event: hooks.PostToolUse, Matcher: "write", Command: `echo "wrote $(basename "$MATRIXCLAW_FILE")"`})
	session := permissionSession(t, db, "session_hook_post", dir, core.PermissionModeFullAuto, "")

	got := resultText(executeTool(t, app, session.ID, "write", `{"file_path":"data.json","content":"{\"a\": 1,}"}`))
	if !strings.Contains(got, "<hook event=\"PostToolUse\">\nwrote data.json\n</hook>") || !strings.Contains(got, "Diagnostics after edit:\n"+filepath.Join(dir, "data.json")+":1:9:") {
		t.Fatalf("result = %q", got)
	}
}
