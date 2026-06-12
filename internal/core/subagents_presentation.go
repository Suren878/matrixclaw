package core

import (
	"fmt"
	"strings"

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

func subagentUserPrompt(goal string, contextText string, workingDir string) string {
	lines := []string{
		"Delegated task:",
		strings.TrimSpace(goal),
	}
	if contextText = strings.TrimSpace(contextText); contextText != "" {
		lines = append(lines, "", "Context:", contextText)
	}
	if workingDir = strings.TrimSpace(workingDir); workingDir != "" {
		lines = append(lines, "", "Working directory:", workingDir)
	}
	lines = append(lines, "", "Return a concise result for the parent agent. Include important files, findings, errors, and verification output. Do not ask the user questions.")
	return strings.Join(lines, "\n")
}

func asyncSubagentUserPrompt(goal string, contextText string, workingDir string, isolation SubagentIsolation) string {
	prompt := subagentUserPrompt(goal, contextText, workingDir)
	switch isolation {
	case SubagentIsolationWorktree:
		prompt += "\nIsolation: worktree requested. Keep edits scoped to the isolated worktree assigned by the parent runtime. Do not edit files outside the working directory."
	default:
		prompt += "\nIsolation: shared working copy. Prefer read-only investigation unless the parent explicitly requested edits."
	}
	return prompt
}

func subagentSystemPrompt() string {
	return "Subagent mode:\n- You are a child agent working for a parent Matrixclaw agent.\n- Complete only the delegated task from the user message.\n- Do not ask the user for input or approval.\n- Return a concise summary for the parent agent, listing important files, findings, errors, and verification output."
}

func subagentToolAllowed(spec tools.Spec) bool {
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	if id == delegateTaskToolName || id == "memory" || strings.HasPrefix(id, "plan_") || id == "text_to_speech" {
		return false
	}
	namespace := strings.ToLower(strings.TrimSpace(spec.Namespace))
	if namespace == "core.memory" || namespace == "core.plan" {
		return false
	}
	switch spec.Category {
	case tools.CategoryAutomation, tools.CategoryStorage, tools.CategorySkills:
		return false
	}
	return true
}

func delegateTaskResultContent(result DelegateTaskResult) string {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "Subagent completed without a text summary."
	}
	return summary
}

func delegateTaskResultStatus(result DelegateTaskResult) tools.ResultStatus {
	if result.IsError {
		return tools.ResultStatusError
	}
	return tools.ResultStatusSuccess
}

func generatedSubagentDisplayName(goal string) string {
	goal = strings.Join(strings.Fields(goal), " ")
	if goal == "" {
		return "Subagent"
	}
	return truncateForTitle(goal, 48)
}

func normalizeSubagentDisplayName(name string, goal string) string {
	if name = strings.Join(strings.Fields(name), " "); name != "" {
		return truncateForTitle(name, 48)
	}
	return generatedSubagentDisplayName(goal)
}

func subagentParentToolName(task SubagentTask) string {
	if task.Mode == SubagentTaskModeAsync {
		return spawnSubagentToolName
	}
	return delegateTaskToolName
}

func spawnSubagentResultContent(result SpawnSubagentResult) string {
	task := result.Task
	prefix := "started"
	if result.Replayed {
		prefix = "already running"
		if subagentTaskTerminalStatus(task.Status) {
			prefix = "already finished"
		}
	}
	lines := []string{
		fmt.Sprintf("Subagent %s %s", subagentTaskAgentName(task), prefix),
		"Task ID: " + task.ID,
		"Status: " + string(task.Status),
	}
	if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
		lines = append(lines, "Task: "+taskLabel)
	}
	if goal := strings.TrimSpace(task.Goal); goal != "" {
		lines = append(lines, "Goal: "+goal)
	}
	return strings.Join(lines, "\n")
}

func formatSubagentTaskList(tasks []SubagentTask) string {
	if len(tasks) == 0 {
		return "No subagents."
	}
	var lines []string
	for _, task := range tasks {
		line := fmt.Sprintf("- %s [%s] %s", subagentTaskAgentName(task), task.Status, task.ID)
		if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
			line += " - " + taskLabel
		}
		if task.Summary != "" && subagentTaskTerminalStatus(task.Status) {
			line += ": " + strings.TrimSpace(task.Summary)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (c *Core) subagentTaskDetail(task SubagentTask) string {
	lines := []string{
		fmt.Sprintf("Subagent: %s", subagentTaskAgentName(task)),
		"Task ID: " + task.ID,
		"Work job: " + task.ID,
		"Status: " + string(task.Status),
		"Runtime: " + task.Runtime,
	}
	if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
		lines = append(lines, "Task: "+taskLabel)
	}
	lines = append(lines, "Goal: "+task.Goal)
	if task.Summary != "" {
		lines = append(lines, "", "Summary:", task.Summary)
	}
	if task.Error != "" {
		lines = append(lines, "", "Error:", task.Error)
	}
	return strings.Join(lines, "\n")
}

func subagentFinishedResultContent(task SubagentTask) string {
	verb := "finished"
	if subagentTaskFailed(task) {
		verb = "failed"
	}
	lines := []string{
		fmt.Sprintf("Subagent %s %s", subagentTaskAgentName(task), verb),
		"Task ID: " + task.ID,
		"Status: " + string(task.Status),
	}
	if taskLabel := strings.TrimSpace(task.DisplayName); taskLabel != "" {
		lines = append(lines, "Task: "+taskLabel)
	}
	if summary := strings.TrimSpace(task.Summary); summary != "" {
		lines = append(lines, "", summary)
	}
	if task.Error != "" && !strings.Contains(strings.TrimSpace(task.Summary), strings.TrimSpace(task.Error)) {
		lines = append(lines, "", "Error: "+task.Error)
	}
	return strings.Join(lines, "\n")
}

func subagentCompletionPrompt(tasks []SubagentTask) string {
	if len(tasks) == 1 {
		task := tasks[0]
		return strings.Join([]string{
			fmt.Sprintf("Subagent %s completed.", subagentTaskAgentName(task)),
			"Task: " + strings.TrimSpace(task.DisplayName),
			"Goal: " + task.Goal,
			"Status: " + string(task.Status),
			"Summary: " + firstNonEmpty(task.Summary, task.Error),
			"",
			"Briefly synthesize this result for the user and decide whether any follow-up action is needed.",
		}, "\n")
	}
	lines := []string{"Multiple subagents completed:"}
	for _, task := range tasks {
		lines = append(lines, fmt.Sprintf("- %s [%s]: %s", subagentTaskAgentName(task), task.Status, firstNonEmpty(task.Summary, task.Error)))
	}
	lines = append(lines, "", "Briefly synthesize these results for the user and decide whether any follow-up action is needed.")
	return strings.Join(lines, "\n")
}

func subagentCompletionTriggerID(parentSessionID string, tasks []SubagentTask) string {
	ids := make([]string, 0, len(tasks)+1)
	ids = append(ids, parentSessionID)
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return "subagent_completion_" + stableIDPart(strings.Join(ids, "_"))
}

func subagentTaskTerminalStatus(status SubagentTaskStatus) bool {
	return status == SubagentTaskStatusCompleted || status == SubagentTaskStatusFailed || status == SubagentTaskStatusCanceled
}

func subagentTaskFailed(task SubagentTask) bool {
	return task.Status == SubagentTaskStatusFailed || task.Status == SubagentTaskStatusCanceled || strings.TrimSpace(task.Error) != ""
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
	if subagentTaskTerminalStatus(task.Status) {
		return ToolLifecycleCompleted
	}
	if task.Status == SubagentTaskStatusWaitingApproval {
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
