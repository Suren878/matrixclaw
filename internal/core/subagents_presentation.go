package core

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/tools"
)

func normalizeSubagentRuntime(runtime string) SubagentRuntime {
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "", string(SubagentRuntimeMatrixClaw), string(SubagentRuntimeAuto):
		return SubagentRuntimeMatrixClaw
	case string(SubagentRuntimeCodex), "codex-app", "openai-codex":
		return SubagentRuntimeCodex
	case string(SubagentRuntimeClaude), "claude-code", "claudecode":
		return SubagentRuntimeClaude
	default:
		return SubagentRuntime(strings.ToLower(strings.TrimSpace(runtime)))
	}
}

func subagentTaskRuntimeLabel(runtime SubagentRuntime, child Session) string {
	if CoreSessionIsExternalAgent(child) && strings.TrimSpace(child.ExternalAgentID) != "" {
		return strings.TrimSpace(child.ExternalAgentID)
	}
	return string(runtime)
}

// subagentUserPrompt is a child's assignment: the parent's prompt, where it
// works and what it may change.
func subagentUserPrompt(prompt string, workingDir string, isolation SubagentIsolation, readonly bool) string {
	lines := []string{"Delegated task:", strings.TrimSpace(prompt)}
	if workingDir = strings.TrimSpace(workingDir); workingDir != "" {
		lines = append(lines, "", "Working directory:", workingDir)
	}
	switch {
	case readonly:
		lines = append(lines, "", "Read-only: inspect and report; do not change files.")
	case isolation == SubagentIsolationWorktree:
		lines = append(lines, "", "This is your own git worktree; keep your changes inside it. The parent merges them.")
	}
	lines = append(lines, "", "Return a concise result for the parent agent. Include important files, findings, errors, and verification output. Do not ask the user questions.")
	return strings.Join(lines, "\n")
}

func subagentSystemPrompt(readonly bool) string {
	lines := []string{
		"Subagent mode:",
		"- You are a child agent working for a parent Matrixclaw agent.",
		"- Complete only the delegated task from the user message.",
		"- Track multi-step work with todo_write.",
		"- Commands you start in the background are stopped when you finish.",
		"- Do not ask the user for input or approval.",
		"- Return a concise summary for the parent agent, listing important files, findings, errors, and verification output.",
	}
	if readonly {
		lines = append(lines, "- You have read-only tools: inspect, do not change anything.")
	}
	return strings.Join(lines, "\n")
}

// subagentToolAllowed says whether children get a tool: not the agent tool,
// await, memory, voice or other automation, storage and skill tools.
func subagentToolAllowed(spec tools.Spec) bool {
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	if id == todo.ToolName {
		return true
	}
	if id == "memory" || id == "text_to_speech" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(spec.Namespace)) == "core.memory" {
		return false
	}
	switch spec.Category {
	case tools.CategoryAutomation, tools.CategoryStorage, tools.CategorySkills:
		return false
	}
	return true
}

func subagentTaskFailed(task SubagentTask) bool {
	return task.Status == TaskStatusFailed || task.Status == TaskStatusCanceled || strings.TrimSpace(task.Error) != ""
}

func subagentTaskToolResultStatus(task SubagentTask) tools.ResultStatus {
	if subagentTaskFailed(task) {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func subagentTaskToolLifecycleState(task SubagentTask) ToolLifecycleState {
	if subagentTaskFailed(task) {
		return ToolLifecycleFailed
	}
	if taskStatusTerminal(task.Status) {
		return ToolLifecycleCompleted
	}
	if task.Status == TaskStatusWaitingApproval {
		return ToolLifecycleWaitingApproval
	}
	return ToolLifecycleRequested
}

func truncateForTitle(value string, maxRunes int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func agentResultContent(result AgentResult) string {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "Subagent completed without a text summary."
	}
	return summary
}

func agentResultStatus(result AgentResult) tools.ResultStatus {
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

// subagentDisplayName is the child's label: the call's description, or the
// start of its prompt.
func subagentDisplayName(description string, prompt string) string {
	if description = strings.Join(strings.Fields(description), " "); description != "" {
		return truncateForTitle(description, 48)
	}
	return truncateForTitle(prompt, 48)
}

// backgroundAgentContent tells the model the background task a child runs as.
func backgroundAgentContent(result AgentResult) string {
	task := result.Task
	state := "started"
	switch {
	case result.Replayed && taskStatusTerminal(task.Status):
		state = "already finished"
	case result.Replayed:
		state = "already running"
	}
	return fmt.Sprintf("Subagent %s %s as background task %s. Its result arrives as a message when it finishes; wait for it with await, or go on with other work.", subagentTaskAgentName(task), state, task.ID)
}
