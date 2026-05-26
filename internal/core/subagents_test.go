package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestDelegateTaskCreatesHiddenMatrixClawChildWithoutParentHistoryOrDelegateTool(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	saveSubagentTestMessage(t, sqliteStore, Message{
		ID:        "msg_parent_secret",
		SessionID: parent.ID,
		Role:      MessageRoleUser,
		Content:   "parent secret must not leak",
		CreatedAt: subagentTestTime(),
	})

	runtime := &recordingRuntime{response: providers.Response{Text: "child summary"}}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	registry := tools.NewCoreReadOnlyRegistry(DelegateTaskToolExecutor(app))
	if err := registry.Register(MemoryToolExecutors(app)...); err != nil {
		t.Fatalf("register memory tools: %v", err)
	}
	if err := registry.Register(PlanToolExecutors(app)...); err != nil {
		t.Fatalf("register plan tools: %v", err)
	}
	app.WithTools(registry)

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"goal":"Inspect the repo","context":"Focus on tests","runtime":"matrixclaw"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(delegate_task): %v", err)
	}
	if result.ToolResultMessage == nil || !strings.Contains(result.ToolResultMessage.Content, "child summary") {
		t.Fatalf("delegate result = %#v, want child summary", result.ToolResultMessage)
	}

	visible, err := app.ListSessions(ctx, SessionListFilter{})
	if err != nil {
		t.Fatalf("ListSessions visible: %v", err)
	}
	if len(visible) != 1 || visible[0].ID != parent.ID {
		t.Fatalf("visible sessions = %#v, want only parent", visible)
	}

	allSessions, err := app.ListSessions(ctx, SessionListFilter{IncludeHidden: true})
	if err != nil {
		t.Fatalf("ListSessions hidden: %v", err)
	}
	child := findChildSession(t, allSessions, parent.ID)
	if child.WorkingDir != parent.WorkingDir {
		t.Fatalf("child working dir = %q, want %q", child.WorkingDir, parent.WorkingDir)
	}

	if len(runtime.requests) != 1 {
		t.Fatalf("runtime requests = %d, want 1", len(runtime.requests))
	}
	request := runtime.requests[0]
	if len(request.Messages) != 1 {
		t.Fatalf("child messages = %#v, want one delegated user prompt", request.Messages)
	}
	if strings.Contains(request.Messages[0].Content, "parent secret") {
		t.Fatalf("child request leaked parent history:\n%s", request.Messages[0].Content)
	}
	if !strings.Contains(request.Messages[0].Content, "Inspect the repo") || !strings.Contains(request.Messages[0].Content, "Focus on tests") {
		t.Fatalf("child prompt missing delegated goal/context:\n%s", request.Messages[0].Content)
	}
	if !strings.Contains(request.SystemPrompt, "child agent") {
		t.Fatalf("child system prompt missing subagent instructions:\n%s", request.SystemPrompt)
	}
	for _, tool := range request.Tools {
		switch tool.Name {
		case "delegate_task", "memory", "plan_get", "plan_set_goal", "plan_add_item", "plan_update_item", "plan_clear":
			t.Fatalf("child tool list included restricted tool %q: %#v", tool.Name, request.Tools)
		}
	}
}

func TestDelegateTaskExternalRuntimeUsesAliasAndReturnsSummary(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	external := &fakeExternalRuntime{id: "claude-code", aliases: []string{"claude"}, displayName: "Claude Code"}
	registry, err := externalagents.NewRegistry(external)
	if err != nil {
		t.Fatalf("external registry: %v", err)
	}
	app.WithExternalAgents(registry, sqliteStore)
	app.WithTools(tools.NewRegistry(DelegateTaskToolExecutor(app)))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"goal":"Run an external check","runtime":"claude","model":"sonnet-test"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(delegate_task claude): %v", err)
	}
	if result.ToolResultMessage == nil || !strings.Contains(result.ToolResultMessage.Content, "external summary") {
		t.Fatalf("delegate result = %#v, want external summary", result.ToolResultMessage)
	}
	if external.started.AgentID != "claude-code" || external.started.Model != "sonnet-test" {
		t.Fatalf("external start = %#v", external.started)
	}
	if !strings.Contains(external.lastInput, "Run an external check") {
		t.Fatalf("external input = %q, want delegated goal", external.lastInput)
	}

	allSessions, err := app.ListSessions(ctx, SessionListFilter{IncludeHidden: true})
	if err != nil {
		t.Fatalf("ListSessions hidden: %v", err)
	}
	child := findChildSession(t, allSessions, parent.ID)
	if child.Kind != SessionKindExternalAgent || child.ExternalAgentID != "claude-code" {
		t.Fatalf("child external session = %#v", child)
	}
}

