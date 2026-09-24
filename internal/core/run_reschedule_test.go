package core_test

import (
	"context"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
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
