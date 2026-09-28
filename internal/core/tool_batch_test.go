package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// sessionIn moves a test session into dir under mode.
func sessionIn(t *testing.T, db *store.SQLiteStore, session core.Session, dir string, mode core.PermissionMode) {
	t.Helper()
	session.WorkingDir, session.PermissionMode = dir, mode
	if err := db.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
}

// toolResults reads the tool results of a request by call ID, in request order.
func toolResults(request providers.Request) []string {
	var out []string
	for _, message := range request.Messages {
		if message.ToolCallID != "" {
			out = append(out, message.ToolCallID+"="+message.Content)
		}
	}
	return out
}

func TestReadsOfOneReplyRunAtOnceInANativeRun(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started, release := make(chan struct{}, 2), make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		started <- struct{}{}
		<-release
		return tools.Result{Content: "inspected " + call.ToolCallID}, nil
	}}))
	var results []string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if results = toolResults(request); len(results) > 0 {
			return providers.Response{Text: "Done."}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{
			{ID: "call-a", Name: "inspect_state", Arguments: []byte(`{"path":"a"}`)},
			{ID: "call-b", Name: "inspect_state", Arguments: []byte(`{"path":"b"}`)},
		}}, nil
	})})
	_, run := saveCrashRecoveryRun(t, db, "parallel_reads", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()

	waitRecoverySignal(t, started, "first read")
	waitRecoverySignal(t, started, "second read while the first runs")
	close(release)

	if err := waitRecoveryError(t, done, "parallel run"); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if strings.Join(results, "|") != "call-a=inspected call-a|call-b=inspected call-b" {
		t.Fatalf("model read %q", results)
	}
}

func TestAllowedMutationsOfTwoSessionsInOneDirectoryTakeTurns(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	dir := t.TempDir()
	first, firstRun := saveCrashRecoveryRun(t, db, "turns_a", core.RunStatusAccepted, false)
	second, secondRun := saveCrashRecoveryRun(t, db, "turns_b", core.RunStatusAccepted, false)
	sessionIn(t, db, first, dir, core.PermissionModeFullAuto)
	sessionIn(t, db, second, dir, core.PermissionModeFullAuto)
	firstStarted, secondReading, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var firstDone, secondSawFirst atomic.Bool
	mutate := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), fn: func(_ context.Context, call tools.Call) (tools.Result, error) {
		if call.SessionID == first.ID {
			close(firstStarted)
			<-release
			firstDone.Store(true)
		} else {
			secondSawFirst.Store(firstDone.Load())
		}
		return tools.Result{Content: "mutated"}, nil
	}}
	inspect := funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		close(secondReading)
		return tools.Result{Content: "inspected"}, nil
	}}
	app.WithTools(tools.NewRegistry(mutate, inspect))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		switch {
		case len(toolResults(request)) > 0:
			return providers.Response{Text: "Done."}, nil
		case request.CacheKey == first.ID:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-first", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		default:
			return providers.Response{ToolCalls: []providers.ToolCall{
				{ID: "call-second", Name: "mutate_state", Arguments: []byte(`{}`)},
				{ID: "call-second-read", Name: "inspect_state", Arguments: []byte(`{}`)},
			}}, nil
		}
	})})
	done := make(chan error, 2)
	go func() { done <- app.ExecuteRun(context.Background(), firstRun.ID) }()
	waitRecoverySignal(t, firstStarted, "first session's mutation")
	go func() { done <- app.ExecuteRun(context.Background(), secondRun.ID) }()
	waitRecoverySignal(t, secondReading, "second session's batch")

	close(release)

	for range 2 {
		if err := waitRecoveryError(t, done, "both runs"); err != nil {
			t.Fatal(err)
		}
	}
	assertRecoveryRunStatus(t, db, firstRun.ID, core.RunStatusCompleted)
	assertRecoveryRunStatus(t, db, secondRun.ID, core.RunStatusCompleted)
	if !secondSawFirst.Load() {
		t.Fatal("the second session mutated the directory while the first one did")
	}
}

func TestDelegatedChildChangesTheParentsDirectoryWithoutDeadlock(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	edit := recoveryToolSpec("mutate_state", tools.EffectMutation)
	edit.Risk, edit.ApprovalMode = tools.RiskSafe, tools.ApprovalNever
	edits := 0
	app.WithTools(tools.NewRegistry(append(core.SubagentToolExecutors(app), funcTool{spec: edit, fn: func(context.Context, tools.Call) (tools.Result, error) {
		edits++
		return tools.Result{Content: "mutated"}, nil
	}})...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		answered := len(toolResults(request)) > 0
		switch {
		case strings.Contains(request.SystemPrompt, "Subagent mode:") && !answered:
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-edit", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		case strings.Contains(request.SystemPrompt, "Subagent mode:"):
			return providers.Response{Text: "child done"}, nil
		case !answered:
			args, _ := json.Marshal(map[string]string{"goal": "edit the file", "runtime": "matrixclaw"})
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: args}}}, nil
		default:
			return providers.Response{Text: "Parent done."}, nil
		}
	})})
	session, run := saveCrashRecoveryRun(t, db, "delegate_edit", core.RunStatusAccepted, false)
	sessionIn(t, db, session, t.TempDir(), core.PermissionModeDefault)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()

	if err := waitRecoveryError(t, done, "delegation"); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if edits != 1 {
		t.Fatalf("child edits = %d, want 1", edits)
	}
}
