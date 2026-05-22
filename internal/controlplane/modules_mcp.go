package controlplane

import (
	"context"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

func (d *Dispatcher) handleMCP(ctx context.Context, args string) (Result, error) {
	if d.mcp == nil {
		return unsupportedRuntime("mcp"), nil
	}
	step, rest := firstCommandStep(args)
	switch step {
	case "":
		return d.mcpPicker(ctx)
	case "enabled":
		return d.mcpEnabledPicker(ctx)
	case "set-enabled":
		return d.setMCPEnabled(ctx, rest)
	default:
		action, actionRest := firstCommandStep(rest)
		switch action {
		case "":
			return d.mcpServerPicker(ctx, step)
		case "enabled":
			return d.mcpServerEnabledPicker(ctx, step)
		case "set-enabled":
			return d.setMCPServerEnabled(ctx, step, actionRest)
		case "info":
			return d.mcpServerInfo(ctx, step)
		case "edit":
			return d.mcpServerEditForm(ctx, step)
		case "field":
			field, _ := firstCommandStep(actionRest)
			return d.mcpServerFieldPrompt(ctx, step, field)
		case "set":
			field, value := firstCommandStep(actionRest)
			return d.setMCPServerField(ctx, step, field, value)
		default:
			return d.mcpServerPicker(ctx, step)
		}
	}
}

func (d *Dispatcher) mcpPicker(ctx context.Context) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	picker := NewPickerData(PickerMCP, "MCP").
		Back(modulesCommand()).
		Row("enabled", "Enabled", formatEnabled(resp.Config.Enabled))
	for _, server := range resp.Config.Servers {
		picker.Item(PickerItem{
			ID:       server.ID,
			Title:    mcpServerTitle(server),
			Info:     mcpServerInfoText(server),
			Selected: resp.Config.Enabled && server.Enabled,
		})
	}
	if len(resp.Config.Servers) == 0 {
		picker.Item(PickerItem{ID: "empty", Title: "No MCP servers", Info: "Configure MCP servers in setup or imported plugins."})
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) mcpEnabledPicker(ctx context.Context) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Picker: NewPickerData(PickerMCPEnabled, "Enable MCP module?").
		Meta("Currently " + strings.ToLower(formatEnabled(resp.Config.Enabled))).
		Back(mcpCommand()).
		Item(PickerItem{ID: "yes", Title: "Yes", Selected: resp.Config.Enabled}).
		Item(PickerItem{ID: "no", Title: "No", Selected: !resp.Config.Enabled}).
		Ptr()}, nil
}

func (d *Dispatcher) setMCPEnabled(ctx context.Context, value string) (Result, error) {
	enabled, ok := parseEnabledChoice(value)
	if !ok {
		return d.mcpEnabledPicker(ctx)
	}
	if _, err := d.mcp.UpdateMCPConfig(ctx, setup.MCPConfigUpdate{Enabled: &enabled}); err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.mcpPicker(ctx)
}

func (d *Dispatcher) mcpServerPicker(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	return Result{Handled: true, Picker: NewPickerData(PickerMCPServer, mcpServerTitle(server)).
		Context(server.ID).
		Meta(mcpServerInfoText(server)).
		Back(mcpCommand()).
		Row("enabled", "Enabled", formatEnabled(server.Enabled)).
		Row("info", "Details", mcpServerTarget(server)).
		Action("edit", "Edit Config", "").
		Ptr()}, nil
}

func (d *Dispatcher) mcpServerEditForm(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	fields := []FormField{
		{ID: "name", Label: "Name", Value: server.Name, EditCommand: mcpServerCommand(server.ID, "field", "name")},
		{ID: "transport", Label: "Transport", Value: server.Transport, EditCommand: mcpServerCommand(server.ID, "field", "transport")},
		{ID: "command", Label: "Command", Value: server.Command, EditCommand: mcpServerCommand(server.ID, "field", "command")},
		{ID: "args", Label: "Args", Value: strings.Join(server.Args, " "), EditCommand: mcpServerCommand(server.ID, "field", "args")},
		{ID: "endpoint", Label: "Endpoint", Value: server.Endpoint, EditCommand: mcpServerCommand(server.ID, "field", "endpoint")},
		{ID: "tool_prefix", Label: "Tool Prefix", Value: server.ToolPrefix, EditCommand: mcpServerCommand(server.ID, "field", "tool_prefix")},
		{ID: "read_only", Label: "Read Only", Value: formatEnabled(server.ReadOnly), EditCommand: mcpServerCommand(server.ID, "field", "read_only")},
		{ID: "require_approval", Label: "Require Approval", Value: formatEnabled(server.RequireApproval), EditCommand: mcpServerCommand(server.ID, "field", "require_approval")},
		{ID: "timeout", Label: "Timeout Seconds", Value: formatInt(server.TimeoutSeconds), EditCommand: mcpServerCommand(server.ID, "field", "timeout")},
	}
	return Result{Handled: true, Form: &FormData{
		Title:         "Edit " + mcpServerTitle(server),
		Fields:        fields,
		SubmitLabel:   "Done",
		CancelLabel:   "Back",
		SubmitCommand: mcpServerCommand(server.ID),
		CancelCommand: mcpServerCommand(server.ID),
	}}, nil
}

func (d *Dispatcher) mcpServerFieldPrompt(ctx context.Context, serverID string, field string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	title, value, placeholder := mcpServerFieldPromptText(server, field)
	return Result{Handled: true, Prompt: &PromptData{
		Title:               title,
		Placeholder:         placeholder,
		Value:               value,
		SubmitCommandPrefix: mcpServerCommand(server.ID, "set", field) + " ",
		CancelCommand:       mcpServerCommand(server.ID, "edit"),
	}}, nil
}

