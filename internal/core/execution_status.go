package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/providers"
)

func (c *Core) completeAssistantTurn(ctx context.Context, run *Run, sessionID string, assistant *Message, assistantSaved bool, response providers.Response) error {
	if run == nil || assistant == nil {
		return errors.New("core: complete assistant turn requires run and assistant")
	}
	finishedAt := c.now().UTC()
	assistant.Content = sanitizeAssistantOutput(response.Text)
	assistant.Parts = providerResponseMessageParts(assistant.Content, response.ReasoningContent)
	if finish := providerUsageFinishPart(response.Usage); finish != nil {
		assistant.Parts = append(assistant.Parts, *finish)
	}
	assistant.Model = response.Model
	assistant.Provider = response.Provider
	if err := c.CompleteSessionPlanRunStep(ctx, *run, assistant.Content); err != nil {
		return fmt.Errorf("complete plan run step: %w", err)
	}
	if err := c.completeActivePlanItemsIfRunFinished(ctx, sessionID); err != nil {
		return fmt.Errorf("complete active plan items: %w", err)
	}
	if !assistantSaved {
		assistant.CreatedAt = finishedAt
		assistant.UpdatedAt = finishedAt
		run.Status = RunStatusCompleted
		run.Error = ""
		run.FinishedAt = &finishedAt
		run.UpdatedAt = finishedAt

		if err := c.store.CompleteRun(ctx, *assistant, *run); err != nil {
			return fmt.Errorf("complete run: %w", err)
		}
		c.saveRunUsage(ctx, *run, *assistant, response.Usage)
		c.publishEvent(Event{
			Type:      EventMessageCreated,
			SessionID: sessionID,
			RunID:     run.ID,
			Payload:   *assistant,
		})
		c.publishEvent(Event{
			Type:      EventRunUpdated,
			SessionID: sessionID,
			RunID:     run.ID,
			Payload:   *run,
		})
		return nil
	}

	assistant.UpdatedAt = finishedAt
	run.Status = RunStatusCompleted
	run.Error = ""
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt

	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return fmt.Errorf("update assistant message: %w", err)
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	c.saveRunUsage(ctx, *run, *assistant, response.Usage)
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: sessionID,
		RunID:     run.ID,
		Payload:   *assistant,
	})
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: sessionID,
		RunID:     run.ID,
		Payload:   *run,
	})
	return nil
}

func planRunLooksBlocked(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	if content == "" {
		return false
	}
	for _, marker := range []string{"plan_blocked", "blocked", "cannot complete", "can't complete", "не могу выполнить", "не удалось", "заблокировано"} {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

func (c *Core) completeDonePlanParents(ctx context.Context, sessionID string) error {
	for {
		plan, err := c.store.GetSessionPlan(ctx, sessionID)
		if err != nil {
			return nil
		}
		children := make(map[string][]PlanItem, len(plan.Items))
		for _, item := range plan.Items {
			parentID := strings.TrimSpace(item.ParentID)
			if parentID != "" {
				children[parentID] = append(children[parentID], item)
			}
		}
		changed := false
		for _, item := range plan.Items {
			if item.Status == PlanItemDone || item.Status == PlanItemSkipped || len(children[item.ID]) == 0 {
				continue
			}
			if !allPlanChildrenTerminal(children[item.ID]) {
				continue
			}
			if _, err := c.UpdatePlanItem(ctx, sessionID, item.ID, PlanItemDone, ""); err != nil {
				return err
			}
			changed = true
			break
		}
		if !changed {
			return nil
		}
	}
}

func allPlanChildrenTerminal(items []PlanItem) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		switch item.Status {
		case PlanItemDone, PlanItemSkipped:
			continue
		default:
			return false
		}
	}
	return true
}

func (c *Core) completeActivePlanItemsIfRunFinished(ctx context.Context, sessionID string) error {
	if planRun, err := c.store.GetPlanRun(ctx, sessionID); err == nil && planRun.Status == PlanRunBlocked {
		return nil
	}
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return err
	}
	var active []PlanItem
	for _, item := range plan.Items {
		switch item.Status {
		case PlanItemPending:
			return nil
		case PlanItemActive:
			active = append(active, item)
		}
	}
	for _, item := range active {
		if _, err := c.UpdatePlanItem(ctx, sessionID, item.ID, PlanItemDone, ""); err != nil {
			return err
		}
	}
	return nil
}

func providerResponseMessageParts(content string, reasoningContent *string) []MessagePart {
	parts := NormalizeMessageParts(content, nil)
	if reasoningContent == nil {
		return parts
	}
	text := *reasoningContent
	return append(parts, MessagePart{
		Kind: MessagePartKindReasoning,
		Reasoning: &ReasoningPart{
			Text: text,
		},
	})
}

func providerUsageFinishPart(usage providers.Usage) *MessagePart {
	if providerUsageIsZero(usage) {
		return nil
	}
	coreUsage := ProviderUsage{
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
		TotalTokens:     usage.TotalTokens,
		CachedTokens:    usage.CachedTokens,
		ReasoningTokens: usage.ReasoningTokens,
		ProviderRaw:     append([]byte(nil), usage.ProviderRaw...),
	}
	payload, err := json.Marshal(struct {
		Usage ProviderUsage `json:"usage"`
	}{Usage: coreUsage})
	if err != nil {
		return nil
	}
	return &MessagePart{
		Kind: MessagePartKindFinish,
		Finish: &FinishPart{
			Reason:  "end_turn",
			Details: payload,
		},
	}
}

