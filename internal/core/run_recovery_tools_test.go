package core_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// slowTool is a read-only tool whose calls last until release is closed.
type slowTool struct {
	recoveryTool
	release chan struct{}
}

func (t *slowTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	<-t.release
	return t.recoveryTool.Execute(ctx, call)
}

func TestRecoveryLeavesReadOnlyCallsInFlightToTheResumedRun(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app := core.New(core.Config{Store: sqliteStore, RunStarter: starter})
	app.SetSessionLLMs(recoveryLLMs{runtime: &recoveryRuntime{text: "Inspected."}})
	slow := &slowTool{recoveryTool: recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}, release: make(chan struct{})}
	app.SetTools(tools.NewRegistry(slow))
	var runs []core.Run
	for _, name := range []string{"slow_a", "slow_b"} {
		_, run := saveCrashRecoveryRun(t, sqliteStore, name, core.RunStatusRunning, false)
		saveInterruptedToolCall(t, sqliteStore, run, "tool_"+name, slow.spec.ID)
		runs = append(runs, run)
	}

	recovered := make(chan error, 1)
	go func() { recovered <- app.Recover(context.Background()) }()
	select {
	case err := <-recovered:
		if err != nil {
			t.Fatalf("Recover: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(slow.release)
		t.Fatal("recovery waited for a read-only call")
	}
	for _, run := range runs {
		if got := starter.count(run.ID); got != 1 {
			t.Fatalf("run %s schedules = %d, want 1", run.ID, got)
		}
	}
	close(slow.release)
	if err := app.ExecuteRun(context.Background(), runs[0].ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, runs[0].ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, runs[0].SessionID, "tool_slow_a", 1)
	if got := slow.callCount(); got != 1 {
		t.Fatalf("read-only tool executions = %d, want 1", got)
	}
}

func TestRecoverMutatingToolRequiresFreshApprovalBeforeSingleReplay(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app := core.New(core.Config{Store: sqliteStore, RunStarter: starter})
	runtime := &recoveryRuntime{text: "finished after approved recovery"}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	app.SetSessionLLMs(recoveryLLMs{runtime: runtime})
	app.SetTools(tools.NewRegistry(mutation))

	_, run := saveCrashRecoveryRun(t, sqliteStore, "mutation", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	recoverRun(t, app, run.ID)
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusWaitingApproval)
	if got := mutation.callCount(); got != 0 {
		t.Fatalf("mutating tool executed without recovery approval: %d", got)
	}
	approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || !strings.Contains(approvals[0].Description, "The daemon restarted") || approvals[0].Suggestion == nil || approvals[0].Suggestion.String() != "mutate_state" {
		t.Fatalf("recovery approvals = %#v, want one restart retry approval", approvals)
	}
	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatalf("ResolveApproval: %v", err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ExecuteRun after approval: %v", err)
	}
	if got := mutation.callCount(); got != 1 {
		t.Fatalf("mutating tool executions after approval = %d, want exactly 1", got)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, run.SessionID, "tool_mutation", 1)
}

func TestRecoveredGrantedCallWaitsForTheRetryApproval(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	app := core.New(core.Config{Store: sqliteStore, RunStarter: &recordingRunStarter{}})
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	mutation.spec.Asks = true
	app.SetSessionLLMs(recoveryLLMs{runtime: &recoveryRuntime{text: "done"}})
	app.SetTools(tools.NewRegistry(mutation))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "granted_then_restart", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	decided := runRecoveryTestTime()
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_granted", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_mutation", ToolName: mutation.spec.ID,
		State: core.ApprovalStateApproved, RequestedAt: decided, DecidedAt: &decided,
	}); err != nil {
		t.Fatal(err)
	}

	recoverRun(t, app, run.ID)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusWaitingApproval)
	if got := mutation.callCount(); got != 0 {
		t.Fatalf("the old grant re-ran the call %d times", got)
	}
	pending, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if _, err := app.ResolveApproval(context.Background(), pending[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if got := mutation.callCount(); got != 1 {
		t.Fatalf("executions after the retry grant = %d, want 1", got)
	}
}

func TestRecoverDeniedToolReturnsTheDenialToTheModel(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app := core.New(core.Config{Store: sqliteStore, RunStarter: starter})
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	var saw string
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		saw = toolResultContent(request, "tool_mutation")
		return providers.Response{Text: "Kept the old config."}, nil
	})})
	app.SetTools(tools.NewRegistry(mutation))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "denied_before_restart", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	decided := runRecoveryTestTime()
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_denied", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_mutation", ToolName: mutation.spec.ID,
		State: core.ApprovalStateRejected, Reason: "keep the old config", RequestedAt: decided, DecidedAt: &decided,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("recovered run schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, run.SessionID, "tool_mutation", 1)
	if saw != "User denied: keep the old config" || mutation.callCount() != 0 {
		t.Fatalf("model read %q, mutations = %d", saw, mutation.callCount())
	}
}