func TestDelegateTaskChildApprovalReturnsControlledError(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	runtime := &recordingRuntime{response: providers.Response{ToolCalls: []providers.ToolCall{{
		ID:        "tool_child_bash",
		Name:      "bash",
		Arguments: json.RawMessage(`{"command":"echo needs approval"}`),
	}}}}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry(DelegateTaskToolExecutor(app)))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"goal":"Run a command that needs approval","runtime":"matrixclaw"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(delegate_task): %v", err)
	}
	if result.ToolResultMessage == nil {
		t.Fatal("delegate_task returned no tool result message")
	}
	if !result.ToolResultMessage.Parts[0].ToolResult.IsError {
		t.Fatalf("delegate_task result was not marked as error: %#v", result.ToolResultMessage.Parts[0].ToolResult)
	}
	if !strings.Contains(result.ToolResultMessage.Content, "requested approval") {
		t.Fatalf("delegate_task result content = %q, want controlled approval error", result.ToolResultMessage.Content)
	}

	allSessions, err := app.ListSessions(ctx, SessionListFilter{IncludeHidden: true})
	if err != nil {
		t.Fatalf("ListSessions hidden: %v", err)
	}
	child := findChildSession(t, allSessions, parent.ID)
	approvals, err := sqliteStore.ListApprovals(ctx, child.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals child: %v", err)
	}
	if len(approvals) != 1 || approvals[0].ToolName != "bash" {
		t.Fatalf("child approvals = %#v, want one pending bash approval", approvals)
	}
}

