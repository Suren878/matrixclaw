package core

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent"
)

type activeRun struct {
	cancel context.CancelCauseFunc
}

func (c *Core) activeRunContext(parent context.Context, runID string) (context.Context, func(), bool) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return parent, func() {}, true
	}

	ctx, cancel := context.WithCancelCause(parent)
	active := &activeRun{cancel: cancel}
	c.mu.Lock()
	if c.activeRuns[runID] != nil {
		c.mu.Unlock()
		cancel(nil)
		return parent, func() {}, false
	}
	delete(c.scheduledRuns, runID)
	c.activeRuns[runID] = active
	c.mu.Unlock()

	return ctx, func() {
		c.mu.Lock()
		if c.activeRuns[runID] == active {
			delete(c.activeRuns, runID)
		}
		c.mu.Unlock()
		cancel(nil)
	}, true
}

func (c *Core) runIsActive(runID string) bool {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return false
	}
	c.mu.RLock()
	active := c.activeRuns[runID]
	c.mu.RUnlock()
	return active != nil
}

// cancelActiveRun stops the run's executor with agent.ErrCanceled as the cause,
// which tells a cancel from an interruption.
func (c *Core) cancelActiveRun(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}

	c.mu.RLock()
	active := c.activeRuns[runID]
	c.mu.RUnlock()
	if active != nil && active.cancel != nil {
		active.cancel(agent.ErrCanceled)
	}
}
