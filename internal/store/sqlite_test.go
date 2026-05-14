package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
)

func openTestSQLite(t *testing.T, dbPath string) *SQLiteStore {
	t.Helper()

	sqliteStore, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	return sqliteStore
}

func createTestSession(t *testing.T, ctx context.Context, sqliteStore *SQLiteStore, session core.Session) core.Session {
	t.Helper()

	now := time.Now().UTC()
	if session.Title == "" {
		session.Title = "Docs"
	}
	if session.Status == "" {
		session.Status = core.SessionStatusActive
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}
	if err := sqliteStore.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return session
}

func TestSQLiteStoreSessionLifecyclePersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")

	first := openTestSQLite(t, dbPath)
	session := createTestSession(t, ctx, first, core.Session{
		ID:             "session_1",
		Kind:           core.SessionKindExternalAgent,
		RuntimeID:      core.SessionRuntimeExternalAgent,
		WorkingDir:     "/workspace/matrixclaw",
		PermissionMode: core.PermissionModeAcceptEdits,
	})
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second := openTestSQLite(t, dbPath)

	got, err := second.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.ID != session.ID {
		t.Fatalf("GetSession().ID = %q, want %q", got.ID, session.ID)
	}
	if got.Kind != core.SessionKindExternalAgent {
		t.Fatalf("GetSession().Kind = %q, want %q", got.Kind, core.SessionKindExternalAgent)
	}
	if got.RuntimeID != core.SessionRuntimeExternalAgent {
		t.Fatalf("GetSession().RuntimeID = %q, want %q", got.RuntimeID, core.SessionRuntimeExternalAgent)
	}
	if got.WorkingDir != session.WorkingDir {
		t.Fatalf("GetSession().WorkingDir = %q, want %q", got.WorkingDir, session.WorkingDir)
	}
	if got.PermissionMode != core.PermissionModeAcceptEdits {
		t.Fatalf("GetSession().PermissionMode = %q, want %q", got.PermissionMode, core.PermissionModeAcceptEdits)
	}

	session.Title = "Renamed"
	session.Kind = core.SessionKindAssistant
	session.RuntimeID = core.SessionRuntimeMatrixClaw
	session.WorkingDir = "/workspace/project"
	session.PermissionMode = core.PermissionModeFullAuto
	session.UpdatedAt = session.UpdatedAt.Add(time.Minute)
	if err := second.UpdateSession(ctx, session); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	if err := second.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened := openTestSQLite(t, dbPath)
	defer reopened.Close()

	got, err = reopened.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got.Title != "Renamed" {
		t.Fatalf("GetSession().Title = %q, want %q", got.Title, "Renamed")
	}
	if got.Kind != core.SessionKindAssistant {
		t.Fatalf("GetSession().Kind = %q, want %q", got.Kind, core.SessionKindAssistant)
	}
	if got.RuntimeID != core.SessionRuntimeMatrixClaw {
		t.Fatalf("GetSession().RuntimeID = %q, want %q", got.RuntimeID, core.SessionRuntimeMatrixClaw)
	}
	if got.WorkingDir != "/workspace/project" {
		t.Fatalf("GetSession().WorkingDir = %q, want %q", got.WorkingDir, "/workspace/project")
	}
	if got.PermissionMode != core.PermissionModeFullAuto {
		t.Fatalf("GetSession().PermissionMode = %q, want %q", got.PermissionMode, core.PermissionModeFullAuto)
	}

	if err := reopened.DeleteSession(ctx, session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := reopened.GetSession(ctx, session.ID); err != core.ErrNotFound {
		t.Fatalf("GetSession() after delete error = %v, want ErrNotFound", err)
	}
}

func TestNewSQLiteCreatesParentDirectory(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "nested", "data", "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	defer sqliteStore.Close()
}

func TestCheckSQLiteReportsCanonicalSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	if err := sqliteStore.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	diag, err := CheckSQLite(dbPath)
	if err != nil {
		t.Fatalf("CheckSQLite() error = %v", err)
	}
	if !diag.Exists {
		t.Fatal("diag.Exists = false, want true")
	}
	if !diag.SchemaReady {
		t.Fatal("diag.SchemaReady = false, want true")
	}
}

