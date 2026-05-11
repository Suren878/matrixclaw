package contracts

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

type StoreFactory func(t *testing.T) core.Store
type SessionStoreFactory func(t *testing.T) core.SessionStore
type BindingStoreFactory func(t *testing.T) BindingStoreContract
type DeliveryStoreFactory func(t *testing.T) core.DeliveryStore
type RunStoreFactory func(t *testing.T) RunStoreContract
type ApprovalStoreFactory func(t *testing.T) ApprovalStoreContract
type FileSnapshotStoreFactory func(t *testing.T) FileSnapshotStoreContract

type BindingStoreContract interface {
	core.SessionStore
	core.BindingStore
}

type RunStoreContract interface {
	core.SessionStore
	core.MessageStore
	core.RunStore
}

type ApprovalStoreContract interface {
	core.SessionStore
	core.ApprovalStore
}

type FileSnapshotStoreContract interface {
	core.SessionStore
	core.FileSnapshotStore
}

var (
	_ core.SessionStore      = (*store.SQLiteStore)(nil)
	_ core.BindingStore      = (*store.SQLiteStore)(nil)
	_ core.DeliveryStore     = (*store.SQLiteStore)(nil)
	_ core.MessageStore      = (*store.SQLiteStore)(nil)
	_ core.RunStore          = (*store.SQLiteStore)(nil)
	_ core.ApprovalStore     = (*store.SQLiteStore)(nil)
	_ core.FileSnapshotStore = (*store.SQLiteStore)(nil)
	_ core.Store             = (*store.SQLiteStore)(nil)
)

func RunStoreContractTests(t *testing.T, newStore StoreFactory) {
	t.Helper()

	RunSessionStoreContractTests(t, func(t *testing.T) core.SessionStore {
		t.Helper()
		return newStore(t)
	})
	RunBindingStoreContractTests(t, func(t *testing.T) BindingStoreContract {
		t.Helper()
		return newStore(t)
	})
	RunDeliveryStoreContractTests(t, func(t *testing.T) core.DeliveryStore {
		t.Helper()
		return newStore(t)
	})
	RunRunStoreContractTests(t, func(t *testing.T) RunStoreContract {
		t.Helper()
		return newStore(t)
	})
	RunApprovalStoreContractTests(t, func(t *testing.T) ApprovalStoreContract {
		t.Helper()
		return newStore(t)
	})
	RunFileSnapshotStoreContractTests(t, func(t *testing.T) FileSnapshotStoreContract {
		t.Helper()
		return newStore(t)
	})
}

func RunSessionStoreContractTests(t *testing.T, newStore SessionStoreFactory) {
	t.Helper()

	t.Run("sessions", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := newStore(t)
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

		session := core.Session{
			ID:         "session_docs",
			Title:      "Docs",
			WorkingDir: "/workspace/matrixclaw",
			Status:     core.SessionStatusActive,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := db.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}

		got, err := db.GetSession(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}
		if got.ID != session.ID {
			t.Fatalf("GetSession().ID = %q, want %q", got.ID, session.ID)
		}
		if got.WorkingDir != session.WorkingDir {
			t.Fatalf("GetSession().WorkingDir = %q, want %q", got.WorkingDir, session.WorkingDir)
		}

		session.Title = "Docs Renamed"
		session.WorkingDir = "/workspace/project"
		session.UpdatedAt = now.Add(time.Minute)
		if err := db.UpdateSession(ctx, session); err != nil {
			t.Fatalf("UpdateSession() error = %v", err)
		}

		renamed, err := db.GetSession(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetSession() after update error = %v", err)
		}
		if renamed.Title != "Docs Renamed" {
			t.Fatalf("GetSession().Title = %q, want %q", renamed.Title, "Docs Renamed")
		}
		if renamed.WorkingDir != "/workspace/project" {
			t.Fatalf("GetSession().WorkingDir = %q, want %q", renamed.WorkingDir, "/workspace/project")
		}

		if err := db.DeleteSession(ctx, session.ID); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}
		if _, err := db.GetSession(ctx, session.ID); err != core.ErrNotFound {
			t.Fatalf("GetSession() after delete error = %v, want ErrNotFound", err)
		}
	})
}

