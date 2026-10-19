package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestRecoverRunningGenerationSkipsPartialAndCompletes(t *testing.T) {
	t.Parallel()
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "recovered answer"}
	app.SetSessionLLMs(recoveryLLMs{runtime: runtime})

	session, run := saveCrashRecoveryRun(t, sqliteStore, "generation", core.RunStatusRunning, false)
	partial := transcript.Message{
		ID: "msg_partial_generation", SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleAssistant, Content: "partial answer that must not enter retry context",
		Parts:     transcript.NormalizeMessageParts("partial answer that must not enter retry context", nil),
		CreatedAt: run.StartedAt.Add(time.Second), UpdatedAt: run.StartedAt.Add(time.Second),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, partial)
	if err := sqliteStore.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
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

func TestEndingTheLifetimeKeepsExecutingRunsForRecovery(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	lifetime, stopLifetime := context.WithCancel(context.Background())
	app := core.New(core.Config{Store: sqliteStore, Lifetime: lifetime})
	interruptedRuntime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.SetSessionLLMs(recoveryLLMs{runtime: interruptedRuntime})
	_, run := saveCrashRecoveryRun(t, sqliteStore, "lifetime_end", core.RunStatusAccepted, false)

	if err := app.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitRecoverySignal(t, interruptedRuntime.started, "native generation start")
	stopLifetime()
	app.WaitRuns()

	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusRunning)
	if _, err := sqliteStore.GetRunCheckpoint(context.Background(), run.ID); err != nil {
		t.Fatalf("GetRunCheckpoint: %v", err)
	}
	idle, _ := saveCrashRecoveryRun(t, sqliteStore, "lifetime_idle", core.RunStatusCompleted, false)
	if _, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: idle.ID, Text: "more"}); !errors.Is(err, core.ErrExecutionUnavailable) {
		t.Fatalf("AcceptRun after WaitRuns error = %v", err)
	}
}

func TestGracefulExecutorStopPreservesAndRecoversNativeGeneration(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	executionCtx, cancelExecution := context.WithCancel(context.Background())
	app := core.New(core.Config{Store: sqliteStore, Lifetime: executionCtx})
	interruptedRuntime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.SetSessionLLMs(recoveryLLMs{runtime: interruptedRuntime})

	_, run := saveCrashRecoveryRun(t, sqliteStore, "graceful_native", core.RunStatusAccepted, false)
	executionDone := make(chan error, 1)
	go func() { executionDone <- app.ExecuteRun(executionCtx, run.ID) }()
	waitRecoverySignal(t, interruptedRuntime.started, "native generation start")
	cancelExecution()
	if err := waitRecoveryError(t, executionDone, "native executor shutdown"); err != nil {
		t.Fatalf("ExecuteRun after shutdown: %v", err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusRunning)
	if _, err := sqliteStore.GetRunCheckpoint(context.Background(), run.ID); err != nil {
		t.Fatalf("GetRunCheckpoint: %v", err)
	}
	messages, err := sqliteStore.ListMessages(context.Background(), run.SessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRecoveryFinishedAssistant(messages, run.ID, "partial before graceful restart") {
		t.Fatalf("gracefully interrupted native partial was not sealed as a stable segment: %#v", messages)
	}

	recoveredRuntime := &recoveryRuntime{text: "native resumed after restart"}
	restarted := core.New(core.Config{Store: sqliteStore})
	restarted.SetSessionLLMs(recoveryLLMs{runtime: recoveredRuntime})
	if err := restarted.Recover(context.Background()); err != nil {
		t.Fatalf("Recover after graceful stop: %v", err)
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
	t.Parallel()
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	interruptedRuntime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.SetSessionLLMs(recoveryLLMs{runtime: interruptedRuntime})

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
	t.Parallel()
	app, sqliteStore, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &recoveryRuntime{text: "single recovered answer"}
	app.SetSessionLLMs(recoveryLLMs{runtime: runtime})

	_, run := saveCrashRecoveryRun(t, sqliteStore, "startup_race", core.RunStatusRunning, false)
	if err := sqliteStore.SaveRunCheckpoint(context.Background(), core.RunCheckpoint{
		RunID: run.ID, UpdatedAt: run.UpdatedAt,
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
		errorsSeen <- app.Recover(context.Background())
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
