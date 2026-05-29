package orchestration

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/Suren878/matrixclaw/internal/safego"
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

	safego.Go("orchestration.executeRun", func() {
		if err := s.executor.ExecuteRun(context.Background(), runID); err != nil {
			log.Printf("orchestration: run %s failed: %v", runID, err)
		}
	})

	return nil
}
