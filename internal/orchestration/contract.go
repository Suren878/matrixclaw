package orchestration

import "context"

type RunStarter interface {
	StartRun(ctx context.Context, runID string) error
}

type RunExecutor interface {
	ExecuteRun(ctx context.Context, runID string) error
}

type RunExecutorFunc func(ctx context.Context, runID string) error

func (f RunExecutorFunc) ExecuteRun(ctx context.Context, runID string) error {
	return f(ctx, runID)
}