func providerUsageIsZero(usage providers.Usage) bool {
	return usage.InputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.TotalTokens == 0 &&
		usage.CachedTokens == 0 &&
		usage.ReasoningTokens == 0 &&
		len(usage.ProviderRaw) == 0
}

func (c *Core) setRunStatus(ctx context.Context, run *Run, status RunStatus, errText string) error {
	if run == nil {
		return nil
	}
	if run.Status == status && run.Error == errText {
		return nil
	}
	run.Status = status
	run.Error = errText
	run.UpdatedAt = c.now().UTC()
	if status != RunStatusCompleted && status != RunStatusFailed {
		run.FinishedAt = nil
	}
	if err := c.store.UpdateRun(ctx, *run); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: run.SessionID,
		RunID:     run.ID,
		Payload:   *run,
	})
	_ = c.touchAsyncSubagentTaskActivity(ctx, run.ID, run.UpdatedAt)
	return nil
}

func (c *Core) markAssistantErrored(ctx context.Context, assistant *Message, cause error) error {
	if assistant == nil {
		return nil
	}
	assistant.UpdatedAt = c.now().UTC()
	assistant.Parts = appendErrorFinishPart(assistant.Content, cause.Error())
	if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageUpdated,
		SessionID: assistant.SessionID,
		RunID:     assistant.RunID,
		Payload:   *assistant,
	})
	return nil
}

func (c *Core) saveAssistantErrored(ctx context.Context, assistant *Message, cause error) error {
	if assistant == nil {
		return nil
	}
	now := c.now().UTC()
	if assistant.CreatedAt.IsZero() {
		assistant.CreatedAt = now
	}
	assistant.UpdatedAt = now
	assistant.Parts = appendErrorFinishPart(assistant.Content, cause.Error())
	if err := c.store.SaveMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: assistant.SessionID,
		RunID:     assistant.RunID,
		Payload:   *assistant,
	})
	return nil
}

func appendErrorFinishPart(content string, message string) []MessagePart {
	parts := NormalizeMessageParts(content, nil)
	parts = append(parts, MessagePart{
		Kind: MessagePartKindFinish,
		Finish: &FinishPart{
			Reason:  "error",
			Message: message,
		},
	})
	return parts
}

func appendCanceledFinishPart(content string, message string) []MessagePart {
	parts := NormalizeMessageParts(content, nil)
	parts = append(parts, MessagePart{
		Kind: MessagePartKindFinish,
		Finish: &FinishPart{
			Reason:  "canceled",
			Message: message,
		},
	})
	return parts
}

func (c *Core) isRunCanceled(ctx context.Context, runID string) (bool, error) {
	runID = normalizeText(runID)
	if runID == "" {
		return false, nil
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return false, err
	}
	return run.Status == RunStatusCanceled, nil
}

func (c *Core) finishCanceledAssistant(ctx context.Context, assistant *Message, assistantSaved bool) error {
	if assistant == nil {
		return nil
	}
	message := "Canceled by user."
	now := c.now().UTC()
	if assistantSaved {
		assistant.UpdatedAt = now
		assistant.Parts = appendCanceledFinishPart(assistant.Content, message)
		if err := c.store.UpdateMessage(ctx, *assistant); err != nil {
			return err
		}
		c.publishEvent(Event{
			Type:      EventMessageUpdated,
			SessionID: assistant.SessionID,
			RunID:     assistant.RunID,
			Payload:   *assistant,
		})
		return nil
	}
	if assistant.CreatedAt.IsZero() {
		assistant.CreatedAt = now
	}
	assistant.UpdatedAt = now
	assistant.Parts = appendCanceledFinishPart(assistant.Content, message)
	if err := c.store.SaveMessage(ctx, *assistant); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventMessageCreated,
		SessionID: assistant.SessionID,
		RunID:     assistant.RunID,
		Payload:   *assistant,
	})
	return nil
}

func (c *Core) failAcceptedRun(ctx context.Context, result AcceptRunResult, cause error) (AcceptRunResult, error) {
	if err := c.failRunByID(ctx, result.Run, cause); err != nil {
		return result, err
	}

	result.Run.Status = RunStatusFailed
	result.Run.Error = cause.Error()
	finishedAt := c.now().UTC()
	result.Run.FinishedAt = &finishedAt
	result.Run.UpdatedAt = finishedAt
	return result, cause
}

func (c *Core) failRunByID(ctx context.Context, run Run, cause error) error {
	finishedAt := c.now().UTC()
	run.Status = RunStatusFailed
	run.Error = cause.Error()
	run.FinishedAt = &finishedAt
	run.UpdatedAt = finishedAt

	if err := c.store.UpdateRun(ctx, run); err != nil {
		return err
	}
	c.publishEvent(Event{
		Type:      EventRunUpdated,
		SessionID: run.SessionID,
		RunID:     run.ID,
		Payload:   run,
	})
	return cause
}
