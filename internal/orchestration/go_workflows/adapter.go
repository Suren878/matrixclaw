package goworkflows

import (
	"context"
	"errors"
	"fmt"
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
	backend      workflowbackend.Backend
	orchestrator *workflowworker.WorkflowOrchestrator
	cancel       context.CancelFunc
	sequence     uint64
}

func New(path string, executor orchestration.RunExecutor) (*Adapter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("go-workflows: sqlite path is required")
	}
	if executor == nil {
		return nil, errors.New("go-workflows: executor is required")
	}

	backend := workflowsqlite.NewSqliteBackend(path, workflowsqlite.WithApplyMigrations(true))
	orchestratorRuntime := workflowworker.NewWorkflowOrchestrator(backend, nil)

	if err := orchestratorRuntime.RegisterWorkflow(runWorkflow); err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("go-workflows: register workflow: %w", err)
	}

	activities := &runActivities{executor: executor}
	if err := orchestratorRuntime.RegisterActivity(activities.ExecuteRun, workflowregistry.WithName(executeRunActivityName)); err != nil {
		_ = backend.Close()
		return nil, fmt.Errorf("go-workflows: register activity: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	if err := orchestratorRuntime.Start(runCtx); err != nil {
		cancel()
		_ = backend.Close()
		return nil, fmt.Errorf("go-workflows: start worker: %w", err)
	}

	return &Adapter{
		backend:      backend,
		orchestrator: orchestratorRuntime,
		cancel:       cancel,
	}, nil
}

func (a *Adapter) StartRun(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return errors.New("go-workflows: run id is required")
	}

	instanceID := fmt.Sprintf("%s_%d", runID, atomic.AddUint64(&a.sequence, 1))
	_, err := a.orchestrator.CreateWorkflowInstance(ctx, workflowclient.WorkflowInstanceOptions{
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
		if err := a.orchestrator.WaitForCompletion(); err != nil && firstErr == nil {
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
