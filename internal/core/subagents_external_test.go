package core_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
)

// childExternalRuntime is an external runtime for subagents: it records the
// sessions it starts and answers every prompt at once. With hold set, starting
// a session signals entered and waits for hold to close.
type childExternalRuntime struct {
	id       string
	entered  chan struct{}
	hold     chan struct{}
	mu       sync.Mutex
	requests []externalagents.StartSessionRequest
}

func (r *childExternalRuntime) ID() string          { return r.id }
func (r *childExternalRuntime) DisplayName() string { return r.id }
func (r *childExternalRuntime) Available(context.Context) externalagents.Availability {
	return externalagents.Availability{Installed: true, Enabled: true}
}
func (r *childExternalRuntime) StartSession(_ context.Context, req externalagents.StartSessionRequest) (externalagents.ExternalSession, error) {
	if r.hold != nil {
		r.entered <- struct{}{}
		<-r.hold
	}
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	return externalagents.ExternalSession{AgentID: r.id, ExternalThreadID: "thread-child", ApprovalPolicy: req.ApprovalPolicy, Sandbox: req.Sandbox}, nil
}
func (r *childExternalRuntime) ResumeSession(_ context.Context, session externalagents.ExternalSession) (externalagents.ExternalSession, error) {
	return session, nil
}
func (r *childExternalRuntime) Send(context.Context, externalagents.ExternalSession, externalagents.Input) (<-chan externalagents.Event, error) {
	events := make(chan externalagents.Event, 2)
	events <- externalagents.Event{Kind: externalagents.EventMessageDelta, Text: "external child done", At: time.Now().UTC()}
	events <- externalagents.Event{Kind: externalagents.EventTurnCompleted, At: time.Now().UTC()}
	close(events)
	return events, nil
}
func (r *childExternalRuntime) Interrupt(context.Context, externalagents.ExternalSession) error {
	return nil
}
func (r *childExternalRuntime) Close() error { return nil }

func (r *childExternalRuntime) started() []externalagents.StartSessionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]externalagents.StartSessionRequest(nil), r.requests...)
}

func TestReadonlyExternalChildNeverAsksAndCannotWrite(t *testing.T) {
	t.Parallel()
	for _, runtimeID := range []string{"codex", "claude"} {
		t.Run(runtimeID, func(t *testing.T) {
			t.Parallel()
			app, db, cleanup := newCrashRecoveryCore(t)
			defer cleanup()
			runtime := &childExternalRuntime{id: runtimeID}
			registry, err := externalagents.NewRegistry(runtime)
			if err != nil {
				t.Fatal(err)
			}
			app.WithExternalAgents(registry, db).WithRunStarter(&recordingRunStarter{})
			session, run := saveCrashRecoveryRun(t, db, "readonly_"+runtimeID, core.RunStatusRunning, false)
			sessionIn(t, db, session, t.TempDir(), core.PermissionModeFullAuto)

			result, err := app.RunAgent(context.Background(), core.AgentInput{ParentSessionID: session.ID, ParentRunID: run.ID, ParentToolCallID: "call-review", Description: "Review", Prompt: "review the diff", Readonly: true, Runtime: runtimeID})
			if err != nil || result.Summary != "external child done" {
				t.Fatalf("result = %+v, %v", result, err)
			}

			started := runtime.started()
			if len(started) != 1 || started[0].ApprovalPolicy != "never" || started[0].Sandbox != "read-only" {
				t.Fatalf("started sessions = %+v", started)
			}
			if _, err := app.UpdateSessionPermissionMode(context.Background(), core.UpdateSessionPermissionModeInput{SessionID: result.Task.ChildSessionID, PermissionMode: core.PermissionModeFullAuto}); err != nil {
				t.Fatal(err)
			}
			attachment, err := db.GetExternalAgentSession(context.Background(), result.Task.ChildSessionID)
			if err != nil || attachment.ApprovalPolicy != "never" || attachment.Sandbox != "read-only" {
				t.Fatalf("attachment = %+v, %v", attachment, err)
			}
		})
	}
}

func TestASlowExternalChildStartDoesNotHoldItsParent(t *testing.T) {
	t.Parallel()
	app, db, cleanup := newCrashRecoveryCore(t)
	defer cleanup()
	runtime := &childExternalRuntime{id: "codex", entered: make(chan struct{}, 1), hold: make(chan struct{})}
	registry, err := externalagents.NewRegistry(runtime)
	if err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(func() { close(runtime.hold) })
	defer release()
	app.WithExternalAgents(registry, db).WithRunStarter(&recordingRunStarter{})
	session, run := saveCrashRecoveryRun(t, db, "slow_start", core.RunStatusRunning, false)
	sessionIn(t, db, session, t.TempDir(), core.PermissionModeFullAuto)
	done := make(chan error, 1)
	go func() {
		_, err := app.RunAgent(context.Background(), core.AgentInput{ParentSessionID: session.ID, ParentRunID: run.ID, ParentToolCallID: "call-codex", Description: "Fix", Prompt: "fix it", Runtime: "codex"})
		done <- err
	}()
	waitRecoverySignal(t, runtime.entered, "the child's session start")

	steered := make(chan error, 1)
	go func() {
		_, err := app.AcceptRun(context.Background(), core.HandleMessageInput{SessionID: session.ID, Text: "also check the tests", BusyMode: core.BusyInputModeSteer})
		steered <- err
	}()
	if err := waitRecoveryError(t, steered, "a steer while the child starts"); err != nil {
		t.Fatal(err)
	}

	release()
	if err := waitRecoveryError(t, done, "the child"); err != nil {
		t.Fatal(err)
	}
}
