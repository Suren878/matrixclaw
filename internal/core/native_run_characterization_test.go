package core_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/orchestration"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type funcTool struct {
	spec tools.Spec
	fn   func(context.Context, tools.Call) (tools.Result, error)
}

func (t funcTool) Spec() tools.Spec { return t.spec }
func (t funcTool) Execute(ctx context.Context, call tools.Call) (tools.Result, error) {
	return t.fn(ctx, call)
}

type windowLLMs struct {
	recoveryLLMs
	window int
}

func (w windowLLMs) ContextWindowTokens(string, string) (int, bool) { return w.window, true }

func drainEvents(events <-chan core.Event) []core.Event {
	var out []core.Event
	for {
		select {
		case event := <-events:
			out = append(out, event)
		default:
			return out
		}
	}
}

func describeEvent(event core.Event) string {
	switch payload := event.Payload.(type) {
	case transcript.Message:
		kinds := make([]string, 0, len(payload.Parts))
		for _, part := range payload.Parts {
			kind := string(part.Kind)
			if part.ToolCall != nil && part.ToolCall.Finished {
				kind += "(done)"
			}
			kinds = append(kinds, kind)
		}
		return fmt.Sprintf("%s %s/%s", event.Type, payload.Role, strings.Join(kinds, ","))
	case core.ToolUpdate:
		return fmt.Sprintf("%s %s", event.Type, payload.State)
	case core.Run:
		return fmt.Sprintf("%s %s", event.Type, payload.Status)
	default:
		return string(event.Type)
	}
}

func sessionMessages(t *testing.T, db *store.SQLiteStore, sessionID string) []transcript.Message {
	t.Helper()
	messages, err := db.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestNativeToolRoundPublishesEventsInOrder(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	now := runRecoveryTestTime()
	app.WithClock(func() time.Time { return now })
	app.WithTools(tools.NewRegistry(&recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}))
	usage := providers.Usage{PromptTokens: 3, OutputTokens: 1}
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			if err := providers.StreamText(ctx, "Checking"); err != nil {
				return providers.Response{}, err
			}
			return providers.Response{Text: "Checking.", Usage: usage, ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		if err := providers.StreamText(ctx, "Done"); err != nil {
			return providers.Response{}, err
		}
		return providers.Response{Text: "Done.", Usage: usage}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "events", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	var trace []string
	for _, event := range drainEvents(events) {
		switch event.Type {
		case core.EventRunUpdated, core.EventMessageCreated, core.EventMessageUpdated, core.EventToolUpdated:
			trace = append(trace, describeEvent(event))
		}
	}
	want := []string{
		"run.updated running",
		"message.created system/text",
		"message.created assistant/text",
		"message.updated assistant/text,finish",
		"message.created assistant/tool_call",
		"tool.updated requested",
		"message.updated assistant/tool_call(done)",
		"message.created tool/tool_result",
		"tool.updated completed",
		"message.created assistant/text",
		"message.updated assistant/text,finish",
		"run.updated completed",
	}
	if got := strings.Join(trace, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("event trace:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

func TestNativeRunCheckpointsModelAndToolPhases(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var run core.Run
	var seen []string
	record := func(label string) {
		checkpoint, err := db.GetRunCheckpoint(context.Background(), run.ID)
		if err != nil {
			seen = append(seen, label+":none")
			return
		}
		seen = append(seen, fmt.Sprintf("%s:%s:%s:%s", label, checkpoint.Phase, checkpoint.ToolCallID, checkpoint.ToolName))
	}
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		record("tool")
		return tools.Result{Content: "inspected"}, nil
	}}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		record(fmt.Sprintf("model%d", calls))
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	_, run = saveCrashRecoveryRun(t, db, "checkpoints", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{"model1:model::", "tool:tool:call-1:inspect_state", "model2:model::"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("checkpoints = %v, want %v", seen, want)
	}
	waitForRecoveryCheckpointGone(t, db, run.ID)
}

func approvalTools(mutations *int) (funcTool, *recoveryTool) {
	mutate := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if !call.Approved {
			return tools.Result{Approval: &tools.ApprovalRequest{ToolID: "mutate_state", ToolCallID: call.ToolCallID, Action: "write_state", Description: "write the state"}}, nil
		}
		*mutations++
		return tools.Result{Content: "mutated"}, nil
	}}
	return mutate, &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
}

func TestNativeRunHoldsLaterCallsBehindAnApprovalBarrier(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	calls := 0
	var resumedWithBothResults bool
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)},
				{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)},
			}}, nil
		}
		resumedWithBothResults = toolResultContent(request, "call-mutate") == "mutated" && toolResultContent(request, "call-inspect") == "recovered tool result"
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "approval", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	assertToolResultCount(t, db, session.ID, "call-mutate", 0)
	assertToolResultCount(t, db, session.ID, "call-inspect", 0)
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].ToolCallRef != "call-mutate" || approvals[0].Action != "write_state" {
		t.Fatalf("pending approvals = %#v", approvals)
	}
	if calls != 1 || mutations != 0 || inspect.callCount() != 0 {
		t.Fatalf("before grant: model=%d mutations=%d inspect=%d", calls, mutations, inspect.callCount())
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, db, session.ID, "call-inspect", 1)
	if calls != 2 || mutations != 1 || inspect.callCount() != 1 || !resumedWithBothResults {
		t.Fatalf("after grant: model=%d mutations=%d inspect=%d both results=%v", calls, mutations, inspect.callCount(), resumedWithBothResults)
	}
}

