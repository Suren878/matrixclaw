package core

import (
	"cmp"
	"context"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/transcript"
)

var subagentAgentNamePool = []string{"Neo", "Trinity", "Morpheus", "Niobe", "Seraph", "Oracle", "Link", "Switch", "Apoc", "Tank", "Dozer", "Mouse"}

// createSubagentSession creates a child's hidden session; a read-only external
// child never asks for approval and runs in its runtime's read-only sandbox.
func (c *Core) createSubagentSession(ctx context.Context, parent Session, runtime SubagentRuntime, model string, workingDir string, displayName string, readonly bool) (Session, error) {
	title := "Subagent: " + truncateForTitle(cmp.Or(displayName, "Task"), 64)
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
			Readonly:        readonly,
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
			ModelID:         cmp.Or(normalizeText(model), parent.ModelID),
			PermissionMode:  parent.PermissionMode,
		})
	}
}

// assignSubagentAgentName picks a name no child of the parent has, nor any of
// the children it is starting.
func (c *Core) assignSubagentAgentName(ctx context.Context, parentSessionID string, starting map[string]bool) (string, error) {
	tasks, err := c.store.ListTasks(ctx, TaskFilter{SessionID: parentSessionID, Kind: TaskKindSubagent})
	if err != nil {
		return "", err
	}
	used := make(map[string]struct{}, len(tasks)+len(starting))
	for name := range starting {
		used[strings.ToLower(name)] = struct{}{}
	}
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

func subagentTaskAgentName(task Task) string {
	if name := strings.Join(strings.Fields(task.AgentName), " "); name != "" {
		return name
	}
	if name := strings.Join(strings.Fields(task.Description), " "); name != "" {
		return name
	}
	if id := strings.TrimSpace(task.ID); id != "" {
		return id
	}
	return "subagent"
}

func (c *Core) createSubagentRun(ctx context.Context, session Session, prompt string) (Run, error) {
	result, err := c.createAcceptedRun(ctx, session, newRun{Text: prompt, Parts: transcript.NormalizeMessageParts(prompt, nil)})
	return result.Run, err
}

// subagentRunSummary is what an ended child's run tells its parent, and
// whether the child failed.
func (c *Core) subagentRunSummary(ctx context.Context, run Run) (string, bool) {
	if run.Status == RunStatusFailed || run.Status == RunStatusCanceled {
		if text := strings.TrimSpace(run.Error); text != "" {
			return "Subagent failed: " + text, true
		}
		return "Subagent failed with status " + string(run.Status) + ".", true
	}
	messages, err := c.store.ListRunMessages(ctx, run.SessionID, run.ID)
	if err != nil {
		return "Subagent failed: " + err.Error(), true
	}
	summary := transcript.RunReply(messages, run.ID)
	if run.StopReason.Continuable() {
		if summary == "" {
			summary = "none"
		}
		return "Subagent stopped early (" + strings.ReplaceAll(string(run.StopReason), "_", " ") + "); partial result: " + summary, false
	}
	if summary != "" {
		return summary, false
	}
	return "Subagent completed without a text summary.", false
}

func isSubagentSession(session Session) bool {
	return strings.TrimSpace(session.ParentSessionID) != "" || session.Hidden
}
