package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/permission"
)

const permissionsUsage = "Usage: /permissions default|accept_edits|full_auto, /permissions add allow|ask|deny <tool> [pattern] [global], /permissions delete <rule id>. " +
	"Allowing a test runner or build (bash: go test:*, npm test:*, make:*) runs code, and under accept_edits the agent can edit that code first."

func (d *Dispatcher) handlePermissions(ctx context.Context, externalKey string, args string) (Result, error) {
	if d.permissions == nil {
		return unsupportedRuntime("permission"), nil
	}
	if d.sessions == nil {
		return unsupportedRuntime("sessions"), nil
	}
	_, session, err := d.currentSession(ctx, externalKey)
	if err != nil {
		return Result{}, err
	}
	if session == nil {
		return Result{Handled: true, Text: "Select or create a session first."}, nil
	}
	if !core.CapabilitiesForSession(*session).PermissionMode {
		return Result{Handled: true, Text: "Permission Mode is available for Matrixclaw sessions only."}, nil
	}

	action, rest := cutWord(args)
	switch strings.ToLower(action) {
	case "":
		return d.permissionsPicker(ctx, *session)
	case "add":
		return d.addPermissionRule(ctx, *session, rest)
	case "delete":
		return d.deletePermissionRule(ctx, *session, rest)
	}
	mode, ok := parsePermissionMode(args)
	if !ok {
		return Result{Handled: true, Text: permissionsUsage}, nil
	}
	if !d.permissions.ManagesPermissionMode() {
		return Result{Handled: true, Text: "Only the owner can switch the permission mode."}, nil
	}
	updated, err := d.permissions.UpdateSessionPermissionMode(ctx, session.ID, mode)
	if err != nil {
		return Result{}, err
	}
	text := "✅ Permission mode: " + permissionModeStatus(updated.PermissionMode)
	if d.messages != nil {
		if _, err := d.messages.CreateSystemMessage(ctx, updated.ID, text); err != nil {
			return Result{}, err
		}
	}
	return Result{
		Handled:        true,
		Text:           text,
		ReloadSnapshot: true,
	}, nil
}

// permissionsPicker offers the permission modes and lists the session's rules;
// choosing a rule deletes it.
func (d *Dispatcher) permissionsPicker(ctx context.Context, session core.Session) (Result, error) {
	picker := NewPickerData(PickerPermissions, "Permissions").Context(session.ID).Select("").Items(permissionModeItems(session.PermissionMode)...)
	if d.rules != nil {
		rules, err := d.rules.SessionPermissionRules(ctx, session.ID)
		if err != nil {
			return Result{}, err
		}
		for _, rule := range rules {
			picker.Danger(rule.ID, string(rule.Effect)+" "+rule.String(), ruleScopeLabel(rule, session.ID), permissionsCommand("delete", rule.ID))
		}
	}
	return Result{Handled: true, Picker: picker.Ptr()}, nil
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
	if d.rules == nil {
		return unsupportedRuntime("permission rule"), nil
	}
	fields := strings.Fields(args)
	if len(fields) < 2 || !permission.Effect(strings.ToLower(fields[0])).Valid() {
		return Result{Handled: true, Text: permissionsUsage}, nil
	}
	request := core.PermissionRuleRequest{Effect: permission.Effect(strings.ToLower(fields[0])), Tool: fields[1], Scope: permission.ScopeSession}
	pattern := fields[2:]
	if last := len(pattern) - 1; last >= 0 && strings.EqualFold(pattern[last], "global") {
		request.Scope, pattern = permission.ScopeGlobal, pattern[:last]
	}
	request.Pattern = strings.Join(pattern, " ")
	if !d.rules.ManagesRules(request.Scope) {
		return Result{Handled: true, Text: rulesRefusal(request.Scope)}, nil
	}
	rule, err := d.rules.AddPermissionRule(ctx, session.ID, request)
	if err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "✅ Rule added: " + string(rule.Effect) + " " + rule.String() + " (" + ruleScopeLabel(rule, session.ID) + ")"}, nil
}

func rulesRefusal(scope permission.Scope) string {
	if scope == permission.ScopeGlobal {
		return "Only the owner can change global rules."
	}
	return "Guests cannot change permission rules."
}

// deletePermissionRule asks first, then deletes one of the session's rules.
func (d *Dispatcher) deletePermissionRule(ctx context.Context, session core.Session, args string) (Result, error) {
	if d.rules == nil {
		return unsupportedRuntime("permission rule"), nil
	}
	ruleID, confirm := cutWord(args)
	rules, err := d.rules.SessionPermissionRules(ctx, session.ID)
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
		return Result{Handled: true, Text: "No such rule for this session."}, nil
	}
	if !d.rules.ManagesRules(rule.Scope) {
		return Result{Handled: true, Text: rulesRefusal(rule.Scope)}, nil
	}
	label := string(rule.Effect) + " " + rule.String()
	if confirm != "confirm" {
		return Result{Handled: true, Confirm: &ConfirmData{
			Title:          "Delete rule",
			Message:        "Delete " + label + " (" + ruleScopeLabel(rule, session.ID) + ")?",
			ConfirmLabel:   "Delete",
			ConfirmCommand: permissionsCommand("delete", rule.ID, "confirm"),
			CancelCommand:  permissionsCommand(),
			ConfirmDanger:  true,
		}}, nil
	}
	if err := d.rules.DeletePermissionRule(ctx, rule.ID); err != nil {
		return Result{}, err
	}
	return Result{Handled: true, Text: "🗑️ Rule deleted: " + label}, nil
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
	}
	items := make([]PickerItem, 0, len(modes))
	for _, mode := range modes {
		items = append(items, PickerItem{
			ID:       string(mode.mode),
			Title:    mode.title,
			Command:  permissionsCommand(string(mode.mode)),
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
	default:
		return core.PermissionModeDefault, false
	}
}
