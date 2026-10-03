package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// continueRunText is the user message of a run started by /continue.
const continueRunText = "Continue"

// maxContinuedRuns bounds how far back a chain of /continue runs is followed.
const maxContinuedRuns = 32

// acceptContinueRun starts a run that continues the session's latest run over the
// same history with a fresh budget.
func (c *Core) acceptContinueRun(ctx context.Context, input HandleMessageInput) (AcceptRunResult, error) {
	session, err := c.resolveSession(ctx, input)
	if err != nil {
		return AcceptRunResult{}, err
	}
	gate := c.sessionGate(session.ID)
	gate.Lock()
	result, err := c.createContinueRun(ctx, session, input)
	gate.Unlock()
	if err != nil {
		return AcceptRunResult{}, err
	}
	if err := c.startRun(ctx, result.Run.ID); err != nil {
		return c.failAcceptedRun(ctx, result, err)
	}
	return result, nil
}

func (c *Core) createContinueRun(ctx context.Context, session Session, input HandleMessageInput) (AcceptRunResult, error) {
	if _, err := c.store.GetActiveRunBySession(ctx, session.ID); err == nil {
		return AcceptRunResult{}, fmt.Errorf("%w: wait for the current run to finish before continuing", ErrRunActive)
	} else if !errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, err
	}
	latest, err := c.store.GetLatestRunBySession(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return AcceptRunResult{}, fmt.Errorf("%w: the session has no run to continue", ErrInvalidInput)
	}
	if err != nil {
		return AcceptRunResult{}, err
	}
	if latest.StopReason == agent.StopContextExhausted {
		return AcceptRunResult{}, fmt.Errorf("%w: the conversation no longer fits the model's context; clear it with /context clear or start a /new session", ErrInvalidInput)
	}
	parts := transcript.NormalizeMessageParts(continueRunText, nil)
	run := clientRun(continueRunText, parts, input.Client, input.ExternalKey, input.ClientCapabilities, deliveryTo{input.DeliveryAddress, input.ReplyOnce})
	run.ContinuesRunID = latest.ID
	return c.createAcceptedRun(ctx, session, run)
}

// continuedRuns lists the runs run continues, nearest first; the chain ends at a
// run that continues nothing or is gone.
func (c *Core) continuedRuns(ctx context.Context, run Run) ([]string, error) {
	var ids []string
	seen := map[string]bool{run.ID: true}
	for id := run.ContinuesRunID; id != "" && !seen[id] && len(ids) < maxContinuedRuns; {
		ids = append(ids, id)
		seen[id] = true
		earlier, err := c.store.GetRun(ctx, id)
		if errors.Is(err, ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		id = earlier.ContinuesRunID
	}
	return ids, nil
}
