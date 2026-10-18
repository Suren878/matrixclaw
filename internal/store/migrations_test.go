package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
)

func TestSessionKindMigrationKeepsSessionsAndAttachments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	before := slices.Index(migrations, sessionKindSQL)
	for i, script := range migrations[:before] {
		if err := applyMigration(db, i+1, script); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO sessions(id, title, kind, runtime_id, permission_mode, status, created_at, updated_at) VALUES
			('agent', 'agent', 'external_agent', 'external_agent', 'accept_edits', 'active', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z'),
			('chat', 'chat', 'assistant', 'matrixclaw', 'default', 'active', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`,
		`INSERT INTO external_agent_sessions(session_id, agent_id, external_thread_id, model, approval_policy, sandbox, created_at, updated_at)
			VALUES ('agent', 'codex-app', 'thread-1', 'gpt-5.4', 'on-request', 'workspace-write', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	for id, want := range map[string]core.SessionKind{"agent": core.SessionKindExternalAgent, "chat": core.SessionKindAssistant} {
		if session, err := st.GetSession(ctx, id); err != nil || session.Kind != want {
			t.Fatalf("session %s = %+v, %v; want kind %s", id, session, err, want)
		}
	}
	if attachment, err := st.GetExternalAgentSession(ctx, "agent"); err != nil || attachment.ExternalThreadID != "thread-1" || attachment.Model != "gpt-5.4" {
		t.Fatalf("attachment = %+v, %v", attachment, err)
	}
	for table, column := range map[string]string{"sessions": "runtime_id", "external_agent_sessions": "sandbox"} {
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s.%s still there (%d, %v)", table, column, count, err)
		}
	}
}

func TestAutomationMigrationKeepsTelegramJobsDelivering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	before := slices.Index(migrations, automationSkillsSQL)
	for i, script := range migrations[:before] {
		if err := applyMigration(db, i+1, script); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO sessions(id, title, kind, permission_mode, status, created_at, updated_at) VALUES ('s', 's', 'assistant', 'default', 'active', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`,
		`INSERT INTO automation_jobs(id, kind, status, session_id, client, external_key, schedule_mode, created_at, updated_at) VALUES
			('tg', 'reminder', 'active', 's', 'telegram', '7', 'once', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z'),
			('tui', 'reminder', 'active', 's', 'terminal', 'host', 'once', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := NewSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	for id, want := range map[string]bool{"tg": true, "tui": false} {
		var raw string
		if err := st.db.QueryRow(`SELECT client_capabilities_json FROM automation_jobs WHERE id = ?`, id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if got := unmarshalClientCapabilities(raw).ReceivesDeliveries; got != want {
			t.Fatalf("job %s receives deliveries = %v, want %v (%q)", id, got, want, raw)
		}
	}
}
