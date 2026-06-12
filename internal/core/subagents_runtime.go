package core

import (
	"context"
	"fmt"
	"strings"
)

var subagentAgentNamePool = []string{"Neo", "Trinity", "Morpheus", "Niobe", "Seraph", "Oracle", "Link", "Switch", "Apoc", "Tank", "Dozer", "Mouse"}

func (c *Core) createSubagentSession(ctx context.Context, parent Session, runtime SubagentRuntime, model string, workingDir string, displayName string) (Session, error) {
	title := "Subagent: " + truncateForTitle(firstNonEmpty(displayName, "Task"), 64)
	switch runtime {
	case SubagentRuntimeCodex, SubagentRuntimeClaude:
		agentID := string(runtime)
		canonical, ok := c.ResolveExternalAgentID(agentID)
		if !ok {
			return Session{}, fmt.Errorf("%w: external agent %q is not configured", ErrExecutionUnavailable, agentID)
		}
		return c.CreateSession(ctx, CreateSessionInput{
			Title:           title,
			Kind:            SessionKindExternalAgent,
			RuntimeID:       SessionRuntimeExternalAgent,
			ParentSessionID: parent.ID,
			Hidden:          true,
			WorkingDir:      workingDir,
			ModelID:         normalizeText(model),
			PermissionMode:  PermissionModeFullAuto,
			ExternalAgentID: canonical,
		})
	default:
		return c.CreateSession(ctx, CreateSessionInput{
			Title:           title,
			Kind:            SessionKindAssistant,
			RuntimeID:       SessionRuntimeMatrixClaw,
			ParentSessionID: parent.ID,
			Hidden:          true,
			WorkingDir:      workingDir,
			ProviderID:      parent.ProviderID,
			ModelID:         firstNonEmpty(normalizeText(model), parent.ModelID),
			PermissionMode:  parent.PermissionMode,
		})
	}
}

func (c *Core) assignSubagentAgentName(ctx context.Context, parentSessionID string) (string, error) {
	tasks, err := c.store.ListSubagentTasks(ctx, SubagentTaskFilter{ParentSessionID: strings.TrimSpace(parentSessionID)})
	if err != nil {
		return "", err
	}
	used := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		name := strings.TrimSpace(task.AgentName)
		if name == "" {
			continue
		}
		used[strings.ToLower(name)] = struct{}{}
	}
	for cycle := 1; ; cycle++ {
		for _, base := range subagentAgentNamePool {
			name := base
			if cycle > 1 {
				name = fmt.Sprintf("%s-%d", base, cycle)
			}
			if _, ok := used[strings.ToLower(name)]; !ok {
				return name, nil
			}
		}
	}
}

func subagentTaskAgentName(task SubagentTask) string {
	if name := strings.Join(strings.Fields(task.AgentName), " "); name != "" {
		return name
	}
	if name := strings.Join(strings.Fields(task.DisplayName), " "); name != "" {
		return name
	}
	if id := strings.TrimSpace(task.ID); id != "" {
		return id
	}
	return "subagent"
}

func (c *Core) createSubagentRun(ctx context.Context, session Session, prompt string) (Run, error) {
	now := c.now().UTC()
	run := Run{
		ID:            c.newID("run"),
		SessionID:     session.ID,
		UserMessageID: c.newID("msg"),
		Status:        RunStatusAccepted,
		StartedAt:     now,
		UpdatedAt:     now,
	}
	message := Message{
		ID:        run.UserMessageID,
		SessionID: session.ID,
		RunID:     run.ID,
		Role:      MessageRoleUser,
		Content:   prompt,
		Parts:     NormalizeMessageParts(prompt, nil),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := c.store.AcceptMessage(ctx, message, run); err != nil {
		return Run{}, err
	}
	c.publishEvent(Event{Type: EventMessageCreated, SessionID: session.ID, RunID: run.ID, Payload: message})
	c.publishEvent(Event{Type: EventRunUpdated, SessionID: session.ID, RunID: run.ID, Payload: run})
	return run, nil
}

func (c *Core) subagentRunSummary(ctx context.Context, sessionID string, runID string, execErr error) (string, bool) {
	if execErr != nil {
		return "Subagent failed: " + execErr.Error(), true
	}
	run, err := c.store.GetRun(ctx, runID)
	if err != nil {
		return "Subagent failed: " + err.Error(), true
	}
	switch run.Status {
	case RunStatusCompleted:
	case RunStatusWaitingApproval:
		return "Subagent requested approval and cannot continue without user interaction in this version.", true
	case RunStatusFailed, RunStatusCanceled:
		if strings.TrimSpace(run.Error) != "" {
			return "Subagent failed: " + strings.TrimSpace(run.Error), true
		}
		return "Subagent failed with status " + string(run.Status) + ".", true
	default:
		return "Subagent stopped with status " + string(run.Status) + ".", true
	}
	messages, err := c.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return "Subagent failed: " + err.Error(), true
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].RunID != runID || messages[i].Role != MessageRoleAssistant {
			continue
		}
		if summary := strings.TrimSpace(messages[i].Content); summary != "" {
			return summary, false
		}
	}
	return "Subagent completed without a text summary.", false
}

func isSubagentSession(session Session) bool {
	return strings.TrimSpace(session.ParentSessionID) != "" || session.Hidden
}
