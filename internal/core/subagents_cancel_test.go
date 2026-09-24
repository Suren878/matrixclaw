package core_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// executingRunStarter records every scheduled run and executes it in the background.
type executingRunStarter struct {
	recordingRunStarter
	app     *core.Core
	running sync.WaitGroup
}

func (s *executingRunStarter) StartRun(ctx context.Context, runID string) error {
	_ = s.recordingRunStarter.StartRun(ctx, runID)
	s.running.Add(1)
	go func() {
		defer s.running.Done()
		_ = s.app.ExecuteRun(context.Background(), runID)
	}()
	return nil
}

func (s *executingRunStarter) wait(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.running.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for started runs to finish")
	}
}

type startSignal struct {
	ch   chan struct{}
	once sync.Once
}

func newStartSignal() *startSignal { return &startSignal{ch: make(chan struct{})} }

func blockUntilCanceled(ctx context.Context, started *startSignal) (providers.Response, error) {
	started.once.Do(func() { close(started.ch) })
	<-ctx.Done()
	return providers.Response{}, ctx.Err()
}

func assertSubagentTaskStatus(t *testing.T, task core.SubagentTask, want core.SubagentTaskStatus) {
	t.Helper()
	if task.Status != want {
		t.Fatalf("subagent task status = %q (%s), want %q", task.Status, task.Error, want)
	}
}

func TestCancelParentCancelsItsBlockingSubagent(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	childStarted := newStartSignal()
	var mu sync.Mutex
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return blockUntilCanceled(ctx, childStarted)
		}
		mu.Lock()
		parentCalls++
		first := parentCalls == 1
		mu.Unlock()
		if first {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "delegate_task", Arguments: []byte(`{"goal":"count files","runtime":"matrixclaw"}`)}}}, nil
		}
		return providers.Response{Text: "Parent done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-delegate", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled parent"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}
	starter.wait(t)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-delegate")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCanceled)
	assertSubagentTaskStatus(t, task, core.SubagentTaskStatusCanceled)
	if got := starter.count(task.ChildRunID) + starter.count(run.ID); got != 0 {
		t.Fatalf("reschedules = %d, want 0", got)
	}
}

func TestCancelParentCancelsItsAsyncSubagentWithoutFollowUp(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &executingRunStarter{app: app}
	app.WithRunStarter(starter)
	app.WithTools(tools.NewRegistry(core.SubagentToolExecutors(app)...))
	childStarted := newStartSignal()
	parentWaiting := newStartSignal()
	var mu sync.Mutex
	parentCalls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			return blockUntilCanceled(ctx, childStarted)
		}
		mu.Lock()
		parentCalls++
		first := parentCalls == 1
		mu.Unlock()
		if first {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-spawn", Name: "spawn_subagent", Arguments: []byte(`{"name":"Scanner","goal":"scan the tree","runtime":"matrixclaw"}`)}}}, nil
		}
		return blockUntilCanceled(ctx, parentWaiting)
	})})
	session, run := saveCrashRecoveryRun(t, db, "cancel-spawn", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	waitRecoverySignal(t, childStarted.ch, "child generation")
	waitRecoverySignal(t, parentWaiting.ch, "parent generation")

	if _, err := app.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled parent"); err != nil {
		t.Fatalf("ExecuteRun after cancel: %v", err)
	}
	starter.wait(t)

	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCanceled)
	task, err := db.GetSubagentTaskByParentToolCall(context.Background(), session.ID, run.ID, "call-spawn")
	if err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, task.ChildRunID, core.RunStatusCanceled)
	assertSubagentTaskStatus(t, task, core.SubagentTaskStatusCanceled)
	if task.CompletionQueuedAt != nil {
		t.Fatal("a canceled async subagent queued a completion for its canceled parent")
	}
	if got := starter.count(task.ChildRunID); got != 1 {
		t.Fatalf("child starts = %d, want 1", got)
	}
	for _, message := range sessionMessages(t, db, session.ID) {
		if message.Role == transcript.MessageRoleUser && message.RunID != run.ID {
			t.Fatalf("parent session got a follow-up run %s after cancel", message.RunID)
		}
	}
}
