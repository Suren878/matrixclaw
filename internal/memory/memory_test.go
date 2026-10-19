package memory_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/memory"
	"github.com/Suren878/matrixclaw/internal/store"
)

func newService(t *testing.T) (*memory.Service, *store.SQLiteStore) {
	t.Helper()
	db, err := store.NewSQLite(filepath.Join(t.TempDir(), "matrixclaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return memory.New(db, nil), db
}

func TestAnUnknownScopeIsRefused(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	kept, err := service.Create(ctx, memory.Entry{Content: "likes tea"})
	if err != nil || kept.Scope != memory.ScopeGlobal {
		t.Fatalf("entry without scope = %+v, %v", kept, err)
	}

	if _, err := service.Create(ctx, memory.Entry{Scope: "projcet", Content: "uses make"}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("create with a typo = %v", err)
	}
	if _, err := service.Update(ctx, memory.Entry{ID: kept.ID, Scope: "usr"}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("update with a typo = %v", err)
	}
	if _, err := service.List(ctx, memory.Filter{Scope: "globl"}); !errors.Is(err, memory.ErrInvalidInput) {
		t.Fatalf("list with a typo = %v", err)
	}
	if entries, err := service.List(ctx, memory.Filter{Scope: "USER"}); err != nil || len(entries) != 0 {
		t.Fatalf("user entries = %+v, %v", entries, err)
	}
}

type brokenStore struct{ memory.Store }

func (brokenStore) ListMemories(context.Context, memory.Filter) ([]memory.Entry, error) {
	return nil, errors.New("disk gone")
}

func TestPromptContextReportsAnUnreadableStore(t *testing.T) {
	_, db := newService(t)
	if _, err := memory.New(brokenStore{db}, nil).PromptContext(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "disk gone") {
		t.Fatalf("PromptContext error = %v", err)
	}
}