func TestDeniedApprovalReturnsTheReasonToTheModel(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	calls := 0
	var denial string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		}
		denial = toolResultContent(request, "call-mutate")
		return providers.Response{Text: "Leaving the state alone."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "denied", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %#v, err = %v", approvals, err)
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Reason: "the state is shared"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if denial != "User denied: the state is shared" || mutations != 0 {
		t.Fatalf("model read %q, mutations = %d", denial, mutations)
	}
	stored, err := db.GetApproval(context.Background(), approvals[0].ID)
	if err != nil || stored.State != core.ApprovalStateRejected || stored.Reason != "the state is shared" {
		t.Fatalf("stored approval = %+v err = %v", stored, err)
	}
}

func askingReadTool(id string) funcTool {
	return funcTool{spec: recoveryToolSpec(id, tools.EffectReadOnly), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if !call.Approved {
			return tools.Result{Approval: &tools.ApprovalRequest{ToolID: id, ToolCallID: call.ToolCallID, Action: "read_secret"}}, nil
		}
		return tools.Result{Content: "secret " + id}, nil
	}}
}

func TestRunResumesOnceEveryApprovalIsDecided(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(askingReadTool("read_a"), askingReadTool("read_b")))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if len(results) == 0 && toolResultContent(request, "call-a") == "" {
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-a", Name: "read_a", Arguments: []byte(`{}`)},
				{ID: "call-b", Name: "read_b", Arguments: []byte(`{}`)},
			}}, nil
		}
		results = []string{toolResultContent(request, "call-a"), toolResultContent(request, "call-b")}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "two-approvals", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 2 {
		t.Fatalf("pending approvals = %#v, err = %v", approvals, err)
	}

	byCall := map[string]string{}
	for _, approval := range approvals {
		byCall[approval.ToolCallRef] = approval.ID
	}
	if _, err := app.ResolveApproval(context.Background(), byCall["call-a"], core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 0 {
		t.Fatalf("run scheduled %d times while an approval was still open", got)
	}
	if _, err := app.ResolveApproval(context.Background(), byCall["call-b"], core.ApprovalResolveRequest{Reason: "not that one"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "secret read_a|User denied: not that one" {
		t.Fatalf("model read %q", results)
	}
}

// parkingStore calls beforePark once, right before a run's waiting_approval
// status is stored: the engine has decided to park, the run is still active.
type parkingStore struct {
	*store.SQLiteStore
	beforePark func()
}

func (s *parkingStore) UpdateRun(ctx context.Context, run core.Run) error {
	if hook := s.beforePark; hook != nil && run.Status == core.RunStatusWaitingApproval {
		s.beforePark = nil
		hook()
	}
	return s.SQLiteStore.UpdateRun(ctx, run)
}

func TestApprovalDecidedWhileTheRunParksResumesIt(t *testing.T) {
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	parking := &parkingStore{SQLiteStore: db}
	app := core.New(parking)
	app.WithTools(tools.NewRegistry(askingReadTool("read_a")))
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	var result string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if result = toolResultContent(request, "call-a"); result == "" {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-a", Name: "read_a", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "decided-while-parking", core.RunStatusAccepted, false)
	parking.beforePark = func() {
		approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
		if err != nil || len(approvals) != 1 {
			t.Errorf("pending approvals = %+v err = %v", approvals, err)
			return
		}
		if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
			t.Errorf("ResolveApproval: %v", err)
		}
	}

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if result != "secret read_a" || starter.count(run.ID) != 1 {
		t.Fatalf("model read %q, schedules = %d", result, starter.count(run.ID))
	}
}

func TestDeniedCallOutsideARunGetsTheDenialAsItsResult(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate))
	session, _ := saveCrashRecoveryRun(t, db, "runless", core.RunStatusCompleted, false)
	pending, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "mutate_state", ToolCallID: "call-api", Args: []byte(`{}`)})
	if err != nil || pending.Approval == nil {
		t.Fatalf("ExecuteTool = %+v err = %v", pending, err)
	}

	if _, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Reason: "not now"}); err != nil {
		t.Fatal(err)
	}

	assertToolResultCount(t, db, session.ID, "call-api", 1)
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleTool && message.Content != "User denied: not now" {
			t.Fatalf("result = %q", message.Content)
		}
	}
	if mutations != 0 {
		t.Fatalf("mutations = %d", mutations)
	}
}

