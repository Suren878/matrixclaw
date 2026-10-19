package core_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/sessionllm"
	"github.com/Suren878/matrixclaw/internal/store"
)

func newSessionLLMCore(t *testing.T, sessions ...core.Session) *core.Core {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	for _, session := range sessions {
		session.Title, session.Kind, session.Status, session.CreatedAt, session.UpdatedAt = session.ID, core.SessionKindAssistant, core.SessionStatusActive, now, now
		if err := db.CreateSession(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	app := core.New(core.Config{Store: db})
	app.SetSessionLLMs(sessionllm.New("active", []sessionllm.ProviderSpec{
		{ID: "active", Name: "Active", Type: "openai-compatible", BaseURL: "http://127.0.0.1:1", Model: "active-model"},
		{ID: "other", Name: "Other", Type: "openai-compatible", BaseURL: "http://127.0.0.1:1", Model: "other-model"},
	}))
	return app
}

func TestUpdateSessionLLMSetsProviderAndModelTogether(t *testing.T) {
	app := newSessionLLMCore(t, core.Session{ID: "s1"})
	ctx := context.Background()

	session, err := app.UpdateSessionLLM(ctx, "s1", "other", "other-large")
	if err != nil || session.ProviderID != "other" || session.ModelID != "other-large" {
		t.Fatalf("switch = %s · %s, %v", session.ProviderID, session.ModelID, err)
	}
	if session, err = app.UpdateSessionLLM(ctx, "s1", "", "other-small"); err != nil || session.ProviderID != "other" || session.ModelID != "other-small" {
		t.Fatalf("model only = %s · %s, %v", session.ProviderID, session.ModelID, err)
	}
	if _, err := app.UpdateSessionLLM(ctx, "s1", "other", ""); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("no model: %v, want invalid input", err)
	}
	if _, err := app.UpdateSessionLLM(ctx, "s1", "missing", "m"); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("unknown provider: %v, want invalid input", err)
	}
}

func TestSessionWithoutModelUsesItsOwnProvidersDefault(t *testing.T) {
	app := newSessionLLMCore(t, core.Session{ID: "s1", ProviderID: "other"}, core.Session{ID: "s2"})
	ctx := context.Background()

	if session, err := app.GetSession(ctx, "s1"); err != nil || session.ProviderID != "other" || session.ModelID != "other-model" {
		t.Fatalf("session with a provider = %s · %s, %v", session.ProviderID, session.ModelID, err)
	}
	if session, err := app.GetSession(ctx, "s2"); err != nil || session.ProviderID != "active" || session.ModelID != "active-model" {
		t.Fatalf("session without a provider = %s · %s, %v", session.ProviderID, session.ModelID, err)
	}
}

func TestModelsForSessionNamesTheModelThatProviderWouldUse(t *testing.T) {
	app := newSessionLLMCore(t, core.Session{ID: "s1", ProviderID: "active", ModelID: "active-large"})
	ctx := context.Background()

	own, err := app.ModelsForSession(ctx, "s1", "")
	if err != nil || own.ProviderID != "active" || own.ModelID != "active-large" {
		t.Fatalf("own provider = %+v, %v", own, err)
	}
	other, err := app.ModelsForSession(ctx, "s1", "other")
	if err != nil || other.ProviderID != "other" || other.ModelID != "other-model" {
		t.Fatalf("other provider = %+v, %v", other, err)
	}
	if other.ModelsError == "" {
		t.Fatalf("an unreachable provider listed models: %+v", other)
	}
}
