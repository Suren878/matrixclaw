package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/orchestration"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type recoveryRuntime struct {
	mu       sync.Mutex
	requests []providers.Request
	text     string
}

type interruptibleRecoveryRuntime struct {
	started chan struct{}
	once    sync.Once
}

func (r *interruptibleRecoveryRuntime) Generate(ctx context.Context, _ providers.Request) (providers.Response, error) {
	if err := providers.StreamText(ctx, "partial before graceful restart"); err != nil {
		return providers.Response{}, err
	}
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return providers.Response{}, ctx.Err()
}

func (r *recoveryRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.mu.Unlock()
	return providers.Response{Text: r.text, Provider: "recovery-test", Model: "test-model"}, nil
}

func (r *recoveryRuntime) lastRequest() providers.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return providers.Request{}
	}
	return r.requests[len(r.requests)-1]
}

func (r *recoveryRuntime) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

type recoveryLLMs struct {
	runtime providers.Runtime
}

func (r recoveryLLMs) ActiveSelection() (string, string) { return "recovery-test", "test-model" }
func (r recoveryLLMs) Providers() []core.SessionProviderOption {
	return []core.SessionProviderOption{{ID: "recovery-test", Configured: true, DefaultModel: "test-model"}}
}
func (r recoveryLLMs) Normalize(providerID string, modelID string) (core.SessionProviderOption, string, error) {
	return core.SessionProviderOption{ID: firstRecoveryValue(providerID, "recovery-test"), Configured: true, DefaultModel: "test-model"}, firstRecoveryValue(modelID, "test-model"), nil
}
func (r recoveryLLMs) Models(context.Context, string) ([]string, error) {
	return []string{"test-model"}, nil
}
func (r recoveryLLMs) Resolve(context.Context, string, string) (providers.Runtime, core.SessionProviderOption, string, error) {
	return r.runtime, core.SessionProviderOption{ID: "recovery-test", Configured: true, DefaultModel: "test-model"}, "test-model", nil
}

