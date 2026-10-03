package core_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
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

// restartableDaemon is a core over a database that outlives it, with the tools
// and models of a parent that delegates to a child asking for approval.
type restartableDaemon struct {
	db        *store.SQLiteStore
	mu        sync.Mutex
	mutations int
	children  int
}

func (d *restartableDaemon) start(t *testing.T) (*core.Core, context.CancelFunc) {
	t.Helper()
	lifetime, stop := context.WithCancel(context.Background())
	app := core.New(d.db).WithLifetime(lifetime)
	mutate := funcTool{spec: recoveryToolSpec("mutate_state", tools.EffectMutation), ask: "write the state", fn: func(context.Context, tools.Call) (tools.Result, error) {
		d.mu.Lock()
		d.mutations++
		d.mu.Unlock()
		return tools.Result{Content: "mutated"}, nil
	}}
	app.WithTools(tools.NewRegistry(append(core.AgentToolExecutors(app), mutate)...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		if strings.Contains(request.SystemPrompt, "Subagent mode:") {
			d.mu.Lock()
			d.children++
			d.mu.Unlock()
			if result := toolResultContent(request, "call-child-mutate"); result != "" {
				return providers.Response{Text: "Child read: " + result}, nil
			}
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-child-mutate", Name: "mutate_state", Arguments: []byte(`{}`)}}}, nil
		}
		if result := toolResultContent(request, "call-delegate"); result != "" {
			return providers.Response{Text: "Parent saw: " + result}, nil
		}
		return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-delegate", Name: "agent", Arguments: []byte(`{"description":"Change the state","prompt":"change the state","runtime":"matrixclaw"}`)}}}, nil
	})})
	return app, func() {
		stop()
		app.WaitRuns()
	}
}

func TestARestartWhileABlockingChildWaitsForApprovalCompletesBothOnce(t *testing.T) {
	t.Parallel()
	d := &restartableDaemon{db: openScenarioStore(t)}
	first, stopFirst := d.start(t)
	session, parent := saveCrashRecoveryRun(t, d.db, "restart-asking", core.RunStatusAccepted, false)
	if err := first.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	approval := waitPendingApproval(t, d.db, session.ID, "call-child-mutate")
	waitRunStatus(t, d.db, approval.RunID, core.RunStatusWaitingApproval)
	assertRecoveryRunStatus(t, d.db, parent.ID, core.RunStatusRunning)
	stopFirst()

	second, stopSecond := d.start(t)
	defer stopSecond()
	if err := second.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pending := pendingApprovalFor(t, d.db, session.ID, "call-child-mutate"); pending.ID != approval.ID {
		t.Fatalf("pending approval after the restart = %+v, want the child's own", pending)
	}
	if _, err := second.ResolveApproval(context.Background(), approval.ID, core.ApprovalResolveRequest{Approved: true}); err != nil {
		t.Fatal(err)
	}

	waitRunStatus(t, d.db, parent.ID, core.RunStatusCompleted)
	assertRecoveryRunStatus(t, d.db, approval.RunID, core.RunStatusCompleted)
	assertToolResultCount(t, d.db, session.ID, "call-delegate", 1)
	if got := storedToolResult(t, d.db, session.ID, "call-delegate"); got != "Child read: mutated" {
		t.Fatalf("delegate result = %q", got)
	}
	task, err := taskOfCall(d.db, session.ID, parent.ID, "call-delegate")
	if err != nil || task.Status != core.TaskStatusCompleted {
		t.Fatalf("task = %+v, %v", task, err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mutations != 1 || d.children != 2 {
		t.Fatalf("mutations = %d, child generations = %d; want each once and the child's reply after the approval", d.mutations, d.children)
	}
}
