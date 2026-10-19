package core_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type recoveryExternalRuntime struct {
	mu    sync.Mutex
	input string
}

type interruptibleExternalRuntime struct {
	started    chan struct{}
	once       sync.Once
	mu         sync.Mutex
	interrupts int
}

func (r *interruptibleExternalRuntime) ID() string                      { return "recovery-external" }
func (r *interruptibleExternalRuntime) DisplayName() string             { return "Interruptible External" }
func (r *interruptibleExternalRuntime) Models(context.Context) []string { return nil }

func (r *interruptibleExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}

func (r *interruptibleExternalRuntime) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{AgentID: r.ID(), ExternalThreadID: "thread-recovery"}, nil
}

func (r *interruptibleExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}

func (r *interruptibleExternalRuntime) Send(_ context.Context, _ externalagents.ExternalSession, _ externalagents.Input) (<-chan externalagents.Event, error) {
	events := make(chan externalagents.Event, 3)
	events <- externalagents.Event{Kind: externalagents.EventMessageDelta, Text: "external partial before restart", At: time.Now().UTC()}
	events <- externalagents.Event{Kind: externalagents.EventToolStarted, ItemID: "external_tool_in_flight", ToolName: "shell", ToolInput: "touch marker", At: time.Now().UTC()}
	r.once.Do(func() { close(r.started) })
	return events, nil
}

func (r *interruptibleExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	r.mu.Lock()
	r.interrupts++
	r.mu.Unlock()
	return nil
}

func (r *interruptibleExternalRuntime) Close() error { return nil }

func (r *recoveryExternalRuntime) ID() string                      { return "recovery-external" }
func (r *recoveryExternalRuntime) DisplayName() string             { return "Recovery External" }
func (r *recoveryExternalRuntime) Models(context.Context) []string { return nil }

func (r *recoveryExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}

func (r *recoveryExternalRuntime) StartSession(context.Context, externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	return externalagents.ExternalSession{AgentID: r.ID(), ExternalThreadID: "thread-recovery"}, nil
}

func (r *recoveryExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}

func (r *recoveryExternalRuntime) Send(_ context.Context, _ externalagents.ExternalSession, input externalagents.Input) (<-chan externalagents.Event, error) {
	r.mu.Lock()
	r.input = input.Text
	r.mu.Unlock()
	events := make(chan externalagents.Event, 2)
	events <- externalagents.Event{Kind: externalagents.EventMessageDelta, Text: "external recovered", At: time.Now().UTC()}
	events <- externalagents.Event{Kind: externalagents.EventTurnCompleted, At: time.Now().UTC()}
	close(events)
	return events, nil
}

func (r *recoveryExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}

func (r *recoveryExternalRuntime) Close() error { return nil }

func (r *recoveryExternalRuntime) receivedInput() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.input
}

func TestRecoverExternalAgentContinuesExistingSession(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	runtime := &recoveryExternalRuntime{}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatal(err)
	}
	app := core.New(core.Config{Store: sqliteStore, ExternalAgents: registry, ExternalAgentStore: sqliteStore})

	session, run := saveCrashRecoveryRun(t, sqliteStore, "external", core.RunStatusRunning, false)
	session.Kind = core.SessionKindExternalAgent
	session.ExternalAgentID = runtime.ID()
	if err := sqliteStore.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.SaveExternalAgentSession(context.Background(), externalagents.SessionAttachment{
		SessionID: session.ID, AgentID: runtime.ID(), ExternalThreadID: "thread-recovery",
		CreatedAt: run.StartedAt, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}
	partial := transcript.Message{
		ID: "msg_external_partial", SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleAssistant, Content: "external partial",
		Parts:     transcript.NormalizeMessageParts("external partial", nil),
		CreatedAt: run.StartedAt.Add(time.Second), UpdatedAt: run.StartedAt.Add(time.Second),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, partial)

	if err := app.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	input := runtime.receivedInput()
	if !strings.Contains(input, "Continue the existing task") || !strings.Contains(input, "Original task for reference") {
		t.Fatalf("external recovery input = %q", input)
	}
}

func TestGracefulExecutorStopRecoversExternalSessionWithoutNativeToolReplay(t *testing.T) {
	t.Parallel()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	interruptedRuntime := &interruptibleExternalRuntime{started: make(chan struct{})}
	registry, err := externalagents.NewRegistry(interruptedRuntime)
	if err != nil {
		t.Fatal(err)
	}
	executionCtx, cancelExecution := context.WithCancel(context.Background())
	app := core.New(core.Config{Store: sqliteStore, ExternalAgents: registry, ExternalAgentStore: sqliteStore, Lifetime: executionCtx})

	session, run := saveCrashRecoveryRun(t, sqliteStore, "graceful_external", core.RunStatusAccepted, false)
	session.Kind = core.SessionKindExternalAgent
	session.ExternalAgentID = interruptedRuntime.ID()
	if err := sqliteStore.UpdateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.SaveExternalAgentSession(context.Background(), externalagents.SessionAttachment{
		SessionID: session.ID, AgentID: interruptedRuntime.ID(), ExternalThreadID: "thread-recovery",
		CreatedAt: run.StartedAt, UpdatedAt: run.UpdatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	executionDone := make(chan error, 1)
	go func() { executionDone <- app.ExecuteRun(executionCtx, run.ID) }()
	waitRecoverySignal(t, interruptedRuntime.started, "external generation start")
	// Let Core consume both buffered deltas before stopping its executor.
	time.Sleep(20 * time.Millisecond)
	cancelExecution()
	if err := waitRecoveryError(t, executionDone, "external executor shutdown"); err != nil {
		t.Fatalf("external ExecuteRun after shutdown: %v", err)
	}
	assertRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusRunning)

	recoveredRuntime := &recoveryExternalRuntime{}
	recoveredRegistry, err := externalagents.NewRegistry(recoveredRuntime)
	if err != nil {
		t.Fatal(err)
	}
	restarted := core.New(core.Config{Store: sqliteStore, ExternalAgents: recoveredRegistry, ExternalAgentStore: sqliteStore})
	if err := restarted.Recover(context.Background()); err != nil {
		t.Fatalf("Recover external after graceful stop: %v", err)
	}
	waitForRecoveryRunStatus(t, sqliteStore, run.ID, core.RunStatusCompleted)
	assertToolResultCount(t, sqliteStore, session.ID, "external_tool_in_flight", 0)
	if !strings.Contains(recoveredRuntime.receivedInput(), "Continue the existing task") {
		t.Fatalf("external runtime did not receive a continuation prompt: %q", recoveredRuntime.receivedInput())
	}
}
