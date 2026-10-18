package core

import (
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/textutil"
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
// works, what it may change and the form of its answer.
func subagentUserPrompt(prompt string, workingDir string, task Task) string {
	lines := []string{"Delegated task:", strings.TrimSpace(prompt)}
	if workingDir = strings.TrimSpace(workingDir); workingDir != "" {
		lines = append(lines, "", "Working directory:", workingDir)
	}
	switch {
	case task.Readonly:
		lines = append(lines, "", "Read-only: inspect and report; do not change files.")
	case task.Isolation == SubagentIsolationWorktree:
		lines = append(lines, "", fmt.Sprintf("This is your own git worktree (%s) on branch %s, started from the parent's files including their uncommitted changes. Keep every change inside it. What you change is committed to the branch when you finish, and the parent merges it.", task.WorkingDir, subagentBranch(task.ID)))
	}
	if len(task.OutputSchema) > 0 {
		lines = append(lines, "", outputSchemaPrompt(task.OutputSchema))
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

func truncateForTitle(value string, maxRunes int) string {
	return textutil.Clip(strings.Join(strings.Fields(value), " "), maxRunes, "")
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

// subagentDisplayName is the child's one-line label: the call's description,
// or else the first line of its prompt, whitespace collapsed.
func subagentDisplayName(description string, prompt string) string {
	label := description
	if strings.TrimSpace(label) == "" {
		label, _, _ = strings.Cut(strings.TrimSpace(prompt), "\n")
	}
	return truncateForTitle(label, 48)
}

// backgroundAgentContent tells the model the background task a child runs as.
func backgroundAgentContent(result AgentResult) string {
	task := result.Task
	state := "started"
	switch {
	case result.Replayed && task.Status.Terminal():
		state = "already finished"
	case result.Replayed:
		state = "already running"
	}
	return fmt.Sprintf("Subagent %s %s as background task %s. Its result arrives as a message when it finishes; wait for it with await, or go on with other work.", subagentTaskAgentName(task), state, task.ID)
}
