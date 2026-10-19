package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

type recoveryRuntime struct {
	mu       sync.Mutex
	requests []providers.Request
	text     string
}

type interruptibleRecoveryRuntime struct {
	started chan struct{}
	once    sync.Once
}

func (r *interruptibleRecoveryRuntime) Generate(ctx context.Context, _ providers.Request) (providers.Response, error) {
	if err := providers.StreamText(ctx, "partial before graceful restart"); err != nil {
		return providers.Response{}, err
	}
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	return providers.Response{}, ctx.Err()
}

func (r *recoveryRuntime) Generate(_ context.Context, request providers.Request) (providers.Response, error) {
	r.mu.Lock()
	r.requests = append(r.requests, request)
	r.mu.Unlock()
	return providers.Response{Text: r.text, Provider: "recovery-test", Model: "test-model"}, nil
}

func (r *recoveryRuntime) lastRequest() providers.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return providers.Request{}
	}
	return r.requests[len(r.requests)-1]
}

func (r *recoveryRuntime) requestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

type recoveryLLMs struct {
	runtime providers.Runtime
}

func (r recoveryLLMs) ActiveSelection() (string, string) { return "recovery-test", "test-model" }
func (r recoveryLLMs) ContextWindow(string, string) int {
	return providers.DefaultFallbackContextWindowTokens
}
func (r recoveryLLMs) Providers() []core.SessionProviderOption {
	return []core.SessionProviderOption{{ID: "recovery-test", Configured: true, DefaultModel: "test-model"}}
}
func (r recoveryLLMs) Normalize(providerID string, modelID string) (core.SessionProviderOption, string, error) {
	return core.SessionProviderOption{ID: textutil.FirstNonEmpty(providerID, "recovery-test"), Configured: true, DefaultModel: "test-model"}, textutil.FirstNonEmpty(modelID, "test-model"), nil
}
func (r recoveryLLMs) Models(context.Context, string) ([]string, error) {
	return []string{"test-model"}, nil
}
func (r recoveryLLMs) Resolve(context.Context, string, string) (providers.Runtime, core.SessionProviderOption, string, error) {
	return r.runtime, core.SessionProviderOption{ID: "recovery-test", Configured: true, DefaultModel: "test-model"}, "test-model", nil
}

type recordingRunStarter struct {
	mu  sync.Mutex
	ids []string
}

func (s *recordingRunStarter) StartRun(_ context.Context, runID string) error {
	s.mu.Lock()
	s.ids = append(s.ids, runID)
	s.mu.Unlock()
	return nil
}

func (s *recordingRunStarter) count(runID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, id := range s.ids {
		if id == runID {
			count++
		}
	}
	return count
}

type recoveryTool struct {
	spec  tools.Spec
	mu    sync.Mutex
	calls int
}

