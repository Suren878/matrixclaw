package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestTriggeredRunsAreMarkedAsAutomation(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	session, _ := saveCrashRecoveryRun(t, db, "trigger", core.RunStatusCompleted, false)

	result, err := app.AcceptTriggeredRun(context.Background(), core.HandleTriggeredRunInput{TriggerID: "job-1", SessionID: session.ID, Text: "scheduled check"})
	if err != nil {
		t.Fatal(err)
	}

	stored, err := db.GetRun(context.Background(), result.Run.ID)
	if err != nil || stored.Trigger != core.RunTriggerAutomation {
		t.Fatalf("triggered run = %+v err = %v", stored, err)
	}
}

func TestTriggerIntoABusySessionIsSkipped(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	session, _ := saveCrashRecoveryRun(t, db, "trigger-busy", core.RunStatusRunning, false)

	_, err := app.AcceptTriggeredRun(context.Background(), core.HandleTriggeredRunInput{TriggerID: "fire-1", SessionID: session.ID, Text: "scheduled check"})

	if !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("error = %v, want ErrRunActive", err)
	}
	if runs, _ := db.ListSessionRuns(context.Background(), session.ID, 10); len(runs) != 1 {
		t.Fatalf("runs = %d, want only the busy one", len(runs))
	}
}
