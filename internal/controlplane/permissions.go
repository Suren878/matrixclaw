package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

const permissionsUsage = "Usage: /permissions default|accept_edits|full_auto|plan, /permissions add allow|ask|deny <tool> [pattern] [global], /permissions delete <rule id>. " +
	"Deny rules match commands, not the code an interpreter runs (python -c, node -e, awk, sed e, make and npm scripts, test runners): " +
	"allowing such a command allows any code, and a test runner or build (bash: go test:*, npm test:*, make:*) runs code the agent may edit first under accept_edits. " +
	"web_fetch rules cover every URL web_fetch fetches, redirects included; MCP browser tools follow mcp rules only."

const planUsage = "Usage: /plan switches this session to plan mode: the agent explores with read-only tools and asks you to approve its plan; " +
	"approving switches the session to accept_edits and the agent carries the plan out. Describe the task in your next message."

// Text after /plan only gets the usage.
var _ = valueVerb(CommandPlan, "", 0)

var (
	permissionsRoot   = valueVerb(CommandPermissions, "", 0)
	permissionsAdd    = valueVerb(CommandPermissions, "add", 0)
	permissionsDelete = verb(CommandPermissions, "delete", 1)
	permissionsDrop   = verb(CommandPermissions, "delete-confirm", 1)
)

// handlePlan switches the session to plan mode, as /permissions plan does.
func (d *Dispatcher) handlePlan(ctx context.Context, r Route) (Result, error) {
	if r.Value != "" {
		return textResult(planUsage), nil
	}
	return d.handlePermissions(ctx, Route{Verb: permissionsRoot, Value: string(core.PermissionModePlan)})
}

func (d *Dispatcher) handlePermissions(ctx context.Context, r Route) (Result, error) {
	_, session, err := d.currentSession(ctx)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return textResult("Select or create a session first."), nil
	}
	if !core.CapabilitiesForSession(*session).PermissionMode {
		return textResult("Permission Mode is available for Matrixclaw sessions only."), nil
	}

	switch {
	case r.Is(permissionsAdd):
		return d.addPermissionRule(ctx, *session, r.Value)
	case r.Is(permissionsDelete), r.Is(permissionsDrop):
		return d.deletePermissionRule(ctx, *session, r.arg(0), r.Is(permissionsDrop))
	case r.Value == "":
		return d.permissionsPicker(ctx, *session)
	}
	mode, ok := parsePermissionMode(r.Value)
	if !ok {
		return textResult(permissionsUsage), nil
	}
	if !d.owner() {
		return textResult("Only the owner can switch the permission mode."), nil
	}
	updated, err := d.daemon.UpdateSessionPermissionMode(ctx, session.ID, mode)
	if err != nil {
		return Result{}, err
	}
	text := "✅ Permission mode: " + permissionModeStatus(updated.PermissionMode)
	if _, err := d.daemon.CreateSystemMessage(ctx, updated.ID, text); err != nil {
		return Result{}, err
	}
	return Result{
		Screen:         TextData{Text: text},
		ReloadSnapshot: true,
	}, nil
}