func TestDenialReasonIsCappedAt2000Characters(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, _ := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate))
	session, _ := saveCrashRecoveryRun(t, db, "long-reason", core.RunStatusCompleted, false)
	pending, err := app.ExecuteTool(context.Background(), core.ExecuteToolInput{SessionID: session.ID, ToolName: "mutate_state", ToolCallID: "call-api", Args: []byte(`{}`)})
	if err != nil || pending.Approval == nil {
		t.Fatalf("ExecuteTool = %+v err = %v", pending, err)
	}

	approval, err := app.ResolveApproval(context.Background(), pending.Approval.ID, core.ApprovalResolveRequest{Reason: strings.Repeat("я", 5000)})
	if err != nil {
		t.Fatal(err)
	}

	if runes := []rune(approval.Reason); len(runes) != 2000 || string(runes[1998:]) != "я…" {
		t.Fatalf("reason has %d runes, ends %q", len(runes), string(runes[len(runes)-2:]))
	}
}

func toolResultContent(request providers.Request, callID string) string {
	for _, message := range request.Messages {
		if message.Role == "tool" && message.ToolCallID == callID {
			return message.Content
		}
	}
	return ""
}

func TestSteerDuringToolIsAppendedToThatToolResult(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started := make(chan struct{})
	release := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		close(started)
		select {
		case <-release:
			return tools.Result{Content: "inspected"}, nil
		case <-ctx.Done():
			return tools.Result{}, ctx.Err()
		}
	}}))
	calls := 0
	var sawGuidance bool
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		for _, message := range request.Messages {
			sawGuidance = sawGuidance || (message.ToolCallID == "call-1" && strings.Contains(message.Content, "User guidance: focus on logs"))
		}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "steer", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, started, "tool start")

	result, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "focus on logs", BusyMode: core.BusyInputModeSteer})
	if err != nil || result.Status != core.AcceptRunStatusSteered {
		t.Fatalf("AcceptRun = %#v, %v", result, err)
	}
	close(release)
	if err := waitRecoveryError(t, done, "steered run"); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if !sawGuidance {
		t.Fatal("next model request lacks the steer guidance in the tool result")
	}
	found := false
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleTool && message.Content == "inspected\n\nUser guidance: focus on logs" {
			found = true
		}
	}
	if !found {
		t.Fatal("stored tool result does not carry the steer guidance")
	}
	pending, err := db.ListPendingSessionInputs(context.Background(), session.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending inputs = %#v, err = %v", pending, err)
	}
}

