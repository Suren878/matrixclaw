package controlplane

import (
	"context"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
)

const managedBrowserMCPServerID = "browser"

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
	case "add":
		return d.mcpServerAddPrompt(), nil
	case "create":
		return d.createMCPServer(ctx, rest)
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
		case "delete":
			return d.mcpServerDeleteConfirm(ctx, step)
		case "delete-confirm":
			return d.deleteMCPServer(ctx, step)
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
	servers := externalMCPServers(resp.Config.Servers)
	picker := NewPickerData(PickerMCP, "External MCP Servers").
		Back(modulesCommand()).
		Row("enabled", "External MCP", formatEnabled(resp.Config.Enabled), mcpCommand("enabled")).
		Action("add", "Add Server", "", mcpCommand("add"))
	for _, server := range servers {
		picker.Item(PickerItem{
			ID:       server.ID,
			Title:    mcpServerTitle(server),
			Info:     mcpServerInfoText(server),
			Selected: resp.Config.Enabled && server.Enabled,
			Command:  mcpServerCommand(server.ID),
		})
	}
	if len(servers) == 0 {
		picker.Static("empty", "No external MCP servers", "Add a server or install a plugin.")
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
}

func (d *Dispatcher) mcpEnabledPicker(ctx context.Context) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Picker: NewPickerData(PickerMCP, "External MCP").
		Meta("Currently " + strings.ToLower(formatEnabled(resp.Config.Enabled))).
		Select(mcpCommand()).
		Item(PickerItem{ID: "on", Title: "On", Selected: resp.Config.Enabled, Command: mcpCommand("set-enabled", "on")}).
		Item(PickerItem{ID: "off", Title: "Off", Selected: !resp.Config.Enabled, Command: mcpCommand("set-enabled", "off")}).
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

func (d *Dispatcher) mcpServerAddPrompt() Result {
	return Result{Handled: true, Prompt: &PromptData{
		Title:               "MCP Server ID",
		Placeholder:         "docs",
		SubmitCommandPrefix: mcpCommand("create") + " ",
		CancelCommand:       mcpCommand(),
	}}
}

func (d *Dispatcher) createMCPServer(ctx context.Context, value string) (Result, error) {
	serverID := mcpServerIDFromInput(value)
	if serverID == "" {
		return d.mcpServerAddPrompt(), nil
	}
	if isManagedMCPServerID(serverID) {
		return Result{Handled: true, Text: "MCP server id is reserved for the Browser module: " + serverID}, nil
	}
	server := setup.MCPServerConfig{
		ID:              serverID,
		Name:            serverID,
		Enabled:         false,
		Transport:       "stdio",
		Command:         serverID,
		ToolPrefix:      serverID,
		RequireApproval: true,
		TimeoutSeconds:  30,
	}
	resp, err := d.mcp.CreateMCPServer(ctx, server)
	if err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	if created, ok := findExternalMCPServer(resp.Config.Servers, serverID); ok {
		return d.mcpServerEditForm(ctx, created.ID)
	}
	return d.mcpPicker(ctx)
}

func (d *Dispatcher) mcpServerPicker(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	return Result{Handled: true, Picker: NewPickerData(PickerMCPServer, mcpServerTitle(server)).
		Context(server.ID).
		Meta(mcpServerInfoText(server)).
		Back(mcpCommand()).
		Row("enabled", "Enabled", formatEnabled(server.Enabled), mcpServerCommand(server.ID, "enabled")).
		Row("info", "Details", mcpServerTarget(server), mcpServerCommand(server.ID, "info")).
		Action("edit", "Edit Config", "", mcpServerCommand(server.ID, "edit")).
		Danger("delete", "Delete Server", "", mcpServerCommand(server.ID, "delete")).
		Ptr()}, nil
}

func (d *Dispatcher) mcpServerEditForm(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
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
		CancelLabel:   "Close",
		SubmitCommand: mcpServerCommand(server.ID),
		CancelCommand: mcpServerCommand(server.ID),
	}}, nil
}

func (d *Dispatcher) mcpServerFieldPrompt(ctx context.Context, serverID string, field string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
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
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	return Result{Handled: true, Picker: NewPickerData(PickerMCPServer, mcpServerTitle(server)).
		Context(server.ID).
		Meta("Currently " + strings.ToLower(formatEnabled(server.Enabled))).
		Select(mcpServerCommand(server.ID)).
		Item(PickerItem{ID: "on", Title: "On", Selected: server.Enabled, Command: mcpServerCommand(server.ID, "set-enabled", "on")}).
		Item(PickerItem{ID: "off", Title: "Off", Selected: !server.Enabled, Command: mcpServerCommand(server.ID, "set-enabled", "off")}).
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

func (d *Dispatcher) mcpServerDeleteConfirm(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return Result{Handled: true, Text: "MCP server not found: " + strings.TrimSpace(serverID)}, nil
	}
	return Result{Handled: true, Confirm: &ConfirmData{
		Title:          "Delete MCP Server",
		Message:        "Delete external MCP server " + mcpServerTitle(server) + "?",
		ConfirmLabel:   "Delete",
		CancelLabel:    "Close",
		ConfirmCommand: mcpServerCommand(server.ID, "delete-confirm"),
		CancelCommand:  mcpServerCommand(server.ID),
		ConfirmDanger:  true,
	}}, nil
}

func (d *Dispatcher) deleteMCPServer(ctx context.Context, serverID string) (Result, error) {
	if _, err := d.mcp.DeleteMCPServer(ctx, serverID); err != nil {
		return Result{Handled: true, Text: err.Error()}, nil
	}
	return d.mcpPicker(ctx)
}

func (d *Dispatcher) mcpServerInfo(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.mcp.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
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
	}}, nil
}

func mcpExternalConfigStatus(cfg setup.MCPConfig) string {
	cfg.Servers = externalMCPServers(cfg.Servers)
	return setup.MCPConfigStatus(cfg)
}

func externalMCPServers(servers []setup.MCPServerConfig) []setup.MCPServerConfig {
	out := make([]setup.MCPServerConfig, 0, len(servers))
	for _, server := range servers {
		if isManagedMCPServer(server) {
			continue
		}
		out = append(out, server)
	}
	return out
}

func findExternalMCPServer(servers []setup.MCPServerConfig, id string) (setup.MCPServerConfig, bool) {
	id = mcpServerIDFromInput(id)
	for _, server := range externalMCPServers(servers) {
		if mcpServerIDFromInput(server.ID) == id {
			return server, true
		}
	}
	return setup.MCPServerConfig{}, false
}

func isManagedMCPServer(server setup.MCPServerConfig) bool {
	return isManagedMCPServerID(server.ID)
}

func isManagedMCPServerID(id string) bool {
	return mcpServerIDFromInput(id) == managedBrowserMCPServerID
}

func mcpServerIDFromInput(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
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