func firstRecoveryValue(value string, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

type recordingRunStarter struct {
	mu  sync.Mutex
	ids []string
}

func (s *recordingRunStarter) StartRun(_ context.Context, runID string) error {
	s.mu.Lock()
	s.ids = append(s.ids, runID)
	s.mu.Unlock()
	return nil
}

func (s *recordingRunStarter) count(runID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, id := range s.ids {
		if id == runID {
			count++
		}
	}
	return count
}

type recoveryTool struct {
	spec  tools.Spec
	mu    sync.Mutex
	calls int
}

func (t *recoveryTool) Spec() tools.Spec { return t.spec }
func (t *recoveryTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return tools.Result{Content: "recovered tool result", Status: tools.ResultStatusSuccess}, nil
}
func (t *recoveryTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func recoveryToolSpec(id string, effect tools.Effect) tools.Spec {
	risk := tools.RiskSafe
	approval := tools.ApprovalNever
	if effect == tools.EffectMutation {
		risk = tools.RiskApproval
		approval = tools.ApprovalOnRequest
	}
	return tools.Spec{
		ID: id, Name: id, Description: "recovery test tool", Risk: risk, Effect: effect,
		ApprovalMode: approval, Namespace: "test.recovery", Category: tools.CategoryFilesystem,
		Profiles: []tools.Profile{tools.ProfileCoding}, OutputKind: tools.OutputText,
		InputJSONSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func TestRecoverRunningGenerationSkipsPartialAndCompletes(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "recovered answer"}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(orchestration.NewStub(app))

	session, run := saveCrashRecoveryRun(t, sqliteStore, "generation", core.RunStatusRunning, false)
	partial := transcript.Message{
		ID: "msg_partial_generation", SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleAssistant, Content: "partial answer that must not enter retry context",
		Parts:     transcript.NormalizeMessageParts("partial answer that must not enter retry context", nil),
		CreatedAt: run.StartedAt.Add(time.Second), UpdatedAt: run.StartedAt.Add(time.Second),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, partial)
	if err := sqliteStore.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, Phase: core.RunCheckpointPhaseModel, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)

	request := runtime.lastRequest()
	notice := false
	for _, message := range request.Messages {
		notice = notice || strings.Contains(message.Content, "daemon restarted")
		if strings.Contains(message.Content, "partial answer") {
			t.Fatalf("interrupted partial assistant leaked into provider retry: %#v", request.Messages)
		}
	}
	if !notice || strings.Contains(request.SystemPrompt, "daemon restarted") {
		t.Fatalf("recovery notice is not a context message: system prompt %q", request.SystemPrompt)
	}
	messages, err := sqliteStore.ListMessages(context.Background(), session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !messageHasRecoveryFinish(messages, partial.ID) {
		t.Fatal("partial assistant message was not marked as interrupted by restart")
	}
	if !hasAssistantContent(messages, run.ID, "recovered answer") {
		t.Fatal("recovered assistant answer was not persisted")
	}
	waitForRecoveryCheckpointGone(t, sqliteStore, run.ID)
}

func TestGracefulExecutorStopPreservesAndRecoversNativeGeneration(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	interruptedRuntime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: interruptedRuntime})

	_, run := saveCrashRecoveryRun(t, sqliteStore, "graceful_native", core.RunStatusAccepted, false)
	executionCtx, cancelExecution := context.WithCancel(context.Background())
	executionDone := make(chan error, 1)
	go func() { executionDone <- app.ExecuteRun(executionCtx, run.ID) }()
	waitRecoverySignal(t, interruptedRuntime.started, "native generation start")
	cancelExecution()
	if err := waitRecoveryError(t, executionDone, "native executor shutdown"); err != nil {
		t.Fatalf("ExecuteRun after shutdown: %v", err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusRunning)
	checkpoint, err := sqliteStore.GetRunCheckpoint(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRunCheckpoint: %v", err)
	}
	if checkpoint.Phase != core.RunCheckpointPhaseRecovering {
		t.Fatalf("checkpoint phase = %q, want recovering", checkpoint.Phase)
	}
	messages, err := sqliteStore.ListMessages(context.Background(), run.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRecoveryFinishedAssistant(messages, run.ID, "partial before graceful restart") {
		t.Fatalf("gracefully interrupted native partial was not sealed as a stable segment: %#v", messages)
	}

	recoveredRuntime := &recoveryRuntime{text: "native resumed after restart"}
	restarted := core.New(sqliteStore).
		WithSessionLLMs(recoveryLLMs{runtime: recoveredRuntime})
	// The stub must execute against the restarted Core, not the stopped one.
	restarted.WithRunStarter(orchestration.NewStub(restarted))
	if err := restarted.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns after graceful stop: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	request := recoveredRuntime.lastRequest()
	for _, message := range request.Messages {
		if strings.Contains(message.Content, "partial before graceful restart") {
			t.Fatalf("interrupted native partial leaked into resumed provider history: %#v", request.Messages)
		}
	}
}

func TestUserCancellationIsNotConvertedIntoRestartRecovery(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	interruptedRuntime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: interruptedRuntime})

	_, run := saveCrashRecoveryRun(t, sqliteStore, "user_cancel", core.RunStatusAccepted, false)
	executionDone := make(chan error, 1)
	go func() { executionDone <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, interruptedRuntime.started, "cancelable native generation")
	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if err := waitRecoveryError(t, executionDone, "canceled native executor"); err != nil {
		t.Fatalf("ExecuteRun after user cancellation: %v", err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCanceled)
	if _, err := sqliteStore.GetRunCheckpoint(context.Background(), run.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("canceled run checkpoint error = %v, want ErrNotFound", err)
	}
	messages, err := sqliteStore.ListMessages(context.Background(), run.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.RunID == run.ID && messageHasFinishPart(message, "daemon_restart") {
			t.Fatal("user-canceled run was incorrectly marked for daemon restart recovery")
		}
	}
}

func TestStartupRecoveryAndPersistedWorkflowRaceExecutesRunOnce(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "single recovered answer"}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(orchestration.NewStub(app))

	_, run := saveCrashRecoveryRun(t, sqliteStore, "startup_race", core.RunStatusRunning, false)
	if err := sqliteStore.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, Phase: core.RunCheckpointPhaseModel, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	go func() {
		<-start
		errorsSeen <- app.ExecuteRun(context.Background(), run.ID)
	}()
	go func() {
		<-start
		errorsSeen <- app.RecoverActiveRuns(context.Background())
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := waitRecoveryError(t, errorsSeen, "startup recovery race"); err != nil {
			t.Fatalf("startup recovery race: %v", err)
		}
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	if got := runtime.requestCount(); got != 1 {
		t.Fatalf("provider generations = %d, want exactly 1", got)
	}
}

func TestRecoverReadOnlyToolReplaysAutomaticallyOnce(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	readTool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(readTool))

	_, run := saveCrashRecoveryRun(t, sqliteStore, "readonly", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_readonly", readTool.spec.ID)
	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	if got := readTool.callCount(); got != 1 {
		t.Fatalf("read-only tool executions = %d, want 1", got)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("recovered run schedules = %d, want 1", got)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusAccepted)
	assertToolResultCount(t, sqliteStore, run.SessionID, "tool_readonly", 1)
}

func TestRecoverMutatingToolRequiresFreshApprovalBeforeSingleReplay(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "finished after approved recovery"}
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation))

	_, run := saveCrashRecoveryRun(t, sqliteStore, "mutation", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusWaitingApproval)
	if got := mutation.callCount(); got != 0 {
		t.Fatalf("mutating tool executed without recovery approval: %d", got)
	}
	approvals, err := sqliteStore.ListApprovals(context.Background(), run.SessionID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].Action != "retry_after_daemon_restart" || approvals[0].Suggestion == nil || approvals[0].Suggestion.String() != "mutate_state" {
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

func TestRecoverDeniedToolReturnsTheDenialToTheModel(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	var saw string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		saw = toolResultContent(request, "tool_mutation")
		return providers.Response{Text: "Kept the old config."}, nil
	})})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation))
	_, run := saveCrashRecoveryRun(t, sqliteStore, "denied_before_restart", core.RunStatusRunning, false)
	saveInterruptedToolCall(t, sqliteStore, run, "tool_mutation", mutation.spec.ID)
	decided := runRecoveryTestTime()
	if err := sqliteStore.CreateApproval(context.Background(), core.Approval{
		ID: "approval_denied", SessionID: run.SessionID, RunID: run.ID, ToolCallRef: "tool_mutation", ToolName: mutation.spec.ID,
		State: core.ApprovalStateRejected, Reason: "keep the old config", RequestedAt: decided, DecidedAt: &decided,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
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
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = []string{toolResultContent(request, "tool_mutation"), toolResultContent(request, "tool_inspect")}
		return providers.Response{Text: "Done."}, nil
	})})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation, inspect))
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

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
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
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	mutation := &recoveryTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation)}
	inspect := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		results = toolResults(request)
		return providers.Response{Text: "Done."}, nil
	})})
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(mutation, inspect))
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

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusWaitingApproval)
	if inspect.callCount() != 1 || mutation.callCount() != 0 {
		t.Fatalf("after recovery: inspections = %d (want the read in flight replayed), mutations = %d", inspect.callCount(), mutation.callCount())
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
		t.Fatalf("inspections = %d, mutations = %d, want the asked read, the deferred read and both writes once", inspect.callCount(), mutation.callCount())
	}
	want := "tool_done=done before the crash|tool_asked=recovered tool result|tool_read=recovered tool result|tool_write=recovered tool result|tool_write_too=recovered tool result|tool_later=recovered tool result"
	if strings.Join(results, "|") != want {
		t.Fatalf("model read %q", results)
	}
	for _, id := range []string{"tool_done", "tool_asked", "tool_read", "tool_write", "tool_write_too", "tool_later"} {
		assertToolResultCount(t, sqliteStore, run.SessionID, id, 1)
	}
}

