package core_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runInDir executes one run of a session working in dir and returns its
// first model request; replyOnce gives the run a reply-once delivery.
func runInDir(t *testing.T, app *core.Core, db *store.SQLiteStore, suffix string, dir string, replyOnce bool) providers.Request {
	t.Helper()
	ctx := context.Background()
	now := runRecoveryTestTime()
	session := core.Session{
		ID: "session_" + suffix, Title: suffix, Kind: core.SessionKindAssistant,
		WorkingDir: dir, ProviderID: "recovery-test", ModelID: "test-model", PermissionMode: core.PermissionModeAcceptEdits,
		Status: core.SessionStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	run := core.Run{ID: "run_" + suffix, SessionID: session.ID, UserMessageID: "msg_" + suffix, Client: "telegram", ExternalKey: "42", Status: core.RunStatusAccepted, StartedAt: now, UpdatedAt: now}
	user := transcript.Message{ID: run.UserMessageID, SessionID: session.ID, RunID: run.ID, Role: transcript.MessageRoleUser, Content: "hi", Parts: transcript.NormalizeMessageParts("hi", nil), CreatedAt: now, UpdatedAt: now}
	delivery := core.ClientDelivery{ID: "delivery_" + suffix, Type: core.ClientDeliveryTypeRun, Client: "telegram", ExternalKey: "42", SessionID: session.ID, RunID: run.ID, Address: json.RawMessage(`{"kind":"chat"}`), ReplyOnce: replyOnce, Status: core.ClientDeliveryStatusPending}
	if err := db.AcceptMessage(ctx, user, run, delivery); err != nil {
		t.Fatal(err)
	}
	var requests []providers.Request
	app.SetSessionLLMs(scriptedLLMs(&requests, providers.Response{Text: "Hello."}))
	if err := app.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	return requests[0]
}

func TestRunInAWorkingDirIsToldItsInstructionsAndState(t *testing.T) {
	t.Parallel()
	db, cleanup := newCrashRecoveryStore(t)
	defer cleanup()
	dir := t.TempDir()
	global := filepath.Join(t.TempDir(), "AGENTS.md")
	for path, content := range map[string]string{filepath.Join(dir, "AGENTS.md"): "Run make check before replying.", global: "Prefer tabs."} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	almaty := time.FixedZone("Almaty", 5*3600)
	app := core.New(core.Config{
		Store:             db,
		PromptEnvironment: core.PromptEnvironment{Timezone: almaty, UserInstructions: global},
		Clock:             func() time.Time { return time.Date(2026, 10, 18, 22, 0, 0, 0, time.UTC) },
	})
	app.SetTools(tools.NewRegistry(funcTool{spec: recoveryToolSpec("read", tools.EffectReadOnly)}))

	request := runInDir(t, app, db, "coding", dir, false)
	for _, want := range []string{"Run make check before replying.", "Prefer tabs.", dir} {
		if !strings.Contains(request.SystemPrompt, want) {
			t.Fatalf("system prompt lacks %q:\n%s", want, request.SystemPrompt)
		}
	}
	note := request.Messages[len(request.Messages)-1].Content
	for _, want := range []string{"2026-10-19", "accept_edits"} {
		if !strings.Contains(note, want) || strings.Contains(request.SystemPrompt, want) {
			t.Fatalf("%q belongs in the context note, not the system prompt; note:\n%s", want, note)
		}
	}
	if !strings.Contains(note, "Telegram") {
		t.Fatalf("the context note does not name the client:\n%s", note)
	}

	chat := runInDir(t, app, db, "guest", dir, true)
	if strings.Contains(chat.SystemPrompt, "Run make check before replying.") || strings.Contains(chat.SystemPrompt, "Prefer tabs.") {
		t.Fatalf("a reply-once run got the project instructions:\n%s", chat.SystemPrompt)
	}
}
