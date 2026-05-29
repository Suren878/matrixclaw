package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestContextClearedMessageContentResetsEffectiveMessages(t *testing.T) {
	ctx := context.Background()
	app, sqliteStore, cleanup := newMemoryTestCore(t)
	defer cleanup()

	session := saveMemoryTestSession(t, sqliteStore, "session_context_clear", "Context clear", "/tmp/project")
	saveMemoryTestMessage(t, sqliteStore, core.Message{
		ID:        "msg_old",
		SessionID: session.ID,
		Role:      core.MessageRoleUser,
		Content:   "old context that should no longer be sent",
		CreatedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	})
	if _, err := app.CreateSystemMessage(ctx, session.ID, core.ContextClearedMessageContent()); err != nil {
		t.Fatalf("CreateSystemMessage: %v", err)
	}

	report, err := app.SessionContext(ctx, session.ID)
	if err != nil {
		t.Fatalf("SessionContext: %v", err)
	}
	if report.MessageCount != 0 {
		t.Fatalf("MessageCount = %d, want 0 after clear marker", report.MessageCount)
	}
	if !hasContextBlock(report, core.ContextBlockClearMarker) {
		t.Fatalf("ContextReport missing clear marker block: %#v", report.Blocks)
	}
	if hasContextBlock(report, core.ContextBlockCompactSummary) {
		t.Fatalf("ContextReport has compact summary block after clear: %#v", report.Blocks)
	}
}

func hasContextBlock(report core.ContextReport, kind core.ContextBlockKind) bool {
	for _, block := range report.Blocks {
		if block.Kind == kind {
			return true
		}
	}
	return false
}