func TestRecoveryLeavesDeferredCallsToTheResumedRun(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app := core.New(core.Config{Store: sqliteStore, RunStarter: starter})
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = []string{toolResultContent(request, "tool_mutation"), toolResultContent(request, "tool_inspect")}
		return providers.Response{Text: "Done."}, nil
	})})
	app.SetTools(tools.NewRegistry(mutation, inspect))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "deferred", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	deferred := agent.ToolCallMessage("tool_inspect", run.SessionID, run.ID, inspect.spec.ID, []byte(`{}`), false, run.StartedAt.Add(2*time.Second))
	deferred.Parts[0].ToolCall.Deferred = true
	saveRunRecoveryTestMessage(t, sqliteStore, deferred)
	decided := runRecoveryTestTime()
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_mutation", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_mutation", ToolName: mutation.spec.ID,
		State: core.ApprovalStateRejected, RequestedAt: decided, DecidedAt: &decided,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if inspect.callCount() != 0 {
		t.Fatal("recovery replayed a deferred call")
	}
	if approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending); err != nil || len(approvals) != 0 {
		t.Fatalf("recovery approvals = %+v err = %v", approvals, err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "User denied.|Not run: an earlier call in this batch was denied (mutate_state)." || inspect.callCount() != 0 || mutation.callCount() != 0 {
		t.Fatalf("model read %q, inspect = %d, mutations = %d", results, inspect.callCount(), mutation.callCount())
	}
}

func TestRecoveryAnswersEveryCallOfABatchInFlight(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app := core.New(core.Config{Store: sqliteStore, RunStarter: starter})
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = toolResults(request)
		return providers.Response{Text: "Done."}, nil
	})})
	app.SetTools(tools.NewRegistry(mutation, inspect))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "batch", core.RunStatusRunning, false)
	at := run.StartedAt.Add(time.Second)
	saveRunRecoveryTestMessage(t, sqliteStore, agent.ToolCallMessage("tool_done", run.SessionID, run.ID, inspect.spec.ID, []byte(`{}`), true, at))
	done, err := agent.ToolResultMessage("result_done", run.SessionID, run.ID, "tool_done", inspect.spec.ID, tools.Result{Content: "done before the crash"}, at)
	if err != nil {
		t.Fatal(err)
	}
	saveRunRecoveryTestMessage(t, sqliteStore, done)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_asked", inspect.spec.ID)
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_asked", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_asked", ToolName: inspect.spec.ID,
		State: core.ApprovalStatePending, RequestedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
	saveInterruptedToolCall(t, sqliteStore, run, "tool_read", inspect.spec.ID)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_write", mutation.spec.ID)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_write_too", mutation.spec.ID)
	deferred := agent.ToolCallMessage("tool_later", run.SessionID, run.ID, inspect.spec.ID, []byte(`{}`), false, at)
	deferred.Parts[0].ToolCall.Deferred = true
	saveRunRecoveryTestMessage(t, sqliteStore, deferred)

	recoverRun(t, app, run.ID)

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusWaitingApproval)
	if inspect.callCount() != 0 || mutation.callCount() != 0 {
		t.Fatalf("after recovery: inspections = %d, mutations = %d, want none before the run resumes", inspect.callCount(), mutation.callCount())
	}
	approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 3 {
		t.Fatalf("pending approvals = %+v err = %v, want the asked call's and one retry per mutation in flight", approvals, err)
	}
	for _, approval := range approvals {
		if _, err := app.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if inspect.callCount() != 3 || mutation.callCount() != 2 {
		t.Fatalf("inspections = %d, mutations = %d, want the asked read, both deferred reads and both writes once", inspect.callCount(), mutation.callCount())
	}
	want := "tool_done=done before the crash|tool_asked=recovered tool result|tool_read=recovered tool result|tool_write=recovered tool result|tool_write_too=recovered tool result|tool_later=recovered tool result"
	if strings.Join(results, "|") != want {
		t.Fatalf("model read %q", results)
	}
	for _, id := range []string{"tool_done", "tool_asked", "tool_read", "tool_write", "tool_write_too", "tool_later"} {
		assertToolResultCount(t, sqliteStore, run.SessionID, id, 1)
	}
}

func TestRecoveryDefersAgainTheCallsTheBatchCheckpointNamesNotStarted(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	app := core.New(core.Config{Store: sqliteStore, RunStarter: &recordingRunStarter{}})
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.SetSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = toolResults(request)
		return providers.Response{Text: "Done."}, nil
	})})
	app.SetTools(tools.NewRegistry(mutation, inspect))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "batch_checkpoint", core.RunStatusRunning, false)
	// The barrier returned and the call behind it lost its deferred mark, but the
	// daemon stopped before the next checkpoint started it.
	saveInterruptedToolCall(t, sqliteStore, run, "tool_write", mutation.spec.ID)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_after", inspect.spec.ID)
	if err := sqliteStore.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, UpdatedAt: runRecoveryTestTime(),
		Batch: &agent.ToolBatch{CallIDs: []string{"tool_write", "tool_after"}, DeferredIDs: []string{"tool_after"}},
	}); err != nil {
		t.Fatal(err)
	}

	recoverRun(t, app, run.ID)

	if inspect.callCount() != 0 {
		t.Fatal("recovery replayed a call that never started")
	}
	approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 || approvals[0].ToolCallRef != "tool_write" {
		t.Fatalf("pending approvals = %+v err = %v, want a retry of the write only", approvals, err)
	}
	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "tool_write=recovered tool result|tool_after=recovered tool result" || inspect.callCount() != 1 || mutation.callCount() != 1 {
		t.Fatalf("model read %q, inspections = %d, mutations = %d", results, inspect.callCount(), mutation.callCount())
	}
}
