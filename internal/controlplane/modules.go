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
			Row("agents", "External Agents", "Codex and future external runtimes", "/modules agents").
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
		return d.updateExternalAgent(ctx, fields[1], true)
	case "disable":
		if len(fields) < 2 {
			return Result{Handled: true, Text: "Usage: /modules agents disable <agent>"}, nil
		}
		return d.updateExternalAgent(ctx, fields[1], false)
	default:
		return d.externalAgentPicker(ctx, fields[0])
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
		Back("/modules agents")
	addExternalAgentDetails(picker, agent)
	if agent.Enabled {
		picker.Row("new", "New Session", "Create session using "+agent.DisplayName)
		picker.Row("disable", "Disable", "Hide from new session picker")
	} else if agent.Installed {
		picker.Row("enable", "Enable", "Allow new sessions using "+agent.DisplayName)
	} else {
		picker.Item(PickerItem{ID: "not_installed", Title: "Not installed", Info: agent.Detail})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) updateExternalAgent(ctx context.Context, agentID string, enabled bool) (Result, error) {
	agents, err := d.externalAgents.UpdateExternalAgent(ctx, agentID, enabled)
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

func externalAgentPickerData(agent core.ExternalAgentDescriptor) *PickerData {
	picker := NewPickerData(PickerExternalAgent, externalAgentTitle(agent)).
		Context(agent.ID).
		Back("/modules agents")
	addExternalAgentDetails(picker, agent)
	if agent.Enabled {
		picker.Row("new", "New Session", "Create session using "+agent.DisplayName)
		picker.Row("disable", "Disable", "Hide from new session picker")
	} else if agent.Installed {
		picker.Row("enable", "Enable", "Allow new sessions using "+agent.DisplayName)
	} else {
		picker.Item(PickerItem{ID: "not_installed", Title: "Not installed", Info: agent.Detail})
	}
	return picker.Ptr()
}

func addExternalAgentDetails(picker *PickerBuilder, agent core.ExternalAgentDescriptor) {
	refreshCommand := "/modules agents " + strings.TrimSpace(agent.ID)
	picker.Item(PickerItem{ID: "state", Title: "State", Info: externalAgentInfo(agent), Command: refreshCommand})
	if mode := strings.TrimSpace(agent.Mode); mode != "" {
		picker.Item(PickerItem{ID: "mode", Title: "Mode", Info: mode, Command: refreshCommand})
	}
	if version := strings.TrimSpace(agent.Version); version != "" {
		picker.Item(PickerItem{ID: "version", Title: "Version", Info: version, Command: refreshCommand})
	}
	if path := strings.TrimSpace(agent.Path); path != "" {
		picker.Item(PickerItem{ID: "path", Title: "Path", Info: path, Command: refreshCommand})
	}
	if detail := strings.TrimSpace(agent.Detail); detail != "" && agent.Installed {
		picker.Item(PickerItem{ID: "detail", Title: "Detail", Info: detail, Command: refreshCommand})
	}
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
