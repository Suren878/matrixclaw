package goworkflows

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	workflowbackend "github.com/cschleiden/go-workflows/backend"
	workflowsqlite "github.com/cschleiden/go-workflows/backend/sqlite"
	workflowclient "github.com/cschleiden/go-workflows/client"
	workflowregistry "github.com/cschleiden/go-workflows/registry"
	workflowworker "github.com/cschleiden/go-workflows/worker"

	"github.com/Suren878/matrixclaw/internal/orchestration"
)

type Adapter struct {
	backend  workflowbackend.Backend
	worker   *workflowworker.WorkflowOrchestrator
	cancel   context.CancelFunc
	sequence uint64
}

// IsolatedStatePath returns a sidecar SQLite path for workflow-engine state.
// The workflow backend uses immediate transactions and active pollers, so it
// must not share MatrixClaw's primary database with messages and runs.
func IsolatedStatePath(mainStorePath string) (string, error) {
	mainStorePath = strings.TrimSpace(mainStorePath)
	if mainStorePath == "" {
		return "", errors.New("go-workflows: main sqlite path is required")
	}
	cleanPath := filepath.Clean(mainStorePath)
	ext := filepath.Ext(cleanPath)
	if ext == "" {
		return cleanPath + "-workflows.db", nil
	}
	return strings.TrimSuffix(cleanPath, ext) + "-workflows" + ext, nil
}

// NewForStore creates a workflow adapter whose SQLite state is isolated from
// the application's primary store.
func NewForStore(mainStorePath string, executor orchestration.RunExecutor) (*Adapter, error) {
	path, err := IsolatedStatePath(mainStorePath)
	if err != nil {
		return nil, err
	}
	return New(path, executor)
}

func New(path string, executor orchestration.RunExecutor) (*Adapter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("go-workflows: sqlite path is required")
	}
	if executor == nil {
		return nil, errors.New("go-workflows: executor is required")
	}

	backend := workflowsqlite.NewSqliteBackend(path, workflowsqlite.WithApplyMigrations(true))
	if err := secureSQLiteFiles(path); err != nil {
		_ = backend.Close()
		return nil, err
	}
	worker := workflowworker.NewWorkflowOrchestrator(backend, nil)

	if err := worker.RegisterWorkflow(runWorkflow); err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("go-workflows: register workflow: %w", err)
	}

	activities := &runActivities{executor: executor}
	if err := worker.RegisterActivity(activities.ExecuteRun, workflowregistry.WithName(executeRunActivityName)); err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("go-workflows: register activity: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	if err := worker.Start(runCtx); err != nil {
		cancel()
		_ = backend.Close()
		return nil, fmt.Errorf("go-workflows: start worker: %w", err)
	}

	return &Adapter{
		backend: backend,
		worker:  worker,
		cancel:  cancel,
	}, nil
}

func secureSQLiteFiles(path string) error {
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(candidate, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("go-workflows: secure sqlite file %s: %w", candidate, err)
		}
	}
	return nil
}

func (a *Adapter) StartRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return errors.New("go-workflows: run id is required")
	}

	instanceID := fmt.Sprintf("%s_%d", runID, atomic.AddUint64(&a.sequence, 1))
	_, err := a.worker.CreateWorkflowInstance(ctx, workflowclient.WorkflowInstanceOptions{
		InstanceID: instanceID,
	}, runWorkflow, runID)
	if err != nil {
		if errors.Is(err, workflowbackend.ErrInstanceAlreadyExists) {
			return nil
		}
		return fmt.Errorf("go-workflows: create workflow instance: %w", err)
	}

	return nil
}

func (a *Adapter) Close() error {
	var firstErr error

	if a.cancel != nil {
		a.cancel()
		if err := a.worker.WaitForCompletion(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("go-workflows: wait for completion: %w", err)
		}
	}

	if a.backend != nil {
		if err := a.backend.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("go-workflows: close backend: %w", err)
		}
	}

	return firstErr
}
