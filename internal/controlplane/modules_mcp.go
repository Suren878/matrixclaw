package controlplane

import (
	"context"
	"strconv"
	"strings"

	"github.com/Suren878/matrixclaw/internal/setup"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

const managedBrowserMCPServerID = "browser"

var (
	mcpRoot             = verb(CommandModules, "mcp", 0)
	mcpEnabled          = verb(CommandModules, "mcp enabled", 0)
	mcpSetEnabled       = verb(CommandModules, "mcp set-enabled", 1)
	mcpAdd              = verb(CommandModules, "mcp add", 0)
	mcpCreate           = valueVerb(CommandModules, "mcp create", 0)
	mcpOpen             = verb(CommandModules, "mcp open", 1)
	mcpServerEnabled    = verb(CommandModules, "mcp server-enabled", 1)
	mcpServerSetEnabled = verb(CommandModules, "mcp server-set-enabled", 2)
	mcpInfo             = verb(CommandModules, "mcp info", 1)
	mcpEdit             = verb(CommandModules, "mcp edit", 1)
	mcpField            = verb(CommandModules, "mcp field", 2)
	mcpSet              = valueVerb(CommandModules, "mcp set", 2)
	mcpSetTransport     = valueVerb(CommandModules, "mcp set-transport", 2)
	mcpDelete           = verb(CommandModules, "mcp delete", 1)
	mcpDeleteConfirm    = verb(CommandModules, "mcp delete-confirm", 1)
)

func (d *Dispatcher) handleMCP(ctx context.Context, r Route) (Result, error) {
	switch r.Verb {
	case mcpEnabled:
		return d.mcpEnabledPicker(ctx)
	case mcpSetEnabled:
		return d.setMCPEnabled(ctx, r.arg(0))
	case mcpAdd:
		return d.mcpServerAddPrompt(), nil
	case mcpCreate:
		return d.createMCPServer(ctx, r.Value)
	}
	serverID := r.arg(0)
	if serverID == "" {
		return d.mcpPicker(ctx)
	}
	switch r.Verb {
	case mcpServerEnabled:
		return d.mcpServerEnabledPicker(ctx, serverID)
	case mcpServerSetEnabled:
		return d.setMCPServerEnabled(ctx, serverID, r.arg(1))
	case mcpInfo:
		return d.mcpServerInfo(ctx, serverID)
	case mcpEdit:
		return d.mcpServerEditForm(ctx, serverID)
	case mcpField:
		return d.mcpServerFieldPrompt(ctx, serverID, r.arg(1))
	case mcpSet:
		return d.setMCPServerField(ctx, serverID, r.arg(1), r.Value)
	case mcpSetTransport:
		return d.setMCPServerTransport(ctx, serverID, r.arg(1), r.Value)
	case mcpDelete:
		return d.mcpServerDeleteConfirm(ctx, serverID)
	case mcpDeleteConfirm:
		return d.deleteMCPServer(ctx, serverID)
	default:
		return d.mcpServerPicker(ctx, serverID)
	}
}

func (d *Dispatcher) mcpPicker(ctx context.Context) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	servers := externalMCPServers(resp.Config.Servers)
	picker := NewPickerData(PickerMCP, "External MCP Servers").
		Command(mcpRoot.command()).
		Back(modulesRoot.command()).
		Row("enabled", "External MCP", formatEnabled(resp.Config.Enabled), mcpEnabled.command()).
		Action("add", "Add Server", "", mcpAdd.command())
	for _, server := range servers {
		picker.Item(PickerItem{
			ID:       server.ID,
			Title:    mcpServerTitle(server),
			Info:     mcpServerInfoText(server),
			Selected: resp.Config.Enabled && server.Enabled,
			Command:  mcpOpen.command(server.ID),
		})
	}
	if len(servers) == 0 {
		picker.Static("empty", "No external MCP servers", "Add a server or install a plugin.")
	}
	return Result{Screen: picker.Build()}, nil
}

func (d *Dispatcher) mcpEnabledPicker(ctx context.Context) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Screen: NewPickerData(PickerMCP, "External MCP").
		Meta("Currently " + strings.ToLower(formatEnabled(resp.Config.Enabled))).
		Select(mcpRoot.command()).
		Item(PickerItem{ID: "on", Title: "On", Selected: resp.Config.Enabled, Command: mcpSetEnabled.command("on")}).
		Item(PickerItem{ID: "off", Title: "Off", Selected: !resp.Config.Enabled, Command: mcpSetEnabled.command("off")}).
		Build()}, nil
}

