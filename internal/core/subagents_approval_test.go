package core_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// askingChild delegates from a parent to a blocking child whose one mutating
// call waits for approval; the parent's lead calls come before its agent call.
type askingChild struct {
	app       *core.Core
	db        *store.SQLiteStore
	parking   *parkingStore
	starter   *executingRunStarter
	mutations atomic.Int32
	mu        sync.Mutex
	saw       string
	requests  []providers.Request
}

func newAskingChild(t *testing.T, lead ...providers.ToolCall) *askingChild {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	parking := &parkingStore{SQLiteStore: db}
	app := core.New(parking)
	b := &askingChild{app: app, db: db, parking: parking, starter: &executingRunStarter{app: app}}
	app.WithRunStarter(b.starter)
	mutate := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if !call.Approved {
			return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "mutate_state", ToolCallID: call.ToolCallID, Action: "write_state", Description: "write the state", Suggestion: &permission.Suggestion{Tool: "mutate_state"}}}, nil
		}
		b.mutations.Add(1)
		return tools.Result{Content: "mutated"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), mutate, askingReadTool("ask_read"))...))
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
		b.requests = append(b.requests, request)
		if parentCalls == 1 {
			delegate := providers.ToolCall{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Change the state","prompt":"change the state","runtime":"matrixclaw"}`)}
			return providers.Response{ToolCalls: append(slices.Clone(lead), delegate)}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	return b
}

func (b *askingChild) childSaw() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saw
}

// lastParentRequest is the newest model request of the parent.
func (b *askingChild) lastParentRequest() providers.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests[len(b.requests)-1]
}

// start runs the parent in the background until its child asks for approval
// and returns the parent's run, the child's approval, the task and the channel
// ExecuteRun of the parent returns on.
func (b *askingChild) start(t *testing.T, name string) (core.Run, core.Approval, core.Task, chan error) {
	t.Helper()
	session, run := saveCrashRecoveryRun(t, b.db, name, core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- b.app.ExecuteRun(context.Background(), run.ID) }()
	approval := waitPendingApproval(t, b.db, session.ID, "call-child-mutate")
	task, err := taskOfCall(b.db, session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, b.db, task.ChildRunID, core.RunStatusWaitingApproval)
	assertRecoveryRunStatus(t, b.db, run.ID, core.RunStatusRunning)
	return run, approval, task, done
}

// finish waits for the parent's ExecuteRun to return and its runs to settle.
func (b *askingChild) finish(t *testing.T, done chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the parent did not return")
	}
	b.starter.wait(t)
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

func TestASubagentAsksTheUserWhileItsParentWaitsInItsCall(t *testing.T) {
	t.Parallel()
	b := newAskingChild(t)
	parent, approval, task, done := b.start(t, "ask-parent")

	if approval.TaskID != task.ID || approval.AgentName != "Neo" || approval.SessionID != task.ChildSessionID || approval.RunID != task.ChildRunID || approval.ToolName != "mutate_state" {
		t.Fatalf("child's approval = %+v, task = %+v", approval, task)
	}
	if stored, err := b.db.GetTask(context.Background(), task.ID); err != nil || stored.Status != core.TaskStatusWaitingApproval {
		t.Fatalf("task = %+v, %v", stored, err)
	}
	if _, err := b.app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	b.finish(t, done)

	assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	if b.mutations.Load() != 1 || b.childSaw() != "mutated" {
		t.Fatalf("mutations = %d, child saw %q", b.mutations.Load(), b.childSaw())
	}
	if got := storedToolResult(t, b.db, parent.SessionID, "call-delegate"); got != "Child read: mutated" {
		t.Fatalf("delegate result = %q", got)
	}
	if stored, err := b.db.GetTask(context.Background(), task.ID); err != nil || stored.Status != core.TaskStatusCompleted || stored.DeliveredAt == nil {
		t.Fatalf("task = %+v, %v", stored, err)
	}
}

func TestDeniedSubagentApprovalLetsTheChildGoOn(t *testing.T) {
	t.Parallel()
	b := newAskingChild(t)
	parent, approval, task, done := b.start(t, "deny-parent")

	if _, err := b.app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Reason: "not in the shared tree"}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	b.finish(t, done)

	assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
	assertRecoveryRunStatus(t, b.db, task.ChildRunID, core.RunStatusCompleted)
	if b.mutations.Load() != 0 || b.childSaw() != "User denied: not in the shared tree" {
		t.Fatalf("mutations = %d, child saw %q", b.mutations.Load(), b.childSaw())
	}
	if got := storedToolResult(t, b.db, parent.SessionID, "call-delegate"); got != "Child read: User denied: not in the shared tree" {
		t.Fatalf("delegate result = %q", got)
	}
}

