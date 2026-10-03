package core

import (
	"context"
	"fmt"
	"strings"
)

func (c *Core) agentGuidancePrompt(ctx context.Context) string {
	lines := []string{
		"Subagents:",
		"- The agent tool runs a child agent on a bounded task. Give it a short description and a prompt with everything it needs: the goal, the context and what to report back. It sees nothing of this conversation.",
		"- Use readonly:true for research, reviews and questions: the child gets read-only tools, and read-only children in one reply run in parallel.",
		"- A child that changes files works in your directory (isolation shared: children you wait for run one at a time, background ones alongside you and each other with only single edits taking turns) or in its own git worktree (isolation worktree, several at once; you merge their work).",
		"- Without background the call returns the child's result, and several such calls in one reply run together; their time does not count against your budget.",
		fmt.Sprintf("- With background:true the call returns a task id at once and the result arrives as a message when the child finishes; wait for it with await. At most %d background children run at once.", c.backgroundAgents),
		"- Children cannot start agents or await; they may use todo_write and background commands. Results return to you, the parent agent.",
		"- Available runtime configuration:",
	}
	runtimes := c.subagentRuntimeInfo(ctx)
	available := 0
	for _, runtime := range runtimes {
		if runtime.Available {
			available++
		}
		lines = append(lines, "- "+runtime.PromptLine())
	}
	if ids := availableSubagentRuntimeIDs(runtimes); len(ids) > 0 {
		lines = append(lines,
			"- Runtime IDs available for the agent tool: "+strings.Join(ids, ", ")+".",
			"- When asked which subagent runtimes are available, answer from that Runtime IDs list and include the native matrixclaw runtime.",
			"- Treat user questions in any language about available or connected subagents as questions about that Runtime IDs list, not only about your base model/provider.",
			"- For the current configuration, if asked which subagents or subagent runtimes are available or connected, answer exactly: "+strings.Join(ids, ", ")+".",
		)
	}
	lines = append(lines,
		"- Do not select unavailable runtimes.",
		"- If the user names an unavailable runtime, say it is unavailable and offer the available alternatives.",
	)
	if available <= 1 {
		lines = append(lines, "- If the user asks for a subagent without naming a runtime, use matrixclaw and do not ask which runtime.")
	} else {
		lines = append(lines, "- If the user asks for a subagent without naming a runtime, ask the user which runtime to use unless the request itself makes the runtime obvious.")
	}
	return strings.Join(lines, "\n")
}

func (c *Core) agentToolDescription(ctx context.Context, base string) string {
	runtimes := c.subagentRuntimeInfo(ctx)
	if len(runtimes) == 0 {
		return base
	}
	lines := []string{strings.TrimSpace(base), "Runtime choices:"}
	for _, runtime := range runtimes {
		lines = append(lines, "- "+runtime.PromptLine())
	}
	return strings.Join(lines, "\n")
}

type subagentRuntimeInfo struct {
	Runtime   string
	Label     string
	Available bool
	Detail    string
	Models    []string
}

func (r subagentRuntimeInfo) PromptLine() string {
	status := "unavailable"
	if r.Available {
		status = "available"
	}
	details := []string{}
	if label := strings.TrimSpace(r.Label); label != "" && label != r.Runtime {
		details = append(details, label)
	}
	if len(r.Models) > 0 {
		details = append(details, "models: "+strings.Join(r.Models, ", "))
	}
	if detail := strings.TrimSpace(r.Detail); detail != "" {
		details = append(details, detail)
	}
	if len(details) == 0 {
		return fmt.Sprintf("%s: %s", r.Runtime, status)
	}
	return fmt.Sprintf("%s: %s (%s)", r.Runtime, status, strings.Join(details, "; "))
}

func (c *Core) subagentRuntimeInfo(ctx context.Context) []subagentRuntimeInfo {
	out := []subagentRuntimeInfo{
		{
			Runtime:   string(SubagentRuntimeMatrixClaw),
			Label:     "native MatrixClaw child session",
			Available: true,
		},
	}
	if c == nil || c.externalAgentRegistry() == nil {
		return out
	}
	for _, descriptor := range c.ExternalAgents(ctx) {
		runtime := subagentRuntimeAlias(descriptor)
		if runtime == "" {
			continue
		}
		models := normalizeModelNames(c.externalAgentModelList(ctx, descriptor.ID))
		out = append(out, subagentRuntimeInfo{
			Runtime:   runtime,
			Label:     strings.TrimSpace(descriptor.DisplayName),
			Available: descriptor.Installed && descriptor.Enabled,
			Detail:    subagentRuntimeDetail(descriptor),
			Models:    models,
		})
	}
	return out
}

func subagentRuntimeAlias(descriptor ExternalAgentDescriptor) string {
	if len(descriptor.Aliases) > 0 {
		return strings.TrimSpace(descriptor.Aliases[0])
	}
	return strings.TrimSpace(descriptor.ID)
}

func subagentRuntimeDetail(descriptor ExternalAgentDescriptor) string {
	if detail := strings.TrimSpace(descriptor.Detail); detail != "" {
		return detail
	}
	if !descriptor.Installed {
		return "not installed"
	}
	if !descriptor.Enabled {
		return "disabled"
	}
	return ""
}

func normalizeModelNames(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	return out
}

func availableSubagentRuntimeIDs(runtimes []subagentRuntimeInfo) []string {
	out := make([]string, 0, len(runtimes))
	for _, runtime := range runtimes {
		if !runtime.Available {
			continue
		}
		id := strings.TrimSpace(runtime.Runtime)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}
