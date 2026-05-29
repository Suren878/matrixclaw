package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleExternalAgents(ctx context.Context, args string) (Result, error) {
	if d.externalAgents == nil {
		return unsupportedRuntime("external agents"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.externalAgentsPicker(ctx)
	}
	step, rest := firstCommandStep(args)
	if step == "" {
		return d.externalAgentsPicker(ctx)
	}
	switch step {
	case "enable":
		agentID, _ := firstCommandToken(rest)
		if agentID == "" {
			return Result{Handled: true, Text: "Usage: /modules agents enable <agent>"}, nil
		}
		return d.updateExternalAgentEnabled(ctx, agentID, true)
	case "disable":
		agentID, _ := firstCommandToken(rest)
		if agentID == "" {
			return Result{Handled: true, Text: "Usage: /modules agents disable <agent>"}, nil
		}
		return d.updateExternalAgentEnabled(ctx, agentID, false)
	default:
		agentID, agentRest := firstCommandToken(args)
		if agentRest == "" {
			return d.externalAgentPicker(ctx, agentID)
		}
		action, actionRest := firstCommandStep(agentRest)
		switch action {
		case "enabled":
			return d.externalAgentEnabledPicker(ctx, agentID)
		case "set-enabled":
			return d.setExternalAgentEnabled(ctx, agentID, actionRest)
		case "path":
			if actionRest == "" {
				return d.externalAgentPathPrompt(ctx, agentID)
			}
			return d.updateExternalAgentPath(ctx, agentID, actionRest)
		default:
			return d.externalAgentPicker(ctx, agentID)
		}
	}
}

func (d *Dispatcher) externalAgentsPicker(ctx context.Context) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerExternalAgents, "External Agents").Back(modulesCommand())
	for _, agent := range agents {
		picker.Item(PickerItem{
			ID:       agent.ID,
			Title:    externalAgentTitle(agent),
			Info:     externalAgentInfo(agent),
			Selected: agent.Enabled,
			Command:  externalAgentCommand(agent.ID),
		})
	}
	if len(agents) == 0 {
		picker.Static("empty", "No external agents", "No runtimes registered")
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) externalAgentPicker(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	picker := NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
		Context(agent.ID).
		Meta(externalAgentMeta(agent)).
		Back(externalAgentsCommand())
	addExternalAgentEditableItems(picker, agent)
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName, externalAgentNewSessionCommand(agent.ID))
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) externalAgentEnabledPicker(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
			Context(agent.ID).
			Meta("Currently " + strings.ToLower(formatEnabled(agent.Enabled))).
			Popup().
			Item(PickerItem{ID: "on", Title: "On", Selected: agent.Enabled, Command: externalAgentSetEnabledCommand(agent.ID, "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: !agent.Enabled, Command: externalAgentSetEnabledCommand(agent.ID, "off")}).
			Ptr(),
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
	agents, err := d.externalAgents.UpdateExternalAgent(ctx, agentID, core.UpdateExternalAgentRequest{Enabled: &enabled})
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent updated."}, nil
	}
	action := "disabled"
	if agent.Enabled {
		action = "enabled"
	}
	return Result{
		Handled: true,
		Text:    agent.DisplayName + " " + action + ".",
		Picker:  externalAgentPickerData(agent),
	}, nil
}

func (d *Dispatcher) externalAgentPathPrompt(ctx context.Context, agentID string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	return Result{Handled: true, Prompt: &PromptData{
		Title:               externalAgentTitle(agent) + " Path",
		Placeholder:         "codex binary path",
		Value:               strings.TrimSpace(agent.Path),
		SubmitCommandPrefix: externalAgentPathCommandPrefix(agent.ID),
		CancelCommand:       externalAgentCommand(agent.ID),
	}}, nil
}

func (d *Dispatcher) updateExternalAgentPath(ctx context.Context, agentID string, path string) (Result, error) {
	agents, err := d.externalAgents.ListExternalAgents(ctx)
	if err != nil {
		return Result{}, err
	}
	agent, ok := findExternalAgent(agents, agentID)
	if !ok {
		return Result{Handled: true, Text: "External agent not found: " + strings.TrimSpace(agentID)}, nil
	}
	agents, err = d.externalAgents.UpdateExternalAgent(ctx, agent.ID, core.UpdateExternalAgentRequest{Path: strings.TrimSpace(path)})
	if err != nil {
		return Result{}, err
	}
	agent, ok = findExternalAgent(agents, agent.ID)
	if !ok {
		return Result{Handled: true, Text: "External agent path updated."}, nil
	}
	return Result{
		Handled: true,
		Text:    agent.DisplayName + " path updated.",
		Picker:  externalAgentPickerData(agent),
	}, nil
}

func externalAgentPickerData(agent core.ExternalAgentDescriptor) *PickerData {
	picker := NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
		Context(agent.ID).
		Meta(externalAgentMeta(agent)).
		Back(externalAgentsCommand())
	addExternalAgentEditableItems(picker, agent)
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName, externalAgentNewSessionCommand(agent.ID))
	}
	return picker.Ptr()
}

func addExternalAgentEditableItems(picker *PickerBuilder, agent core.ExternalAgentDescriptor) {
	picker.Row("path", "Path", externalAgentPathInfo(agent), externalAgentCommand(agent.ID, "path"))
	picker.Row("enabled", "Enabled", formatEnabled(agent.Enabled), externalAgentEnabledCommand(agent.ID))
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
		return firstNonEmptyTrimmed(agent.Detail, "codex binary not found")
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
		return firstNonEmptyTrimmed(agent.Detail, "Not installed")
	case agent.Enabled:
		return "Enabled · " + firstNonEmptyTrimmed(agent.Version, agent.Mode, "external")
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