func TestDelegateTaskRejectedForExternalAgentSession(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	external := &fakeExternalRuntime{id: "claude-code", aliases: []string{"claude"}, displayName: "Claude Code"}
	registry, err := externalagents.NewRegistry(external)
	if err != nil {
		t.Fatalf("external registry: %v", err)
	}
	app.WithExternalAgents(registry, sqliteStore)
	session, err := app.CreateSession(ctx, CreateSessionInput{
		Title:           "External",
		Kind:            SessionKindExternalAgent,
		RuntimeID:       SessionRuntimeExternalAgent,
		WorkingDir:      "/tmp/project",
		ExternalAgentID: "claude",
		PermissionMode:  PermissionModeFullAuto,
	})
	if err != nil {
		t.Fatalf("CreateSession external: %v", err)
	}
	app.WithTools(tools.NewRegistry(DelegateTaskToolExecutor(app)))

	_, err = app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  session.ID,
		RunID:      "run_external",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: session.WorkingDir,
		Args:       json.RawMessage(`{"goal":"Should be rejected"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "Matrixclaw sessions only") {
		t.Fatalf("ExecuteTool external err = %v, want Matrixclaw-only rejection", err)
	}

	messages, err := app.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages external: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("external session messages = %#v, want rejection before tool call persistence", messages)
	}
}

func TestDelegateTaskRejectedForChildSessionBeforeCreatingTask(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	child := saveSubagentTestSession(t, sqliteStore, "session_child", "Child", "/tmp/project")
	child.ParentSessionID = parent.ID
	child.Hidden = true
	if err := sqliteStore.UpdateSession(ctx, child); err != nil {
		t.Fatalf("update child session: %v", err)
	}
	app.WithTools(tools.NewRegistry(DelegateTaskToolExecutor(app)))

	_, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  child.ID,
		RunID:      "run_child",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: child.WorkingDir,
		Args:       json.RawMessage(`{"goal":"Should be rejected"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "not available to child subagents") {
		t.Fatalf("ExecuteTool child err = %v, want child tool-policy rejection", err)
	}

	messages, err := app.ListMessages(ctx, child.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages child: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("child session messages = %#v, want rejection before tool call persistence", messages)
	}
}

func newSubagentTestCore(t *testing.T) (*Core, *store.SQLiteStore, func()) {
	t.Helper()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	app := New(sqliteStore).WithIDGenerator(sequentialTestIDs())
	return app, sqliteStore, func() { _ = sqliteStore.Close() }
}

func saveSubagentTestSession(t *testing.T, sqliteStore *store.SQLiteStore, id string, title string, workingDir string) Session {
	t.Helper()
	session := Session{
		ID:             id,
		Title:          title,
		Kind:           SessionKindAssistant,
		RuntimeID:      SessionRuntimeMatrixClaw,
		WorkingDir:     workingDir,
		PermissionMode: PermissionModeDefault,
		Status:         SessionStatusActive,
		CreatedAt:      subagentTestTime(),
		UpdatedAt:      subagentTestTime(),
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	return session
}

func saveSubagentTestMessage(t *testing.T, sqliteStore *store.SQLiteStore, message Message) {
	t.Helper()
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.CreatedAt
	}
	if err := sqliteStore.SaveMessage(context.Background(), message); err != nil {
		t.Fatalf("save message: %v", err)
	}
}

func findChildSession(t *testing.T, sessions []Session, parentID string) Session {
	t.Helper()
	for _, session := range sessions {
		if session.ParentSessionID == parentID {
			return session
		}
	}
	t.Fatalf("no child session for parent %q in %#v", parentID, sessions)
	return Session{}
}

func subagentTestTime() time.Time {
	return time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
}

func sequentialTestIDs() func(string) string {
	var n int
	return func(prefix string) string {
		n++
		return fmt.Sprintf("%s_test_%d", prefix, n)
	}
}

type recordingRuntime struct {
	response providers.Response
	requests []providers.Request
}

func (r *recordingRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	r.requests = append(r.requests, request)
	return r.response, nil
}

type subagentLLMs struct {
	runtime providers.Runtime
}

func (l *subagentLLMs) ActiveSelection() (string, string) {
	return "test-provider", "test-model"
}

func (l *subagentLLMs) Providers() []SessionProviderOption {
	return []SessionProviderOption{{ID: "test-provider", Label: "Test", DefaultModel: "test-model", Configured: true}}
}

func (l *subagentLLMs) Normalize(providerID string, modelID string) (SessionProviderOption, string, error) {
	if strings.TrimSpace(providerID) == "" {
		providerID = "test-provider"
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "test-model"
	}
	return SessionProviderOption{ID: providerID, Label: "Test", DefaultModel: modelID, Configured: true}, modelID, nil
}

func (l *subagentLLMs) Models(context.Context, string) ([]string, error) {
	return []string{"test-model"}, nil
}

func (l *subagentLLMs) Resolve(context.Context, string, string) (providers.Runtime, SessionProviderOption, string, error) {
	return l.runtime, SessionProviderOption{ID: "test-provider", Label: "Test", Configured: true}, "test-model", nil
}

type fakeExternalRuntime struct {
	id          string
	aliases     []string
	displayName string
	started     externalagents.ExternalSession
	lastInput   string
}

func (r *fakeExternalRuntime) ID() string { return r.id }

func (r *fakeExternalRuntime) Aliases() []string { return r.aliases }

func (r *fakeExternalRuntime) DisplayName() string { return r.displayName }

func (r *fakeExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}

func (r *fakeExternalRuntime) Capabilities() externalagents.Capabilities {
	return externalagents.Capabilities{StartSession: true, ResumeSession: true, StreamingEvents: true}
}

func (r *fakeExternalRuntime) StartSession(_ context.Context, req externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	r.started = externalagents.ExternalSession{
		AgentID:           r.id,
		ExternalThreadID:  "thread_1",
		ExternalSessionID: "session_1",
		CWD:               req.CWD,
		Model:             req.Model,
		ApprovalPolicy:    req.ApprovalPolicy,
		Sandbox:           req.Sandbox,
	}
	return r.started, nil
}

func (r *fakeExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}

func (r *fakeExternalRuntime) Send(_ context.Context, session externalagents.ExternalSession, input externalagents.Input) (<-chan externalagents.Event, error) {
	r.lastInput = input.Text
	ch := make(chan externalagents.Event, 2)
	ch <- externalagents.Event{Kind: externalagents.EventMessageDelta, AgentID: session.AgentID, Text: "external summary", At: subagentTestTime()}
	ch <- externalagents.Event{Kind: externalagents.EventTurnCompleted, AgentID: session.AgentID, At: subagentTestTime()}
	close(ch)
	return ch, nil
}

func (r *fakeExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}

func (r *fakeExternalRuntime) Close() error { return nil }