func (d *Dispatcher) setMCPEnabled(ctx context.Context, value string) (Result, error) {
	enabled, ok := parseEnabledChoice(value)
	if !ok {
		return d.mcpEnabledPicker(ctx)
	}
	if _, err := d.daemon.UpdateMCPConfig(ctx, setup.MCPConfigUpdate{Enabled: &enabled}); err != nil {
		return textResult(err.Error()), nil
	}
	return d.mcpPicker(ctx)
}

func (d *Dispatcher) mcpServerAddPrompt() Result {
	return Result{Screen: PromptData{
		Title:         "MCP Server ID",
		Placeholder:   "docs",
		Submit:        mcpCreate.command(),
		CancelCommand: mcpRoot.command(),
	}}
}

func (d *Dispatcher) createMCPServer(ctx context.Context, value string) (Result, error) {
	serverID := mcpServerIDFromInput(value)
	if serverID == "" {
		return d.mcpServerAddPrompt(), nil
	}
	if isManagedMCPServerID(serverID) {
		return textResult("MCP server id is reserved for the Browser module: " + serverID), nil
	}
	server := setup.MCPServerConfig{
		ID:             serverID,
		Name:           serverID,
		Enabled:        false,
		Transport:      "stdio",
		Command:        serverID,
		ToolPrefix:     serverID,
		TimeoutSeconds: 30,
	}
	resp, err := d.daemon.CreateMCPServer(ctx, server)
	if err != nil {
		return textResult(err.Error()), nil
	}
	if created, ok := findExternalMCPServer(resp.Config.Servers, serverID); ok {
		return d.mcpServerEditForm(ctx, created.ID)
	}
	return d.mcpPicker(ctx)
}

func (d *Dispatcher) mcpServerPicker(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
	}
	return Result{Screen: NewPickerData(PickerMCPServer, mcpServerTitle(server)).
		Meta(mcpServerInfoText(server)).
		Back(mcpRoot.command()).
		Row("enabled", "Enabled", formatEnabled(server.Enabled), mcpServerEnabled.command(server.ID)).
		Row("info", "Details", mcpServerTarget(server), mcpInfo.command(server.ID)).
		Action("edit", "Edit Config", "", mcpEdit.command(server.ID)).
		Danger("delete", "Delete Server", "", mcpDelete.command(server.ID)).
		Build()}, nil
}

func (d *Dispatcher) mcpServerEditForm(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
	}
	fields := []FormField{
		{ID: "name", Label: "Name", Value: server.Name},
		{ID: "transport", Label: "Transport", Value: server.Transport},
		{ID: "command", Label: "Command", Value: server.Command},
		{ID: "args", Label: "Args", Value: strings.Join(server.Args, " ")},
		{ID: "endpoint", Label: "Endpoint", Value: server.Endpoint},
		{ID: "tool_prefix", Label: "Tool Prefix", Value: server.ToolPrefix},
		{ID: "read_only", Label: "Read Only", Value: formatEnabled(server.ReadOnly)},
		{ID: "timeout", Label: "Timeout Seconds", Value: formatInt(server.TimeoutSeconds)},
	}
	for i := range fields {
		fields[i].EditCommand = mcpField.command(server.ID, fields[i].ID)
	}
	return Result{Screen: FormData{
		Title:         "Edit " + mcpServerTitle(server),
		Fields:        fields,
		SubmitLabel:   "Done",
		CancelLabel:   "Close",
		SubmitCommand: mcpOpen.command(server.ID),
		CancelCommand: mcpOpen.command(server.ID),
	}}, nil
}

func (d *Dispatcher) mcpServerFieldPrompt(ctx context.Context, serverID string, field string) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
	}
	title, value, placeholder := mcpServerFieldPromptText(server, field)
	return Result{Screen: PromptData{
		Title:         title,
		Placeholder:   placeholder,
		Value:         value,
		Submit:        mcpSet.command(server.ID, field),
		CancelCommand: mcpEdit.command(server.ID),
	}}, nil
}

