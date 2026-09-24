package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestContinueStartsARunThatContinuesTheLatestOne(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	starter := &recordingRunStarter{}
	app.WithRunStarter(starter)
	session, previous := saveCrashRecoveryRun(t, db, "continue", core.RunStatusCompleted, false)

	result, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Continue: true})

	if err != nil {
		t.Fatal(err)
	}
	if result.Run.ContinuesRunID != previous.ID || result.UserMessage.Role != transcript.MessageRoleUser || result.UserMessage.Content != "Continue" {
		t.Fatalf("result = %+v", result)
	}
	stored, err := db.GetRun(context.Background(), result.Run.ID)
	if err != nil || stored.ContinuesRunID != previous.ID {
		t.Fatalf("stored run = %+v err = %v", stored, err)
	}
	if got := starter.count(result.Run.ID); got != 1 {
		t.Fatalf("starts = %d, want 1", got)
	}
}

func TestContinueIsRejectedWhileARunIsActive(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	session, _ := saveCrashRecoveryRun(t, db, "continue-busy", core.RunStatusRunning, false)

	_, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Continue: true})

	if !errors.Is(err, core.ErrRunActive) {
		t.Fatalf("error = %v, want ErrRunActive", err)
	}
}

func TestContinueNeedsAnEarlierRun(t *testing.T) {
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	now := runRecoveryTestTime()
	session := core.Session{ID: "session_empty", Title: "empty", Kind: core.SessionKindAssistant, RuntimeID: core.SessionRuntimeMatrixClaw, Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := db.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}

	_, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Continue: true})

	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
