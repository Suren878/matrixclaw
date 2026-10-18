package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/agent"
	"github.com/Suren878/matrixclaw/internal/externalagents"
	"github.com/Suren878/matrixclaw/internal/externalagents/reply"
	"github.com/Suren878/matrixclaw/internal/transcript"
)

const externalRunHeartbeatInterval = 5 * time.Second

// tryExecuteExternalAgentRun executes a claimed run of an external agent's
// session and reports whether the session was one.
func (c *Core) tryExecuteExternalAgentRun(ctx context.Context, runCtx context.Context, claimed claimedRun, session Session) (bool, error) {
	if c.externalStore == nil {
		return false, nil
	}
	run := claimed.Run
	attachment, err := c.externalStore.GetExternalAgentSession(ctx, session.ID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, c.failRun(ctx, run, err)
	}
	runtime, err := c.externalRuntime(attachment.AgentID)
	if err != nil {
		return true, c.failRun(ctx, run, err)
	}
	readonly, err := c.readonlySubagent(ctx, session.ID)
	if err != nil {
		return true, c.failRun(ctx, run, err)
	}
	externalSession := attachment.ExternalSession()
	externalSession.Permissions = externalagents.Permissions{Mode: string(session.PermissionMode), Readonly: readonly}
	return true, c.executeExternalAgentRun(ctx, runCtx, claimed, runtime, attachment, externalSession)
}

func (c *Core) externalRuntime(agentID string) (externalagents.RuntimeAgent, error) {
	registry := c.externalAgentRegistry()
	if registry == nil {
		return nil, fmt.Errorf("%w: external agent registry unavailable", ErrExecutionUnavailable)
	}
	runtime, ok := registry.Get(agentID)
	if !ok {
		return nil, fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
	}
	return runtime, nil
}

func (c *Core) executeExternalAgentRun(ctx context.Context, runCtx context.Context, claimed claimedRun, runtime externalagents.RuntimeAgent, attachment externalagents.SessionAttachment, externalSession externalagents.ExternalSession) error {
	run := claimed.Run
	userMessage, err := c.findRunUserMessage(ctx, run)
	if err != nil {
		return c.failRun(ctx, run, err)
	}
	inputText := userMessage.Content
	if claimed.Recovering {
		// The agent's own tools may have run; only it can say where it stopped.
		messages, err := c.store.ListRunMessages(ctx, run.SessionID, run.ID)
		if err == nil {
			err = c.sealExternalReply(ctx, run.ID, messages)
		}
		if err != nil {
			return c.failRun(ctx, run, err)
		}
		inputText = runRecoveryNotice + "\n\nContinue the existing task from where it stopped. Inspect the workspace before making further changes. Original task for reference:\n" + userMessage.Content
	}
	if strings.TrimSpace(externalSession.Model) == "" {
		externalSession.Model = c.externalAgentDefaultModel(ctx, attachment.AgentID)
		attachment.Model = externalSession.Model
	}
	events, err := runtime.Send(runCtx, externalSession, externalagents.Input{Text: inputText})
	if err != nil {
		if runCtx.Err() != nil {
			return c.finishExternalRunAfterContextStopped(run, nil, false, runtime, externalSession)
		}
		return c.failRun(ctx, run, err)
	}

	assistant := transcript.Message{
		ID:        c.newID("msg"),
		SessionID: run.SessionID,
		RunID:     run.ID,
		Role:      transcript.MessageRoleAssistant,
		Model:     attachment.Model,
		Provider:  attachment.AgentID,
	}
	assistantSaved := false
	progressDirty := false
	lastProgressFlush := time.Time{}
	flushProgress := func(force bool) error {
		if !progressDirty {
			return nil
		}
		now := c.now().UTC()
		if !force && assistantSaved && !lastProgressFlush.IsZero() && now.Sub(lastProgressFlush) < agent.ProgressFlushInterval {
			return nil
		}
		if err := c.saveExternalAssistantProgress(ctx, &assistant, &assistantSaved); err != nil {
			return err
		}
		progressDirty = false
		lastProgressFlush = now
		return c.touchExternalRunActivity(ctx, &run, now)
	}

	ticker := time.NewTicker(agent.ProgressFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-runCtx.Done():
			return c.finishExternalRunAfterContextStopped(run, &assistant, assistantSaved, runtime, externalSession)
		case <-ticker.C:
			if err := flushProgress(false); err != nil {
				return c.failRun(ctx, run, err)
			}
			if err := c.touchExternalRunActivity(ctx, &run, c.now().UTC()); err != nil {
				return c.failRun(ctx, run, err)
			}
			continue
		case event, ok := <-events:
			// What the agent already sent is kept even when the run stopped
			// meanwhile; only the turn's end is not acted on then.
			if ok {
				progressDirty = reply.Apply(&assistant, event) || progressDirty
			}
			if runCtx.Err() != nil {
				return c.finishExternalRunAfterContextStopped(run, &assistant, assistantSaved, runtime, externalSession)
			}
			if !ok {
				return c.failWithReply(ctx, run, &assistant, assistantSaved, "", errors.New("external agent event stream ended before turn completed"))
			}
			switch event.Kind {
			case externalagents.EventTurnStarted:
				if err := c.updateExternalAgentSessionFromEvent(ctx, &attachment, &externalSession, event); err != nil {
					return c.failRun(ctx, run, err)
				}
				if err := c.touchExternalRunActivity(ctx, &run, event.At); err != nil {
					return c.failRun(ctx, run, err)
				}
			case externalagents.EventHeartbeat:
				if err := c.touchExternalRunActivity(ctx, &run, event.At); err != nil {
					return c.failRun(ctx, run, err)
				}
			case externalagents.EventTurnCompleted:
				return c.completeExternalAgentRun(ctx, &run, &assistant, assistantSaved)
			case externalagents.EventTurnFailed:
				errText := strings.TrimSpace(event.Error)
				if errText == "" {
					errText = "external agent turn failed"
				}
				return c.failWithReply(ctx, run, &assistant, assistantSaved, "", errors.New(errText))
			}
			if progressDirty && !assistantSaved {
				if err := flushProgress(true); err != nil {
					return c.failRun(ctx, run, err)
				}
			}
		}
	}
}

