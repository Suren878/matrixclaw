package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

var (
	agentsRoot       = verb(CommandModules, "agents", 0)
	agentsOpen       = verb(CommandModules, "agents open", 1)
	agentsEnable     = verb(CommandModules, "agents enable", 1)
	agentsDisable    = verb(CommandModules, "agents disable", 1)
	agentsEnabled    = verb(CommandModules, "agents enabled", 1)
	agentsSetEnabled = verb(CommandModules, "agents set-enabled", 2)
	agentsPath       = valueVerb(CommandModules, "agents path", 1)
)

func (d *Dispatcher) handleExternalAgents(ctx context.Context, r Route) (Result, error) {
	agentID := r.arg(0)
	if r.Is(agentsRoot) || agentID == "" {
		return d.externalAgentsPicker(ctx)
	}
	switch r.Verb {
	case agentsEnable:
		return d.updateExternalAgentEnabled(ctx, agentID, true)
	case agentsDisable:
		return d.updateExternalAgentEnabled(ctx, agentID, false)
	case agentsEnabled:
		return d.externalAgentEnabledPicker(ctx, agentID)
	case agentsSetEnabled:
		return d.setExternalAgentEnabled(ctx, agentID, r.arg(1))
	case agentsPath:
		if r.Value == "" {
			return d.externalAgentPathPrompt(ctx, agentID)
		}
		return d.updateExternalAgentPath(ctx, agentID, r.Value)
	default:
		return d.externalAgentPicker(ctx, agentID)
	}
}

func (d *Dispatcher) externalAgentsPicker(ctx context.Context) (Result, error) {
	agents, err := d.daemon.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerExternalAgents, "External Agents").Command(agentsRoot.command()).Back(modulesRoot.command())
	for _, agent := range agents {
		picker.Item(PickerItem{
			ID:       agent.ID,
			Title:    externalAgentTitle(agent),
			Info:     externalAgentInfo(agent),
			Selected: agent.Enabled,
			Command:  agentsOpen.command(agent.ID),
		})
	}
	if len(agents) == 0 {
		picker.Static("empty", "No external agents", "No runtimes registered")
	}
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) externalAgentPicker(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.daemon.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return textResult("External agent not found: " + strings.TrimSpace(agentID)), nil
	}
	return Result{Screen: externalAgentPickerData(agent)}, nil
}

func (d *Dispatcher) externalAgentEnabledPicker(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.daemon.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return textResult("External agent not found: " + strings.TrimSpace(agentID)), nil
	}
	return Result{
		Screen: NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
			Meta("Currently " + strings.ToLower(formatEnabled(agent.Enabled))).
			Select(agentsOpen.command(agent.ID)).
			Item(PickerItem{ID: "on", Title: "On", Selected: agent.Enabled, Command: agentsSetEnabled.command(agent.ID, "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: !agent.Enabled, Command: agentsSetEnabled.command(agent.ID, "off")}).
			Build(),
	}, nil
}

func (d *Dispatcher) setExternalAgentEnabled(ctx context.Context, agentID string, value string) (Result, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enable", "enabled", "on", "yes", "true":
		return d.updateExternalAgentEnabled(ctx, agentID, true)
	case "disable", "disabled", "off", "no", "false":
		return d.updateExternalAgentEnabled(ctx, agentID, false)
	default:
		return d.externalAgentEnabledPicker(ctx, agentID)
	}
}

func (d *Dispatcher) updateExternalAgentEnabled(ctx context.Context, agentID string, enabled bool) (Result, error) {
	agents, err := d.daemon.UpdateExternalAgent(ctx, agentID, core.UpdateExternalAgentRequest{Enabled: &enabled})
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return textResult("External agent updated."), nil
	}
	return Result{
		Screen: externalAgentPickerData(agent),
	}, nil
}

func (d *Dispatcher) externalAgentPathPrompt(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.daemon.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return textResult("External agent not found: " + strings.TrimSpace(agentID)), nil
	}
	return Result{Screen: PromptData{
		Title:         externalAgentTitle(agent) + " Path",
		Placeholder:   "codex binary path",
		Value:         strings.TrimSpace(agent.Path),
		Submit:        agentsPath.command(agent.ID),
		CancelCommand: agentsOpen.command(agent.ID),
	}}, nil
}

func (d *Dispatcher) updateExternalAgentPath(ctx context.Context, agentID string, path string) (Result, error) {
	agents, err := d.daemon.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return textResult("External agent not found: " + strings.TrimSpace(agentID)), nil
	}
	agents, err = d.daemon.UpdateExternalAgent(ctx, agent.ID, core.UpdateExternalAgentRequest{Path: strings.TrimSpace(path)})
	if err != nil {
		return Result{}, err
	}
	agent, ok = findExternalAgent(agents, agent.ID)
	if !ok {
		return textResult("External agent path updated."), nil
	}
	return Result{
		Screen: externalAgentPickerData(agent),
	}, nil
}

func externalAgentPickerData(agent core.ExternalAgentDescriptor) PickerData {
	picker := NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
		Meta(externalAgentMeta(agent)).
		Back(agentsRoot.command()).
		Row("path", "Path", externalAgentPathInfo(agent), agentsPath.command(agent.ID)).
		Row("enabled", "Enabled", formatEnabled(agent.Enabled), agentsEnabled.command(agent.ID))
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName, sessionNew.command(agent.ID))
	}
	return picker.Build()
}

func externalAgentMeta(agent core.ExternalAgentDescriptor) string {
	parts := []string{}
	if version := strings.TrimSpace(agent.Version); version != "" {
		parts = append(parts, version)
	}
	if mode := strings.TrimSpace(agent.Mode); mode != "" {
		parts = append(parts, mode)
	}
	if len(parts) == 0 {
		return externalAgentInfo(agent)
	}
	return strings.Join(parts, " · ")
}

func externalAgentPathInfo(agent core.ExternalAgentDescriptor) string {
	if path := strings.TrimSpace(agent.Path); path != "" {
		return path
	}
	if !agent.Installed {
		return textutil.FirstNonEmpty(agent.Detail, "codex binary not found")
	}
	return "Default codex"
}

func findExternalAgent(agents []core.ExternalAgentDescriptor, agentID string) (core.ExternalAgentDescriptor, bool) {
	agentID = strings.ToLower(strings.TrimSpace(agentID))
	for _, agent := range agents {
		if strings.EqualFold(strings.TrimSpace(agent.ID), agentID) {
			return agent, true
		}
		for _, alias := range agent.Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), agentID) {
				return agent, true
			}
		}
	}
	return core.ExternalAgentDescriptor{}, false
}

func externalAgentTitle(agent core.ExternalAgentDescriptor) string {
	if title := strings.TrimSpace(agent.DisplayName); title != "" {
		return title
	}
	return strings.TrimSpace(agent.ID)
}

func externalAgentInfo(agent core.ExternalAgentDescriptor) string {
	switch {
	case !agent.Installed:
		return textutil.FirstNonEmpty(agent.Detail, "Not installed")
	case agent.Enabled:
		return "Enabled · " + textutil.FirstNonEmpty(agent.Version, agent.Mode, "external")
	default:
		return "Installed · disabled" + externalAgentVersionSuffix(agent)
	}
}

func externalAgentVersionSuffix(agent core.ExternalAgentDescriptor) string {
	if version := strings.TrimSpace(agent.Version); version != "" {
		return " · " + version
	}
	return ""
}
