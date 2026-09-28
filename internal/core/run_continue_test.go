package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

func TestContinueStartsARunThatContinuesTheLatestOne(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestContinueKeepsTheOriginalAssignmentVerbatim(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	app.WithRunStarter(&recordingRunStarter{})
	app.WithSessionLLMs(windowLLMs{window: 100_000, recoveryLLMs: recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.HasPrefix(request.SystemPrompt, "You compact matrixclaw chat histories") {
			return providers.Response{Text: "SUMMARY"}, nil
		}
		return providers.Response{Text: "Done."}, nil
	})}})
	ctx := context.Background()
	session, first := saveNativeRunWithHistory(t, db, "assignment")
	answer := strings.Repeat("x", 330_000)
	saveRunRecoveryTestMessage(t, db, transcript.Message{
		ID: "msg_long_answer", SessionID: session.ID, RunID: first.ID, Role: transcript.MessageRoleAssistant, Content: answer,
		Parts: transcript.NormalizeMessageParts(answer, nil), CreatedAt: runRecoveryTestTime(), UpdatedAt: runRecoveryTestTime(),
	})
	first.Status = core.RunStatusCompleted
	if err := db.UpdateRun(ctx, first); err != nil {
		t.Fatal(err)
	}
	continued, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: session.ID, Continue: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := app.ExecuteRun(ctx, continued.Run.ID); err != nil {
		t.Fatal(err)
	}

	assertRecoveryRunStatus(t, db, continued.Run.ID, core.RunStatusCompleted)
	boundary, err := db.LatestCompaction(ctx, session.ID)
	if err != nil || len(boundary.Compaction.Kept) != 1 || boundary.Compaction.Kept[0] != "User: original task assignment" {
		t.Fatalf("boundary = %+v err = %v", boundary.Compaction, err)
	}
}