func (c *Core) updateExternalAgentSessionFromEvent(ctx context.Context, attachment *externalagents.SessionAttachment, session *externalagents.ExternalSession, event externalagents.Event) error {
	if c == nil || c.externalStore == nil || attachment == nil || session == nil {
		return nil
	}
	threadID := strings.TrimSpace(event.ExternalThreadID)
	sessionID := strings.TrimSpace(event.ExternalSessionID)
	if threadID == "" && sessionID == "" {
		return nil
	}
	if threadID == "" {
		threadID = sessionID
	}
	if sessionID == "" {
		sessionID = threadID
	}
	if attachment.ExternalThreadID == threadID && attachment.ExternalSessionID == sessionID {
		return nil
	}
	attachment.ExternalThreadID = threadID
	attachment.ExternalSessionID = sessionID
	attachment.UpdatedAt = c.now().UTC()
	if strings.TrimSpace(attachment.CWD) == "" {
		attachment.CWD = session.CWD
	}
	if strings.TrimSpace(attachment.Model) == "" {
		attachment.Model = session.Model
	}
	session.ExternalThreadID = threadID
	session.ExternalSessionID = sessionID
	return c.externalStore.SaveExternalAgentSession(ctx, *attachment)
}

func (c *Core) findRunUserMessage(ctx context.Context, run Run) (transcript.Message, error) {
	message, err := c.store.GetMessage(ctx, run.UserMessageID)
	if err == nil && message.SessionID != run.SessionID {
		err = ErrNotFound
	}
	return message, err
}

func (c *Core) saveExternalAssistantProgress(ctx context.Context, assistant *transcript.Message, saved *bool) error {
	now := c.now().UTC()
	if !*saved {
		assistant.CreatedAt = now
		assistant.UpdatedAt = now
		if _, err := c.store.SaveMessageProgress(ctx, *assistant); err != nil {
			return err
		}
		*saved = true
		c.publishEvent(Event{Type: EventMessageCreated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
		return nil
	}
	assistant.UpdatedAt = now
	if err := c.store.UpdateMessageProgress(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventMessageUpdated, SessionID: assistant.SessionID, RunID: assistant.RunID, Payload: *assistant})
	return nil
}

func (c *Core) touchExternalRunActivity(ctx context.Context, run *Run, at time.Time) error {
	if at.IsZero() {
		at = c.now().UTC()
	} else {
		at = at.UTC()
	}
	if !run.UpdatedAt.IsZero() && at.Sub(run.UpdatedAt) < externalRunHeartbeatInterval {
		return nil
	}
	run.UpdatedAt = at
	if err := c.store.TouchRun(ctx, run.ID, at); err != nil {
		return err
	}
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: run.SessionID, RunID: run.ID, Payload: *run})
	return nil
}

// completeExternalAgentRun ends the run with the agent's finished turn.
func (c *Core) completeExternalAgentRun(ctx context.Context, run *Run, assistant *transcript.Message, assistantSaved bool) error {
	assistant.Parts = append(transcript.NormalizeMessageParts(assistant.Content, assistant.Parts), transcript.MessagePart{
		Kind:   transcript.MessagePartKindFinish,
		Finish: &transcript.FinishPart{Reason: transcript.FinishReasonEndTurn},
	})
	return c.transition(ctx, run, runChange{To: RunStatusCompleted, Reply: assistant, ReplySaved: assistantSaved})
}

func (c *Core) finishExternalRunAfterContextStopped(run Run, assistant *transcript.Message, assistantSaved bool, runtime externalagents.RuntimeAgent, session externalagents.ExternalSession) error {
	ctx, cancel := context.WithTimeout(context.Background(), runInterruptionPersistenceTimeout)
	defer cancel()

	current, err := c.store.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if runtime != nil {
		if err := runtime.Interrupt(ctx, session); err != nil {
			log.Printf("core: interrupt external agent %s of run %s: %v", session.AgentID, run.ID, err)
		}
	}
	if current.Status == RunStatusCanceled {
		return c.sealCanceledReply(ctx, assistant, assistantSaved)
	}
	if current.Status.Terminal() {
		return nil
	}
	return c.sealInterruptedReply(ctx, assistant, assistantSaved)
}
