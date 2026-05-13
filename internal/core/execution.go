package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/providers"
)

// maxRunToolSteps caps the tool-call loop per run to prevent unbounded execution.
const maxRunToolSteps = 8

type runExecution struct {
	Run  Run
	Turn turnExecution
}

func newRunExecution(run Run, session Session, runtime providers.Runtime) *runExecution {
	return &runExecution{
		Run:  run,
		Turn: newTurnExecution(run, session, runtime),
	}
}

func (c *Core) ExecuteRun(ctx context.Context, runID string) error {
	if handled, err := c.tryExecuteExternalAgentRun(ctx, runID); handled || err != nil {
		return err
	}

	execution, ok, err := c.prepareRunExecution(ctx, runID)
	if err != nil || !ok {
		return err
	}

	runCtx, unregisterRun := c.activeRunContext(ctx, execution.Run.ID)
	defer unregisterRun()

	return c.executeRunLoop(ctx, runCtx, execution)
}

func (c *Core) prepareRunExecution(ctx context.Context, runID string) (*runExecution, bool, error) {
	run, err := c.store.GetRun(ctx, normalizeText(runID))
	if err != nil {
		return nil, false, err
	}

	switch run.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCanceled:
		return nil, false, nil
	case RunStatusRunning:
		return nil, false, nil
	}

	session, err := c.store.GetSession(ctx, run.SessionID)
	if err != nil {
		return nil, false, c.failRunByID(ctx, run, err)
	}
	session = c.decorateSessionLLM(session)

	runtime, err := c.resolveSessionRuntime(ctx, session)
	if err != nil {
		return nil, false, c.failRunByID(ctx, run, err)
	}

	if err := c.setRunStatus(ctx, &run, RunStatusRunning, ""); err != nil {
		return nil, false, err
	}

	return newRunExecution(run, session, runtime), true, nil
}

func (c *Core) executeRunLoop(ctx context.Context, runCtx context.Context, execution *runExecution) error {
	for step := 0; step < maxRunToolSteps; step++ {
		result, err := c.executeRunStep(ctx, runCtx, execution)
		if err != nil {
			return err
		}
		done, err := c.applyRunTurnResult(ctx, execution, result)
		if err != nil {
			return err
		}
		if !done {
			continue
		}
		return nil
	}

	return c.failRunByID(ctx, execution.Run, fmt.Errorf("tool loop exceeded %d steps", maxRunToolSteps))
}

func (c *Core) executeRunStep(ctx context.Context, runCtx context.Context, execution *runExecution) (turnStepResult, error) {
	if execution == nil {
		return turnStepResult{Outcome: turnStepCompleted}, errors.New("core: run execution is required")
	}
	turn := execution.Turn
	waitingApproval, err := c.resumeApprovedTools(ctx, turn)
	if err != nil {
		return turnStepResult{Outcome: turnStepCompleted, Err: err}, nil
	}
	if waitingApproval {
		return turnStepResult{Outcome: turnStepWaitingApproval}, nil
	}

	request, err := c.buildProviderRequest(ctx, turn)
	if err != nil {
		return turnStepResult{Outcome: turnStepCompleted, Err: err}, nil
	}

	assistant, assistantSaved, response, err := c.generateAssistantTurn(runCtx, turn, request)
	if err != nil {
		return turnStepResult{
			Outcome:              turnStepCompleted,
			Assistant:            &assistant,
			AssistantSaved:       assistantSaved,
			Response:             response,
			Err:                  err,
			MarkAssistantErrored: true,
		}, nil
	}
	if handled, cancelErr := c.checkAndHandleCanceled(ctx, execution.Run, &assistant, assistantSaved); handled {
		return turnStepResult{Outcome: turnStepCompleted}, cancelErr
	}

	return c.handleAssistantTurnResponse(runCtx, turn, &assistant, assistantSaved, response), nil
}

func (c *Core) applyRunTurnResult(ctx context.Context, execution *runExecution, result turnStepResult) (bool, error) {
	if execution == nil {
		return true, errors.New("core: run execution is required")
	}
	if result.Assistant != nil {
		if handled, cancelErr := c.checkAndHandleCanceled(ctx, execution.Run, result.Assistant, result.AssistantSaved); handled {
			return true, cancelErr
		}
	}
	if result.Err != nil {
		if result.MarkAssistantErrored && result.Assistant != nil {
			return true, c.persistAssistantError(ctx, execution.Run, result.Assistant, result.AssistantSaved, result.Err)
		}
		return true, c.failRunByID(ctx, execution.Run, result.Err)
	}
	switch result.Outcome {
	case turnStepContinue:
		return false, nil
	case turnStepWaitingApproval:
		return true, c.setRunStatus(ctx, &execution.Run, RunStatusWaitingApproval, "")
	case turnStepCompleted:
		if result.Assistant == nil {
			return true, nil
		}
		return true, c.completeAssistantTurn(ctx, &execution.Run, execution.Turn.SessionID, result.Assistant, result.AssistantSaved, result.Response)
	default:
		return true, fmt.Errorf("unknown turn step outcome %d", result.Outcome)
	}
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

func (c *Core) checkAndHandleCanceled(ctx context.Context, run Run, assistant *Message, assistantSaved bool) (bool, error) {
	canceled, err := c.isRunCanceled(ctx, run.ID)
	if err != nil || !canceled {
		return false, nil
	}
	return true, c.finishCanceledAssistant(ctx, assistant, assistantSaved)
}

func (c *Core) persistAssistantError(ctx context.Context, run Run, assistant *Message, assistantSaved bool, cause error) error {
	if assistantSaved {
		if updateErr := c.markAssistantErrored(ctx, assistant, cause); updateErr != nil {
			return c.failRunByID(ctx, run, fmt.Errorf("%v (assistant update failed: %w)", cause, updateErr))
		}
	} else {
		if saveErr := c.saveAssistantErrored(ctx, assistant, cause); saveErr != nil {
			return c.failRunByID(ctx, run, fmt.Errorf("%v (assistant save failed: %w)", cause, saveErr))
		}
	}
	return c.failRunByID(ctx, run, cause)
}
