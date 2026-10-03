package core_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// A cancel racing the run's own completion ends the run once: either it
// completed with its reply, or it was canceled and its reply says so.
func TestCancelRacingCompletionEndsTheRunOnce(t *testing.T) {
	t.Parallel()
	for i := range 20 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			app.WithRunStarter(&recordingRunStarter{})
			generating := make(chan struct{})
			app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
				close(generating)
				if err := providers.StreamText(ctx, "Done"); err != nil {
					return providers.Response{}, err
				}
				time.Sleep(time.Duration(i%4) * time.Millisecond)
				return providers.Response{Text: "Done."}, nil
			})})
			session, run := saveCrashRecoveryRun(t, db, "race", core.RunStatusAccepted, false)
			executed := make(chan error, 1)
			go func() { executed <- app.ExecuteRun(context.Background(), run.ID) }()
			<-generating
			time.Sleep(time.Duration(i%3) * time.Millisecond)
			canceled, cancelErr := app.CancelRun(context.Background(), run.ID)
			if err := <-executed; err != nil {
				t.Fatal(err)
			}

			stored, err := db.GetRun(context.Background(), run.ID)
			if err != nil || cancelErr != nil {
				t.Fatalf("run = %+v err = %v, cancel err = %v", stored, err, cancelErr)
			}
			if canceled.Status != stored.Status || stored.FinishedAt == nil {
				t.Fatalf("CancelRun returned %s, stored %+v", canceled.Status, stored)
			}
			if _, err := db.GetRunCheckpoint(context.Background(), run.ID); !errors.Is(err, core.ErrNotFound) {
				t.Fatalf("checkpoint of the ended run: %v", err)
			}
			var replies []transcript.Message
			for _, message := range sessionMessages(t, db, session.ID) {
				if message.Role == transcript.MessageRoleAssistant {
					replies = append(replies, message)
				}
			}
			switch stored.Status {
			case core.RunStatusCompleted:
				if len(replies) != 1 || transcript.HasFinishReason(replies[0], "canceled") {
					t.Fatalf("completed run replies = %+v", replies)
				}
			case core.RunStatusCanceled:
				if len(replies) > 1 || len(replies) == 1 && !transcript.HasFinishReason(replies[0], "canceled") {
					t.Fatalf("canceled run replies = %+v", replies)
				}
			default:
				t.Fatalf("run ended %s", stored.Status)
			}
		})
	}
}
