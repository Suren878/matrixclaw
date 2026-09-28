package core_test

import (
	"context"
	"encoding/json"
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

var telegramAddress = json.RawMessage(`{"chat_id":42}`)

// wakeScenario is a session a Telegram chat last wrote to; its model answers
// every request with "Reported." and records them.
type wakeScenario struct {
	app      *core.Core
	db       *store.SQLiteStore
	session  core.Session
	starter  *executingRunStarter
	mu       sync.Mutex
	requests []providers.Request
}

func newWakeScenario(t *testing.T) *wakeScenario {
	t.Helper()
	db := openScenarioStore(t)
	app := core.New(db)
	s := &wakeScenario{app: app, db: db, starter: &executingRunStarter{app: app}}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(s.starter)
	app.WithTools(tools.NewRegistry(core.AwaitToolExecutors(app)...))
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests = append(s.requests, request)
		return providers.Response{Text: "Reported."}, nil
	})})
	s.session = permissionSession(t, db, "session_wake", t.TempDir(), core.PermissionModeDefault, "")
	return s
}

// userRun is a finished run the Telegram chat started.
func (s *wakeScenario) userRun(t *testing.T) core.Run {
	t.Helper()
	accepted, err := s.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: s.session.ID, Client: "telegram", ExternalKey: "telegram:42", DeliveryAddress: telegramAddress, Text: "run the tests"})
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, s.db, accepted.Run.ID, core.RunStatusCompleted)
	return accepted.Run
}

func (s *wakeScenario) runTask(t *testing.T, command string) string {
	t.Helper()
	started, err := s.app.RunCommand(context.Background(), tools.Call{SessionID: s.session.ID}, tools.Command{Command: command, Background: true})
	if err != nil {
		t.Fatal(err)
	}
	return started.TaskID
}

func (s *wakeScenario) runs(t *testing.T) []core.Run {
	t.Helper()
	runs, err := s.db.ListSessionRuns(context.Background(), s.session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func TestFinishedTaskWakesAnIdleSessionWhereTheUserWrote(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)

	taskID := s.runTask(t, "echo passed")

	var wake core.Run
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if runs := s.runs(t); runs[0].Trigger == core.RunTriggerWake && runs[0].Status == core.RunStatusCompleted {
			wake = runs[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no wake run: %+v", s.runs(t))
		}
	}
	s.starter.wait(t)
	if wake.Client != "telegram" || wake.ExternalKey != "telegram:42" {
		t.Fatalf("wake run = %+v", wake)
	}
	deliveries, err := s.db.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{RunID: wake.ID})
	if err != nil || len(deliveries) != 1 || string(deliveries[0].Address) != string(telegramAddress) {
		t.Fatalf("deliveries = %+v, %v", deliveries, err)
	}
	task, err := s.db.GetTask(context.Background(), taskID)
	if err != nil || task.DeliveredRunID != wake.ID {
		t.Fatalf("task = %+v, %v", task, err)
	}
	s.mu.Lock()
	last := s.requests[len(s.requests)-1]
	s.mu.Unlock()
	var sent []string
	for _, message := range last.Messages {
		sent = append(sent, message.Content)
	}
	if text := strings.Join(sent, "\n"); !strings.Contains(text, "Background task "+taskID+" finished with exit code 0.") || !strings.Contains(text, "passed") {
		t.Fatalf("the wake request sends:\n%s", text)
	}
}

func TestWakeChainStopsAfterTwentyRunsWithoutTheUser(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)
	ctx := context.Background()
	for i := range 20 {
		at := time.Now().UTC().Add(time.Duration(i+1) * time.Second)
		id := fmt.Sprintf("run_wake_%d", i)
		message := transcript.Message{ID: "msg_" + id, SessionID: s.session.ID, RunID: id, Role: transcript.MessageRoleUser, Content: "woken", CreatedAt: at, UpdatedAt: at}
		run := core.Run{ID: id, SessionID: s.session.ID, UserMessageID: message.ID, Trigger: core.RunTriggerWake, Status: core.RunStatusCompleted, StartedAt: at, UpdatedAt: at}
		if err := s.db.AcceptMessage(ctx, message, run); err != nil {
			t.Fatal(err)
		}
	}
	before := len(s.runs(t))

	taskID := s.runTask(t, "true")

	var notices []core.ClientDelivery
	for deadline := time.Now().Add(10 * time.Second); len(notices) == 0; time.Sleep(10 * time.Millisecond) {
		var err error
		if notices, err = s.db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{Type: core.ClientDeliveryTypeNotice}); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("no notice")
		}
	}
	if len(notices) != 1 || notices[0].Client != "telegram" || string(notices[0].Address) != string(telegramAddress) || !strings.Contains(notices[0].Summary, taskID) {
		t.Fatalf("notices = %+v", notices)
	}
	if after := len(s.runs(t)); after != before {
		t.Fatalf("runs = %d, want %d", after, before)
	}
	shown := false
	for _, message := range sessionMessages(t, s.db, s.session.ID) {
		shown = shown || message.Role == transcript.MessageRoleSystem && strings.Contains(message.Content, "waits for your message")
	}
	if !shown {
		t.Fatal("the session shows no notice")
	}
}

func TestStoppedTasksDoNotWakeAnIdleSession(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)
	before := len(s.runs(t))
	taskID := s.runTask(t, "sleep 30")

	if _, err := s.app.CancelTask(context.Background(), taskID); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, s.db, taskID, core.TaskStatusCanceled)
	time.Sleep(100 * time.Millisecond)

	if after := len(s.runs(t)); after != before {
		t.Fatalf("a stopped task started a run: %d runs, want %d", after, before)
	}
	events, err := s.db.ListTasks(context.Background(), core.TaskFilter{SessionID: s.session.ID, Undelivered: true})
	if err != nil || len(events) != 1 {
		t.Fatalf("the next run should still read the task: %+v, %v", events, err)
	}
}

func TestMessageToABusySessionSteersByDefault(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	session, _ := saveCrashRecoveryRun(t, db, "busy", core.RunStatusRunning, false)

	accepted, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "also check the logs"})

	if err != nil || accepted.Status != core.AcceptRunStatusSteered || accepted.Input == nil || accepted.Input.Mode != core.BusyInputModeSteer {
		t.Fatalf("accepted = %+v, %v", accepted, err)
	}
}
