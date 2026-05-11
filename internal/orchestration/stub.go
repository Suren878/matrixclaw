package orchestration

import (
	"context"
	"errors"
)

type Stub struct {
	executor RunExecutor
}

func NewStub(executor RunExecutor) *Stub {
	return &Stub{executor: executor}
}

func (s *Stub) StartRun(ctx context.Context, runID string) error {
	if s.executor == nil {
		return errors.New("orchestration stub: executor not configured")
	}

	go func() {
		_ = s.executor.ExecuteRun(context.Background(), runID)
	}()

	return nil
}