func TestAlwaysAllowOnASubagentApprovalKeepsTheRuleForTheParent(t *testing.T) {
	t.Parallel()
	b := newAskingChild(t)
	parent, approval, task, done := b.start(t, "always-parent")
	if approval.Suggestion == nil || approval.Suggestion.String() != "mutate_state" {
		t.Fatalf("suggestion = %+v", approval.Suggestion)
	}

	if _, err := b.app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true, Always: permission.ScopeSession}); err != nil {
		t.Fatal(err)
	}
	b.finish(t, done)

	rules, err := b.db.ListPermissionRules(context.Background(), []string{task.ChildSessionID})
	if err != nil || len(rules) != 0 {
		t.Fatalf("child rules = %+v err = %v", rules, err)
	}
	rules, err = b.db.ListPermissionRules(context.Background(), []string{parent.SessionID})
	if err != nil || len(rules) != 1 || rules[0].String() != "mutate_state" || rules[0].SessionID != parent.SessionID {
		t.Fatalf("parent rules = %+v err = %v", rules, err)
	}
	if b.mutations.Load() != 1 {
		t.Fatalf("mutations = %d", b.mutations.Load())
	}
}

func TestParentWaitsForItsChildAfterEveryApprovalIsDecided(t *testing.T) {
	t.Parallel()
	for _, childFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "child first", false: "parent first"}[childFirst], func(t *testing.T) {
			t.Parallel()
			b := newAskingChild(t, providers.ToolCall{ID: "call-ask", Name: "ask_read", Arguments: []byte(`{}`)})
			parent, child, task, done := b.start(t, "both-ask")
			order := []string{child.ID, pendingApprovalFor(t, b.db, parent.SessionID, "call-ask").ID}
			if !childFirst {
				slices.Reverse(order)
			}

			if _, err := b.app.ResolveApproval(context.Background(), order[0], core.ApprovalResolveRequest{Approved: true}); err != nil {
				t.Fatal(err)
			}
			if !childFirst {
				assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusRunning)
			}
			if _, err := b.app.ResolveApproval(context.Background(), order[1], core.ApprovalResolveRequest{Approved: true}); err != nil {
				t.Fatal(err)
			}
			if !childFirst {
				b.finish(t, done)
			}
			waitRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
			b.starter.wait(t)

			assertRecoveryRunStatus(t, b.db, task.ChildRunID, core.RunStatusCompleted)
			if got := storedToolResult(t, b.db, parent.SessionID, "call-delegate"); got != "Child read: mutated" {
				t.Fatalf("delegate result = %q", got)
			}
			if got := storedToolResult(t, b.db, parent.SessionID, "call-ask"); got != "secret ask_read" {
				t.Fatalf("ask_read result = %q", got)
			}
		})
	}
}

func TestChildApprovalDecidedWhileTheChildParksKeepsTheParentWaiting(t *testing.T) {
	t.Parallel()
	b := newAskingChild(t)
	b.parking.beforePark = func() {
		task, err := taskOfCall(b.db, "session_park-race", "run_park-race", "call-delegate")
		if err != nil {
			t.Errorf("subagent task: %v", err)
			return
		}
		child := pendingApprovalFor(t, b.db, task.ChildSessionID, "call-child-mutate")
		if _, err := b.app.ResolveApproval(context.Background(), child.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
			t.Errorf("ResolveApproval: %v", err)
		}
	}
	session, run := saveCrashRecoveryRun(t, b.db, "park-race", core.RunStatusAccepted, false)

	if err := b.app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	b.starter.wait(t)

	assertRecoveryRunStatus(t, b.db, run.ID, core.RunStatusCompleted)
	if got := storedToolResult(t, b.db, session.ID, "call-delegate"); got != "Child read: mutated" {
		t.Fatalf("delegate result = %q", got)
	}
}