func RunBindingStoreContractTests(t *testing.T, newStore BindingStoreFactory) {
	t.Helper()

	t.Run("bindings", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := newStore(t)
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

		session := core.Session{
			ID:        "session_bindings",
			Title:     "Bindings",
			Status:    core.SessionStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}

		binding := core.ClientBinding{
			Client:      "telegram",
			ExternalKey: "chat:42",
			SessionID:   session.ID,
			UpdatedAt:   now,
		}
		if err := db.SaveBinding(ctx, binding); err != nil {
			t.Fatalf("SaveBinding() error = %v", err)
		}

		got, err := db.GetBinding(ctx, binding.Client, binding.ExternalKey)
		if err != nil {
			t.Fatalf("GetBinding() error = %v", err)
		}
		if got.SessionID != session.ID {
			t.Fatalf("GetBinding().SessionID = %q, want %q", got.SessionID, session.ID)
		}
	})
}

func RunDeliveryStoreContractTests(t *testing.T, newStore DeliveryStoreFactory) {
	t.Helper()

	t.Run("client deliveries", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := newStore(t)
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

		delivery := core.ClientDelivery{
			ID:        "delivery_1",
			Type:      core.ClientDeliveryTypeDaemonRestart,
			Client:    "test-client",
			SessionID: "session_1",
			RunID:     "run_1",
			TaskID:    "task_1",
			Summary:   "restart complete",
			Address:   json.RawMessage(`{"room":"ops","thread":"daemon","message":"restart-99"}`),
			Status:    core.ClientDeliveryStatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.CreateClientDelivery(ctx, delivery); err != nil {
			t.Fatalf("CreateClientDelivery() error = %v", err)
		}

		pending, err := db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{
			Client: "test-client",
			Type:   core.ClientDeliveryTypeDaemonRestart,
			Status: core.ClientDeliveryStatusPending,
		})
		if err != nil {
			t.Fatalf("ListClientDeliveries() error = %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("pending deliveries = %#v, want one", pending)
		}
		assertRawJSONEqual(t, pending[0].Address, delivery.Address)
		if pending[0].Client != "test-client" {
			t.Fatalf("pending[0].Client = %q, want %q", pending[0].Client, "test-client")
		}
		if pending[0].SessionID != "session_1" || pending[0].RunID != "run_1" || pending[0].TaskID != "task_1" || pending[0].Summary != "restart complete" {
			t.Fatalf("pending[0] delivery refs = %#v", pending[0])
		}

		byRun, err := db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{RunID: "run_1"})
		if err != nil {
			t.Fatalf("ListClientDeliveries(run) error = %v", err)
		}
		if len(byRun) != 1 {
			t.Fatalf("deliveries by run = %#v, want one", byRun)
		}

		finishedAt := now.Add(time.Minute)
		pending[0].Status = core.ClientDeliveryStatusSent
		pending[0].UpdatedAt = finishedAt
		pending[0].FinishedAt = &finishedAt
		if err := db.UpdateClientDelivery(ctx, pending[0]); err != nil {
			t.Fatalf("UpdateClientDelivery() error = %v", err)
		}

		remaining, err := db.ListClientDeliveries(ctx, core.ClientDeliveryFilter{
			Client: "test-client",
			Type:   core.ClientDeliveryTypeDaemonRestart,
			Status: core.ClientDeliveryStatusPending,
		})
		if err != nil {
			t.Fatalf("ListClientDeliveries(pending) error = %v", err)
		}
		if len(remaining) != 0 {
			t.Fatalf("pending after update = %#v, want none", remaining)
		}
	})
}

func RunRunStoreContractTests(t *testing.T, newStore RunStoreFactory) {
	t.Helper()

	t.Run("messages and runs", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := newStore(t)
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

		session := core.Session{
			ID:        "session_messages",
			Title:     "Messages",
			Status:    core.SessionStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}

		message := core.Message{
			ID:        "msg_1",
			SessionID: session.ID,
			RunID:     "run_1",
			Role:      core.MessageRoleUser,
			Content:   "hello",
			CreatedAt: now,
		}
		run := core.Run{
			ID:            "run_1",
			SessionID:     session.ID,
			UserMessageID: "msg_1",
			Status:        core.RunStatusAccepted,
			StartedAt:     now,
			UpdatedAt:     now,
		}
		if err := db.AcceptMessage(ctx, message, run); err != nil {
			t.Fatalf("AcceptMessage() error = %v", err)
		}

		finishedAt := now.Add(time.Minute)
		assistant := core.Message{
			ID:        "msg_2",
			SessionID: session.ID,
			RunID:     "run_1",
			Role:      core.MessageRoleAssistant,
			Content:   "hi",
			CreatedAt: finishedAt,
		}
		run.Status = core.RunStatusCompleted
		run.UpdatedAt = finishedAt
		run.FinishedAt = &finishedAt
		if err := db.CompleteRun(ctx, assistant, run); err != nil {
			t.Fatalf("CompleteRun() error = %v", err)
		}

		messages, err := db.ListMessages(ctx, session.ID, 10)
		if err != nil {
			t.Fatalf("ListMessages() error = %v", err)
		}
		if len(messages) != 2 {
			t.Fatalf("len(ListMessages()) = %d, want 2", len(messages))
		}

		storedRun, err := db.GetRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}
		if storedRun.UserMessageID != message.ID {
			t.Fatalf("GetRun().UserMessageID = %q, want %q", storedRun.UserMessageID, message.ID)
		}
		if storedRun.Status != core.RunStatusCompleted {
			t.Fatalf("GetRun().Status = %q, want %q", storedRun.Status, core.RunStatusCompleted)
		}
	})
}

