package core_test

import (
	"context"
	"errors"
	"testing"

	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/core"
)

func TestContextMarkersAreRejectedWhileANativeRunExecutes(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &interruptibleRecoveryRuntime{started: make(chan struct{})}
	app.WithSessionLLMs(recoveryLLMs{runtime: runtime})
	session, run := saveCrashRecoveryRun(t, db, "guard", core.RunStatusAccepted, false)
	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, run.ID) }()
	waitRecoverySignal(t, runtime.started, "native generation start")

	if _, err := app.CompactSession(ctx, session.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("CompactSession error = %v, want ErrRunActive", err)
	}
	if _, err := app.CreateSystemMessage(ctx, session.ID, agentcontext.ClearedMarkerContent()); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("clear marker error = %v, want ErrRunActive", err)
	}
	if _, err := app.CreateSystemMessage(ctx, session.ID, "Model changed."); err != nil {
		t.Fatalf("plain system notice: %v", err)
	}

	if _, err := app.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := waitRecoveryError(t, done, "canceled run"); err != nil {
		t.Fatal(err)
	}
}
