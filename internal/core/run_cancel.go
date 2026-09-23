package core

import (
	"context"
	"strings"
)

type activeRun struct {
	cancel context.CancelFunc
}

func (c *Core) activeRunContext(parent context.Context, runID string) (context.Context, func(), bool) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return parent, func() {}, true
	}

	ctx, cancel := context.WithCancel(parent)
	active := &activeRun{cancel: cancel}
	c.mu.Lock()
	if c.activeRuns[runID] != nil {
		c.mu.Unlock()
		cancel()
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
		cancel()
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

func (c *Core) cancelActiveRun(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}

	c.mu.RLock()
	active := c.activeRuns[runID]
	c.mu.RUnlock()
	if active != nil && active.cancel != nil {
		active.cancel()
	}
}
