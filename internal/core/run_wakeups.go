package core

import (
	"context"
	"errors"
	"log"
	"slices"
	"time"

	"github.com/Suren878/matrixclaw/internal/tools"
)

// wakeupInterval is how often due run wakeups are looked for.
const wakeupInterval = time.Second

// parkRun leaves a run waiting for what its await asked for; its wakeup keeps
// the timer and the awaited tasks.
func (c *Core) parkRun(ctx context.Context, run Run, await *tools.Await) error {
	if await == nil {
		return errors.New("core: a run waiting for events has nothing to wait for")
	}
	wakeup := RunWakeup{RunID: run.ID, SessionID: run.SessionID, WakeAt: await.Until, TaskIDs: await.TaskIDs}
	if err := c.store.SaveRunWakeup(ctx, wakeup); err != nil {
		return err
	}
	return c.setRunStatus(ctx, &run, RunStatusWaitingEvents, "")
}

// wakeWaitingRun starts a run parked in waiting_events once what it waits for
// happened: an awaited task finished, the user wrote, or its timer ran out.
// The session gate makes a wake and the park's own check start it once.
func (c *Core) wakeWaitingRun(ctx context.Context, sessionID string, runID string) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	run, err := c.store.GetRun(ctx, runID)
	if errors.Is(err, ErrNotFound) {
		return c.store.DeleteRunWakeup(ctx, runID)
	}
	if err != nil {
		return err
	}
	if subagentRunStatusTerminal(run.Status) {
		return c.store.DeleteRunWakeup(ctx, runID)
	}
	if run.Status != RunStatusWaitingEvents {
		return nil
	}
	due, err := c.waitOver(ctx, run)
	if err != nil || !due {
		return err
	}
	if err := c.store.DeleteRunWakeup(ctx, run.ID); err != nil {
		return err
	}
	return c.startRun(ctx, run.ID)
}

// waitOver reports whether a waiting run has something to go on with.
func (c *Core) waitOver(ctx context.Context, run Run) (bool, error) {
	wakeup, err := c.store.GetRunWakeup(ctx, run.ID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !c.now().Before(wakeup.WakeAt) {
		return true, nil
	}
	steers, err := c.store.ListPendingSteerInputs(ctx, run.SessionID, run.ID)
	if err != nil || len(steers) > 0 {
		return len(steers) > 0, err
	}
	events, err := c.store.ListTasks(ctx, TaskFilter{SessionID: run.SessionID, Undelivered: true})
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(events, func(task Task) bool {
		return len(wakeup.TaskIDs) == 0 || slices.Contains(wakeup.TaskIDs, task.ID)
	}), nil
}

// wakeSessionRun wakes the session's run if it waits for events.
func (c *Core) wakeSessionRun(ctx context.Context, sessionID string) error {
	active, err := c.store.GetActiveRunBySession(ctx, sessionID)
	if errors.Is(err, ErrNotFound) || err == nil && active.Status != RunStatusWaitingEvents {
		return nil
	}
	if err != nil {
		return err
	}
	return c.wakeWaitingRun(ctx, sessionID, active.ID)
}

// WakeDueRuns starts the waiting runs whose timer ran out.
func (c *Core) WakeDueRuns(ctx context.Context) error {
	due, err := c.store.ListDueRunWakeups(ctx, c.now().UTC())
	if err != nil {
		return err
	}
	var errs []error
	for _, wakeup := range due {
		errs = append(errs, c.wakeWaitingRun(ctx, wakeup.SessionID, wakeup.RunID))
	}
	return errors.Join(errs...)
}

// RunWakeups wakes due runs until ctx ends; timers stored before a restart
// fire on its first tick.
func (c *Core) RunWakeups(ctx context.Context) {
	ticker := time.NewTicker(wakeupInterval)
	defer ticker.Stop()
	for {
		if err := c.WakeDueRuns(ctx); err != nil && ctx.Err() == nil {
			log.Printf("core: wake due runs: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