// permissionsPicker offers the permission modes and lists the session's rules;
// choosing a rule deletes it.
func (d *Dispatcher) permissionsPicker(ctx context.Context, session core.Session) (Result, error) {
	picker := NewPickerData(PickerPermissions, "Permissions").Select("").Items(permissionModeItems(session.PermissionMode)...)
	rules, err := d.daemon.SessionPermissionRules(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	for _, rule := range rules {
		picker.Danger(rule.ID, string(rule.Effect)+" "+rule.String(), ruleScopeLabel(rule, session.ID), permissionsDelete.command(rule.ID))
	}
	return Result{Screen: picker.Build()}, nil
}

func ruleScopeLabel(rule permission.Rule, sessionID string) string {
	switch {
	case rule.Scope == permission.ScopeGlobal:
		return "global"
	case rule.SessionID == sessionID:
		return "this session"
	default:
		return "parent session"
	}
}

// addPermissionRule reads "allow|ask|deny <tool> [pattern] [global]".
func (d *Dispatcher) addPermissionRule(ctx context.Context, session core.Session, args string) (Result, error) {
	fields := strings.Fields(args)
	if len(fields) < 2 || !permission.Effect(strings.ToLower(fields[0])).Valid() {
		return textResult(permissionsUsage), nil
	}
	request := core.PermissionRuleRequest{Effect: permission.Effect(strings.ToLower(fields[0])), Tool: fields[1], Scope: permission.ScopeSession}
	pattern := fields[2:]
	if last := len(pattern) - 1; last >= 0 && strings.EqualFold(pattern[last], "global") {
		request.Scope, pattern = permission.ScopeGlobal, pattern[:last]
	}
	request.Pattern = strings.Join(pattern, " ")
	if !d.managesRules(request.Scope) {
		return textResult(rulesRefusal(request.Scope)), nil
	}
	rule, err := d.daemon.AddPermissionRule(ctx, session.ID, request)
	if err != nil {
		return Result{}, err
	}
	return textResult("✅ Rule added: " + string(rule.Effect) + " " + rule.String() + " (" + ruleScopeLabel(rule, session.ID) + ")"), nil
}

func rulesRefusal(scope permission.Scope) string {
	if scope == permission.ScopeGlobal {
		return "Only the owner can change global rules."
	}
	return "Guests cannot change permission rules."
}

// deletePermissionRule asks first, then deletes one of the session's rules.
func (d *Dispatcher) deletePermissionRule(ctx context.Context, session core.Session, ruleID string, confirmed bool) (Result, error) {
	rules, err := d.daemon.SessionPermissionRules(ctx, session.ID)
	if err != nil {
		return Result{}, err
	}
	var rule permission.Rule
	for _, candidate := range rules {
		if candidate.ID == ruleID {
			rule = candidate
		}
	}
	if rule.ID == "" {
		return textResult("No such rule for this session."), nil
	}
	if !d.managesRules(rule.Scope) {
		return textResult(rulesRefusal(rule.Scope)), nil
	}
	label := string(rule.Effect) + " " + rule.String()
	if !confirmed {
		return Result{Screen: ConfirmData{
			Title:          "Delete rule",
			Message:        "Delete " + label + " (" + ruleScopeLabel(rule, session.ID) + ")?",
			ConfirmLabel:   "Delete",
			ConfirmCommand: permissionsDrop.command(rule.ID),
			CancelCommand:  permissionsRoot.command(),
			ConfirmDanger:  true,
		}}, nil
	}
	if err := d.daemon.DeletePermissionRule(ctx, rule.ID); err != nil {
		return Result{}, err
	}
	return textResult("🗑️ Rule deleted: " + label), nil
}

func permissionModeItems(current core.PermissionMode) []PickerItem {
	current = core.NormalizePermissionMode(string(current))
	modes := []struct {
		mode  core.PermissionMode
		title string
	}{
		{mode: core.PermissionModeDefault, title: "Ask First"},
		{mode: core.PermissionModeAcceptEdits, title: "Edits Only"},
		{mode: core.PermissionModeFullAuto, title: "Full Auto"},
		{mode: core.PermissionModePlan, title: "Plan"},
	}
	items := make([]PickerItem, 0, len(modes))
	for _, mode := range modes {
		items = append(items, PickerItem{
			ID:       string(mode.mode),
			Title:    mode.title,
			Command:  permissionsRoot.commandValue(string(mode.mode)),
			Selected: current == mode.mode,
		})
	}
	return items
}

func parsePermissionMode(value string) (core.PermissionMode, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
	switch normalized {
	case "default", "safe":
		return core.PermissionModeDefault, true
	case "accept_edits", "accept", "edits", "auto_edit", "auto_edits":
		return core.PermissionModeAcceptEdits, true
	case "full_auto", "full_access", "full", "auto", "access":
		return core.PermissionModeFullAuto, true
	case "plan":
		return core.PermissionModePlan, true
	default:
		return core.PermissionModeDefault, false
	}
}
