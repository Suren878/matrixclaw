package core

import (
	"context"
	"fmt"
	"strings"
)

func joinPromptSections(sections ...string) string {
	values := make([]string, 0, len(sections))
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			values = append(values, section)
		}
	}
	return strings.Join(values, "\n\n")
}

func AssistantSystemPrompt(profile AssistantProfile) string {
	name := strings.Join(strings.Fields(profile.Name), " ")
	systemPrompt := strings.TrimSpace(profile.SystemPrompt)
	languageGuidance := responseLanguageGuidancePrompt()
	if name == "" {
		return joinPromptSections(systemPrompt, languageGuidance)
	}
	identity := fmt.Sprintf("Assistant identity:\n- Your configured assistant name is %q. Use this exact name when asked who you are. If older/default instructions mention a different assistant name, this configured name takes precedence.", name)
	if systemPrompt == "" {
		return joinPromptSections(identity, languageGuidance)
	}
	return joinPromptSections(identity, systemPrompt, languageGuidance)
}

func responseLanguageGuidancePrompt() string {
	return "Response language:\n- Reply in the same language the user uses for the current request.\n- If the user mixes languages, use the language that best matches the user's latest request.\n- Do not force English or Russian unless the user asks for that language."
}

func (c *Core) webResearchPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	_, ok := c.tools.Spec("web_research")
	return ok
}

func webResearchGuidancePrompt() string {
	return strings.TrimSpace(`Web research:
- For current or changing information, websites, ratings, reviews, prices, schedules, web search, or source-backed answers, prefer web_research over legacy web_search/web_fetch.
- For follow-up questions about prior web research, use web_research_ask with the prior research_id before starting a new search.
- web_research returns compact facts, sources, warnings, next actions, and a research_id; raw HTML, long page text, DOM snapshots, and screenshots are stored as artifacts and should not be pasted into chat.
- Use browser=always only when the user explicitly asks to visit/look at a site, the task is visual, or fetch results are empty/dynamic/blocked. If browser fallback is unavailable, report the setup hint returned by the tool.`)
}

func voiceOutputGuidancePrompt() string {
	return "Voice output:\n- When the user asks for spoken, audio, voice, or TTS output, call the text_to_speech tool with the text that should be spoken.\n- Do not use shell commands, Piper runtime inspection, or local audio files for client voice output.\n- After a successful text_to_speech tool call, keep any follow-up text minimal unless the user asks for an explanation."
}

func (c *Core) delegateTaskPromptAvailable() bool {
	if c == nil || c.tools == nil {
		return false
	}
	if _, ok := c.tools.Spec(delegateTaskToolName); ok {
		return true
	}
	_, ok := c.tools.Spec(spawnSubagentToolName)
	return ok
}

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
			"- Russian requests like \"какие субагенты доступны\" or \"какие субагенты подключены\" also refer to that delegate_task Runtime IDs list, not only to your base model/provider.",
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

func currentProjectRootPrompt(workingDir string) string {
	return fmt.Sprintf("Current project root:\n- The filesystem working directory for this session is %q.\n- Resolve relative filesystem tool paths under this directory.\n- Use paths inside this project root unless the user explicitly asks for another location.", workingDir)
}

func (c *Core) sessionPlanPrompt(ctx context.Context, sessionID string) string {
	if c == nil || c.store == nil {
		return ""
	}
	plan, err := c.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return ""
	}
	lines := []string{
		"Session goal and plan:",
		"- Use plan tools for multi-step work: plan_get, plan_set_goal, plan_add_item, plan_update_item, plan_clear.",
		"- Skip plans for simple one-step requests; for larger tasks, keep top-level items current and mark completed work done before claiming completion.",
		"- Use subtasks only for genuinely large items; finish subtasks before marking the parent done.",
	}
	if strings.TrimSpace(plan.Goal) != "" {
		lines = append(lines, "- Current goal: "+strings.TrimSpace(plan.Goal))
	}
	depths := PlanItemDepths(plan.Items)
	for i, item := range plan.Items {
		indent := strings.Repeat("  ", min(depths[item.ID], 4))
		lines = append(lines, fmt.Sprintf("- %s%d. [%s] %s (id: %s)", indent, i+1, item.Status, item.Text, item.ID))
	}
	return strings.Join(lines, "\n")
}
