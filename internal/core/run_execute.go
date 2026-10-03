package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/agent/toolsched"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/safego"
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
	runCtx, release, registered := c.activeRunContext(ctx, runID)
	if !registered {
		return nil
	}
	kept := false
	defer func() {
		release()
		c.afterRun(context.Background(), runID)
		if kept {
			c.rescheduleInterruptedRun(runID)
		}
	}()
	claimed, ok, err := c.claim(ctx, runID)
	if err != nil || !ok {
		return err
	}
	run := claimed.Run
	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return c.failRun(ctx, run, err)
	}
	if handled, err := c.tryExecuteExternalAgentRun(ctx, runCtx, claimed, session); handled || err != nil {
		return err
	}
	session = c.decorateSessionLLM(session)
	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return c.failRun(ctx, run, err)
	}
	task, engine, err := c.nativeEngine(ctx, claimed, session, runtime)
	if err != nil {
		return c.failRun(ctx, run, err)
	}
	outcome, err := engine.Run(runCtx, task)
	if err != nil {
		return err
	}
	c.carryCounters(ctx, session.ID, outcome.Counters)
	kept, err = c.applyOutcome(ctx, run, outcome)
	return err
}

// goroutineStarter executes each run in a goroutine under the daemon lifetime.
type goroutineStarter struct{ c *Core }

func (s goroutineStarter) StartRun(_ context.Context, runID string) error {
	c := s.c
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return fmt.Errorf("%w: the daemon is stopping", ErrExecutionUnavailable)
	}
	c.executing.Add(1)
	c.mu.Unlock()
	safego.Go("core.executeRun", func() {
		defer c.executing.Done()
		if err := c.ExecuteRun(c.lifetime, runID); err != nil {
			log.Printf("core: run %s failed: %v", runID, err)
		}
	})
	return nil
}

// WaitRuns stops starting runs and waits for the executing ones to return;
// the daemon calls it after its lifetime ended, so they stop at once.
func (c *Core) WaitRuns() {
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	c.executing.Wait()
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

// nativeEngine builds the engine and task of one native run; the task resumes the
// counters of the run's checkpoint, or else those the session's last run carried.
func (c *Core) nativeEngine(ctx context.Context, claimed claimedRun, session Session, runtime providers.Runtime) (agent.Task, *agent.Engine, error) {
	run := claimed.Run
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
	readonly, err := c.readonlySubagent(ctx, session.ID)
	if err != nil {
		return agent.Task{}, nil, err
	}
	turn := nativeTurn{
		RunID:              run.ID,
		Continues:          continues,
		SessionID:          session.ID,
		WorkingDir:         session.WorkingDir,
		Subagent:           isSubagentSession(session),
		Readonly:           readonly,
		ClientCapabilities: run.ClientCapabilities,
		ToolUse:            agent.ToolUseAllowed(runtime),
	}
	engine := agent.New(agent.Config{
		Journal:     coreJournal{c: c},
		Tools:       coreTools{c: c, turn: turn, authorized: &sync.Map{}},
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
	if claimed.Recovering {
		task.Recovering = true
		if task.Interrupted, err = c.interruptedCalls(ctx, run, claimed.Batch); err != nil {
			return agent.Task{}, nil, err
		}
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
