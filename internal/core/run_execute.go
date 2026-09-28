package core

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// DefaultModelConcurrency is how many model requests native runs make at once
// unless the daemon configures another limit.
const DefaultModelConcurrency = 4

// WithModelConcurrency bounds the model requests all native runs, subagents
// included, make at once; 0 or less keeps the default.
func (c *Core) WithModelConcurrency(n int) *Core {
	if n <= 0 {
		n = DefaultModelConcurrency
	}
	c.modelSlots = toolsched.NewSemaphore(n)
	return c
}

// ExecuteRun claims a run and executes it with its external agent or the native engine.
func (c *Core) ExecuteRun(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	reschedule := false
	defer func() {
		if reschedule {
			c.rescheduleInterruptedRun(runID)
		}
	}()
	runCtx, unregisterRun, claimed := c.activeRunContext(ctx, runID)
	if !claimed {
		return nil
	}
	defer c.resumeParkedRun(runID)
	defer unregisterRun()
	defer func() {
		if err := c.afterRunExecution(context.Background(), runID); err != nil {
			log.Printf("core: after run execution for %q failed: %v", runID, err)
		}
	}()
	ready, err := c.prepareClaimedRun(ctx, runID)
	if err != nil || !ready {
		return err
	}
	if handled, err := c.tryExecuteExternalAgentRun(ctx, runCtx, runID); handled || err != nil {
		return err
	}
	run, session, runtime, ok, err := c.prepareNativeRun(ctx, runID)
	if err != nil || !ok {
		return err
	}
	task, engine, err := c.nativeEngine(ctx, run, session, runtime)
	if err != nil {
		return c.failRunByID(ctx, run, err)
	}
	outcome, err := engine.Run(runCtx, task)
	if err != nil {
		return err
	}
	c.carryCounters(ctx, session.ID, outcome.Counters)
	reschedule, err = c.applyOutcome(ctx, run, outcome)
	return err
}

// rescheduleInterruptedRun hands a run kept for recovery back to the run starter
// while the daemon keeps running.
func (c *Core) rescheduleInterruptedRun(runID string) {
	if c.lifetime.Err() != nil {
		return
	}
	if err := c.startRun(context.Background(), runID); err != nil {
		log.Printf("core: reschedule interrupted run %q failed: %v", runID, err)
	}
}

// resumeParkedRun starts a run whose approvals were all decided while it was
// still parking: ResolveApproval found it active and left the start to this check.
func (c *Core) resumeParkedRun(runID string) {
	if c.lifetime.Err() != nil {
		return
	}
	ctx := context.Background()
	run, err := c.store.GetRun(ctx, runID)
	if err == nil {
		err = c.resumeDecidedRun(ctx, run.SessionID, runID)
	}
	if err != nil && !ignoreMissing(err) {
		log.Printf("core: resume parked run %q failed: %v", runID, err)
	}
}

// prepareNativeRun marks an accepted native run as running and resolves its model.
func (c *Core) prepareNativeRun(ctx context.Context, runID string) (Run, Session, providers.Runtime, bool, error) {
	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return Run{}, Session{}, nil, false, err
	}
	switch run.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return Run{}, Session{}, nil, false, nil
	case RunStatusRunning:
		return Run{}, Session{}, nil, false, c.failOrphanedRun(ctx, run)
	}
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return Run{}, Session{}, nil, false, c.failRunByID(ctx, run, err)
	}
	session = c.decorateSessionLLM(session)
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return Run{}, Session{}, nil, false, c.failRunByID(ctx, run, err)
	}
	if err := c.setRunStatus(ctx, &run, RunStatusRunning, ""); err != nil {
		return Run{}, Session{}, nil, false, err
	}
	return run, session, runtime, true, nil
}

// nativeEngine builds the engine and task of one native run; the task resumes the
// counters of the run's checkpoint, or else those the session's last run carried.
func (c *Core) nativeEngine(ctx context.Context, run Run, session Session, runtime providers.Runtime) (agent.Task, *agent.Engine, error) {
	resume, err := c.resumeCounters(ctx, run.ID)
	if err == nil && resume == (agent.Counters{}) {
		resume, err = c.carriedCounters(ctx, session.ID)
	}
	if err != nil {
		return agent.Task{}, nil, err
	}
	budget, err := c.runBudget(ctx, run, session)
	if err != nil {
		return agent.Task{}, nil, err
	}
	continues, err := c.continuedRuns(ctx, run)
	if err != nil {
		return agent.Task{}, nil, err
	}
	turn := nativeTurn{
		RunID:              run.ID,
		SessionID:          session.ID,
		WorkingDir:         session.WorkingDir,
		Subagent:           isSubagentSession(session),
		ClientCapabilities: run.ClientCapabilities,
		ToolUse:            agent.ToolUseAllowed(runtime),
	}
	engine := agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Tools:       coreTools{c: c, turn: turn},
		Approvals:   coreApprovals{c: c, sessionID: session.ID},
		Inbox:       coreInbox{c: c, session: session},
		Sink:        coreSink{c: c},
		Prompts:     &corePrompts{c: c, turn: turn},
		Todos:       coreTodos{c: c},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
		ModelSlots:  c.modelSlots,
		Locks:       c.toolLocks,
	})
	task := agent.Task{
		RunID:        run.ID,
		SessionID:    session.ID,
		Client:       run.Client,
		ExternalKey:  run.ExternalKey,
		WorkingDir:   session.WorkingDir,
		Model:        runtime,
		WindowTokens: c.sessionContextWindowTokens(session),
		Budget:       budget,
		Resume:       resume,
		Continues:    continues,
	}
	if compact, windowTokens := c.compactRuntime(ctx); compact != nil {
		task.CompactModel, task.CompactWindowTokens = compact, windowTokens
	}
	return task, engine, nil
}

func (c *Core) resolveSessionRuntime(ctx context.Context, session Session) (providers.Runtime, error) {
	llms := c.sessionLLMs()
	if llms == nil {
		return nil, fmt.Errorf("%w: provider registry unavailable", ErrExecutionUnavailable)
	}
	runtime, _, _, err := llms.Resolve(ctx, session.ProviderID, session.ModelID)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		return nil, fmt.Errorf("%w: provider not configured", ErrExecutionUnavailable)
	}
	return runtime, nil
}