func TestRecoverBlockingSubagentCompletesChildThenParentWithoutDuplicate(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "recovered completion"}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	app.WithRunStarter(orchestration.NewStub(app))
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))

	parentSession, parentRun := saveCrashRecoveryRun(t, sqliteStore, "parent", core.RunStatusRunning, false)
	childSession, childRun := saveCrashRecoveryRun(t, sqliteStore, "child", core.RunStatusRunning, true)
	parentRun.StartedAt = childRun.StartedAt.Add(-time.Minute)
	parentRun.UpdatedAt = parentRun.StartedAt
	if err := sqliteStore.UpdateRun(context.Background(), parentRun); err != nil {
		t.Fatal(err)
	}
	delegateArgs := `{"goal":"finish child work","runtime":"matrixclaw"}`
	saveInterruptedToolCallWithInput(t, sqliteStore, parentRun, "tool_delegate", "delegate_task", delegateArgs)
	now := runRecoveryTestTime().Add(3 * time.Second)
	if err := sqliteStore.CreateSubagentTask(context.Background(), core.SubagentTask{
		ID: "subagent_recovery", AgentName: "Neo", DisplayName: "Recovery child",
		Mode: core.SubagentTaskModeBlocking, Isolation: core.SubagentIsolationShared,
		ParentSessionID: parentSession.ID, ParentRunID: parentRun.ID, ParentToolCallID: "tool_delegate",
		ChildSessionID: childSession.ID, ChildRunID: childRun.ID, Runtime: "matrixclaw",
		Goal: "finish child work", Status: core.SubagentTaskStatusRunning,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, childRun.ID, core.RunStatusCompleted)
	waitForRecoveryRunStatus(t, sqliteStore, parentRun.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, parentSession.ID, "tool_delegate", 1)
	task, err := sqliteStore.GetSubagentTask(context.Background(), "subagent_recovery")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.SubagentTaskStatusCompleted {
		t.Fatalf("subagent task status = %q, want completed", task.Status)
	}
}

