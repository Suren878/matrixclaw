package core_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func interruptNativeRun(t *testing.T, app *core.Core, runID string, runtime *interruptibleRecoveryRuntime) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, runID) }()
	waitRecoverySignal(t, runtime.started, "native generation start")
	cancel()
	if err := waitRecoveryError(t, done, "interrupted run"); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
}

func TestInterruptedNativeRunIsRescheduledWhileTheDaemonRuns(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	runtime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	_, run := saveCrashRecoveryRun(t, db, "reschedule", core.RunStatusAccepted, false)

	interruptNativeRun(t, app, run.ID, runtime)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)
	if got := starter.count(run.ID); got != 1 {
		t.Fatalf("reschedules = %d, want 1", got)
	}
}

func TestInterruptedNativeRunWaitsForStartupRecoveryAfterShutdown(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	lifetime, stop := context.WithCancel(context.Background())
	stop()
	app.WithLifetime(lifetime)
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	runtime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	_, run := saveCrashRecoveryRun(t, db, "shutdown", core.RunStatusAccepted, false)

	interruptNativeRun(t, app, run.ID, runtime)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)
	if got := starter.count(run.ID); got != 0 {
		t.Fatalf("reschedules = %d, want 0", got)
	}
}

func TestRunInterruptedDuringToolReplaysItAndCompletes(t *testing.T) {
	t.Parallel()
	for _, restart := range []bool{false, true} {
		name := map[bool]string{false: "rescheduled", true: "after restart"}[restart]
		t.Run(name, func(t *testing.T) {
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			var mu sync.Mutex
			toolCalls := 0
			var toolResult string
			started := newStartSignal()
			configure := func(app *core.Core) {
				app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(ctx context.Context, _ tools.Call) (tools.Result, error) {
					mu.Lock()
					toolCalls++
					first := toolCalls == 1
					mu.Unlock()
					if first {
						_, err := blockUntilCanceled(ctx, started)
						return tools.Result{}, err
					}
					return tools.Result{Content: "state ok", Status: tools.ResultStatusSuccess}, nil
				}}))
				app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
					mu.Lock()
					defer mu.Unlock()
					for _, message := range request.Messages {
						if message.ToolCallID == "call-inspect" {
							toolResult = message.Content
							return providers.Response{Text: "Done after recovery."}, nil
						}
					}
					return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-inspect", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
				})})
			}
			configure(app)
			if restart {
				lifetime, stop := context.WithCancel(context.Background())
				stop()
				app.WithLifetime(lifetime)
			}
			session, run := saveCrashRecoveryRun(t, db, "tool-interrupt", core.RunStatusAccepted, false)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- app.ExecuteRun(ctx, run.ID) }()
			waitRecoverySignal(t, started.ch, "tool start")
			cancel()
			if err := waitRecoveryError(t, done, "interrupted run"); err != nil {
				t.Fatalf("ExecuteRun: %v", err)
			}
			if restart {
				assertRecoveryRunStatus(t, db, run.ID, core.RunStatusRunning)
				restarted := core.New(db)
				configure(restarted)
				if err := restarted.RecoverActiveRuns(context.Background()); err != nil {
					t.Fatalf("RecoverActiveRuns: %v", err)
				}
			}

			waitForRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
			mu.Lock()
			defer mu.Unlock()
			if toolCalls != 2 || toolResult != "state ok" {
				t.Fatalf("tool calls = %d, result seen by the model = %q", toolCalls, toolResult)
			}
			assertToolResultCount(t, db, session.ID, "call-inspect", 1)
			if !hasAssistantContent(sessionMessages(t, db, session.ID), run.ID, "Done after recovery.") {
				t.Fatal("final reply missing")
			}
		})
	}
}

func TestInterruptedParentAndBlockingChildAreBothRescheduledAndComplete(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.AgentToolExecutors(app)...))
	childStarted := newStartSignal()
	var mu sync.Mutex
	childCalls := 0
	var delegateResult string
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		mu.Lock()
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			childCalls++
			first := childCalls == 1
			mu.Unlock()
			if first {
				return blockUntilCanceled(ctx, childStarted)
			}
			return providers.Response{Text: "child found 3 files"}, nil
		}
		defer mu.Unlock()
		for _, message := range request.Messages {
			if message.ToolCallID == "call-delegate" {
				delegateResult = message.Content
				return providers.Response{Text: "Parent done."}, nil
			}
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Count files","prompt":"count files","runtime":"matrixclaw"}`)}}}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "interrupt-delegate", core.RunStatusAccepted, false)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")
	cancel()
	if err := waitRecoveryError(t, done, "interrupted parent"); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}

	waitForRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	starter.wait(t)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCompleted)
	if task.Status != core.TaskStatusCompleted || task.Summary != "child found 3 files" {
		t.Fatalf("task = %s %q", task.Status, task.Summary)
	}
	if starter.count(run.ID) == 0 || starter.count(task.ChildRunID) == 0 {
		t.Fatalf("reschedules parent=%d child=%d, want both", starter.count(run.ID), starter.count(task.ChildRunID))
	}
	assertToolResultCount(t, db, session.ID, "call-delegate", 1)
	mu.Lock()
	defer mu.Unlock()
	if delegateResult != "child found 3 files" {
		t.Fatalf("delegate result seen by the parent = %q", delegateResult)
	}
}