func TestMessagesToAParentWaitingForItsChildSteerOrQueueAsUsual(t *testing.T) {
	t.Parallel()
	for _, mode := range []core.BusyInputMode{core.BusyInputModeSteer, core.BusyInputModeQueue} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			b := newAskingChild(t)
			parent, approval, _, done := b.start(t, "busy-"+string(mode))

			accepted, err := b.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: parent.SessionID, Text: "also check the logs", BusyMode: mode})
			if err != nil || accepted.Input == nil || accepted.Input.Mode != mode {
				t.Fatalf("accepted = %+v, %v", accepted, err)
			}
			if _, err := b.app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
				t.Fatal(err)
			}
			b.finish(t, done)

			switch mode {
			case core.BusyInputModeSteer:
				assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCompleted)
				if got := toolResultContent(b.lastParentRequest(), "call-delegate"); !strings.Contains(got, "User guidance: also check the logs") {
					t.Fatalf("delegate result the parent read = %q", got)
				}
			case core.BusyInputModeQueue:
				runs, err := b.db.ListSessionRuns(context.Background(), parent.SessionID, 0)
				if err != nil || len(runs) != 2 || runs[0].ID == parent.ID {
					t.Fatalf("runs = %+v, %v; want the queued message's run after the parent", runs, err)
				}
				waitRunStatus(t, b.db, runs[0].ID, core.RunStatusCompleted)
				b.starter.wait(t)
			}
		})
	}
}

func TestCancelingAParentWaitingForItsChildEndsTheWaitAndTheChild(t *testing.T) {
	t.Parallel()
	b := newAskingChild(t)
	parent, approval, task, done := b.start(t, "cancel-waiting")

	if _, err := b.app.CancelRun(context.Background(), parent.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the parent kept waiting for its child after it was canceled")
	}
	b.starter.wait(t)

	assertRecoveryRunStatus(t, b.db, parent.ID, core.RunStatusCanceled)
	assertRecoveryRunStatus(t, b.db, task.ChildRunID, core.RunStatusCanceled)
	if decided, err := b.db.GetApproval(context.Background(), approval.ID); err != nil || decided.State != core.ApprovalStateRejected {
		t.Fatalf("child's approval = %+v, %v", decided, err)
	}
	if stored, err := b.db.GetTask(context.Background(), task.ID); err != nil || stored.Status != core.TaskStatusCanceled || stored.DeliveredAt == nil {
		t.Fatalf("task = %+v, %v", stored, err)
	}
	// The call ends canceled, or with its child's end when that came first.
	if got := storedToolResult(t, b.db, parent.SessionID, "call-delegate"); got != "Canceled by user." && got != "Subagent canceled with its parent run." {
		t.Fatalf("delegate result = %q", got)
	}
	if b.mutations.Load() != 0 {
		t.Fatalf("mutations = %d", b.mutations.Load())
	}
}

func waitPendingApproval(t *testing.T, db *store.SQLiteStore, sessionID string, callID string) core.Approval {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		approvals, err := db.ListApprovals(context.Background(), sessionID, core.ApprovalStatePending)
		if err != nil {
			t.Fatal(err)
		}
		for _, approval := range approvals {
			if approval.ToolCallRef == callID {
				return approval
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pending approval for %s", callID)
		}
	}
}

func TestParentAwaitingABackgroundChildWakesAfterTheChildsApproval(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db)
	starter := &executingRunStarter{app: app}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(starter)
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(append(append(core.AgentToolExecutors(app), core.AwaitToolExecutors(app)...), mutate)...))
	var mu sync.Mutex
	parked := make(chan struct{})
	childCalls, parentCalls := 0, 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			<-parked
			mu.Lock()
			defer mu.Unlock()
			childCalls++
			if childCalls == 1 {
				return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
			}
			return providers.Response{Text: "Child done."}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		parentCalls++
		switch parentCalls {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "agent", Arguments: []byte(`{"description":"Writer","prompt":"change the state","background":true,"runtime":"matrixclaw"}`)}}}, nil
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-await", Name: "await", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "await-child", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingEvents)
	close(parked)

	approval := waitPendingApproval(t, db, session.ID, "call-child-mutate")
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingEvents)
	if _, err := app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}

	waitRunStatus(t, db, run.ID, core.RunStatusCompleted)
	starter.wait(t)
	if mutations != 1 {
		t.Fatalf("mutations = %d", mutations)
	}
	if _, err := db.GetRunWakeup(context.Background(), run.ID); err != core.ErrNotFound {
		t.Fatalf("wakeup after the run completed: %v", err)
	}
}