type recoveryExternalRuntime struct {
	mu    sync.Mutex
	input string
}

type interruptibleExternalRuntime struct {
	started    chan struct{}
	once       sync.Once
	mu         sync.Mutex
	interrupts int
}

func (r *interruptibleExternalRuntime) ID() string          { return "recovery-external" }
func (r *interruptibleExternalRuntime) DisplayName() string { return "Interruptible External" }
func (r *interruptibleExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}
func (r *interruptibleExternalRuntime) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{AgentID: r.ID(), ExternalThreadID: "thread-recovery"}, nil
}
func (r *interruptibleExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}
func (r *interruptibleExternalRuntime) Send(_ context.Context, _ externalagents.ExternalSession, _ externalagents.Input) (<-chan externalagents.Event, error) {
	events := make(chan externalagents.Event, 3)
	events <- externalagents.Event{Kind: externalagents.EventMessageDelta, Text: "external partial before restart", At: time.Now().UTC()}
	events <- externalagents.Event{Kind: externalagents.EventToolStarted, ItemID: "external_tool_in_flight", ToolName: "shell", ToolInput: "touch marker", At: time.Now().UTC()}
	r.once.Do(func() { close(r.started) })
	return events, nil
}
func (r *interruptibleExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	r.mu.Lock()
	r.interrupts++
	r.mu.Unlock()
	return nil
}
func (r *interruptibleExternalRuntime) Close() error { return nil }

func (r *recoveryExternalRuntime) ID() string          { return "recovery-external" }
func (r *recoveryExternalRuntime) DisplayName() string { return "Recovery External" }
func (r *recoveryExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}
func (r *recoveryExternalRuntime) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{AgentID: r.ID(), ExternalThreadID: "thread-recovery"}, nil
}
func (r *recoveryExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}
func (r *recoveryExternalRuntime) Send(_ context.Context, _ externalagents.ExternalSession, input externalagents.Input) (<-chan externalagents.Event, error) {
	r.mu.Lock()
	r.input = input.Text
	r.mu.Unlock()
	events := make(chan externalagents.Event, 2)
	events <- externalagents.Event{Kind: externalagents.EventMessageDelta, Text: "external recovered", At: time.Now().UTC()}
	events <- externalagents.Event{Kind: externalagents.EventTurnCompleted, At: time.Now().UTC()}
	close(events)
	return events, nil
}
func (r *recoveryExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}
func (r *recoveryExternalRuntime) Close() error { return nil }
func (r *recoveryExternalRuntime) receivedInput() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.input
}

