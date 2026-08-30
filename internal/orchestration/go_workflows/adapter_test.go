package goworkflows

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/orchestration"
	_ "modernc.org/sqlite"
)

func TestIsolatedStatePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "db extension", path: "/state/matrixclaw.db", want: "/state/matrixclaw-workflows.db"},
		{name: "sqlite extension", path: "/state/matrixclaw.sqlite", want: "/state/matrixclaw-workflows.sqlite"},
		{name: "no extension", path: "/state/matrixclaw", want: "/state/matrixclaw-workflows.db"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsolatedStatePath(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("IsolatedStatePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestNewForStoreDoesNotCreatePrimaryDatabase(t *testing.T) {
	mainStorePath := filepath.Join(t.TempDir(), "matrixclaw.db")
	adapter, err := NewForStore(mainStorePath, orchestration.RunExecutorFunc(func(context.Context, string) error {
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(mainStorePath); !os.IsNotExist(err) {
		t.Fatalf("primary database was touched: stat error = %v", err)
	}
	workflowPath, err := IsolatedStatePath(mainStorePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workflowPath); err != nil {
		t.Fatalf("workflow database was not created: %v", err)
	}
	info, err := os.Stat(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("workflow database mode = %o, want 600", got)
	}
}

func TestNewForStoreDoesNotBlockPrimaryWrites(t *testing.T) {
	mainStorePath := filepath.Join(t.TempDir(), "matrixclaw.db")
	primary, err := sql.Open("sqlite", mainStorePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = primary.Close() }()
	primary.SetMaxOpenConns(1)
	if _, err := primary.Exec(`PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 100; CREATE TABLE writes (id INTEGER PRIMARY KEY);`); err != nil {
		t.Fatal(err)
	}

	executed := make(chan error, 1)
	adapter, err := NewForStore(mainStorePath, orchestration.RunExecutorFunc(func(ctx context.Context, _ string) error {
		for i := 0; i < 250; i++ {
			if _, err := primary.ExecContext(ctx, `INSERT INTO writes(id) VALUES(?)`, i); err != nil {
				executed <- err
				return err
			}
		}
		executed <- nil
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adapter.Close() }()

	if err := adapter.StartRun(context.Background(), "run_isolated_writes"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-executed:
		if err != nil {
			t.Fatalf("primary write failed while workflow pollers were active: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for workflow activity")
	}

	var count int
	if err := primary.QueryRow(`SELECT COUNT(*) FROM writes`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 250 {
		t.Fatalf("primary write count = %d, want 250", count)
	}
}
