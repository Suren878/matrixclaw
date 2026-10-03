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

// chatCapabilities are a client that fetches its replies from the delivery queue.
var chatCapabilities = core.ClientCapabilities{ReceivesDeliveries: true}

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
	accepted, err := s.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: s.session.ID, Client: "telegram", ExternalKey: "telegram:42", DeliveryAddress: telegramAddress, ClientCapabilities: chatCapabilities, Text: "run the tests"})
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
	s.addRuns(t, core.RunTriggerWake, 20, false)
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
	if err := s.app.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}

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

// visionRuntime is a model that takes images.
type visionRuntime struct{ generationRuntimeFunc }

func (visionRuntime) ModelCapabilities() providers.ModelCapabilities {
	return providers.ModelCapabilities{ToolCalling: true, ImageInput: true}
}

func TestAPhotoSteeredIntoABusyRunReachesTheModel(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	started := make(chan struct{})
	release := make(chan struct{})
	app.WithTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("inspect_state", tools.EffectReadOnly), fn: func(context.Context, tools.Call) (tools.Result, error) {
		close(started)
		<-release
		return tools.Result{Content: "inspected"}, nil
	}}))
	var last providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: visionRuntime{func(_ context.Context, request providers.Request) (providers.Response, error) {
		last = request
		if len(request.Messages) == 1 {
			return providers.Response{ToolCalls: []providers.ToolCall{{ID: "call-img", Name: "inspect_state", Arguments: []byte(`{}`)}}}, nil
		}
		return providers.Response{Text: "A cat."}, nil
	}}})
	session, run := saveCrashRecoveryRun(t, db, "steer_img", core.RunStatusAccepted, false)
	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(context.Background(), run.ID) }()
	<-started
	parts := []transcript.MessagePart{
		{Kind: transcript.MessagePartKindText, Text: &transcript.TextPart{Text: "what is on this screenshot?"}},
		{Kind: transcript.MessagePartKindImage, Image: &transcript.ImagePart{MIMEType: "image/png", DataBase64: "iVBORw0KGgo="}},
	}
	result, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "what is on this screenshot?", Parts: parts})
	if err != nil || result.Status != core.AcceptRunStatusSteered {
		t.Fatalf("AcceptRun = %#v, %v", result, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	steer := last.Messages[len(last.Messages)-1]
	if steer.Role != "user" || steer.Content != "what is on this screenshot?" || len(steer.Images) != 1 {
		t.Fatalf("last message the model read = %+v", steer)
	}
}

func TestSteeringFromAnotherChatDeliversTheRunThere(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	session, run := saveCrashRecoveryRun(t, db, "steered", core.RunStatusRunning, false)
	steer := core.HandleMessageInput{SessionID: session.ID, Client: "telegram", ExternalKey: "42", DeliveryAddress: telegramAddress, ClientCapabilities: chatCapabilities, Text: "also check the logs"}

	for range 2 {
		if accepted, err := app.AcceptRun(context.Background(), steer); err != nil || accepted.Status != core.AcceptRunStatusSteered || accepted.Input.TargetRunID != run.ID {
			t.Fatalf("accepted = %+v, %v", accepted, err)
		}
	}

	deliveries, err := db.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{RunID: run.ID})
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("deliveries = %+v, %v", deliveries, err)
	}
	if got := deliveries[0]; got.Type != core.ClientDeliveryTypeRun || got.Client != "telegram" || got.ExternalKey != "42" || string(got.Address) != string(telegramAddress) || got.Status != core.ClientDeliveryStatusPending {
		t.Fatalf("delivery = %+v", got)
	}
}