func (d *Dispatcher) setMCPServerField(ctx context.Context, serverID string, field string, value string) (Result, error) {
	if field == "transport" {
		transport, target, _ := strings.Cut(value, " ")
		return d.setMCPServerTransport(ctx, serverID, transport, target)
	}
	update, ok := mcpServerUpdateForField(field, value)
	if !ok {
		return d.mcpServerEditForm(ctx, serverID)
	}
	if _, err := d.daemon.UpdateMCPServer(ctx, serverID, update); err != nil {
		return textResult(err.Error()), nil
	}
	return d.mcpServerEditForm(ctx, serverID)
}

// setMCPServerTransport switches a server's transport with the endpoint or
// command it needs; when it lacks one and target is empty, it asks for it so
// both are saved together.
func (d *Dispatcher) setMCPServerTransport(ctx context.Context, serverID string, transport string, target string) (Result, error) {
	transport, target = strings.TrimSpace(transport), strings.TrimSpace(target)
	if transport == "" {
		return d.mcpServerEditForm(ctx, serverID)
	}
	field := "command"
	if setup.NormalizeMCPTransport(transport) == "http" {
		field = "endpoint"
	}
	if target == "" {
		resp, err := d.daemon.MCPConfig(ctx)
		if err != nil {
			return Result{}, err
		}
		server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
		if !ok {
			return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
		}
		if title, current, placeholder := mcpServerFieldPromptText(server, field); current == "" {
			return Result{Screen: PromptData{
				Title:         title,
				Placeholder:   placeholder,
				Submit:        mcpSetTransport.command(server.ID, transport),
				CancelCommand: mcpEdit.command(server.ID),
			}}, nil
		}
	}
	update := setup.MCPServerUpdate{Transport: &transport}
	switch {
	case target == "":
	case field == "endpoint":
		update.Endpoint = &target
	default:
		update.Command = &target
	}
	if _, err := d.daemon.UpdateMCPServer(ctx, serverID, update); err != nil {
		return textResult(err.Error()), nil
	}
	return d.mcpServerEditForm(ctx, serverID)
}

func (d *Dispatcher) mcpServerEnabledPicker(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
	}
	return Result{Screen: NewPickerData(PickerMCPServer, mcpServerTitle(server)).
		Meta("Currently " + strings.ToLower(formatEnabled(server.Enabled))).
		Select(mcpOpen.command(server.ID)).
		Item(PickerItem{ID: "on", Title: "On", Selected: server.Enabled, Command: mcpServerSetEnabled.command(server.ID, "on")}).
		Item(PickerItem{ID: "off", Title: "Off", Selected: !server.Enabled, Command: mcpServerSetEnabled.command(server.ID, "off")}).
		Build()}, nil
}

func (d *Dispatcher) setMCPServerEnabled(ctx context.Context, serverID string, value string) (Result, error) {
	enabled, ok := parseEnabledChoice(value)
	if !ok {
		return d.mcpServerEnabledPicker(ctx, serverID)
	}
	if _, err := d.daemon.UpdateMCPServer(ctx, serverID, setup.MCPServerUpdate{Enabled: &enabled}); err != nil {
		return textResult(err.Error()), nil
	}
	return d.mcpServerPicker(ctx, serverID)
}

func (d *Dispatcher) mcpServerDeleteConfirm(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
	}
	return Result{Screen: ConfirmData{
		Title:          "Delete MCP Server",
		Message:        "Delete external MCP server " + mcpServerTitle(server) + "?",
		ConfirmLabel:   "Delete",
		CancelLabel:    "Close",
		ConfirmCommand: mcpDeleteConfirm.command(server.ID),
		CancelCommand:  mcpOpen.command(server.ID),
		ConfirmDanger:  true,
	}}, nil
}

func (d *Dispatcher) deleteMCPServer(ctx context.Context, serverID string) (Result, error) {
	if _, err := d.daemon.DeleteMCPServer(ctx, serverID); err != nil {
		return textResult(err.Error()), nil
	}
	return d.mcpPicker(ctx)
}

func (d *Dispatcher) mcpServerInfo(ctx context.Context, serverID string) (Result, error) {
	resp, err := d.daemon.MCPConfig(ctx)
	if err != nil {
		return Result{}, err
	}
	server, ok := findExternalMCPServer(resp.Config.Servers, serverID)
	if !ok {
		return textResult("MCP server not found: " + strings.TrimSpace(serverID)), nil
	}
	return Result{Screen: InfoData{
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
	return strings.Join(textutil.NonBlank(formatEnabled(server.Enabled), server.Transport, mcpServerTarget(server)), " · ")
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
