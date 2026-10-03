package core_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type burstExternalRuntime struct {
	messageDeltas int
	toolDeltas    int
}

func (r *burstExternalRuntime) ID() string          { return "burst-test" }
func (r *burstExternalRuntime) DisplayName() string { return "Burst Test" }
func (r *burstExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}
func (r *burstExternalRuntime) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{AgentID: r.ID(), ExternalThreadID: "thread-1"}, nil
}
func (r *burstExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}
func (r *burstExternalRuntime) Send(context.Context, externalagents.ExternalSession, externalagents.Input) (<-chan externalagents.Event, error) {
	events := make(chan externalagents.Event, 64)
	go func() {
		defer close(events)
		now := time.Now().UTC()
		events <- externalagents.Event{Kind: externalagents.EventTurnStarted, AgentID: r.ID(), ExternalThreadID: "thread-1", ExternalTurnID: "turn-1", At: now}
		for i := 0; i < r.toolDeltas; i++ {
			events <- externalagents.Event{Kind: externalagents.EventToolOutputDelta, AgentID: r.ID(), ExternalThreadID: "thread-1", ExternalTurnID: "turn-1", ItemID: "tool-1", ToolName: "shell", Text: strings.Repeat("z", 4096), At: now}
		}
		for i := 0; i < r.messageDeltas; i++ {
			events <- externalagents.Event{Kind: externalagents.EventMessageDelta, AgentID: r.ID(), ExternalThreadID: "thread-1", ExternalTurnID: "turn-1", Text: "x", At: now}
		}
		events <- externalagents.Event{Kind: externalagents.EventTurnCompleted, AgentID: r.ID(), ExternalThreadID: "thread-1", ExternalTurnID: "turn-1", At: now}
	}()
	return events, nil
}
func (r *burstExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}
func (r *burstExternalRuntime) Close() error { return nil }

func TestExternalAgentBurstPersistsCoalescedProgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	defer func() { _ = sqliteStore.Close() }()

	runtime := &burstExternalRuntime{messageDeltas: 4096, toolDeltas: 512}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	// A frozen clock keeps the whole burst inside one progress flush interval.
	now := time.Now().UTC()
	app := core.New(sqliteStore).WithExternalAgents(registry, sqliteStore).WithClock(func() time.Time { return now })
	session := core.Session{
		ID: "session-burst", Title: "Burst", Kind: core.SessionKindExternalAgent,
		RuntimeID: core.SessionRuntimeExternalAgent, Status: core.SessionStatusActive,
		WorkingDir: t.TempDir(), CreatedAt: now, UpdatedAt: now,
	}
	if err := sqliteStore.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sqliteStore.SaveExternalAgentSession(ctx, externalagents.SessionAttachment{
		SessionID: session.ID, AgentID: runtime.ID(), ExternalThreadID: "thread-1",
		Model: "test", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save external attachment: %v", err)
	}
	run := core.Run{
		ID: "run-burst", SessionID: session.ID, UserMessageID: "msg-user-burst",
		Status: core.RunStatusAccepted, StartedAt: now, UpdatedAt: now,
	}
	userMessage := transcript.Message{
		ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleUser, Content: "work", CreatedAt: now, UpdatedAt: now,
	}
	if err := sqliteStore.AcceptMessage(ctx, userMessage, run); err != nil {
		t.Fatalf("accept message: %v", err)
	}

	eventCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	liveEvents := app.SubscribeEvents(eventCtx, session.ID)
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("execute run: %v", err)
	}

	updatedEvents := 0
	drain := true
	for drain {
		select {
		case event := <-liveEvents:
			if event.Type == core.EventMessageUpdated && event.RunID == run.ID {
				updatedEvents++
			}
		default:
			drain = false
		}
	}
	if updatedEvents != 1 {
		t.Fatalf("message.updated events = %d, want the burst coalesced into the final write", updatedEvents)
	}

	gotRun, err := sqliteStore.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if gotRun.Status != core.RunStatusCompleted {
		t.Fatalf("run status = %q, want completed", gotRun.Status)
	}
	messages, err := sqliteStore.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	var assistant transcript.Message
	for _, message := range messages {
		if message.RunID == run.ID && message.Role == transcript.MessageRoleAssistant {
			assistant = message
		}
	}
	if len(assistant.Content) != runtime.messageDeltas {
		t.Fatalf("assistant content bytes = %d, want %d", len(assistant.Content), runtime.messageDeltas)
	}
	for _, part := range assistant.Parts {
		if part.ToolResult == nil {
			continue
		}
		if len(part.ToolResult.Content) > 64*1024 {
			t.Fatalf("persisted tool output bytes = %d, want at most 65536", len(part.ToolResult.Content))
		}
		if !strings.Contains(part.ToolResult.Content, "output truncated") {
			t.Fatalf("persisted tool output lacks truncation marker")
		}
	}
}

// hangingExternalRuntime streams one delta and then works until its turn's
// context stops.
type hangingExternalRuntime struct {
	burstExternalRuntime
	started     chan struct{}
	interrupted chan struct{}
}

func (r *hangingExternalRuntime) Send(ctx context.Context, _ externalagents.ExternalSession, _ externalagents.Input) (<-chan externalagents.Event, error) {
	events := make(chan externalagents.Event, 4)
	go func() {
		defer close(events)
		now := time.Now().UTC()
		events <- externalagents.Event{Kind: externalagents.EventMessageDelta, AgentID: r.ID(), Text: "partial", At: now}
		close(r.started)
		<-ctx.Done()
	}()
	return events, nil
}

func (r *hangingExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	close(r.interrupted)
	return nil
}

func TestCancelingAnExternalRunInterruptsItsAgentAndSealsTheReply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	runtime := &hangingExternalRuntime{started: make(chan struct{}), interrupted: make(chan struct{})}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatal(err)
	}
	app := core.New(db).WithExternalAgents(registry, db)
	now := time.Now().UTC()
	session := core.Session{ID: "session-hang", Title: "Hang", Kind: core.SessionKindExternalAgent, RuntimeID: core.SessionRuntimeExternalAgent, Status: core.SessionStatusActive, WorkingDir: t.TempDir(), CreatedAt: now, UpdatedAt: now}
	if err := db.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveExternalAgentSession(ctx, externalagents.SessionAttachment{SessionID: session.ID, AgentID: runtime.ID(), ExternalThreadID: "thread-1", Model: "test", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	run := core.Run{ID: "run-hang", SessionID: session.ID, UserMessageID: "msg-hang", Status: core.RunStatusAccepted, StartedAt: now, UpdatedAt: now}
	if err := db.AcceptMessage(ctx, transcript.Message{ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser, Content: "work", CreatedAt: now, UpdatedAt: now}, run); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- app.ExecuteRun(ctx, run.ID) }()
	<-runtime.started
	if _, err := app.CancelRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the canceled external run did not stop")
	}

	<-runtime.interrupted
	got, err := db.GetRun(ctx, run.ID)
	if err != nil || got.Status != core.RunStatusCanceled {
		t.Fatalf("run = %+v err = %v", got, err)
	}
	messages, err := db.ListRunMessages(ctx, session.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := messages[len(messages)-1]
	if last.Role != transcript.MessageRoleAssistant || last.Content != "partial" || !transcript.HasFinishReason(last, transcript.FinishReasonCanceled) {
		t.Fatalf("last message = %+v", last)
	}
}
