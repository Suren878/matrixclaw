package core_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
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

func TestNativeRunFailsAfterThirtyTwoToolSteps(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	tool := &recoveryTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly)}
	app.WithTools(tools.NewRegistry(tool))
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: fmt.Sprintf("step-%d", calls), Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "step-cap", core.RunStatusAccepted, false)

	err := app.ExecuteRun(context.Background(), run.ID)

	if err == nil || err.Error() != "tool loop exceeded 32 steps" {
		t.Fatalf("ExecuteRun error = %v", err)
	}
	got, getErr := db.GetRun(context.Background(), run.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status != core.RunStatusFailed || got.Error != "tool loop exceeded 32 steps" {
		t.Fatalf("run = %s (%s)", got.Status, got.Error)
	}
	if calls != 32 || tool.callCount() != 32 {
		t.Fatalf("model calls=%d tool calls=%d, want 32/32", calls, tool.callCount())
	}
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

func TestNativeRunParksForApprovalAndResumesAfterGrant(t *testing.T) {
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
		results := map[string]string{}
		for _, message := range request.Messages {
			if message.Role == "tool" {
				results[message.ToolCallID] = message.Content
			}
		}
		resumedWithBothResults = results["call-mutate"] == "mutated" && results["call-inspect"] == "recovered tool result"
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "approval", core.RunStatusAccepted, false)

	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)
	assertToolResultCount(t, db, session.ID, "call-inspect", 1)
	assertToolResultCount(t, db, session.ID, "call-mutate", 0)
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != 1 || approvals[0].ToolCallRef != "call-mutate" || approvals[0].Action != "write_state" {
		t.Fatalf("pending approvals = %#v", approvals)
	}
	if calls != 1 || mutations != 0 || inspect.callCount() != 1 {
		t.Fatalf("before grant: model=%d mutations=%d inspect=%d", calls, mutations, inspect.callCount())
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("resume schedules = %d, want 1", got)
	}
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, db, session.ID, "call-mutate", 1)
	if calls != 2 || mutations != 1 || !resumedWithBothResults {
		t.Fatalf("after grant: model=%d mutations=%d both results=%v", calls, mutations, resumedWithBothResults)
	}
}

func TestNativeRunFailsWhenApprovalIsDenied(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	app.WithRunStarter(&recordingRunStarter{})
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "denied", core.RunStatusAccepted, false)
	if err := app.ExecuteRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	approvals, err := db.ListApprovals(context.Background(), session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %#v, err = %v", approvals, err)
	}

	if _, err := app.ResolveApproval(context.Background(), approvals[0].ID, false); err == nil || err.Error() != "approval denied" {
		t.Fatalf("ResolveApproval(deny) error = %v, want approval denied", err)
	}

	got, err := db.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.RunStatusFailed || got.Error != "approval denied" || mutations != 0 {
		t.Fatalf("run = %s (%s), mutations = %d", got.Status, got.Error, mutations)
	}
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