func TestWakeRunTakesItsEventsBeforeItStarts(t *testing.T) {
	t.Parallel()
	db := openScenarioStore(t)
	app := core.New(db)
	starter := &recordingRunStarter{}
	app.WithSessionFiles(t.TempDir()).WithRunStarter(starter)
	var requests []providers.Request
	app.WithSessionLLMs(recoveryLLMs{runtime: generationRuntimeFunc(func(_ context.Context, request providers.Request) (providers.Response, error) {
		requests = append(requests, request)
		return providers.Response{Text: "Reported."}, nil
	})})
	session := permissionSession(t, db, "session_wake_events", t.TempDir(), core.PermissionModeDefault, "")
	ctx := context.Background()
	now := runRecoveryTestTime()
	if err := db.CreateTask(ctx, core.Task{ID: "task_done", SessionID: session.ID, Kind: core.TaskKindSubagent, Status: core.TaskStatusRunning, Command: "check the logs", Background: true, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishTask(ctx, "task_done", core.TaskEnd{Status: core.TaskStatusCompleted, At: now}); err != nil {
		t.Fatal(err)
	}

	if err := app.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	if len(starter.ids) != 1 {
		t.Fatalf("started runs = %v", starter.ids)
	}
	wakeID := starter.ids[0]
	task, err := db.GetTask(ctx, "task_done")
	if err != nil || task.DeliveredRunID != wakeID {
		t.Fatalf("before the wake run started, task = %+v, %v", task, err)
	}
	if err := app.ExecuteRun(ctx, wakeID); err != nil {
		t.Fatal(err)
	}
	var sent []string
	for _, message := range requests[0].Messages {
		sent = append(sent, message.Content)
	}
	if text := strings.Join(sent, "\n"); strings.Count(text, "Subagent task_done (task_done) finished") != 1 {
		t.Fatalf("the wake request sends:\n%s", text)
	}
}

// addRuns stores n completed runs with the trigger after the session's runs;
// delivered ones went to the Telegram chat.
func (s *wakeScenario) addRuns(t *testing.T, trigger core.RunTrigger, n int, delivered bool) []string {
	t.Helper()
	ctx := context.Background()
	var ids []string
	for range n {
		at := time.Now().UTC()
		id := fmt.Sprintf("run_%s_%d", trigger, len(s.runs(t)))
		message := transcript.Message{ID: "msg_" + id, SessionID: s.session.ID, RunID: id, Role: transcript.MessageRoleUser, Content: "woken", CreatedAt: at, UpdatedAt: at}
		run := core.Run{ID: id, SessionID: s.session.ID, UserMessageID: message.ID, Trigger: trigger, Status: core.RunStatusCompleted, StartedAt: at, UpdatedAt: at}
		var deliveries []core.ClientDelivery
		if delivered {
			run.Client, run.ExternalKey = "telegram", "telegram:42"
			deliveries = append(deliveries, core.ClientDelivery{ID: "delivery_" + id, Type: core.ClientDeliveryTypeRun, Client: "telegram", ExternalKey: "telegram:42", SessionID: s.session.ID, RunID: id, Address: telegramAddress, Status: core.ClientDeliveryStatusPending, CreatedAt: at, UpdatedAt: at})
		}
		if err := s.db.AcceptMessage(ctx, message, run, deliveries...); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// finishedTask stores a background command that finished unseen.
func (s *wakeScenario) finishedTask(t *testing.T, id string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.db.CreateTask(ctx, core.Task{ID: id, SessionID: s.session.ID, Kind: core.TaskKindShell, Status: core.TaskStatusRunning, Command: "make " + id, Background: true, StartedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	code := 0
	if _, err := s.db.FinishTask(ctx, id, core.TaskEnd{Status: core.TaskStatusCompleted, ExitCode: &code, At: now}); err != nil {
		t.Fatal(err)
	}
}

func (s *wakeScenario) notices(t *testing.T) []core.ClientDelivery {
	t.Helper()
	notices, err := s.db.ListClientDeliveries(context.Background(), core.ClientDeliveryFilter{Type: core.ClientDeliveryTypeNotice})
	if err != nil {
		t.Fatal(err)
	}
	return notices
}

func TestFailedWakeRunStopsTheChainWithOneNotice(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)
	s.starter.wait(t)
	s.app.WithSessionLLMs(recoveryLLMs{})
	ctx := context.Background()

	s.finishedTask(t, "task_a")
	if err := s.app.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.starter.wait(t)
	s.finishedTask(t, "task_b")
	if err := s.app.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.starter.wait(t)

	runs := s.runs(t)
	if len(runs) != 2 || runs[0].Trigger != core.RunTriggerWake || runs[0].Status != core.RunStatusFailed {
		t.Fatalf("runs = %+v", runs)
	}
	if notices := s.notices(t); len(notices) != 1 || notices[0].Client != "telegram" || string(notices[0].Address) != string(telegramAddress) {
		t.Fatalf("notices = %+v", notices)
	}
	if task, err := s.db.GetTask(ctx, "task_b"); err != nil || task.DeliveredAt != nil {
		t.Fatalf("the next run should read task_b: %+v, %v", task, err)
	}
}

// replyOnceRun is a finished run a Telegram target that takes one reply started.
func (s *wakeScenario) replyOnceRun(t *testing.T, externalKey string, address string) {
	t.Helper()
	accepted, err := s.app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: s.session.ID, Client: "telegram", ExternalKey: externalKey, DeliveryAddress: json.RawMessage(address), ReplyOnce: true, ClientCapabilities: chatCapabilities, Text: "look it up"})
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, s.db, accepted.Run.ID, core.RunStatusCompleted)
	s.starter.wait(t)
}

func TestWakeRunIsDeliveredToAChatNeverToAReplyOnceTarget(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.userRun(t)
	s.replyOnceRun(t, "42", `{"inline":"inline_1"}`)
	s.replyOnceRun(t, "guest:query_1", `{"guest":"query_1"}`)
	ctx := context.Background()

	s.finishedTask(t, "task_a")
	if err := s.app.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	s.starter.wait(t)

	wake := s.runs(t)[0]
	deliveries, err := s.db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{RunID: wake.ID})
	if err != nil || wake.Trigger != core.RunTriggerWake || len(deliveries) != 1 || deliveries[0].ExternalKey != "telegram:42" || string(deliveries[0].Address) != string(telegramAddress) {
		t.Fatalf("wake run %+v delivered to %+v, %v", wake, deliveries, err)
	}
}

func TestWakeChainCountsWakeRunsTheUserDidNotReach(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, s *wakeScenario)
		wakes bool
	}{
		{"an automation run does not end it", func(t *testing.T, s *wakeScenario) {
			s.addRuns(t, core.RunTriggerWake, 20, false)
			s.addRuns(t, core.RunTriggerAutomation, 1, false)
		}, false},
		{"a steer from the user ends it", func(t *testing.T, s *wakeScenario) {
			ids := s.addRuns(t, core.RunTriggerWake, 20, false)
			now := time.Now().UTC()
			steer := core.SessionInput{ID: "input_steer", SessionID: s.session.ID, TargetRunID: ids[19], Mode: core.BusyInputModeSteer, Status: core.SessionInputStatusConsumed, Text: "also check the logs", ConsumedRunID: ids[19], ConsumedAt: &now, CreatedAt: now, UpdatedAt: now}
			if err := s.db.CreateSessionInput(ctx, steer); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWakeScenario(t)
			s.userRun(t)
			s.starter.wait(t)
			tc.setup(t, s)
			before := len(s.runs(t))

			s.finishedTask(t, "task_a")
			if err := s.app.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			s.starter.wait(t)

			if woke := len(s.runs(t)) > before; woke != tc.wakes {
				t.Fatalf("woke = %v, want %v", woke, tc.wakes)
			}
		})
	}
}