// cancelOnToolResultStore runs onToolResult and fails the write of a tool result.
type cancelOnToolResultStore struct {
	*store.SQLiteStore
	onToolResult func()
}

func (s cancelOnToolResultStore) AppendMessage(ctx context.Context, message transcript.Message) (int64, error) {
	if message.Role == transcript.MessageRoleTool {
		s.onToolResult()
		return 0, context.Canceled
	}
	return s.SQLiteStore.AppendMessage(ctx, message)
}

func TestSteerIsRequeuedWhenCancelStopsItsToolResultWrite(t *testing.T) {
	_, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	session, run := saveCrashRecoveryRun(t, db, "steer_cancel", core.RunStatusAccepted, false)
	var app *core.Core
	app = core.New(cancelOnToolResultStore{SQLiteStore: db, onToolResult: func() {
		if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
			t.Error(err)
		}
	}})
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	started := make(chan struct{})
	release := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		close(started)
		<-release
		return tools.Result{Content: "inspected"}, nil
	}}))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, started, "tool start")

	result, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "focus on logs", BusyMode: core.BusyInputModeSteer})
	if err != nil || result.Status != core.AcceptRunStatusSteered {
		t.Fatalf("AcceptRun = %#v, %v", result, err)
	}
	close(release)
	_ = waitRecoveryError(t, done, "canceled run")

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	starter.mu.Lock()
	scheduled := append([]string(nil), starter.ids...)
	starter.mu.Unlock()
	if len(scheduled) != 1 || scheduled[0] == run.ID {
		t.Fatalf("scheduled runs = %v, want one new run for the steer", scheduled)
	}
	next, err := db.GetRun(context.Background(), scheduled[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.ID == next.UserMessageID && message.Content == "focus on logs" {
			return
		}
	}
	t.Fatal("the new run does not carry the steer text")
}

func TestCancelDuringToolStopsRunWithoutAnotherModelCall(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
		close(started)
		<-ctx.Done()
		return tools.Result{}, ctx.Err()
	}}))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-1", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-tool", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, started, "tool start")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled run"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	if calls != 1 {
		t.Fatalf("model calls = %d, want 1", calls)
	}
	sealed := false
	for _, message := range sessionMessages(t, db, session.ID) {
		sealed = sealed || (message.RunID == run.ID && message.Role == transcript.MessageRoleAssistant && messageHasFinishPart(message, "canceled"))
	}
	if !sealed {
		t.Fatal("tool-turn assistant message was not sealed as canceled")
	}
	if _, err := db.GetRunCheckpoint(context.Background(), run.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("canceled run checkpoint error = %v, want ErrNotFound", err)
	}
}

