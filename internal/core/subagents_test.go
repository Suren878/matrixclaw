package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
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

	replayed, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: parent.WorkingDir,
		Approved:   true,
		Args:       json.RawMessage(`{"goal":"Inspect the repo","context":"Focus on tests","runtime":"matrixclaw"}`),
	})
	if err != nil {
		t.Fatalf("replay ExecuteTool(delegate_task): %v", err)
	}
	if replayed.ToolResultMessage == nil || !strings.Contains(replayed.ToolResultMessage.Content, "child summary") {
		t.Fatalf("replayed delegate result = %#v, want child summary", replayed.ToolResultMessage)
	}
	if len(runtime.requests) != 1 {
		t.Fatalf("runtime requests after replay = %d, want existing child reused", len(runtime.requests))
	}
}

func TestDelegateTaskExternalRuntimeUsesAliasAndReturnsSummary(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
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

func TestDelegateTaskMatrixClawChildApprovalMirrorsToParent(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
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
	if result.Approval == nil {
		t.Fatalf("delegate_task returned no approval: %#v", result)
	}
	if result.ToolResultMessage != nil {
		t.Fatalf("delegate_task saved a result while child approval is pending: %#v", result.ToolResultMessage)
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

	parentApprovals, err := sqliteStore.ListApprovals(ctx, parent.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals parent: %v", err)
	}
	if len(parentApprovals) != 1 || parentApprovals[0].ToolName != "delegate_task" {
		t.Fatalf("parent approvals = %#v, want one pending delegate_task approval", parentApprovals)
	}
	if !strings.Contains(parentApprovals[0].Description, "Subagent") || !strings.Contains(parentApprovals[0].Description, "bash") {
		t.Fatalf("parent approval description = %q, want subagent bash source", parentApprovals[0].Description)
	}
	var params struct {
		TaskID          string `json:"task_id"`
		ChildSessionID  string `json:"child_session_id"`
		ChildRunID      string `json:"child_run_id"`
		ChildApprovalID string `json:"child_approval_id"`
		ChildToolCallID string `json:"child_tool_call_id"`
		Source          string `json:"source"`
	}
	if err := json.Unmarshal(parentApprovals[0].Params, &params); err != nil {
		t.Fatalf("decode parent approval params: %v", err)
	}
	if params.Source != "subagent_approval_bridge" || params.ChildSessionID != child.ID || params.ChildApprovalID != approvals[0].ID || params.ChildToolCallID != approvals[0].ToolCallRef {
		t.Fatalf("parent approval params = %#v, child approval = %#v", params, approvals[0])
	}
	task, err := sqliteStore.GetSubagentTaskByParentToolCall(ctx, parent.ID, "run_parent", "tool_delegate")
	if err != nil {
		t.Fatalf("GetSubagentTaskByParentToolCall: %v", err)
	}
	if task.Status != SubagentTaskStatusWaitingApproval {
		t.Fatalf("task status = %q, want waiting_approval", task.Status)
	}
}

func TestDelegateTaskMatrixClawChildInheritsFullAuto(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	parent.PermissionMode = PermissionModeFullAuto
	if err := sqliteStore.UpdateSession(ctx, parent); err != nil {
		t.Fatalf("UpdateSession parent: %v", err)
	}
	runtime := &subagentApprovalBridgeRuntime{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry(DelegateTaskToolExecutor(app)))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "delegate_task",
		ToolCallID: "tool_delegate",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"goal":"Run a command without approval","runtime":"matrixclaw"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(delegate_task): %v", err)
	}
	if result.Approval != nil {
		t.Fatalf("delegate_task returned approval in full_auto: %#v", result.Approval)
	}
	if result.ToolResultMessage == nil || !strings.Contains(result.ToolResultMessage.Content, "child summary") {
		t.Fatalf("delegate result = %#v, want child summary", result.ToolResultMessage)
	}

	allSessions, err := app.ListSessions(ctx, SessionListFilter{IncludeHidden: true})
	if err != nil {
		t.Fatalf("ListSessions hidden: %v", err)
	}
	child := findChildSession(t, allSessions, parent.ID)
	if child.PermissionMode != PermissionModeFullAuto {
		t.Fatalf("child permission mode = %q, want full_auto", child.PermissionMode)
	}
	approvals, err := sqliteStore.ListApprovals(ctx, child.ID, "")
	if err != nil {
		t.Fatalf("ListApprovals child: %v", err)
	}
	if len(approvals) != 0 {
		t.Fatalf("child approvals = %#v, want none in full_auto", approvals)
	}
}

func TestResolveMirroredSubagentApprovalApprovesChildAndCompletesParent(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(syncRunStarter{app: app})

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	runtime := &subagentApprovalBridgeRuntime{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry(DelegateTaskToolExecutor(app)))

	if _, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: parent.ID,
		Text:      "Use a subagent",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}
	parentRun, err := sqliteStore.GetRun(ctx, "run_test_1")
	if err != nil {
		t.Fatalf("GetRun parent: %v", err)
	}
	if parentRun.Status != RunStatusWaitingApproval {
		t.Fatalf("parent status = %q, want waiting_approval", parentRun.Status)
	}
	parentApprovals, err := sqliteStore.ListApprovals(ctx, parent.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals parent: %v", err)
	}
	if len(parentApprovals) != 1 {
		t.Fatalf("parent approvals = %#v, want one", parentApprovals)
	}

	if _, err := app.ResolveApproval(ctx, parentApprovals[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval approve parent mirror: %v", err)
	}

	parentRun, err = sqliteStore.GetRun(ctx, "run_test_1")
	if err != nil {
		t.Fatalf("GetRun parent after approve: %v", err)
	}
	if parentRun.Status != RunStatusCompleted {
		t.Fatalf("parent status after approve = %q, error %q, want completed", parentRun.Status, parentRun.Error)
	}
	messages, err := sqliteStore.ListMessages(ctx, parent.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages parent: %v", err)
	}
	if !messagesContain(messages, "parent complete after child summary") {
		t.Fatalf("parent messages missing final summary: %#v", messages)
	}
	task, err := sqliteStore.GetSubagentTaskByParentToolCall(ctx, parent.ID, "run_test_1", "tool_delegate")
	if err != nil {
		t.Fatalf("GetSubagentTaskByParentToolCall: %v", err)
	}
	if task.Status != SubagentTaskStatusCompleted || task.Summary != "child summary" {
		t.Fatalf("task after approve = %#v, want completed child summary", task)
	}
	if runtime.parentDelegateCalls != 1 {
		t.Fatalf("parent delegate calls = %d, want one child task", runtime.parentDelegateCalls)
	}
}

func TestResolveMirroredSubagentApprovalBridgesRepeatedChildApprovals(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(syncRunStarter{app: app})

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	runtime := &subagentRepeatedApprovalRuntime{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry(DelegateTaskToolExecutor(app)))

	if _, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: parent.ID,
		Text:      "Use a subagent that needs two approvals",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}
	parentApprovals, err := sqliteStore.ListApprovals(ctx, parent.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals parent first: %v", err)
	}
	if len(parentApprovals) != 1 {
		t.Fatalf("first parent approvals = %#v, want one", parentApprovals)
	}

	if _, err := app.ResolveApproval(ctx, parentApprovals[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval first parent mirror: %v", err)
	}

	parentApprovals, err = sqliteStore.ListApprovals(ctx, parent.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals parent second: %v", err)
	}
	if len(parentApprovals) != 1 {
		t.Fatalf("second parent approvals = %#v, want one mirrored approval for second child request", parentApprovals)
	}
	if !strings.Contains(parentApprovals[0].Description, "echo second") {
		t.Fatalf("second parent approval description = %q, want second child command", parentApprovals[0].Description)
	}

	if _, err := app.ResolveApproval(ctx, parentApprovals[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval second parent mirror: %v", err)
	}

	parentRun, err := sqliteStore.GetRun(ctx, "run_test_1")
	if err != nil {
		t.Fatalf("GetRun parent after second approve: %v", err)
	}
	if parentRun.Status != RunStatusCompleted {
		t.Fatalf("parent status after second approve = %q, error %q, want completed", parentRun.Status, parentRun.Error)
	}
	messages, err := sqliteStore.ListMessages(ctx, parent.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages parent: %v", err)
	}
	if !messagesContain(messages, "parent complete after repeated approvals") {
		t.Fatalf("parent messages missing repeated approval completion: %#v", messages)
	}
}

func TestResolveMirroredSubagentApprovalRejectsChildAndResumesParentWithToolError(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(syncRunStarter{app: app})

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	runtime := &subagentApprovalBridgeRuntime{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry(DelegateTaskToolExecutor(app)))

	if _, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: parent.ID,
		Text:      "Use a subagent",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}
	parentApprovals, err := sqliteStore.ListApprovals(ctx, parent.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals parent: %v", err)
	}
	if len(parentApprovals) != 1 {
		t.Fatalf("parent approvals = %#v, want one", parentApprovals)
	}

	if _, err := app.ResolveApproval(ctx, parentApprovals[0].ID, false); err != nil {
		t.Fatalf("ResolveApproval reject parent mirror: %v", err)
	}

	parentRun, err := sqliteStore.GetRun(ctx, "run_test_1")
	if err != nil {
		t.Fatalf("GetRun parent after reject: %v", err)
	}
	if parentRun.Status != RunStatusCompleted {
		t.Fatalf("parent status after reject = %q, error %q, want completed", parentRun.Status, parentRun.Error)
	}
	messages, err := sqliteStore.ListMessages(ctx, parent.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages parent: %v", err)
	}
	if !messagesContain(messages, "parent handled subagent denial") {
		t.Fatalf("parent messages missing denial handling: %#v", messages)
	}
	task, err := sqliteStore.GetSubagentTaskByParentToolCall(ctx, parent.ID, "run_test_1", "tool_delegate")
	if err != nil {
		t.Fatalf("GetSubagentTaskByParentToolCall: %v", err)
	}
	if task.Status != SubagentTaskStatusFailed || !strings.Contains(task.Error, "approval denied") {
		t.Fatalf("task after reject = %#v, want failed approval denied", task)
	}
}

func TestRecoverSubagentTasksResumesBlockingDelegateAfterChildTerminal(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()
	app.WithRunStarter(syncRunStarter{app: app})

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", "/tmp/project")
	runtime := &subagentApprovalBridgeRuntime{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithTools(tools.NewCoreCodingRegistry(DelegateTaskToolExecutor(app)))

	if _, err := app.AcceptRun(ctx, HandleMessageInput{
		SessionID: parent.ID,
		Text:      "Use a subagent",
	}); err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}

	allSessions, err := app.ListSessions(ctx, SessionListFilter{IncludeHidden: true})
	if err != nil {
		t.Fatalf("ListSessions hidden: %v", err)
	}
	child := findChildSession(t, allSessions, parent.ID)
	childApprovals, err := sqliteStore.ListApprovals(ctx, child.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals child: %v", err)
	}
	if len(childApprovals) != 1 {
		t.Fatalf("child approvals = %#v, want one", childApprovals)
	}

	if _, err := app.ResolveApproval(ctx, childApprovals[0].ID, true); err != nil {
		t.Fatalf("ResolveApproval child: %v", err)
	}
	childRun, err := sqliteStore.GetRun(ctx, childApprovals[0].RunID)
	if err != nil {
		t.Fatalf("GetRun child: %v", err)
	}
	if childRun.Status != RunStatusCompleted {
		t.Fatalf("child status = %q, error %q, want completed", childRun.Status, childRun.Error)
	}

	parentApprovals, err := sqliteStore.ListApprovals(ctx, parent.ID, ApprovalStatePending)
	if err != nil {
		t.Fatalf("ListApprovals parent pending: %v", err)
	}
	if len(parentApprovals) != 1 {
		t.Fatalf("parent pending approvals = %#v, want one bridge approval", parentApprovals)
	}
	approvedAt := subagentTestTime()
	parentApprovals[0].State = ApprovalStateApproved
	parentApprovals[0].DecidedAt = &approvedAt
	if err := sqliteStore.UpdateApproval(ctx, parentApprovals[0]); err != nil {
		t.Fatalf("UpdateApproval parent bridge: %v", err)
	}

	task, err := sqliteStore.GetSubagentTaskByParentToolCall(ctx, parent.ID, "run_test_1", "tool_delegate")
	if err != nil {
		t.Fatalf("GetSubagentTaskByParentToolCall: %v", err)
	}
	task.Status = SubagentTaskStatusRunning
	task.UpdatedAt = subagentTestTime()
	if err := sqliteStore.UpdateSubagentTask(ctx, task); err != nil {
		t.Fatalf("UpdateSubagentTask: %v", err)
	}

	if err := app.RecoverSubagentTasks(ctx); err != nil {
		t.Fatalf("RecoverSubagentTasks: %v", err)
	}

	parentRun, err := sqliteStore.GetRun(ctx, "run_test_1")
	if err != nil {
		t.Fatalf("GetRun parent after recovery: %v", err)
	}
	if parentRun.Status != RunStatusCompleted {
		t.Fatalf("parent status after recovery = %q, error %q, want completed", parentRun.Status, parentRun.Error)
	}
	recoveredTask, err := sqliteStore.GetSubagentTaskByParentToolCall(ctx, parent.ID, "run_test_1", "tool_delegate")
	if err != nil {
		t.Fatalf("GetSubagentTaskByParentToolCall after recovery: %v", err)
	}
	if recoveredTask.Status != SubagentTaskStatusCompleted || recoveredTask.Summary != "child summary" {
		t.Fatalf("task after recovery = %#v, want completed child summary", recoveredTask)
	}
	messages, err := sqliteStore.ListMessages(ctx, parent.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages parent: %v", err)
	}
	if !messagesContain(messages, "parent complete after child summary") {
		t.Fatalf("parent messages missing final summary after recovery: %#v", messages)
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

func TestSpawnSubagentReturnsImmediatelyAndCompanionToolsInspectTask(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	runtime := &recordingRuntime{response: providers.Response{Text: "child summary"}}
	starter := &recordingRunStarter{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(SubagentToolExecutors(app)...))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Repo scan","goal":"Inspect the repo","context":"Focus on tests","runtime":"matrixclaw","isolation":"shared"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(spawn_subagent): %v", err)
	}
	if result.ToolResultMessage == nil || !strings.Contains(result.ToolResultMessage.Content, "Subagent Neo started") || !strings.Contains(result.ToolResultMessage.Content, "Task: Repo scan") {
		t.Fatalf("spawn result = %#v, want started message", result.ToolResultMessage)
	}
	if len(runtime.requests) != 0 {
		t.Fatalf("runtime requests = %d, want spawn_subagent to return before child execution", len(runtime.requests))
	}
	if len(starter.calls) != 1 {
		t.Fatalf("started runs = %#v, want one child run start", starter.calls)
	}

	var task SubagentTask
	if err := json.Unmarshal(result.ToolResultMessage.Parts[0].ToolResult.Metadata, &task); err != nil {
		t.Fatalf("decode spawn task metadata: %v", err)
	}
	if task.DisplayName != "Repo scan" || task.Mode != SubagentTaskModeAsync || task.Isolation != SubagentIsolationShared || task.Status != SubagentTaskStatusRunning {
		t.Fatalf("spawn task metadata = %#v, want async running Repo scan shared task", task)
	}
	if task.AgentName != "Neo" {
		t.Fatalf("spawn task agent name = %q, want Neo", task.AgentName)
	}

	replayed, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Repo scan","goal":"Inspect the repo","context":"Focus on tests","runtime":"matrixclaw","isolation":"shared"}`),
	})
	if err != nil {
		t.Fatalf("replay ExecuteTool(spawn_subagent): %v", err)
	}
	if replayed.ToolResultMessage == nil || !strings.Contains(replayed.ToolResultMessage.Content, task.ID) {
		t.Fatalf("replayed spawn result = %#v, want existing task handle %q", replayed.ToolResultMessage, task.ID)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("started runs after replay = %#v, want existing child reused", starter.calls)
	}

	listed, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "list_subagents",
		ToolCallID: "tool_list",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"include_recent":true}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(list_subagents): %v", err)
	}
	if listed.ToolResultMessage == nil || !strings.Contains(listed.ToolResultMessage.Content, "Neo") || !strings.Contains(listed.ToolResultMessage.Content, "Repo scan") || !strings.Contains(listed.ToolResultMessage.Content, "running") {
		t.Fatalf("list_subagents result = %#v, want running Neo Repo scan", listed.ToolResultMessage)
	}

	read, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "read_subagent_result",
		ToolCallID: "tool_read",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, task.ID)),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(read_subagent_result): %v", err)
	}
	if read.ToolResultMessage == nil || !strings.Contains(read.ToolResultMessage.Content, "Neo") || !strings.Contains(read.ToolResultMessage.Content, "Repo scan") || !strings.Contains(read.ToolResultMessage.Content, "running") {
		t.Fatalf("read_subagent_result = %#v, want running Neo Repo scan detail", read.ToolResultMessage)
	}
}