func RunApprovalStoreContractTests(t *testing.T, newStore ApprovalStoreFactory) {
	t.Helper()

	t.Run("approvals", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := newStore(t)
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

		session := core.Session{
			ID:        "session_approvals",
			Title:     "Approvals",
			Status:    core.SessionStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}

		approval := core.Approval{
			ID:          "approval_1",
			SessionID:   session.ID,
			RunID:       "run_approval",
			ToolCallRef: "call_1",
			ToolName:    "shell",
			State:       core.ApprovalStatePending,
			RequestedAt: now,
		}
		if err := db.CreateApproval(ctx, approval); err != nil {
			t.Fatalf("CreateApproval() error = %v", err)
		}

		pending, err := db.ListApprovals(ctx, session.ID, core.ApprovalStatePending)
		if err != nil {
			t.Fatalf("ListApprovals() error = %v", err)
		}
		if len(pending) != 1 {
			t.Fatalf("pending approvals = %#v, want one", pending)
		}

		approval.State = core.ApprovalStateApproved
		decidedAt := now.Add(time.Minute)
		approval.DecidedAt = &decidedAt
		if err := db.UpdateApproval(ctx, approval); err != nil {
			t.Fatalf("UpdateApproval() error = %v", err)
		}

		got, err := db.GetApproval(ctx, approval.ID)
		if err != nil {
			t.Fatalf("GetApproval() error = %v", err)
		}
		if got.State != core.ApprovalStateApproved {
			t.Fatalf("GetApproval().State = %q, want %q", got.State, core.ApprovalStateApproved)
		}
	})
}

func RunFileSnapshotStoreContractTests(t *testing.T, newStore FileSnapshotStoreFactory) {
	t.Helper()

	t.Run("file snapshots", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		db := newStore(t)
		now := time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)

		session := core.Session{
			ID:        "session_files",
			Title:     "Files",
			Status:    core.SessionStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := db.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession() error = %v", err)
		}

		snapshot := core.FileSnapshot{
			SessionID: session.ID,
			Path:      "docs/a.txt",
			Content:   "version 1",
			CreatedAt: now,
			UpdatedAt: now,
		}
		created, err := db.CreateFileSnapshot(ctx, snapshot)
		if err != nil {
			t.Fatalf("CreateFileSnapshot() error = %v", err)
		}
		if created.Version != 0 {
			t.Fatalf("CreateFileSnapshot().Version = %d, want 0", created.Version)
		}

		snapshots, err := db.ListFileSnapshots(ctx, session.ID)
		if err != nil {
			t.Fatalf("ListFileSnapshots() error = %v", err)
		}
		if len(snapshots) != 1 {
			t.Fatalf("snapshots = %#v, want one", snapshots)
		}
		if snapshots[0].Path != snapshot.Path {
			t.Fatalf("snapshot path = %q, want %q", snapshots[0].Path, snapshot.Path)
		}
	})
}

func assertRawJSONEqual(t *testing.T, got json.RawMessage, want json.RawMessage) {
	t.Helper()

	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("got address is not valid JSON: %v; payload %s", err, got)
	}
	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("want address is not valid JSON: %v; payload %s", err, want)
	}
	gotPayload, err := json.Marshal(gotValue)
	if err != nil {
		t.Fatalf("Marshal(got address) error = %v", err)
	}
	wantPayload, err := json.Marshal(wantValue)
	if err != nil {
		t.Fatalf("Marshal(want address) error = %v", err)
	}
	if string(gotPayload) != string(wantPayload) {
		t.Fatalf("address = %s, want %s", gotPayload, wantPayload)
	}
}

func TestSQLiteStoreContracts(t *testing.T) {
	RunStoreContractTests(t, func(t *testing.T) core.Store {
		t.Helper()

		sqliteStore, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
		if err != nil {
			t.Fatalf("NewSQLite() error = %v", err)
		}
		t.Cleanup(func() {
			if err := sqliteStore.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
		return sqliteStore
	})
}
