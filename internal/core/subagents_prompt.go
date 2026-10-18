package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Suren878/matrixclaw/internal/agent/prompt"
	"github.com/Suren878/matrixclaw/internal/textutil"
	"github.com/Suren878/matrixclaw/internal/tools"
)

// subagentsPrompt is the parent's guidance for the agent tool with the
// runtimes and agent types it may pick now; a coding parent may pick the
// user's agent types too.
func (c *Core) subagentsPrompt(ctx context.Context, coding bool, workingDir string) string {
	runtimes := c.subagentRuntimeInfo(ctx)
	var external []string
	for _, runtime := range runtimes[1:] { // the native runtime comes first
		external = append(external, runtime.PromptLine())
	}
	var types []prompt.AgentType
	for _, profile := range c.agentProfiles(workingDir, coding) {
		types = append(types, prompt.AgentType{Name: profile.Name, Description: profile.Description})
	}
	return prompt.Subagents(prompt.SubagentsInfo{
		Coding:     coding,
		Runtimes:   availableSubagentRuntimeIDs(runtimes),
		External:   external,
		Types:      types,
		Background: c.backgroundAgents,
		Steps:      c.budgets.Subagent.Steps,
	})
}

// agentToolSpec limits the agent tool's runtime and agent type choices to
// those the turn may pick now.
func (c *Core) agentToolSpec(ctx context.Context, spec tools.Spec, turn nativeTurn) tools.Spec {
	var schema map[string]any
	if err := json.Unmarshal(spec.InputJSONSchema, &schema); err != nil {
		return spec
	}
	properties, _ := schema["properties"].(map[string]any)
	runtime, _ := properties["runtime"].(map[string]any)
	agentType, _ := properties["agent_type"].(map[string]any)
	if runtime == nil || agentType == nil {
		return spec
	}
	runtime["enum"] = availableSubagentRuntimeIDs(c.subagentRuntimeInfo(ctx))
	agentType["enum"] = agentTypeNames(c.agentProfiles(turn.WorkingDir, c.codingRun(ctx, turn)))
	if raw, err := json.Marshal(schema); err == nil {
		spec.InputJSONSchema = raw
	}
	return spec
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
		models := textutil.UniqueNonBlank(c.externalAgentModelList(ctx, descriptor.ID)...)
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
