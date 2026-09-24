package core

import (
	"context"
	"fmt"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	agentcontext "github.com/Suren878/matrixclaw/internal/agent/context"
	"github.com/Suren878/matrixclaw/internal/providers"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

// runStepGenerator records each successful summary generation of a run as a compact step.
type runStepGenerator struct {
	c       *Core
	runtime providers.Runtime
	runID   string
}

func (g runStepGenerator) Generate(ctx context.Context, request providers.Request) (providers.Response, error) {
	started := time.Now()
	response, err := g.runtime.Generate(ctx, request)
	if err == nil {
		g.c.recordRunStep(ctx, agent.Step{RunID: g.runID, Model: response.Model, Provider: response.Provider, Usage: response.Usage, StopReason: "compact", Latency: time.Since(started), ToolCalls: len(response.ToolCalls)})
	}
	return response, err
}

func (c *Core) autoCompactSessionIfNeeded(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, report, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	if !report.Compact.Recommended || agentcontext.CompactBackoffActive(messages) {
		return false, nil
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages, turn.RunID)
}

func (c *Core) forceCompactSessionForRetry(ctx context.Context, turn turnExecution) (bool, error) {
	session, messages, _, err := c.sessionContextSnapshot(ctx, turn.SessionID)
	if err != nil {
		return false, err
	}
	return c.compactSessionWithLoadedMessages(ctx, session, messages, turn.RunID)
}

func (c *Core) providerRequestNeedsCompact(ctx context.Context, turn turnExecution, request providers.Request) bool {
	sessionID := normalizeText(turn.SessionID)
	if sessionID == "" {
		return false
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return false
	}
	window := c.sessionContextWindowTokens(c.decorateSessionLLM(session))
	threshold := agentcontext.Threshold(window)
	return threshold > 0 && agentcontext.EstimateRequestTokens(request) >= threshold
}

func (c *Core) sessionContextSnapshot(ctx context.Context, sessionID string) (Session, []transcript.Message, ContextReport, error) {
	sessionID = normalizeText(sessionID)
	if sessionID == "" {
		return Session{}, nil, ContextReport{}, ErrSessionRequired
	}
	session, err := c.store.GetSession(ctx, sessionID)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	session = c.decorateSessionLLM(session)
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return Session{}, nil, ContextReport{}, err
	}
	return session, messages, c.contextReportForSession(session, messages), nil
}

func (c *Core) compactSessionWithLoadedMessages(ctx context.Context, session Session, messages []transcript.Message, runID string) (bool, error) {
	if _, effective := agentcontext.LatestSummary(messages); len(effective) == 0 {
		return false, nil
	}
	if _, err := c.compactSession(ctx, session.ID, runID); err != nil {
		return false, fmt.Errorf("auto compact session: %w", err)
	}
	return true, nil
}