func saveNativeRunWithHistory(t *testing.T, db *store.SQLiteStore, suffix string, history ...transcript.Message) (core.Session, core.Run) {
	t.Helper()
	ctx := context.Background()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: "session_" + suffix, Title: suffix, Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw,
		ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeDefault,
		Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	for i, message := range history {
		message.SessionID = session.ID
		message.CreatedAt = now.Add(time.Duration(i-len(history)) * time.Minute)
		message.UpdatedAt = message.CreatedAt
		saveRunRecoveryTestMessage(t, db, message)
	}
	run := core.Run{ID: "run_" + suffix, SessionID: session.ID, UserMessageID: "msg_user_" + suffix, Status: core.RunStatusAccepted, StartedAt: now, UpdatedAt: now}
	user := transcript.Message{
		ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser,
		Content: "original task " + suffix, Parts: transcript.NormalizeMessageParts("original task "+suffix, nil),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.AcceptMessage(ctx, user, run); err != nil {
		t.Fatal(err)
	}
	return session, run
}

func countBoundaries(t *testing.T, db *store.SQLiteStore, sessionID string) int {
	t.Helper()
	count := 0
	for _, message := range sessionMessages(t, db, sessionID) {
		if message.Compaction != nil && !message.Compaction.Cleared {
			count++
		}
	}
	return count
}

func TestNativeRunCompactsLargeHistoryBeforeTheModelCall(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	big := strings.Repeat("x", 330_000)
	var summaryRequests, mainRequests int
	var mainPrompt, mainFirst string
	var leaked bool
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			summaryRequests++
			return providers.Response{Text: "SUMMARY"}, nil
		}
		mainRequests++
		mainPrompt, mainFirst = request.SystemPrompt, request.Messages[0].Content
		for _, message := range request.Messages {
			leaked = leaked || strings.Contains(message.Content, big[:1000])
		}
		return providers.Response{Text: "Done."}, nil
	})}})
	session, run := saveNativeRunWithHistory(t, db, "compact", transcript.Message{
		ID: "msg_old_user", Role: transcript.MessageRoleUser, Content: big, Parts: transcript.NormalizeMessageParts(big, nil),
	})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if summaryRequests != 1 || mainRequests != 1 {
		t.Fatalf("summary=%d main=%d, want 1/1", summaryRequests, mainRequests)
	}
	if !strings.Contains(mainFirst, "SUMMARY") || strings.Contains(mainPrompt, "SUMMARY") || leaked {
		t.Fatalf("main request: first message %.80q, summary in system prompt=%v, old history leaked=%v", mainFirst, strings.Contains(mainPrompt, "SUMMARY"), leaked)
	}
	if got := countBoundaries(t, db, session.ID); got != 1 {
		t.Fatalf("boundaries = %d, want 1", got)
	}
}

func TestContextLengthErrorForcesCompactionAndRetriesOnce(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var summaryRequests, mainRequests int
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			summaryRequests++
			return providers.Response{Text: "SUMMARY"}, nil
		}
		mainRequests++
		if mainRequests == 1 {
			return providers.Response{}, errors.New("provider: context_length_exceeded")
		}
		return providers.Response{Text: "Recovered."}, nil
	})}})
	old := strings.Repeat("y", 200_000)
	session, run := saveNativeRunWithHistory(t, db, "overflow", transcript.Message{
		ID: "msg_old_user", Role: transcript.MessageRoleUser, Content: old, Parts: transcript.NormalizeMessageParts(old, nil),
	})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if summaryRequests != 1 || mainRequests != 2 {
		t.Fatalf("summary=%d main=%d, want 1/2", summaryRequests, mainRequests)
	}
	if got := countBoundaries(t, db, session.ID); got != 1 {
		t.Fatalf("boundaries = %d, want 1", got)
	}
}

func TestNativeRunSeesOnlyTheNewestBoundaryAndLaterMessages(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	var seen providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		seen = request
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveNativeRunWithHistory(t, db, "window", transcript.Message{
		ID: "msg_forgotten", Role: transcript.MessageRoleUser, Content: "forgotten detail", Parts: transcript.NormalizeMessageParts("forgotten detail", nil),
	})
	forgotten, err := db.GetMessage(context.Background(), "msg_forgotten")
	if err != nil {
		t.Fatal(err)
	}
	saveRunRecoveryTestMessage(t, db, transcript.Message{
		ID: "msg_boundary", SessionID: session.ID, Role: transcript.MessageRoleSystem, Content: "Context compacted.",
		Compaction: &transcript.Compaction{Summary: "EARLIER WORK", CoversThroughSeq: forgotten.Seq},
		CreatedAt:  runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime(),
	})

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	if len(seen.Messages) != 3 || !strings.Contains(seen.Messages[0].Content, "EARLIER WORK") || seen.Messages[1].Content != "original task window" || !strings.HasPrefix(seen.Messages[2].Content, "Context update") {
		t.Fatalf("request messages = %+v", seen.Messages)
	}
}

