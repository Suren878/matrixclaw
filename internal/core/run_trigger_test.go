package core_test

import (
	"context"
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
