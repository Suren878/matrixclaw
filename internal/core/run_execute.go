package core

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/providers"
)

// ExecuteRun claims a run and executes it with its external agent or the native engine.
func (c *Core) ExecuteRun(ctx context.Context, runID string) error {
	runID = normalizeText(runID)
	runCtx, unregisterRun, claimed := c.activeRunContext(ctx, runID)
	if !claimed {
		return nil
	}
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
	task, engine := c.nativeEngine(run, session, runtime)
	outcome, err := engine.Run(runCtx, task)
	if err != nil {
		return err
	}
	return c.applyOutcome(ctx, run, outcome)
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

// nativeEngine builds the engine and task of one native run.
func (c *Core) nativeEngine(run Run, session Session, runtime providers.Runtime) (agent.Task, *agent.Engine) {
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
		Prompts:     corePrompts{c: c, turn: turn},
		Attachments: c.attachments,
		Now:         func() time.Time { return c.now().UTC() },
		NewID:       c.newID,
	})
	task := agent.Task{
		RunID:       run.ID,
		SessionID:   session.ID,
		Client:      run.Client,
		ExternalKey: run.ExternalKey,
		WorkingDir:  session.WorkingDir,
		Model:       runtime,
	}
	return task, engine
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
