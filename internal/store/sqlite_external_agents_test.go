package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/externalagents"
)

func TestSQLiteStoreExternalAgentSessionLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openTestSQLite(t, filepath.Join(t.TempDir(), "matrixclaw.db"))
	session := createTestSession(t, ctx, sqliteStore, core.Session{ID: "session_external"})

	createdAt := time.Now().UTC().Add(-time.Minute)
	updatedAt := time.Now().UTC()
	attachment := externalagents.SessionAttachment{
		SessionID:         session.ID,
		AgentID:           "codex-app",
		ExternalThreadID:  "thread_123",
		ExternalSessionID: "session_abc",
		CWD:               "/workspace/project",
		Model:             "gpt-5.4",
		ApprovalPolicy:    "never",
		Sandbox:           "danger-full-access",
		MetadataJSON:      `{"mode":"app-server"}`,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}
	if err := sqliteStore.SaveExternalAgentSession(ctx, attachment); err != nil {
		t.Fatalf("SaveExternalAgentSession() error = %v", err)
	}

	got, err := sqliteStore.GetExternalAgentSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetExternalAgentSession() error = %v", err)
	}
	if got.AgentID != "codex-app" || got.ExternalThreadID != "thread_123" {
		t.Fatalf("unexpected attachment: %+v", got)
	}
	if got.MetadataJSON != attachment.MetadataJSON {
		t.Fatalf("MetadataJSON = %q, want %q", got.MetadataJSON, attachment.MetadataJSON)
	}
	if got.ApprovalPolicy != "never" || got.Sandbox != "danger-full-access" {
		t.Fatalf("codex policy = %q/%q, want never/danger-full-access", got.ApprovalPolicy, got.Sandbox)
	}

	attachment.ExternalThreadID = "thread_456"
	attachment.ApprovalPolicy = "on-request"
	attachment.Sandbox = "workspace-write"
	attachment.MetadataJSON = ""
	if err := sqliteStore.SaveExternalAgentSession(ctx, attachment); err != nil {
		t.Fatalf("SaveExternalAgentSession() update error = %v", err)
	}
	got, err = sqliteStore.GetExternalAgentSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetExternalAgentSession() after update error = %v", err)
	}
	if got.ExternalThreadID != "thread_456" {
		t.Fatalf("ExternalThreadID = %q, want thread_456", got.ExternalThreadID)
	}
	if got.ApprovalPolicy != "on-request" || got.Sandbox != "workspace-write" {
		t.Fatalf("updated policy = %q/%q, want on-request/workspace-write", got.ApprovalPolicy, got.Sandbox)
	}
	if got.MetadataJSON != "{}" {
		t.Fatalf("MetadataJSON default = %q, want {}", got.MetadataJSON)
	}

	if err := sqliteStore.DeleteExternalAgentSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteExternalAgentSession() error = %v", err)
	}
	if _, err := sqliteStore.GetExternalAgentSession(ctx, session.ID); err != core.ErrNotFound {
		t.Fatalf("GetExternalAgentSession() after delete error = %v, want ErrNotFound", err)
	}
}

func TestSQLiteStoreExternalAgentSessionCascadesWithSessionDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openTestSQLite(t, filepath.Join(t.TempDir(), "matrixclaw.db"))
	session := createTestSession(t, ctx, sqliteStore, core.Session{ID: "session_external"})

	if err := sqliteStore.SaveExternalAgentSession(ctx, externalagents.SessionAttachment{
		SessionID:        session.ID,
		AgentID:          "codex-app",
		ExternalThreadID: "thread_123",
	}); err != nil {
		t.Fatalf("SaveExternalAgentSession() error = %v", err)
	}
	if err := sqliteStore.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := sqliteStore.GetExternalAgentSession(ctx, session.ID); err != core.ErrNotFound {
		t.Fatalf("GetExternalAgentSession() after session delete error = %v, want ErrNotFound", err)
	}
}
