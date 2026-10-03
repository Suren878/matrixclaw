package core

import (
	"context"
	"errors"
	"log"
	"time"
)

// wakeupInterval is how often due run wakeups are looked for.
const wakeupInterval = time.Second

// wakeWaitingRun starts a run parked in waiting_events once what it waits for
// happened: an awaited task finished, the user wrote, or its timer ran out.
// The session gate makes a wake and the park's own check start it once.
func (c *Core) wakeWaitingRun(ctx context.Context, sessionID string, runID string) error {
	gate := c.sessionGate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return ignoreNotFound(err)
	}
	if run.Status != RunStatusWaitingEvents {
		return nil
	}
	if err := c.steerQueuedInputs(ctx, run); err != nil {
		return err
	}
	due, err := c.waitOver(ctx, run)
	if err != nil || !due {
		return err
	}
	// The wakeup stays until the run executes, so a failed start is retried.
	return c.startRun(ctx, run.ID)
}

// steerQueuedInputs turns messages queued behind a waiting run into steers
// for it, so that they wake it.
func (c *Core) steerQueuedInputs(ctx context.Context, run Run) error {
	inputs, err := c.store.ListPendingSessionInputs(ctx, run.SessionID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if input.Mode != BusyInputModeQueue {
			continue
		}
		input.Mode = BusyInputModeSteer
		input.TargetRunID = run.ID
		input.UpdatedAt = c.now().UTC()
		if err := c.store.UpdateSessionInput(ctx, input); err != nil {
			return err
		}
		c.publishSessionInputUpdated(input)
	}
	return nil
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
	if len(wakeup.TaskIDs) > 0 {
		return c.anyTaskFinished(ctx, wakeup.TaskIDs)
	}
	events, err := c.store.ListTasks(ctx, TaskFilter{SessionID: run.SessionID, Undelivered: true})
	return len(events) > 0, err
}

// anyTaskFinished reports whether any of the tasks ended, whether or not its
// end was delivered to a run.
func (c *Core) anyTaskFinished(ctx context.Context, taskIDs []string) (bool, error) {
	for _, id := range taskIDs {
		task, err := c.store.GetTask(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil || task.FinishedAt != nil {
			return err == nil, err
		}
	}
	return false, nil
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
