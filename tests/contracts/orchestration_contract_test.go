package contracts

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/orchestration"
	goworkflows "github.com/Suren878/matrixclaw/internal/orchestration/go_workflows"
)

type RunStarterFactory func(t *testing.T, executor orchestration.RunExecutor) orchestration.RunStarter

func RunStarterContractTests(t *testing.T, newRunStarter RunStarterFactory) {
	t.Helper()

	t.Run("starts execution for accepted run", func(t *testing.T) {
		t.Parallel()

		calls := make(chan string, 1)
		executor := orchestration.RunExecutorFunc(func(ctx context.Context, runID string) error {
			calls <- runID
			return nil
		})

		runStarter := newRunStarter(t, executor)
		if closer, ok := runStarter.(interface{ Close() error }); ok {
			t.Cleanup(func() {
				if err := closer.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			})
		}

		if err := runStarter.StartRun(context.Background(), "run_1"); err != nil {
			t.Fatalf("StartRun() error = %v", err)
		}

		select {
		case runID := <-calls:
			if runID != "run_1" {
				t.Fatalf("executor run id = %q, want %q", runID, "run_1")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("StartRun() did not trigger executor")
		}
	})

	t.Run("rejects blank run id", func(t *testing.T) {
		t.Parallel()

		runStarter := newRunStarter(t, orchestration.RunExecutorFunc(func(ctx context.Context, runID string) error {
			t.Fatal("executor should not be called for blank run id")
			return nil
		}))
		if closer, ok := runStarter.(interface{ Close() error }); ok {
			t.Cleanup(func() {
				if err := closer.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			})
		}

		if err := runStarter.StartRun(context.Background(), " \t "); err == nil {
			t.Fatal("StartRun() error = nil, want error")
		}
	})
}

func TestStubRunStarterContracts(t *testing.T) {
	RunStarterContractTests(t, func(t *testing.T, executor orchestration.RunExecutor) orchestration.RunStarter {
		t.Helper()
		return orchestration.NewStub(executor)
	})
}

func TestGoWorkflowsRunStarterContracts(t *testing.T) {
	RunStarterContractTests(t, func(t *testing.T, executor orchestration.RunExecutor) orchestration.RunStarter {
		t.Helper()

		runtime, err := goworkflows.New(filepath.Join(t.TempDir(), "matrixclaw.db"), executor)
		if err != nil {
			t.Fatalf("goworkflows.New() error = %v", err)
		}
		return runtime
	})
}
