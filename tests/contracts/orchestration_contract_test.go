package contracts

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Suren878/matrixclaw/internal/orchestration"
	goworkflows "github.com/Suren878/matrixclaw/internal/orchestration/go_workflows"
)

type OrchestratorFactory func(t *testing.T, executor orchestration.RunExecutor) orchestration.RunStarter

func RunOrchestratorContractTests(t *testing.T, newOrchestrator OrchestratorFactory) {
	t.Helper()

	t.Run("starts execution for accepted run", func(t *testing.T) {
		t.Parallel()

		calls := make(chan string, 1)
		executor := orchestration.RunExecutorFunc(func(ctx context.Context, runID string) error {
			calls <- runID
			return nil
		})

		orchestratorRuntime := newOrchestrator(t, executor)
		if closer, ok := orchestratorRuntime.(interface{ Close() error }); ok {
			t.Cleanup(func() {
				if err := closer.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			})
		}

		if err := orchestratorRuntime.StartRun(context.Background(), "run_1"); err != nil {
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
}

func TestGoWorkflowsOrchestratorContracts(t *testing.T) {
	RunOrchestratorContractTests(t, func(t *testing.T, executor orchestration.RunExecutor) orchestration.RunStarter {
		t.Helper()

		runtime, err := goworkflows.New(filepath.Join(t.TempDir(), "matrixclaw.db"), executor)
		if err != nil {
			t.Fatalf("goworkflows.New() error = %v", err)
		}
		return runtime
	})
}