func (t *recoveryTool) Spec() tools.Spec { return t.spec }
func (t *recoveryTool) Execute(context.Context, tools.Call) (tools.Result, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return tools.Result{Content: "recovered tool result", Status: tools.ResultStatusSuccess}, nil
}
func (t *recoveryTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func recoveryToolSpec(id string, effect tools.Effect) tools.Spec {
	return tools.Spec{
		ID: id, Description: "recovery test tool", Effect: effect, Namespace: "test.recovery", Category: tools.CategoryFilesystem,
		InputJSONSchema: json.RawMessage(`{"type":"object"}`),
	}
}

// recoverRun recovers the daemon's active runs and executes run, as the run
// starter of a test that only records starts does not.
func recoverRun(t *testing.T, app *core.Core, runID string) {
	t.Helper()
	if err := app.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if err := app.ExecuteRun(context.Background(), runID); err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
}

func newCrashRecoveryCore(t *testing.T) (*core.Core, *store.SQLiteStore, func()) {
	t.Helper()
	sqliteStore, cleanup := newCrashRecoveryStore(t)
	return core.New(core.Config{Store: sqliteStore}), sqliteStore, cleanup
}

func newCrashRecoveryStore(t *testing.T) (*store.SQLiteStore, func()) {
	t.Helper()
	sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	return sqliteStore, func() { _ = sqliteStore.Close() }
}

func saveCrashRecoveryRun(t *testing.T, sqliteStore *store.SQLiteStore, suffix string, status core.RunStatus, hidden bool) (core.Session, core.Run) {
	t.Helper()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: "session_" + suffix, Title: suffix, Kind: core.SessionKindAssistant, Hidden: hidden,
		ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeDefault,
		Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if hidden {
		session.ParentSessionID = "session_parent"
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	run := core.Run{
		ID: "run_" + suffix, SessionID: session.ID, UserMessageID: "msg_user_" + suffix,
		Status: status, StartedAt: now, UpdatedAt: now,
	}
	user := transcript.Message{
		ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID,
		Role: transcript.MessageRoleUser, Content: "original task " + suffix,
		Parts:     transcript.NormalizeMessageParts("original task "+suffix, nil),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := sqliteStore.AcceptMessage(context.Background(), user, run); err != nil {
		t.Fatal(err)
	}
	return session, run
}

func saveInterruptedToolCall(t *testing.T, sqliteStore *store.SQLiteStore, run core.Run, id string, name string) {
	t.Helper()
	saveInterruptedToolCallWithInput(t, sqliteStore, run, id, name, `{}`)
}

func saveInterruptedToolCallWithInput(t *testing.T, sqliteStore *store.SQLiteStore, run core.Run, id string, name string, input string) {
	t.Helper()
	message := transcript.Message{
		ID: id, SessionID: run.SessionID, RunID: run.ID, Role: transcript.MessageRoleAssistant,
		Parts:     []transcript.MessagePart{{Kind: transcript.MessagePartKindToolCall, ToolCall: &transcript.ToolCallPart{ID: id, Name: name, Input: input}}},
		CreatedAt: run.StartedAt.Add(time.Second), UpdatedAt: run.StartedAt.Add(time.Second),
	}
	saveRunRecoveryTestMessage(t, sqliteStore, message)
}

func waitForRecoveryRunStatus(t *testing.T, sqliteStore *store.SQLiteStore, runID string, want core.RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := sqliteStore.GetRun(context.Background(), runID)
		if err == nil && run.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assertRecoveryRunStatus(t, sqliteStore, runID, want)
}

func waitForRecoveryCheckpointGone(t *testing.T, sqliteStore *store.SQLiteStore, runID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, err := sqliteStore.GetRunCheckpoint(context.Background(), runID)
		if errors.Is(err, core.ErrNotFound) {
			return
		}
		if err != nil {
			t.Fatalf("GetRunCheckpoint: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err := sqliteStore.GetRunCheckpoint(context.Background(), runID)
	t.Fatalf("terminal run checkpoint error = %v, want ErrNotFound", err)
}

func assertRecoveryRunStatus(t *testing.T, sqliteStore *store.SQLiteStore, runID string, want core.RunStatus) {
	t.Helper()
	run, err := sqliteStore.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != want {
		t.Fatalf("run %s status = %q (%s), want %q", runID, run.Status, run.Error, want)
	}
}

func assertToolResultCount(t *testing.T, sqliteStore *store.SQLiteStore, sessionID string, toolCallID string, want int) {
	t.Helper()
	messages, err := sqliteStore.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.ToolResult != nil && part.ToolResult.ToolCallID == toolCallID {
				count++
			}
		}
	}
	if count != want {
		t.Fatalf("tool result %s count = %d, want %d", toolCallID, count, want)
	}
}

func messageHasRecoveryFinish(messages []transcript.Message, messageID string) bool {
	for _, message := range messages {
		if message.ID != messageID {
			continue
		}
		for _, part := range message.Parts {
			if part.Finish != nil && part.Finish.Reason == "daemon_restart" {
				return true
			}
		}
	}
	return false
}

func messageHasFinishPart(message transcript.Message, reason string) bool {
	for _, part := range message.Parts {
		if part.Finish != nil && part.Finish.Reason == reason {
			return true
		}
	}
	return false
}

func hasAssistantContent(messages []transcript.Message, runID string, content string) bool {
	for _, message := range messages {
		if message.RunID == runID && message.Role == transcript.MessageRoleAssistant && message.Content == content {
			return true
		}
	}
	return false
}

func hasRecoveryFinishedAssistant(messages []transcript.Message, runID string, content string) bool {
	for _, message := range messages {
		if message.RunID != runID || message.Role != transcript.MessageRoleAssistant || message.Content == "" || !strings.HasPrefix(content, message.Content) {
			continue
		}
		for _, part := range message.Parts {
			if part.Finish != nil && part.Finish.Reason == "daemon_restart" {
				return true
			}
		}
	}
	return false
}

func waitRecoverySignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitRecoveryError(t *testing.T, result <-chan error, label string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
		return nil
	}
}