func TestWakeChainNoticeReachesTheChatPastTheNewestRuns(t *testing.T) {
	t.Parallel()
	s := newWakeScenario(t)
	s.addRuns(t, core.RunTriggerWake, 25, true)

	taskID := s.runTask(t, "true")

	var notices []core.ClientDelivery
	for deadline := time.Now().Add(10 * time.Second); len(notices) == 0; time.Sleep(10 * time.Millisecond) {
		notices = s.notices(t)
		if time.Now().After(deadline) {
			t.Fatal("no notice")
		}
	}
	if len(notices) != 1 || notices[0].ExternalKey != "telegram:42" || string(notices[0].Address) != string(telegramAddress) || !strings.Contains(notices[0].Summary, taskID) {
		t.Fatalf("notices = %+v", notices)
	}
}

func TestOnlyClientsThatFetchDeliveriesGetARunDelivery(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	ctx := context.Background()
	first := permissionSession(t, db, "session_terminal", t.TempDir(), core.PermissionModeDefault, "")
	second := permissionSession(t, db, "session_chat", t.TempDir(), core.PermissionModeDefault, "")
	terminal, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: first.ID, Client: "terminal", ExternalKey: "local", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := app.AcceptRun(ctx, core.HandleMessageInput{SessionID: second.ID, Client: "telegram", ExternalKey: "42", DeliveryAddress: telegramAddress, ClientCapabilities: chatCapabilities, Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		runID string
		want  int
	}{{terminal.Run.ID, 0}, {chat.Run.ID, 1}} {
		deliveries, err := db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{RunID: tc.runID})
		if err != nil || len(deliveries) != tc.want {
			t.Fatalf("run %s: deliveries = %+v, %v; want %d", tc.runID, deliveries, err, tc.want)
		}
	}
}