func TestSpawnSubagentAgentNameUsesMatrixPoolWithSuffixes(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	pool := []string{"Neo", "Trinity", "Morpheus", "Niobe", "Seraph", "Oracle", "Link", "Switch", "Apoc", "Tank", "Dozer", "Mouse"}
	for i, name := range pool {
		task := SubagentTask{
			ID:              fmt.Sprintf("subagent_existing_%02d", i),
			AgentName:       name,
			DisplayName:     fmt.Sprintf("Task %02d", i),
			Mode:            SubagentTaskModeBlocking,
			Isolation:       SubagentIsolationShared,
			ParentSessionID: parent.ID,
			Runtime:         string(SubagentRuntimeMatrixClaw),
			Goal:            "Existing task",
			Status:          SubagentTaskStatusCompleted,
			CreatedAt:       subagentTestTime().Add(time.Duration(i) * time.Minute),
			UpdatedAt:       subagentTestTime().Add(time.Duration(i) * time.Minute),
		}
		if err := sqliteStore.CreateSubagentTask(ctx, task); err != nil {
			t.Fatalf("CreateSubagentTask %s: %v", name, err)
		}
	}
	starter := &recordingRunStarter{}
	app.WithSessionLLMs(&subagentLLMs{runtime: &recordingRuntime{}})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(SubagentToolExecutors(app)...))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent_suffix",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn_suffix",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Suffix task","goal":"Check suffixing","runtime":"matrixclaw","isolation":"shared"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(spawn_subagent suffix): %v", err)
	}
	var task SubagentTask
	if result.ToolResultMessage == nil {
		t.Fatalf("spawn suffix result is nil")
	}
	if err := json.Unmarshal(result.ToolResultMessage.Parts[0].ToolResult.Metadata, &task); err != nil {
		t.Fatalf("decode spawn suffix task: %v", err)
	}
	if task.AgentName != "Neo-2" {
		t.Fatalf("spawn suffix agent name = %q, want Neo-2", task.AgentName)
	}
}