func (d *Dispatcher) setMCPServerField(ctx context.Context, serverID string, field string, value string) (Result, error) {
	update, ok := mcpServerUpdateForField(field, value)
	if !ok {
		return d.mcpServerEditForm(ctx, serverID)
	}
	if _, err := d.mcp.UpdateMCPServer(ctx, serverID, update); err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.mcpServerEditForm(ctx, serverID)
}

func (d *Dispatcher) mcpServerEnabledPicker(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	return Result{Handled: true, Picker: NewPickerData(PickerMCPServerOn, "Enable "+mcpServerTitle(server)+"?").
		Context(server.ID).
		Meta("Currently " + strings.ToLower(formatEnabled(server.Enabled))).
		Back(mcpServerCommand(server.ID)).
		Item(PickerItem{ID: "yes", Title: "Yes", Selected: server.Enabled}).
		Item(PickerItem{ID: "no", Title: "No", Selected: !server.Enabled}).
		Ptr()}, nil
}

func (d *Dispatcher) setMCPServerEnabled(ctx context.Context, serverID string, value string) (Result, error) {
	enabled, ok := parseEnabledChoice(value)
	if !ok {
		return d.mcpServerEnabledPicker(ctx, serverID)
	}
	if _, err := d.mcp.UpdateMCPServer(ctx, serverID, setup.MCPServerUpdate{Enabled: &enabled}); err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.mcpServerPicker(ctx, serverID)
}

func (d *Dispatcher) mcpServerInfo(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	return Result{Handled: true, Info: &InfoData{
		Title: mcpServerTitle(server),
		Rows: []InfoRow{
			{Label: "ID", Value: server.ID},
			{Label: "Enabled", Value: formatEnabled(server.Enabled)},
			{Label: "Transport", Value: server.Transport},
			{Label: "Target", Value: mcpServerTarget(server)},
			{Label: "Tool Prefix", Value: server.ToolPrefix},
		},
		CloseCommand: mcpServerCommand(server.ID),
	}}, nil
}

func findMCPServer(servers []setup.MCPServerConfig, id string) (setup.MCPServerConfig, bool) {
	for _, server := range servers {
		if strings.EqualFold(strings.TrimSpace(server.ID), strings.TrimSpace(id)) {
			return server, true
		}
	}
	return setup.MCPServerConfig{}, false
}

func mcpServerTitle(server setup.MCPServerConfig) string {
	if strings.TrimSpace(server.Name) != "" {
		return strings.TrimSpace(server.Name)
	}
	return strings.TrimSpace(server.ID)
}

func mcpServerInfoText(server setup.MCPServerConfig) string {
	return strings.Join(nonEmptyStrings(formatEnabled(server.Enabled), server.Transport, mcpServerTarget(server)), " · ")
}

func mcpServerTarget(server setup.MCPServerConfig) string {
	if strings.TrimSpace(server.Endpoint) != "" {
		return strings.TrimSpace(server.Endpoint)
	}
	return strings.Join(append([]string{strings.TrimSpace(server.Command)}, server.Args...), " ")
}

func mcpServerFieldPromptText(server setup.MCPServerConfig, field string) (title string, value string, placeholder string) {
	switch field {
	case "name":
		return "MCP Server Name", server.Name, "Docs"
	case "transport":
		return "MCP Transport", server.Transport, "stdio or http"
	case "command":
		return "MCP Command", server.Command, "server command"
	case "args":
		return "MCP Args", strings.Join(server.Args, " "), "arg1 arg2"
	case "endpoint":
		return "MCP Endpoint", server.Endpoint, "http://127.0.0.1:3333"
	case "tool_prefix":
		return "MCP Tool Prefix", server.ToolPrefix, server.ID
	case "read_only":
		return "Read Only", formatEnabled(server.ReadOnly), "yes or no"
	case "require_approval":
		return "Require Approval", formatEnabled(server.RequireApproval), "yes or no"
	case "timeout":
		return "Timeout Seconds", formatInt(server.TimeoutSeconds), "30"
	default:
		return "MCP Field", "", ""
	}
}

func mcpServerUpdateForField(field string, value string) (setup.MCPServerUpdate, bool) {
	value = strings.TrimSpace(value)
	switch field {
	case "name":
		return setup.MCPServerUpdate{Name: &value}, true
	case "transport":
		return setup.MCPServerUpdate{Transport: &value}, true
	case "command":
		return setup.MCPServerUpdate{Command: &value}, true
	case "args":
		return setup.MCPServerUpdate{Args: strings.Fields(value)}, true
	case "endpoint":
		return setup.MCPServerUpdate{Endpoint: &value}, true
	case "tool_prefix":
		return setup.MCPServerUpdate{ToolPrefix: &value}, true
	case "read_only":
		enabled, ok := parseEnabledChoice(value)
		if !ok {
			return setup.MCPServerUpdate{}, false
		}
		return setup.MCPServerUpdate{ReadOnly: &enabled}, true
	case "require_approval":
		enabled, ok := parseEnabledChoice(value)
		if !ok {
			return setup.MCPServerUpdate{}, false
		}
		return setup.MCPServerUpdate{RequireApproval: &enabled}, true
	case "timeout":
		timeout, err := strconv.Atoi(value)
		if err != nil || timeout < 0 {
			return setup.MCPServerUpdate{}, false
		}
		return setup.MCPServerUpdate{TimeoutSeconds: &timeout}, true
	default:
		return setup.MCPServerUpdate{}, false
	}
}

func formatInt(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func parseEnabledChoice(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "yes", "enable", "enabled", "on", "true":
		return true, true
	case "no", "disable", "disabled", "off", "false":
		return false, true
	default:
		return false, false
	}
}
