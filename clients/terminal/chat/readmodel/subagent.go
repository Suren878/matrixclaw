package readmodel

import (
	"cmp"
	"strings"

	surfacemessage "github.com/Suren878/matrixclaw/clients/terminal/ui/surface/message"
	"github.com/Suren878/matrixclaw/internal/core"
)

// subagentFrom is the only place that reads the daemon's subagent task.
func subagentFrom(task core.SubagentTask) surfacemessage.Subagent {
	return surfacemessage.Subagent{
		ID:               task.ID,
		Name:             cmp.Or(oneLine(task.AgentName), oneLine(task.DisplayName), runtimeLabel(task.Runtime)),
		Task:             oneLine(task.DisplayName),
		Goal:             oneLine(task.Goal),
		Runtime:          strings.TrimSpace(task.Runtime),
		State:            subagentState(task),
		Summary:          strings.TrimSpace(task.Summary),
		Error:            strings.TrimSpace(task.Error),
		Blocking:         task.Mode == core.SubagentTaskModeBlocking,
		ParentRunID:      strings.TrimSpace(task.ParentRunID),
		ParentToolCallID: strings.TrimSpace(task.ParentToolCallID),
	}
}

func subagentState(task core.SubagentTask) surfacemessage.SubagentState {
	switch task.Status {
	case core.TaskStatusPending:
		return surfacemessage.SubagentPending
	case core.TaskStatusWaitingApproval:
		return surfacemessage.SubagentWaitingApproval
	case core.TaskStatusCompleted:
		return surfacemessage.SubagentCompleted
	case core.TaskStatusCanceled:
		return surfacemessage.SubagentCanceled
	case core.TaskStatusFailed, core.TaskStatusLost:
		return surfacemessage.SubagentFailed
	default:
		return surfacemessage.SubagentRunning
	}
}

// runtimeLabel names the runtime a subagent runs on.
func runtimeLabel(runtime string) string {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "", "matrixclaw", "auto":
		return "MatrixClaw"
	case "codex":
		return "Codex"
	case "claude":
		return "Claude Code"
	default:
		return strings.TrimSpace(runtime)
	}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
