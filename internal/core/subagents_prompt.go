package core

import (
	"context"
	"fmt"
	"strings"
)

func (c *Core) delegateTaskGuidancePrompt(ctx context.Context) string {
	lines := []string{
		"Subagents:",
		"- You have delegate_task for blocking child-agent work and spawn_subagent for async background child-agent work.",
		"- Use delegate_task when the child result is needed before your next response.",
		"- Use spawn_subagent only for independent tasks where you can continue without the result; it returns a handle immediately and the result will be delivered back to this parent session later.",
		"- Use list_subagents and read_subagent_result to inspect async subagents without pulling full child transcripts into context.",
		"- Keep at most 4 active async subagents per parent session.",
		"- Give every async subagent a short name, a bounded goal, expected output, and only the minimum context needed.",
		"- Use isolation=shared for read-only/research tasks and isolation=worktree for independent write-heavy tasks. Do not run multiple writer subagents against the same files.",
		"- Do not create agent teams, shared task lists, or direct communication between subagents. Results return to you, the parent agent.",
		"- Do not call delegate_task or spawn_subagent recursively from child subagent sessions.",
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
			"- Runtime IDs available for delegate_task: "+strings.Join(ids, ", ")+".",
			"- When asked which subagent runtimes are available, answer from that Runtime IDs list and include the native matrixclaw runtime.",
			"- Treat user questions in any language about available or connected subagents as questions about that delegate_task Runtime IDs list, not only about your base model/provider.",
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

func (c *Core) delegateTaskToolDescription(ctx context.Context, base string) string {
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
	if c == nil || c.externalAgents == nil {
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
