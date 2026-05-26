package core_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func TestSessionSearchToolGroupsMatchesBySession(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newMemoryTestCore(t)
	defer cleanup()

	session := saveMemoryTestSession(t, sqliteStore, "session_alpha", "MCP work", "/tmp/project")
	saveMemoryTestMessage(t, sqliteStore, core.Message{
		ID:        "msg_1",
		SessionID: session.ID,
		Role:      core.MessageRoleUser,
		Content:   "We decided to test Context7 MCP first.",
		CreatedAt: time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC),
	})

	registry := tools.NewRegistry(core.MemoryToolExecutors(app)...)
	result, err := registry.Execute(ctx, "session_search", tools.Call{
		Args: json.RawMessage(`{"query":"Context7 MCP","limit":5}`),
	})
	if err != nil {
		t.Fatalf("session_search execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("session_search returned error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "MCP work") || !strings.Contains(result.Content, "Context7") {
		t.Fatalf("session_search content missing session/snippet:\n%s", result.Content)
	}
	report, ok := result.Metadata.(core.SessionSearchReport)
	if !ok {
		t.Fatalf("metadata type = %T, want core.SessionSearchReport", result.Metadata)
	}
	if len(report.Sessions) != 1 || report.Sessions[0].Session.ID != session.ID {
		t.Fatalf("search report sessions = %#v", report.Sessions)
	}
}

func TestMemoryToolRequiresApprovalAndFeedsPromptContext(t *testing.T) {
	ctx := context.Background()
	app, _, cleanup := newMemoryTestCore(t)
	defer cleanup()

	registry := tools.NewRegistry(core.MemoryToolExecutors(app)...)
	addArgs := json.RawMessage(`{"action":"add","scope":"user","content":"Пользователь предпочитает короткие ответы на русском."}`)
	pending, err := registry.Execute(ctx, "memory", tools.Call{Args: addArgs})
	if err != nil {
		t.Fatalf("memory add pending: %v", err)
	}
	if pending.Approval == nil {
		t.Fatalf("memory add without approval returned no approval request: %#v", pending)
	}

	added, err := registry.Execute(ctx, "memory", tools.Call{Args: addArgs, Approved: true})
	if err != nil {
		t.Fatalf("memory approved add: %v", err)
	}
	if added.IsError {
		t.Fatalf("memory approved add returned error: %s", added.Content)
	}

	listed, err := registry.Execute(ctx, "memory", tools.Call{Args: json.RawMessage(`{"action":"list"}`)})
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if !strings.Contains(listed.Content, "короткие ответы") {
		t.Fatalf("memory list missing entry:\n%s", listed.Content)
	}

	prompt := app.MemoryPromptContext(ctx, "")
	if !strings.Contains(prompt, "Memory:") || !strings.Contains(prompt, "короткие ответы") {
		t.Fatalf("MemoryPromptContext missing entry:\n%s", prompt)
	}
}

func newMemoryTestCore(t *testing.T) (*core.Core, *store.SQLiteStore, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	app := core.New(sqliteStore).WithIDGenerator(func(prefix string) string {
		return prefix + "_test"
	})
	return app, sqliteStore, func() { _ = sqliteStore.Close() }
}

func saveMemoryTestSession(t *testing.T, sqliteStore *store.SQLiteStore, id string, title string, workingDir string) core.Session {
	t.Helper()
	session := core.Session{
		ID:             id,
		Title:          title,
		Kind:           core.SessionKindAssistant,
		RuntimeID:      core.SessionRuntimeMatrixClaw,
		WorkingDir:     workingDir,
		PermissionMode: core.PermissionModeDefault,
		Status:         core.SessionStatusActive,
		CreatedAt:      time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC),
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("save session: %v", err)
	}
	return session
}

func saveMemoryTestMessage(t *testing.T, sqliteStore *store.SQLiteStore, message core.Message) {
	t.Helper()
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.CreatedAt
	}
	if err := sqliteStore.SaveMessage(context.Background(), message); err != nil {
		t.Fatalf("save message: %v", err)
	}
}
