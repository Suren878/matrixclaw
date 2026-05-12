package orchestration

import (
	"context"
	"errors"
	"strings"
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
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return errors.New("orchestration stub: run id is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	go func() {
		_ = s.executor.ExecuteRun(context.Background(), runID)
	}()

	return nil
}
