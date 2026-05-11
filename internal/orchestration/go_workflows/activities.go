package goworkflows

import (
	"context"
	"errors"

	"github.com/Suren878/matrixclaw/internal/orchestration"
)

type runActivities struct {
	executor orchestration.RunExecutor
}

func (a *runActivities) ExecuteRun(ctx context.Context, runID string) (bool, error) {
	if a.executor == nil {
		return false, errors.New("go-workflows: executor not configured")
	}

	if err := a.executor.ExecuteRun(ctx, runID); err != nil {
		return false, err
	}

	return true, nil
}