func TestBlockingSubagentReturnsChildSummaryToParent(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	parentCalls := 0
	var delegateResult string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child found 3 files"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		for _, message := range request.Messages {
			if message.ToolCallID == "call-delegate" {
				delegateResult = message.Content
			}
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if delegateResult != "child found 3 files" {
		t.Fatalf("delegate result = %q", delegateResult)
	}
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.SubagentTaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q", task.Status, task.Summary)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
}

func TestSubagentSummaryJoinsAReplyCutByTheOutputLimit(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	parentCalls, childCalls := 0, 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			childCalls++
			if childCalls == 1 {
				return providers.Response{Text: "child fo", StopReason: providers.StopMaxTokens}, nil
			}
			return providers.Response{Text: "und 3 files"}, nil
		}
		parentCalls++
		if parentCalls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate-cut", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.SubagentTaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q, want the cut reply joined with its continuation", task.Status, task.Summary)
	}
}

type asyncSubagentScenario struct {
	db      *store.SQLiteStore
	session core.Session
	run     core.Run
	events  []core.Event
}

func runAsyncSubagentScenario(t *testing.T) asyncSubagentScenario {
	t.Helper()
	app, db, cleanup := newCrashRecoveryCore(t)
	t.Cleanup(cleanup)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	app.WithRunStarter(orchestration.NewStub(app))
	var mu sync.Mutex
	spawned := false
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return providers.Response{Text: "child async result"}, nil
		}
		for _, message := range request.Messages {
			if message.Role == "user" && strings.HasPrefix(message.Content, "Subagent ") && strings.Contains(message.Content, "completed.") {
				return providers.Response{Text: "Synthesized."}, nil
			}
		}
		if !spawned {
			spawned = true
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "spawn_subagent", Arguments: []byte(`{"name":"Scanner","goal":"scan the tree","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Spawned."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "async", core.RunStatusAccepted, false)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := app.SubscribeEvents(ctx, session.ID)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)

	deadline := time.Now().Add(10 * time.Second)
	for {
		followUp := ""
		for _, message := range sessionMessages(t, db, session.ID) {
			if message.Role == transcript.MessageRoleUser && strings.HasPrefix(message.Content, "Subagent ") && strings.Contains(message.Content, "completed.") {
				followUp = message.RunID
			}
		}
		if followUp != "" {
			if got, err := db.GetRun(context.Background(), followUp); err == nil && got.Status == core.RunStatusCompleted {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("subagent completion follow-up run did not complete")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return asyncSubagentScenario{db: db, session: session, run: run, events: drainEvents(events)}
}

func TestAsyncSubagentCompletionStartsParentFollowUpRun(t *testing.T) {
	scenario := runAsyncSubagentScenario(t)

	task, err := scenario.db.GetSubagentTaskByParentToolCall(context.Background(), scenario.session.ID, scenario.run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != core.SubagentTaskStatusCompleted || task.CompletionDeliveredAt == nil {
		t.Fatalf("task = %s delivered=%v", task.Status, task.CompletionDeliveredAt)
	}
}

func TestContextOverflowWithNothingToSummariseFailsAsContextExhausted(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{}, errors.New("provider: context_length_exceeded")
	})})
	_, run := saveCrashRecoveryRun(t, db, "exhausted", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	stored, getErr := db.GetRun(context.Background(), run.ID)
	if !errors.Is(err, agent.ErrContextExhausted) || getErr != nil || stored.Status != core.RunStatusFailed || stored.StopReason != agent.StopContextExhausted {
		t.Fatalf("ExecuteRun err = %v, run = %+v (%v)", err, stored, getErr)
	}
}
