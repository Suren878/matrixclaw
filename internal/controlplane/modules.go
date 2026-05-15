package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

func (d *Dispatcher) handleModules(ctx context.Context, args string) (Result, error) {
	fields := strings.Fields(strings.TrimSpace(args))
	if len(fields) == 0 {
		return modulesPicker(), nil
	}
	switch strings.ToLower(fields[0]) {
	case "agents":
		return d.handleExternalAgents(ctx, strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
	case "storage":
		return d.handleStorage(ctx, strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
	default:
		return modulesPicker(), nil
	}
}

func modulesPicker() Result {
	return Result{
		Handled: true,
		Picker: NewPickerData(PickerModules, "Modules").
			HideBack(true).
			Row("agents", "External Agents", "Codex", "/modules agents").
			Row("storage", "Storage", "Files", "/modules storage").
			CloseItem().
			Ptr(),
	}
}

func (d *Dispatcher) handleExternalAgents(ctx context.Context, args string) (Result, error) {
	if d.externalAgents == nil {
		return unsupportedRuntime("external agents"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.externalAgentsPicker(ctx)
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return d.externalAgentsPicker(ctx)
	}
	switch strings.ToLower(fields[0]) {
	case "enable":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /modules agents enable <agent>"}, nil
		}
		return d.updateExternalAgentEnabled(ctx, fields[1], true)
	case "disable":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /modules agents disable <agent>"}, nil
		}
		return d.updateExternalAgentEnabled(ctx, fields[1], false)
	default:
		agentID := fields[0]
		if len(fields) == 1 {
			return d.externalAgentPicker(ctx, agentID)
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(args, fields[0])), fields[1]))
		switch strings.ToLower(fields[1]) {
		case "enabled":
			return d.externalAgentEnabledPicker(ctx, agentID)
		case "set-enabled":
			return d.setExternalAgentEnabled(ctx, agentID, rest)
		case "path":
			if rest == "" {
				return d.externalAgentPathPrompt(ctx, agentID)
			}
			return d.updateExternalAgentPath(ctx, agentID, rest)
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
	picker := NewPickerData(PickerExternalAgents, "External Agents").Back("/modules")
	for _, agent := range agents {
		picker.Item(PickerItem{
			ID:       agent.ID,
			Title:    externalAgentTitle(agent),
			Info:     externalAgentInfo(agent),
			Selected: agent.Enabled,
		})
	}
	if len(agents) == 0 {
		picker.Item(PickerItem{ID: "empty", Title: "No external agents", Info: "No runtimes registered"})
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
		Back("/modules agents")
	addExternalAgentEditableItems(picker, agent)
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName)
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
		Picker: NewPickerData(PickerExternalAgentOn, "Enabled").
			Context(agent.ID).
			Meta(externalAgentTitle(agent)).
			Back("/modules agents " + agent.ID).
			Item(PickerItem{ID: "enable", Title: "Enable", Selected: agent.Enabled}).
			Item(PickerItem{ID: "disable", Title: "Disable", Selected: !agent.Enabled}).
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
		SubmitCommandPrefix: "/modules agents " + agent.ID + " path ",
		CancelCommand:       "/modules agents " + agent.ID,
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
		Back("/modules agents")
	addExternalAgentEditableItems(picker, agent)
	if agent.Enabled {
		picker.Action("new", "New Session", "Create session using "+agent.DisplayName)
	}
	return picker.Ptr()
}

func addExternalAgentEditableItems(picker *PickerBuilder, agent core.ExternalAgentDescriptor) {
	picker.Row("path", "Path", externalAgentPathInfo(agent))
	picker.Row("enabled", "Enabled", formatEnabled(agent.Enabled))
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

func (d *Dispatcher) handleStorage(ctx context.Context, args string) (Result, error) {
	if d.storage == nil {
		return unsupportedRuntime("storage"), nil
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return d.storagePicker(ctx)
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return d.storagePicker(ctx)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
	switch strings.ToLower(fields[0]) {
	case "import":
		return d.storageImport(ctx, rest)
	case "temp":
		return d.storageTempPicker(ctx)
	case "temp-file":
		return d.storageTempFilePicker(ctx, rest)
	case "temp-promote":
		return d.storageTempPromote(ctx, rest)
	case "temp-delete":
		return d.storageTempDeleteConfirm(rest), nil
	case "temp-delete-confirm":
		return d.storageTempDelete(ctx, rest)
	case "temp-cleanup":
		return d.storageTempCleanup(ctx)
	case "temp-cleanup-confirm":
		return d.storageTempCleanupConfirmed(ctx)
	case "temp-cleanup-mode":
		return d.storageTempCleanupModePicker(ctx)
	case "temp-toggle":
		return d.storageTempToggle(ctx, rest)
	case "temp-days":
		return d.storageTempDays(ctx, rest)
	case "temp-max":
		return d.storageTempMax(ctx, rest)
	case "files":
		return d.storageFilesPicker(ctx)
	case "file":
		return d.storageFilePicker(ctx, rest)
	case "read":
		return d.storageRead(ctx, rest)
	case "delete":
		return d.storageDeleteConfirm(rest), nil
	case "delete-confirm":
		return d.storageDelete(ctx, rest)
	default:
		return d.storagePicker(ctx)
	}
}
