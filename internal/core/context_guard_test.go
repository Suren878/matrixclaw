package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/tools"
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
	if _, err := app.ClearContext(ctx, session.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("ClearContext error = %v, want ErrRunActive", err)
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

func TestContextBoundariesAreRejectedWhileARunWaitsForApproval(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	mutations := 0
	mutate, inspect := approvalTools(&mutations)
	app.WithTools(tools.NewRegistry(mutate, inspect))
	app.WithRunStarter(&recordingRunStarter{})
	calls := 0
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})})
	session, run := saveCrashRecoveryRun(t, db, "parked", core.RunStatusAccepted, false)
	ctx := context.Background()
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusWaitingApproval)

	if _, err := app.ClearContext(ctx, session.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("ClearContext error = %v, want ErrRunActive", err)
	}
	if _, err := app.CompactSession(ctx, session.ID); !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("CompactSession error = %v, want ErrRunActive", err)
	}

	approvals, err := db.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
	if err != nil || len(approvals) != 1 {
		t.Fatalf("pending approvals = %+v err = %v", approvals, err)
	}
	if _, err := app.ResolveApproval(ctx, approvals[0].ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	assertRecoveryRunStatus(t, db, run.ID, core.RunStatusCompleted)
	if mutations != 1 {
		t.Fatalf("mutations = %d, want 1", mutations)
	}
}