func TestCanonicalSchemaOmitsDeadTables(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	defer sqliteStore.Close()

	for _, table := range []string{"usage", "tasks", "plan_steps", "claw_schema_migrations"} {
		var exists int
		if err := sqliteStore.db.QueryRow(`SELECT COUNT(1) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&exists); err != nil {
			t.Fatalf("query table %s error = %v", table, err)
		}
		if exists != 0 {
			t.Fatalf("table %s exists = %d, want 0", table, exists)
		}
	}
}

func TestCanonicalClientDeliveriesSchemaOmitsPayloadAndDefaults(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	defer sqliteStore.Close()

	rows, err := sqliteStore.db.Query(`PRAGMA table_info(client_deliveries)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info(client_deliveries) error = %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan client_deliveries column error = %v", err)
		}
		if name == "payload_json" {
			t.Fatal("client_deliveries.payload_json exists, want removed")
		}
		if defaultValue != nil {
			t.Fatalf("client_deliveries.%s default = %v, want none", name, defaultValue)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate client_deliveries columns error = %v", err)
	}
}

func TestUnmarshalMessagePartsDoesNotFallbackToContent(t *testing.T) {
	t.Parallel()

	if got := unmarshalMessageParts(""); len(got) != 0 {
		t.Fatalf("unmarshalMessageParts(empty) = %#v, want empty", got)
	}
	if got := unmarshalMessageParts("{"); len(got) != 0 {
		t.Fatalf("unmarshalMessageParts(invalid) = %#v, want empty", got)
	}
	if got := unmarshalMessageParts("[]"); len(got) != 0 {
		t.Fatalf("unmarshalMessageParts(empty json) = %#v, want empty", got)
	}
}

func TestSQLiteStorePersistsMessagePartsAndListsWithoutLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	defer sqliteStore.Close()

	session := createTestSession(t, ctx, sqliteStore, core.Session{ID: "session_1"})

	now := time.Now().UTC()
	messages := []core.Message{
		{
			ID:        "msg_1",
			SessionID: session.ID,
			Role:      core.MessageRoleAssistant,
			Content:   "first",
			CreatedAt: now,
		},
		{
			ID:        "msg_2",
			SessionID: session.ID,
			RunID:     "run_1",
			Role:      core.MessageRoleAssistant,
			Content:   "done",
			Parts: []core.MessagePart{
				{
					Kind: core.MessagePartKindText,
					Text: &core.TextPart{Text: "done"},
				},
				{
					Kind: core.MessagePartKindFinish,
					Finish: &core.FinishPart{
						Reason:  "stop",
						Message: "completed",
					},
				},
			},
			CreatedAt: now.Add(time.Second),
		},
		{
			ID:        "msg_3",
			SessionID: session.ID,
			Role:      core.MessageRoleAssistant,
			Content:   "last",
			CreatedAt: now.Add(2 * time.Second),
		},
	}
	for i, message := range messages {
		if err := sqliteStore.SaveMessage(ctx, message); err != nil {
			t.Fatalf("SaveMessage(%d) error = %v", i, err)
		}
	}

	got, err := sqliteStore.ListMessages(ctx, session.ID, 0)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(ListMessages(limit=0)) = %d, want 3", len(got))
	}
	if got[1].ID != "msg_2" {
		t.Fatalf("ListMessages()[1].ID = %q, want %q", got[1].ID, "msg_2")
	}
	if len(got[1].Parts) != 2 {
		t.Fatalf("len(message.Parts) = %d, want 2", len(got[1].Parts))
	}
	if got[1].Parts[1].Finish == nil || got[1].Parts[1].Finish.Message != "completed" {
		t.Fatalf("finish part mismatch: %#v", got[1].Parts[1])
	}
}

func TestSQLiteStorePersistsApprovals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	defer sqliteStore.Close()

	session := createTestSession(t, ctx, sqliteStore, core.Session{ID: "session_1"})

	approval := core.Approval{
		ID:          "approval_1",
		SessionID:   session.ID,
		RunID:       "run_1",
		ToolCallRef: "tool_1",
		ToolName:    "write",
		Description: "Create or replace file",
		Action:      "write",
		Params:      json.RawMessage(`{"file_path":"notes.txt","content":"hello"}`),
		Path:        "/tmp/notes.txt",
		State:       core.ApprovalStatePending,
		RequestedAt: time.Now().UTC(),
	}
	if err := sqliteStore.CreateApproval(ctx, approval); err != nil {
		t.Fatalf("CreateApproval() error = %v", err)
	}

	got, err := sqliteStore.GetApproval(ctx, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if got.ToolName != approval.ToolName || got.Path != approval.Path {
		t.Fatalf("approval mismatch: got=%#v want=%#v", got, approval)
	}
	if string(got.Params) != string(approval.Params) {
		t.Fatalf("approval params = %s, want %s", string(got.Params), string(approval.Params))
	}

	decidedAt := time.Now().UTC()
	got.State = core.ApprovalStateApproved
	got.DecidedAt = &decidedAt
	if err := sqliteStore.UpdateApproval(ctx, got); err != nil {
		t.Fatalf("UpdateApproval() error = %v", err)
	}

	approvals, err := sqliteStore.ListApprovals(ctx, session.ID, core.ApprovalStateApproved)
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if len(approvals) != 1 || approvals[0].State != core.ApprovalStateApproved {
		t.Fatalf("approved approvals mismatch: %#v", approvals)
	}
}

func TestSQLiteStorePersistsFileSnapshots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore := openTestSQLite(t, dbPath)
	defer sqliteStore.Close()

	session := createTestSession(t, ctx, sqliteStore, core.Session{ID: "session_1"})

	first, err := sqliteStore.CreateFileSnapshot(ctx, core.FileSnapshot{
		ID:        "file_1",
		SessionID: session.ID,
		Path:      "/tmp/notes.txt",
		Content:   "one",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateFileSnapshot(first) error = %v", err)
	}
	second, err := sqliteStore.CreateFileSnapshot(ctx, core.FileSnapshot{
		ID:        "file_2",
		SessionID: session.ID,
		Path:      "/tmp/notes.txt",
		Content:   "two",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateFileSnapshot(second) error = %v", err)
	}

	if first.Version != 0 || second.Version != 1 {
		t.Fatalf("snapshot versions = (%d,%d), want (0,1)", first.Version, second.Version)
	}

	files, err := sqliteStore.ListFileSnapshots(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListFileSnapshots() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("len(ListFileSnapshots()) = %d, want 2", len(files))
	}
	if files[0].Version != 0 || files[1].Version != 1 {
		t.Fatalf("persisted versions = %#v, want ascending versions", files)
	}
}
