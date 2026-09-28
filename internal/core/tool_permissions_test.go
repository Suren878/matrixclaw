package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// commandTool asks before every command, like bash, and names the command to rules.
type commandTool struct {
	ran []string
}

func (t *commandTool) Spec() tools.Spec {
	spec := recoveryToolSpec("bash", tools.EffectMutation)
	spec.Category = tools.CategoryShell
	return spec
}

func (t *commandTool) Execute(_ context.Context, call tools.Call) (tools.Result, error) {
	command := commandOf(call)
	if !call.Approved {
		return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "bash", ToolCallID: call.ToolCallID, Action: "execute", Description: command}}, nil
	}
	t.ran = append(t.ran, command)
	return tools.Result{Content: "ran " + command}, nil
}

func (t *commandTool) PermissionSubject(call tools.Call) permission.Subject {
	return permission.Subject{Kind: permission.KindCommand, Value: commandOf(call)}
}

func commandOf(call tools.Call) string {
	var params struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(call.Args, &params)
	return params.Command
}

// webTool fetches from one host and is redirected to evil.example.
type webTool struct{}

func (webTool) Spec() tools.Spec {
	spec := recoveryToolSpec("web_fetch", tools.EffectReadOnly)
	spec.Category = tools.CategoryWeb
	return spec
}

func (webTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	if call.Recheck == nil {
		return tools.Result{Content: "no recheck"}, nil
	}
	if err := call.Recheck(ctx, permission.Subject{Kind: permission.KindDomain, Value: "Evil.Example."}); err != nil {
		return tools.Result{Content: err.Error(), IsError: true}, nil
	}
	if call.Guarded {
		return tools.Result{Content: "fetched, guarded"}, nil
	}
	return tools.Result{Content: "fetched"}, nil
}

func (webTool) PermissionSubject(tools.Call) permission.Subject {
	return permission.Subject{Kind: permission.KindDomain, Value: "docs.example"}
}

// permissionCore has the real file tools and a fake bash over a temp working directory.
func permissionCore(t *testing.T) (*core.Core, *store.SQLiteStore, *commandTool, string) {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"notes.txt": "public notes", "secret/key.txt": "hunter2"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bash := &commandTool{}
	registry := tools.NewCoreReadOnlyRegistry(tools.NewWriteExecutor(), bash, webTool{})
	app.WithTools(registry)
	return app, db, bash, dir
}

func permissionSession(t *testing.T, db *store.SQLiteStore, id string, dir string, mode core.PermissionMode, parentID string) core.Session {
	t.Helper()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: id, Title: id, Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		WorkingDir: dir, PermissionMode: mode, ParentSessionID: parentID, Hidden: parentID != "",
		ProviderID: "recovery-test", ModelID: "test-model", Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func saveRule(t *testing.T, db *store.SQLiteStore, id string, rule permission.Rule) {
	t.Helper()
	rule.ID, rule.CreatedAt = id, runRecoveryTestTime()
	if rule.Scope == "" {
		rule.Scope = permission.ScopeSession
	}
	if err := db.CreatePermissionRule(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
}

func executeTool(t *testing.T, app *core.Core, sessionID string, tool string, args string, approved bool) core.ExecuteToolResult {
	t.Helper()
	result, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: sessionID, ToolName: tool, Args: json.RawMessage(args), Approved: approved})
	if err != nil {
		t.Fatalf("ExecuteTool(%s %s): %v", tool, args, err)
	}
	return result
}

func resultText(result core.ExecuteToolResult) string {
	if result.ToolResultMessage == nil {
		return ""
	}
	return result.ToolResultMessage.Content
}

func TestDenyRuleBlocksAReadOnlyToolForRunsAndDirectCalls(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_deny", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_secret", permission.Rule{Tool: "read", Pattern: filepath.Join(dir, "secret") + "/**", Effect: permission.Deny, SessionID: session.ID})
	blocked := "Blocked by rule read: " + filepath.Join(dir, "secret") + "/**"

	if got := resultText(executeTool(t, app, session.ID, "read", `{"file_path":"secret/key.txt"}`, false)); got != blocked {
		t.Fatalf("direct call result = %q", got)
	}
	if got := resultText(executeTool(t, app, session.ID, "read", `{"file_path":"secret/key.txt"}`, true)); got != blocked {
		t.Fatalf("approved replay result = %q", got)
	}
	if got := resultText(executeTool(t, app, session.ID, "read", `{"file_path":"notes.txt"}`, false)); got == blocked || got == "" {
		t.Fatalf("unrelated read result = %q", got)
	}

	var saw string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if saw = toolResultContent(request, "call-read"); saw == "" {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-read", Name: "read", Arguments: []byte(`{"file_path":"secret/key.txt"}`)}}}, nil
		}
		return providers.Response{Text: "The key is off limits."}, nil
	})})
	run := core.Run{ID: "run_deny", SessionID: session.ID, UserMessageID: "msg_deny", Status: core.RunStatusAccepted, StartedAt: runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime()}
	user := transcript.Message{ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser, Content: "read the key", CreatedAt: run.StartedAt, UpdatedAt: run.StartedAt}
	if err := db.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if saw != blocked {
		t.Fatalf("model read %q", saw)
	}
}