func TestRecoverExternalAgentContinuesExistingSession(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryExternalRuntime{}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatal(err)
	}
	app.WithExternalAgents(registry, sqliteStore)
	app.WithRunStarter(orchestration.NewStub(app))

	session, run := saveCrashRecoveryRun(t, sqliteStore, "external", core.RunStatusRunning, false)
	session.Kind = core.SessionKindExternalAgent
	session.RuntimeID = core.SessionRuntimeExternalAgent
	session.ExternalAgentID = runtime.ID()
	if err := sqliteStore.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.SaveExternalAgentSession(context.Background(), externalagents.SessionAttachment{
		SessionID: session.ID, AgentID: runtime.ID(), ExternalThreadID: "thread-recovery",
		CreatedAt: run.StartedAt, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	partial := transcript.Message{
		ID: "msg_external_partial", SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleAssistant, Content: "external partial",
		Parts:     transcript.NormalizeMessageParts("external partial", nil),
		CreatedAt: run.StartedAt.Add(time.Second), UpdatedAt: run.StartedAt.Add(time.Second),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, partial)

	if err := app.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	input := runtime.receivedInput()
	if !strings.Contains(input, "Continue the existing task") || !strings.Contains(input, "Original task for reference") {
		t.Fatalf("external recovery input = %q", input)
	}
}

func TestGracefulExecutorStopRecoversExternalSessionWithoutNativeToolReplay(t *testing.T) {
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	interruptedRuntime := &interruptibleExternalRuntime{started: make(chan struct{})}
	registry, err := externalagents.NewRegistry(interruptedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	app.WithExternalAgents(registry, sqliteStore)

	session, run := saveCrashRecoveryRun(t, sqliteStore, "graceful_external", core.RunStatusAccepted, false)
	session.Kind = core.SessionKindExternalAgent
	session.RuntimeID = core.SessionRuntimeExternalAgent
	session.ExternalAgentID = interruptedRuntime.ID()
	if err := sqliteStore.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.SaveExternalAgentSession(context.Background(), externalagents.SessionAttachment{
		SessionID: session.ID, AgentID: interruptedRuntime.ID(), ExternalThreadID: "thread-recovery",
		CreatedAt: run.StartedAt, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	executionCtx, cancelExecution := context.WithCancel(context.Background())
	executionDone := make(chan error, 1)
	go func() { executionDone <- app.ExecuteRun(executionCtx, run.ID) }()
	waitRecoverySignal(t, interruptedRuntime.started, "external generation start")
	// Let Core consume both buffered deltas before stopping its executor.
	time.Sleep(20 * time.Millisecond)
	cancelExecution()
	if err := waitRecoveryError(t, executionDone, "external executor shutdown"); err != nil {
		t.Fatalf("external ExecuteRun after shutdown: %v", err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusRunning)

	recoveredRuntime := &recoveryExternalRuntime{}
	recoveredRegistry, err := externalagents.NewRegistry(recoveredRuntime)
	if err != nil {
		t.Fatal(err)
	}
	restarted := core.New(sqliteStore).WithExternalAgents(recoveredRegistry, sqliteStore)
	restarted.WithRunStarter(orchestration.NewStub(restarted))
	if err := restarted.RecoverActiveRuns(context.Background()); err != nil {
		t.Fatalf("RecoverActiveRuns external after graceful stop: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, session.ID, "external_tool_in_flight", 0)
	if !strings.Contains(recoveredRuntime.receivedInput(), "Continue the existing task") {
		t.Fatalf("external runtime did not receive a continuation prompt: %q", recoveredRuntime.receivedInput())
	}
}

func newCrashRecoveryCore(t *testing.T) (*core.Core, *store.SQLiteStore, func()) {
	t.Helper()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	return core.New(sqliteStore), sqliteStore, func() { _ = sqliteStore.Close() }
}

func saveCrashRecoveryRun(t *testing.T, sqliteStore *store.SQLiteStore, suffix string, status core.RunStatus, hidden bool) (core.Session, core.Run) {
	t.Helper()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: "session_" + suffix, Title: suffix, Kind: core.SessionKindAssistant,
		RuntimeID: core.SessionRuntimeMatrixClaw, Hidden: hidden,
		ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeDefault,
		Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if hidden {
		session.ParentSessionID = "session_parent"
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	run := core.Run{
		ID: "run_" + suffix, SessionID: session.ID, UserMessageID: "msg_user_" + suffix,
		Status: status, StartedAt: now, UpdatedAt: now,
	}
	user := transcript.Message{
		ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleUser, Content: "original task " + suffix,
		Parts:     transcript.NormalizeMessageParts("original task "+suffix, nil),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := sqliteStore.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
	return session, run
}

func saveInterruptedToolCall(t *testing.T, sqliteStore *store.SQLiteStore, run core.Run, id string, name string) {
	t.Helper()
	saveInterruptedToolCallWithInput(t, sqliteStore, run, id, name, `{}`)
}

func saveInterruptedToolCallWithInput(t *testing.T, sqliteStore *store.SQLiteStore, run core.Run, id string, name string, input string) {
	t.Helper()
	message := transcript.Message{
		ID: id, SessionID: run.SessionID, RunID: run.ID, Role: transcript.MessageRoleAssistant,
		Parts:     []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: name, Input: input}}},
		CreatedAt: run.StartedAt.Add(time.Second), UpdatedAt: run.StartedAt.Add(time.Second),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, message)
}

func waitForRecoveryRunStatus(t *testing.T, sqliteStore *store.SQLiteStore, runID string, want core.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := sqliteStore.GetRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertRecoveryRunStatus(t, sqliteStore, runID, want)
}

func waitForRecoveryCheckpointGone(t *testing.T, sqliteStore *store.SQLiteStore, runID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, err := sqliteStore.GetRunCheckpoint(context.Background(), runID)
		if errors.Is(err, core.ErrNotFound) {
			return
		}
		if err != nil {
			t.Fatalf("GetRunCheckpoint: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := sqliteStore.GetRunCheckpoint(context.Background(), runID)
	t.Fatalf("terminal run checkpoint error = %v, want ErrNotFound", err)
}

func assertRecoveryRunStatus(t *testing.T, sqliteStore *store.SQLiteStore, runID string, want core.RunStatus) {
	t.Helper()
	run, err := sqliteStore.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != want {
		t.Fatalf("run %s status = %q (%s), want %q", runID, run.Status, run.Error, want)
	}
}

func assertToolResultCount(t *testing.T, sqliteStore *store.SQLiteStore, sessionID string, toolCallID string, want int) {
	t.Helper()
	messages, err := sqliteStore.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == toolCallID {
				count++
			}
		}
	}
	if count != want {
		t.Fatalf("tool result %s count = %d, want %d", toolCallID, count, want)
	}
}

func messageHasRecoveryFinish(messages []transcript.Message, messageID string) bool {
	for _, message := range messages {
		if message.ID != messageID {
			continue
		}
		for _, part := range message.Parts {
			if part.Finish != nil && part.Finish.Reason == "daemon_restart" {
				return true
			}
		}
	}
	return false
}

func messageHasFinishPart(message transcript.Message, reason string) bool {
	for _, part := range message.Parts {
		if part.Finish != nil && part.Finish.Reason == reason {
			return true
		}
	}
	return false
}

func hasAssistantContent(messages []transcript.Message, runID string, content string) bool {
	for _, message := range messages {
		if message.RunID == runID && message.Role == transcript.MessageRoleAssistant && message.Content == content {
			return true
		}
	}
	return false
}

func hasRecoveryFinishedAssistant(messages []transcript.Message, runID string, content string) bool {
	for _, message := range messages {
		if message.RunID != runID || message.Role != transcript.MessageRoleAssistant || message.Content == "" || !strings.HasPrefix(content, message.Content) {
			continue
		}
		for _, part := range message.Parts {
			if part.Finish != nil && part.Finish.Reason == "daemon_restart" {
				return true
			}
		}
	}
	return false
}

func waitRecoverySignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitRecoveryError(t *testing.T, result <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return nil
	}
}
