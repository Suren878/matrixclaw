package core_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// bridgedChild delegates from a parent to a child whose one mutating call waits
// for approval; the parent's lead calls come before its delegate call.
type bridgedChild struct {
	app       *core.Core
	db        *store.SQLiteStore
	mutations int
	mu        sync.Mutex
	saw       string
}

func newBridgedChild(t *testing.T, starter func(*core.Core) core.RunStarter, lead ...providers.ToolCall) *bridgedChild {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	b := &bridgedChild{app: app, db: db}
	app.WithRunStarter(starter(app))
	mutate, _ := approvalTools(&b.mutations)
	app.WithTools(tools.NewRegistry(append(core.SubagentToolExecutors(app), mutate, askingReadTool("ask_read"))...))
	childCalls, parentCalls := 0, 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			childCalls++
			if childCalls == 1 {
				return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
			}
			b.saw = toolResultContent(request, "call-child-mutate")
			return providers.Response{Text: "Child read: " + b.saw}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			delegate := providers.ToolCall{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"change the state","runtime":"matrixclaw"}`)}
			return providers.Response{ToolCalls: append(slices.Clone(lead), delegate)}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	return b
}

func (b *bridgedChild) childSaw() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saw
}

// park runs the parent until it waits for the child's approval and returns the
// parent's run, its bridged approval and the child's run.
func (b *bridgedChild) park(t *testing.T) (core.Run, core.Approval, string) {
	t.Helper()
	session, run := saveCrashRecoveryRun(t, b.db, "bridge-parent", core.RunStatusAccepted, false)
	if err := b.app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, b.db, run.ID, core.RunStatusWaitingApproval)
	bridge := pendingApprovalFor(t, b.db, session.ID, "call-delegate")
	task, err := b.db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, b.db, task.ChildRunID, core.RunStatusWaitingApproval)
	return run, bridge, task.ChildRunID
}

func pendingApprovalFor(t *testing.T, db *store.SQLiteStore, sessionID string, callID string) core.Approval {
	t.Helper()
	approvals, err := db.ListApprovals(context.Background(), sessionID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	for _, approval := range approvals {
		if approval.ToolCallRef == callID {
			return approval
		}
	}
	t.Fatalf("no pending approval for %s in %+v", callID, approvals)
	return core.Approval{}
}

func storedToolResult(t *testing.T, db *store.SQLiteStore, sessionID string, callID string) string {
	t.Helper()
	for _, message := range sessionMessages(t, db, sessionID) {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == callID {
				return part.ToolResult.Content
			}
		}
	}
	return ""
}

func TestGrantingABridgedApprovalBeforeTheChildResumes(t *testing.T) {
	starter := &recordingRunStarter{}
	b := newBridgedChild(t, func(*core.Core) core.RunStarter { return starter })
	parent, bridge, childRunID := b.park(t)

	if _, err := b.app.ResolveApproval(context.Background(), bridge.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if got := starter.count(childRunID); got != 1 {
		t.Fatalf("child schedules = %d, want 1", got)
	}
	if err := b.app.ExecuteRun(context.Background(), childRunID); err != nil {
		t.Fatal(err)
	}
	if err := b.app.ExecuteRun(context.Background(), parent.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	if b.mutations != 1 || b.childSaw() != "mutated" {
		t.Fatalf("mutations = %d, child saw %q", b.mutations, b.childSaw())
	}
}

func TestDeniedBridgedApprovalLetsTheChildGoOn(t *testing.T) {
	var starter *executingRunStarter
	b := newBridgedChild(t, func(app *core.Core) core.RunStarter {
		starter = &executingRunStarter{app: app}
		return starter
	})
	parent, bridge, childRunID := b.park(t)

	if _, err := b.app.ResolveApproval(context.Background(), bridge.ID, core.ApprovalResolveRequest{Reason: "not in the shared tree"}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	waitForRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	starter.wait(t)

	assertRecoveryRunStatus(t, b.db, childRunID, core.RunStatusCompleted)
	if b.mutations != 0 || b.childSaw() != "User denied: not in the shared tree" {
		t.Fatalf("mutations = %d, child saw %q", b.mutations, b.childSaw())
	}
	task, err := b.db.GetSubagentTaskByParentToolCall(context.Background(), parent.SessionID, parent.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertSubagentTaskStatus(t, task, core.SubagentTaskStatusCompleted)
	for _, message := range sessionMessages(t, b.db, parent.SessionID) {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == "call-delegate" && part.ToolResult.Content != "Child read: User denied: not in the shared tree" {
				t.Fatalf("delegate result = %q", part.ToolResult.Content)
			}
		}
	}
}

func TestAlwaysAllowOnABridgedApprovalKeepsTheRuleForTheParent(t *testing.T) {
	var starter *executingRunStarter
	b := newBridgedChild(t, func(app *core.Core) core.RunStarter {
		starter = &executingRunStarter{app: app}
		return starter
	})
	parent, bridge, childRunID := b.park(t)
	if bridge.Suggestion == nil || bridge.Suggestion.String() != "mutate_state" {
		t.Fatalf("bridged suggestion = %+v", bridge.Suggestion)
	}

	if _, err := b.app.ResolveApproval(context.Background(), bridge.ID, core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession}); err != nil {
		t.Fatal(err)
	}
	waitForRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	starter.wait(t)

	child, err := b.db.GetRun(context.Background(), childRunID)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := b.db.ListPermissionRules(context.Background(), []string{child.SessionID})
	if err != nil || len(rules) != 0 {
		t.Fatalf("child rules = %+v err = %v", rules, err)
	}
	rules, err = b.db.ListPermissionRules(context.Background(), []string{parent.SessionID})
	if err != nil || len(rules) != 1 || rules[0].String() != "mutate_state" || rules[0].SessionID != parent.SessionID {
		t.Fatalf("parent rules = %+v err = %v", rules, err)
	}
	if b.mutations != 1 {
		t.Fatalf("mutations = %d", b.mutations)
	}
}

func TestParentWaitsForItsChildAfterEveryApprovalIsDecided(t *testing.T) {
	for _, bridgedFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "bridged first", false: "read first"}[bridgedFirst], func(t *testing.T) {
			starter := &recordingRunStarter{}
			b := newBridgedChild(t, func(*core.Core) core.RunStarter { return starter }, providers.ToolCall{ID: "call-ask", Name: "ask_read", Arguments: []byte(`{}`)})
			parent, bridge, childRunID := b.park(t)
			order := []string{bridge.ID, pendingApprovalFor(t, b.db, parent.SessionID, "call-ask").ID}
			if !bridgedFirst {
				slices.Reverse(order)
			}

			for _, id := range order {
				if _, err := b.app.ResolveApproval(context.Background(), id, core.ApprovalResolveRequest{Approved: true}); err != nil {
					t.Fatalf("ResolveApproval(%s): %v", id, err)
				}
			}
			if got := starter.count(parent.ID); got != 0 {
				t.Fatalf("parent scheduled %d times while its child still works", got)
			}
			assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusWaitingApproval)

			if err := b.app.ExecuteRun(context.Background(), childRunID); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for starter.count(parent.ID) == 0 {
				if time.Now().After(deadline) {
					t.Fatal("parent not scheduled after its child finished")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := b.app.ExecuteRun(context.Background(), parent.ID); err != nil {
				t.Fatal(err)
			}

			assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
			if got := storedToolResult(t, b.db, parent.SessionID, "call-delegate"); got != "Child read: mutated" {
				t.Fatalf("delegate result = %q", got)
			}
			if got := storedToolResult(t, b.db, parent.SessionID, "call-ask"); got != "secret ask_read" {
				t.Fatalf("ask_read result = %q", got)
			}
		})
	}
}
