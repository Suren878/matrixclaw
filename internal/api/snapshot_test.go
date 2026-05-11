package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/store"
)

func TestHandleSnapshotReturnsBoundSessionSnapshot(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer func() {
		if err := sqliteStore.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	app := core.New(sqliteStore)
	ctx := t.Context()

	session, err := app.CreateSession(ctx, core.CreateSessionInput{Title: "Docs"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := app.UseBinding(ctx, core.UseBindingInput{
		Client:      "terminal",
		ExternalKey: "local",
		SessionID:   session.ID,
	}); err != nil {
		t.Fatalf("UseBinding() error = %v", err)
	}

	server := httptest.NewServer(New(app).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/snapshot?client=terminal&external_key=local")
	if err != nil {
		t.Fatalf("GET /v1/snapshot error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/snapshot status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var payload struct {
		Snapshot core.ClientSnapshot `json:"snapshot"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(snapshot) error = %v", err)
	}
	if payload.Snapshot.SessionID != session.ID {
		t.Fatalf("snapshot.SessionID = %q, want %q", payload.Snapshot.SessionID, session.ID)
	}
	if payload.Snapshot.Session == nil || payload.Snapshot.Session.Title != "Docs" {
		t.Fatalf("snapshot.Session = %#v, want title Docs", payload.Snapshot.Session)
	}
	if payload.Snapshot.Context == nil || payload.Snapshot.Context.SessionID != session.ID {
		t.Fatalf("snapshot.Context = %#v, want session context", payload.Snapshot.Context)
	}
}

func TestHandleSessionContextReturnsContextReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "matrixclaw.db")
	sqliteStore, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite() error = %v", err)
	}
	defer func() {
		if err := sqliteStore.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	app := core.New(sqliteStore)
	ctx := t.Context()
	session, err := app.CreateSession(ctx, core.CreateSessionInput{Title: "Docs"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	server := httptest.NewServer(New(app).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/v1/sessions/" + session.ID + "/context")
	if err != nil {
		t.Fatalf("GET /v1/sessions/{id}/context error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/sessions/{id}/context status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var payload struct {
		Context core.ContextReport `json:"context"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode(context) error = %v", err)
	}
	if payload.Context.SessionID != session.ID {
		t.Fatalf("context.SessionID = %q, want %q", payload.Context.SessionID, session.ID)
	}
}