func TestSpawnSubagentLimitsActiveAsyncTasksPerParent(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	app.WithSessionLLMs(&subagentLLMs{runtime: &recordingRuntime{}})
	app.WithRunStarter(&recordingRunStarter{})
	app.WithTools(tools.NewRegistry(SubagentToolExecutors(app)...))

	for i := 0; i < 4; i++ {
		_, err := app.ExecuteTool(ctx, ExecuteToolInput{
			SessionID:  parent.ID,
			RunID:      "run_parent",
			ToolName:   "spawn_subagent",
			ToolCallID: fmt.Sprintf("tool_spawn_%d", i),
			WorkingDir: parent.WorkingDir,
			Args:       json.RawMessage(fmt.Sprintf(`{"name":"Worker %d","goal":"Task %d"}`, i+1, i+1)),
		})
		if err != nil {
			t.Fatalf("spawn %d: %v", i+1, err)
		}
	}

	_, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn_5",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Worker 5","goal":"Task 5"}`),
	})
	if err == nil || !strings.Contains(err.Error(), "active async subagent limit") {
		t.Fatalf("fifth spawn err = %v, want active async subagent limit", err)
	}
}

func TestSpawnSubagentCompletionUpdatesParentCardAndQueuesAutoResume(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	runtime := &recordingRuntime{response: providers.Response{Text: "child summary"}}
	starter := &recordingRunStarter{}
	app.WithSessionLLMs(&subagentLLMs{runtime: runtime})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(SubagentToolExecutors(app)...))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Repo scan","goal":"Inspect the repo"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(spawn_subagent): %v", err)
	}
	var task SubagentTask
	if err := json.Unmarshal(result.ToolResultMessage.Parts[0].ToolResult.Metadata, &task); err != nil {
		t.Fatalf("decode spawn task metadata: %v", err)
	}
	if err := app.ExecuteRun(ctx, task.ChildRunID); err != nil {
		t.Fatalf("ExecuteRun child: %v", err)
	}

	completed, err := sqliteStore.GetSubagentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetSubagentTask: %v", err)
	}
	if completed.Status != SubagentTaskStatusCompleted || completed.Summary != "child summary" || completed.ResultMessageID == "" || completed.CompletionDeliveredAt == nil {
		t.Fatalf("completed task = %#v, want completed summary, result message, delivered completion", completed)
	}

	messages, err := sqliteStore.ListMessages(ctx, parent.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages parent: %v", err)
	}
	if !messagesContain(messages, "Subagent Neo completed") || !messagesContain(messages, "child summary") {
		t.Fatalf("parent messages missing completion delivery: %#v", messages)
	}
	var updatedResult *Message
	for i := range messages {
		if messages[i].ID == completed.ResultMessageID {
			updatedResult = &messages[i]
			break
		}
	}
	if updatedResult == nil || !strings.Contains(updatedResult.Content, "Subagent Neo finished") || !strings.Contains(updatedResult.Content, "child summary") {
		t.Fatalf("updated result message = %#v, want finished child summary", updatedResult)
	}
	if len(starter.calls) < 2 {
		t.Fatalf("started runs = %#v, want child run plus parent auto-resume run", starter.calls)
	}
}

func TestPendingUserInputDefersSubagentCompletionAutoResumeDuringRecovery(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	app.WithSessionLLMs(&subagentLLMs{runtime: &recordingRuntime{response: providers.Response{Text: "queued input done"}}})

	if err := sqliteStore.CreateSessionInput(ctx, SessionInput{
		ID:        "input_pending",
		SessionID: parent.ID,
		Mode:      BusyInputModeQueue,
		Status:    SessionInputStatusPending,
		Text:      "user message should run first",
		CreatedAt: subagentTestTime(),
		UpdatedAt: subagentTestTime(),
	}); err != nil {
		t.Fatalf("CreateSessionInput: %v", err)
	}
	queuedAt := subagentTestTime()
	task := SubagentTask{
		ID:                 "subagent_done",
		DisplayName:        "Repo scan",
		Mode:               SubagentTaskModeAsync,
		Isolation:          SubagentIsolationShared,
		ParentSessionID:    parent.ID,
		ChildSessionID:     "session_child",
		ChildRunID:         "run_child",
		Runtime:            string(SubagentRuntimeMatrixClaw),
		Goal:               "Inspect the repo",
		Status:             SubagentTaskStatusCompleted,
		Summary:            "child summary",
		CompletionQueuedAt: &queuedAt,
		CreatedAt:          subagentTestTime(),
		UpdatedAt:          subagentTestTime(),
		FinishedAt:         &queuedAt,
	}
	if err := sqliteStore.CreateSubagentTask(ctx, task); err != nil {
		t.Fatalf("CreateSubagentTask: %v", err)
	}

	if err := app.RecoverSubagentTasks(ctx); err != nil {
		t.Fatalf("RecoverSubagentTasks: %v", err)
	}
	if len(starter.calls) != 0 {
		t.Fatalf("starter calls after subagent recovery = %#v, want no auto-resume while user input is pending", starter.calls)
	}
	recoveredTask, err := sqliteStore.GetSubagentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetSubagentTask after subagent recovery: %v", err)
	}
	if recoveredTask.CompletionDeliveredAt != nil {
		t.Fatalf("task delivered at %v while user input was pending", recoveredTask.CompletionDeliveredAt)
	}

	if err := app.RecoverSessionInputs(ctx); err != nil {
		t.Fatalf("RecoverSessionInputs: %v", err)
	}
	if len(starter.calls) != 1 {
		t.Fatalf("starter calls after input recovery = %#v, want queued user input started", starter.calls)
	}
	recoveredTask, err = sqliteStore.GetSubagentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetSubagentTask after input recovery: %v", err)
	}
	if recoveredTask.CompletionDeliveredAt != nil {
		t.Fatalf("task delivered at %v before queued user input completed", recoveredTask.CompletionDeliveredAt)
	}

	if err := app.ExecuteRun(ctx, starter.calls[0]); err != nil {
		t.Fatalf("ExecuteRun queued input: %v", err)
	}
	recoveredTask, err = sqliteStore.GetSubagentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetSubagentTask after queued input: %v", err)
	}
	if recoveredTask.CompletionDeliveredAt == nil || recoveredTask.CompletionAutoResumeRunID == "" {
		t.Fatalf("task after queued input = %#v, want delivered auto-resume", recoveredTask)
	}
	if len(starter.calls) != 2 {
		t.Fatalf("starter calls after queued input = %#v, want queued input plus subagent auto-resume", starter.calls)
	}
}

func TestAsyncSubagentPublishesActivityUpdatesWhileChildRuns(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	base := subagentTestTime()
	var tick int
	app.WithClock(func() time.Time {
		tick++
		return base.Add(time.Duration(tick) * time.Second)
	})
	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", t.TempDir())
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	app.WithSessionLLMs(&subagentLLMs{runtime: streamingSubagentRuntime{}})
	app.WithTools(tools.NewRegistry(SubagentToolExecutors(app)...))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Repo scan","goal":"Inspect the repo"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(spawn_subagent): %v", err)
	}
	var task SubagentTask
	if err := json.Unmarshal(result.ToolResultMessage.Parts[0].ToolResult.Metadata, &task); err != nil {
		t.Fatalf("decode spawn task metadata: %v", err)
	}
	events := app.SubscribeEvents(ctx, parent.ID)

	if err := app.ExecuteRun(ctx, task.ChildRunID); err != nil {
		t.Fatalf("ExecuteRun child: %v", err)
	}

	sawRunningActivity := false
	for {
		select {
		case event := <-events:
			if event.Type != EventSubagentUpdated {
				continue
			}
			updated, ok := event.Payload.(SubagentTask)
			if !ok || updated.ID != task.ID {
				continue
			}
			if updated.Status == SubagentTaskStatusRunning && updated.UpdatedAt.After(task.UpdatedAt) {
				sawRunningActivity = true
			}
		default:
			if !sawRunningActivity {
				t.Fatalf("did not receive running subagent activity update for task %#v", task)
			}
			return
		}
	}
}

func TestSpawnSubagentWorktreeIsolationCreatesDetachedChildWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	app, sqliteStore, cleanup := newSubagentTestCore(t)
	defer cleanup()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "init")

	parent := saveSubagentTestSession(t, sqliteStore, "session_parent", "Parent", repo)
	app.WithSessionLLMs(&subagentLLMs{runtime: &recordingRuntime{}})
	app.WithRunStarter(&recordingRunStarter{})
	app.WithTools(tools.NewRegistry(SubagentToolExecutors(app)...))

	result, err := app.ExecuteTool(ctx, ExecuteToolInput{
		SessionID:  parent.ID,
		RunID:      "run_parent",
		ToolName:   "spawn_subagent",
		ToolCallID: "tool_spawn_worktree",
		WorkingDir: parent.WorkingDir,
		Args:       json.RawMessage(`{"name":"Writer","goal":"Edit independently","isolation":"worktree"}`),
	})
	if err != nil {
		t.Fatalf("ExecuteTool(spawn_subagent worktree): %v", err)
	}
	var task SubagentTask
	if err := json.Unmarshal(result.ToolResultMessage.Parts[0].ToolResult.Metadata, &task); err != nil {
		t.Fatalf("decode spawn task metadata: %v", err)
	}
	child, err := sqliteStore.GetSession(ctx, task.ChildSessionID)
	if err != nil {
		t.Fatalf("GetSession child: %v", err)
	}
	if task.Isolation != SubagentIsolationWorktree {
		t.Fatalf("task isolation = %q, want worktree", task.Isolation)
	}
	if child.WorkingDir == repo || !strings.Contains(child.WorkingDir, task.ID) {
		t.Fatalf("child working dir = %q, parent repo = %q, want task-specific worktree", child.WorkingDir, repo)
	}
	gotRoot := runGitOutput(t, child.WorkingDir, "rev-parse", "--show-toplevel")
	if strings.TrimSpace(gotRoot) != child.WorkingDir {
		t.Fatalf("child git root = %q, want child working dir %q", strings.TrimSpace(gotRoot), child.WorkingDir)
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

func messagesContain(messages []Message, text string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runGitOutput(t, dir, args...)
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

type syncRunStarter struct {
	app *Core
}

func (s syncRunStarter) StartRun(ctx context.Context, runID string) error {
	return s.app.ExecuteRun(ctx, runID)
}

type recordingRunStarter struct {
	calls []string
}

func (s *recordingRunStarter) StartRun(_ context.Context, runID string) error {
	s.calls = append(s.calls, runID)
	return nil
}

type recordingRuntime struct {
	response providers.Response
	requests []providers.Request
}

func (r *recordingRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	r.requests = append(r.requests, request)
	return r.response, nil
}

type streamingSubagentRuntime struct{}

func (streamingSubagentRuntime) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	if strings.Contains(request.SystemPrompt, "child agent") {
		if err := providers.StreamText(ctx, "child is working"); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{Text: "child summary"}, nil
	}
	return providers.Response{Text: "parent summary"}, nil
}

type subagentApprovalBridgeRuntime struct {
	parentDelegateCalls int
}

func (r *subagentApprovalBridgeRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	if strings.Contains(request.SystemPrompt, "child agent") {
		if requestContainsToolResult(request, "bash") {
			return providers.Response{Text: "child summary"}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{
			ID:        "tool_child_bash",
			Name:      "bash",
			Arguments: json.RawMessage(`{"command":"echo child","description":"child command"}`),
		}}}, nil
	}
	if requestContainsToolResult(request, "delegate_task") {
		if requestContainsText(request, "approval denied") {
			return providers.Response{Text: "parent handled subagent denial"}, nil
		}
		return providers.Response{Text: "parent complete after child summary"}, nil
	}
	r.parentDelegateCalls++
	return providers.Response{ToolCalls: []providers.ToolCall{{
		ID:        "tool_delegate",
		Name:      "delegate_task",
		Arguments: json.RawMessage(`{"goal":"Run a child command","runtime":"matrixclaw"}`),
	}}}, nil
}

type subagentRepeatedApprovalRuntime struct{}

func (r *subagentRepeatedApprovalRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	if strings.Contains(request.SystemPrompt, "child agent") {
		switch requestToolResultCount(request, "bash") {
		case 0:
			return providers.Response{ToolCalls: []providers.ToolCall{{
				ID:        "tool_child_bash_1",
				Name:      "bash",
				Arguments: json.RawMessage(`{"command":"echo first","description":"first child command"}`),
			}}}, nil
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{
				ID:        "tool_child_bash_2",
				Name:      "bash",
				Arguments: json.RawMessage(`{"command":"echo second","description":"second child command"}`),
			}}}, nil
		default:
			return providers.Response{Text: "child summary after repeated approvals"}, nil
		}
	}
	if requestContainsToolResult(request, "delegate_task") {
		return providers.Response{Text: "parent complete after repeated approvals"}, nil
	}
	return providers.Response{ToolCalls: []providers.ToolCall{{
		ID:        "tool_delegate",
		Name:      "delegate_task",
		Arguments: json.RawMessage(`{"goal":"Run two child commands","runtime":"matrixclaw"}`),
	}}}, nil
}

func requestContainsToolResult(request providers.Request, toolName string) bool {
	return requestToolResultCount(request, toolName) > 0
}

func requestToolResultCount(request providers.Request, toolName string) int {
	toolCallIDs := map[string]struct{}{}
	count := 0
	for _, message := range request.Messages {
		for _, call := range message.ToolCalls {
			if call.Name == toolName {
				toolCallIDs[call.ID] = struct{}{}
			}
		}
		if _, ok := toolCallIDs[message.ToolCallID]; ok && message.Role == string(MessageRoleTool) {
			count++
		}
	}
	return count
}

func requestContainsText(request providers.Request, text string) bool {
	for _, message := range request.Messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
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