func TestAskRuleAsksBeforeAReadOnlyTool(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_ask", dir, core.PermissionModeDefault, "")
	saveRule(t, db, "rule_ask", permission.Rule{Tool: "read", Pattern: dir + "/**", Effect: permission.Ask, SessionID: session.ID})

	pending := executeTool(t, app, session.ID, "read", `{"file_path":"notes.txt"}`, false)
	if pending.Approval == nil || pending.Approval.Action != "ask_rule" || pending.ToolResultMessage != nil {
		t.Fatalf("result = %+v", pending)
	}
	if _, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	assertToolResultCount(t, db, session.ID, pending.ToolCallMessage.ID, 1)
}

func TestAllowRuleRunsMatchingCommandsWithoutAsking(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	session := permissionSession(t, db, "session_allow", dir, core.PermissionModeDefault, "")
	saveRule(t, db, "rule_echo", permission.Rule{Tool: "bash", Pattern: "echo:*", Effect: permission.Allow, SessionID: session.ID})

	if got := resultText(executeTool(t, app, session.ID, "bash", `{"command":"echo ok && echo done"}`, false)); got != "ran echo ok && echo done" {
		t.Fatalf("allowed command result = %q", got)
	}
	for _, command := range []string{"echo ok > out.txt", "rm -rf build", "echo ok; rm -rf build"} {
		args, _ := json.Marshal(map[string]string{"command": command})
		if result := executeTool(t, app, session.ID, "bash", string(args), false); result.Approval == nil {
			t.Errorf("%q ran without approval: %+v", command, result)
		}
	}
	if len(bash.ran) != 1 {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestSubagentsAndOtherSessionsFollowInheritedAndGlobalRules(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	parent := permissionSession(t, db, "session_parent", dir, core.PermissionModeFullAuto, "")
	child := permissionSession(t, db, "session_child", dir, core.PermissionModeFullAuto, parent.ID)
	other := permissionSession(t, db, "session_other", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_parent", permission.Rule{Tool: "read", Pattern: filepath.Join(dir, "secret") + "/**", Effect: permission.Deny, SessionID: parent.ID})
	saveRule(t, db, "rule_global", permission.Rule{Tool: "bash", Pattern: "rm:*", Effect: permission.Deny, Scope: permission.ScopeGlobal})

	if got := resultText(executeTool(t, app, child.ID, "read", `{"file_path":"secret/key.txt"}`, false)); got != "Blocked by rule read: "+filepath.Join(dir, "secret")+"/**" {
		t.Fatalf("child read = %q", got)
	}
	if got := resultText(executeTool(t, app, other.ID, "read", `{"file_path":"secret/key.txt"}`, false)); got == "" || strings.HasPrefix(got, "Blocked") {
		t.Fatalf("other session read = %q", got)
	}
	if got := resultText(executeTool(t, app, other.ID, "bash", `{"command":"rm -rf build"}`, false)); got != "Blocked by rule bash: rm:*" {
		t.Fatalf("global rule result = %q", got)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("ran = %v", bash.ran)
	}
}

func TestModePresetsAllowEditsInsideTheWorkingDirectoryOrEverything(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	edits := permissionSession(t, db, "session_edits", dir, core.PermissionModeAcceptEdits, "")
	auto := permissionSession(t, db, "session_auto", dir, core.PermissionModeFullAuto, "")
	outside := filepath.Join(t.TempDir(), "outside.txt")

	if result := executeTool(t, app, edits.ID, "write", `{"file_path":"inside.txt","content":"x"}`, false); result.Approval != nil {
		t.Fatalf("write inside the working directory asked: %+v", result.Approval)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"file_path": outside, "content": "x"})
	if result := executeTool(t, app, edits.ID, "write", string(args), false); result.Approval == nil {
		t.Fatal("write outside the working directory ran without approval")
	}
	if result := executeTool(t, app, edits.ID, "bash", `{"command":"go test ./..."}`, false); result.Approval == nil {
		t.Fatal("accept_edits ran a command without approval")
	}
	if result := executeTool(t, app, auto.ID, "bash", `{"command":"go test ./..."}`, false); result.Approval != nil || len(bash.ran) != 1 {
		t.Fatalf("full_auto asked: %+v ran = %v", result.Approval, bash.ran)
	}
}

func TestAlwaysAllowKeepsTheSuggestedRule(t *testing.T) {
	for _, scope := range []permission.Scope{permission.ScopeSession, permission.ScopeGlobal} {
		app, db, bash, dir := permissionCore(t)
		session := permissionSession(t, db, "session_always", dir, core.PermissionModeDefault, "")
		other := permissionSession(t, db, "session_other", dir, core.PermissionModeDefault, "")

		pending := executeTool(t, app, session.ID, "bash", `{"command":"go test ./..."}`, false)
		if pending.Approval == nil || pending.Approval.Suggestion == nil || pending.Approval.Suggestion.String() != "bash: go test:*" {
			t.Fatalf("%s: approval = %+v", scope, pending.Approval)
		}
		if _, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Approved: true, Always: scope}); err != nil {
			t.Fatal(err)
		}

		if result := executeTool(t, app, session.ID, "bash", `{"command":"go test ./internal/..."}`, false); result.Approval != nil {
			t.Fatalf("%s: the kept rule did not allow the next test run", scope)
		}
		if result := executeTool(t, app, other.ID, "bash", `{"command":"go test ./..."}`, false); (result.Approval == nil) != (scope == permission.ScopeGlobal) {
			t.Fatalf("%s: other session approval = %+v", scope, result.Approval)
		}
		if len(bash.ran) < 2 {
			t.Fatalf("%s: ran = %v", scope, bash.ran)
		}
	}
}

func TestAlwaysAllowNeedsASuggestedRule(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_risky", dir, core.PermissionModeDefault, "")
	pending := executeTool(t, app, session.ID, "bash", `{"command":"go test ./... > out.txt"}`, false)
	if pending.Approval == nil || pending.Approval.Suggestion != nil {
		t.Fatalf("approval = %+v", pending.Approval)
	}

	_, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession})

	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if stored, _ := db.GetApproval(context.Background(), pending.Approval.ID); stored.State != core.ApprovalStatePending {
		t.Fatalf("approval state = %s, want still pending", stored.State)
	}
}

func TestAddedRulesNameAbsolutePathsAndKnownTools(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_rules", dir, core.PermissionModeDefault, "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if home, err = filepath.EvalSymlinks(home); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		request core.PermissionRuleRequest
		want    string
	}{
		{core.PermissionRuleRequest{Tool: "read", Pattern: "secret/**", Effect: permission.Deny, Scope: permission.ScopeSession}, "read: " + dir + "/secret/**"},
		{core.PermissionRuleRequest{Tool: "write", Pattern: "~/notes/*.md", Effect: permission.Ask, Scope: permission.ScopeGlobal}, "write: " + home + "/notes/*.md"},
		{core.PermissionRuleRequest{Tool: "bash", Pattern: "go test:*", Effect: permission.Allow, Scope: permission.ScopeSession}, "bash: go test:*"},
		{core.PermissionRuleRequest{Tool: "*", Effect: permission.Deny, Scope: permission.ScopeSession}, "*"},
		{core.PermissionRuleRequest{Tool: "*", Pattern: "~/secrets/**", Effect: permission.Deny, Scope: permission.ScopeSession}, "*: " + home + "/secrets/**"},
		{core.PermissionRuleRequest{Tool: "*", Pattern: "go test:*", Effect: permission.Ask, Scope: permission.ScopeSession}, "*: go test:*"},
		{core.PermissionRuleRequest{Tool: "web_fetch", Pattern: "*.Bücher.Example.", Effect: permission.Deny, Scope: permission.ScopeSession}, "web_fetch: *.xn--bcher-kva.example"},
	} {
		rule, err := app.AddPermissionRule(context.Background(), session.ID, tc.request)
		if err != nil || rule.String() != tc.want || (rule.Scope == permission.ScopeSession) != (rule.SessionID == session.ID) {
			t.Errorf("%+v: rule = %+v (%s) err = %v, want %s", tc.request, rule, rule.String(), err, tc.want)
		}
	}
	for _, request := range []core.PermissionRuleRequest{
		{Tool: "rm_everything", Effect: permission.Deny, Scope: permission.ScopeSession},
		{Tool: "bash", Effect: "sometimes", Scope: permission.ScopeSession},
		{Tool: "bash", Effect: permission.Allow, Scope: "forever"},
	} {
		if _, err := app.AddPermissionRule(context.Background(), session.ID, request); !errors.Is(err, core.ErrInvalidInput) {
			t.Errorf("%+v: error = %v, want ErrInvalidInput", request, err)
		}
	}
	rules, err := app.SessionPermissionRules(context.Background(), session.ID)
	if err != nil || len(rules) != 7 {
		t.Fatalf("rules = %+v err = %v", rules, err)
	}
}

// mcpTool is a remote MCP tool, named "server__tool" to rules.
type mcpTool struct{}

func (mcpTool) Spec() tools.Spec {
	spec := recoveryToolSpec("mcp_github_create_issue", tools.EffectMutation)
	spec.Namespace, spec.Category = "mcp.github", tools.CategoryWeb
	return spec
}

func (mcpTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	return tools.Result{Content: "created"}, nil
}

func (mcpTool) PermissionSubject(tools.Call) permission.Subject {
	return permission.Subject{Kind: permission.KindName, Value: "github__create_issue"}
}

func TestAddedRuleForAnMCPToolNamesThatTool(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	app.WithTools(tools.NewCoreReadOnlyRegistry(mcpTool{}))
	session := permissionSession(t, db, "session_mcp", dir, core.PermissionModeDefault, "")
	rule, err := app.AddPermissionRule(context.Background(), session.ID, core.PermissionRuleRequest{Tool: "mcp_github_create_issue", Effect: permission.Allow, Scope: permission.ScopeSession})
	if err != nil || rule.String() != "mcp: github__create_issue" {
		t.Fatalf("rule = %s err = %v, want mcp: github__create_issue", rule.String(), err)
	}
}

func TestRedirectsMeetTheRulesOfTheCall(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_redirect", dir, core.PermissionModeFullAuto, "")
	if got := resultText(executeTool(t, app, session.ID, "web_fetch", `{}`, false)); got != "fetched" {
		t.Fatalf("unruled redirect = %q", got)
	}
	saveRule(t, db, "rule_other", permission.Rule{Tool: "web_fetch", Pattern: "other.example", Effect: permission.Ask, SessionID: session.ID})
	if got := resultText(executeTool(t, app, session.ID, "web_fetch", `{}`, false)); got != "fetched, guarded" {
		t.Fatalf("redirect under an unrelated rule = %q", got)
	}
	saveRule(t, db, "rule_evil", permission.Rule{Tool: "web_fetch", Pattern: "evil.example", Effect: permission.Deny, SessionID: session.ID})
	if got := resultText(executeTool(t, app, session.ID, "web_fetch", `{}`, false)); !strings.Contains(got, "web_fetch: evil.example") {
		t.Fatalf("denied redirect = %q", got)
	}
}

func TestAskRuleAsksBeforeAToolThatAsksOnItsOwn(t *testing.T) {
	app, db, bash, dir := permissionCore(t)
	session := permissionSession(t, db, "session_ask_bash", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_ask", permission.Rule{Tool: "bash", Pattern: "git push:*", Effect: permission.Ask, SessionID: session.ID})

	pending := executeTool(t, app, session.ID, "bash", `{"command":"git push"}`, false)
	if pending.Approval == nil || pending.Approval.Action != "ask_rule" || len(bash.ran) != 0 {
		t.Fatalf("result = %+v ran = %v", pending, bash.ran)
	}
}

func TestGrepDoesNotFollowALinkToADeniedFile(t *testing.T) {
	app, db, _, dir := permissionCore(t)
	session := permissionSession(t, db, "session_link", dir, core.PermissionModeFullAuto, "")
	saveRule(t, db, "rule_secret", permission.Rule{Tool: "read", Pattern: filepath.Join(dir, "secret") + "/**", Effect: permission.Deny, SessionID: session.ID})
	if err := os.MkdirAll(filepath.Join(dir, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "secret", "key.txt"), filepath.Join(dir, "public", "key.txt")); err != nil {
		t.Fatal(err)
	}

	if got := resultText(executeTool(t, app, session.ID, "grep", `{"pattern":"hunter2","path":"public"}`, false)); strings.Contains(got, "hunter2") {
		t.Fatalf("grep read through the link: %q", got)
	}
}