func TestStoppingABackgroundChildParkedOnApprovalEndsIt(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db)
	starter := &executingRunStarter{app: app}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(starter)
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(append(append(core.AgentToolExecutors(app), core.AwaitToolExecutors(app)...), mutate)...))
	var mu sync.Mutex
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		parentCalls++
		switch parentCalls {
		case 1:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "agent", Arguments: []byte(`{"description":"Writer","prompt":"change the state","background":true,"runtime":"matrixclaw"}`)}}}, nil
		case 2:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-await", Name: "await", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "stop-parked-child", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approval := waitPendingApproval(t, db, session.ID, "call-child-mutate")
	task, err := taskOfCall(db, session.ID, run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, task.ChildRunID, core.RunStatusWaitingApproval)

	stopped, err := app.CancelTask(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}

	if stopped.Status != core.TaskStatusCanceled || stopped.FinishedAt == nil {
		t.Fatalf("stopped task = %+v", stopped)
	}
	if decided, err := db.GetApproval(context.Background(), approval.ID); err != nil || decided.State != core.ApprovalStateRejected {
		t.Fatalf("child's approval = %+v, %v", decided, err)
	}
	waitRunStatus(t, db, run.ID, core.RunStatusCompleted)
	starter.wait(t)
	if mutations != 0 {
		t.Fatalf("mutations = %d", mutations)
	}
}

func TestBackgroundChildsApprovalGoesToTheChatTheSessionAnswersIn(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db)
	starter := &executingRunStarter{app: app}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(starter)
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), mutate)...))
	var mu sync.Mutex
	parentCalls := 0
	parentDone := make(chan struct{})
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			<-parentDone
			if toolResultContent(request, "call-child-mutate") != "" {
				return providers.Response{Text: "Child stopped."}, nil
			}
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "agent", Arguments: []byte(`{"description":"Writer","prompt":"change the state","background":true,"runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Started the writer."}, nil
	})})
	session, earlier := saveCrashRecoveryRun(t, db, "approval-delivery", core.RunStatusCompleted, false)
	run := core.Run{ID: "run_from_telegram", SessionID: session.ID, UserMessageID: "msg_from_telegram", Client: "telegram", ExternalKey: "42", Status: core.RunStatusAccepted, StartedAt: earlier.StartedAt.Add(time.Second), UpdatedAt: earlier.StartedAt.Add(time.Second)}
	user := transcript.Message{ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser, Content: "start a writer", Parts: transcript.NormalizeMessageParts("start a writer", nil), CreatedAt: run.StartedAt, UpdatedAt: run.StartedAt}
	if err := db.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
	address := json.RawMessage(`{"kind":"chat","chat_id":42}`)
	if err := db.CreateClientDelivery(context.Background(), core.ClientDelivery{ID: "delivery_run", Type: core.ClientDeliveryTypeRun, Client: "telegram", ExternalKey: "42", SessionID: session.ID, RunID: run.ID, Address: address, Status: core.ClientDeliveryStatusSent, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	close(parentDone)

	approval := waitPendingApproval(t, db, session.ID, "call-child-mutate")
	var deliveries []core.ClientDelivery
	for deadline := time.Now().Add(5 * time.Second); len(deliveries) == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		var err error
		if deliveries, err = db.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{Type: core.ClientDeliveryTypeApproval}); err != nil {
			t.Fatal(err)
		}
	}
	if len(deliveries) != 1 {
		t.Fatalf("approval deliveries = %+v", deliveries)
	}
	var payload core.ApprovalDeliveryPayload
	delivery := deliveries[0]
	if err := json.Unmarshal(delivery.Payload, &payload); err != nil || payload.ApprovalID != approval.ID {
		t.Fatalf("payload = %s, %v", delivery.Payload, err)
	}
	if delivery.Client != "telegram" || delivery.ExternalKey != "42" || delivery.SessionID != session.ID || string(delivery.Address) != string(address) || delivery.Status != core.ClientDeliveryStatusPending {
		t.Fatalf("delivery = %+v", delivery)
	}
	if delivery.RunID != run.ID || delivery.TaskID == "" {
		t.Fatalf("delivery = %+v, want the parent's run and the child's task", delivery)
	}
	if _, err := app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{}); err != nil {
		t.Fatal(err)
	}
	starter.wait(t)
}
