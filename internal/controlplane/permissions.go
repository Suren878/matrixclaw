package controlplane

import (
	"context"
	"strings"

	"github.com/Suren878/matrixclaw/internal/core"
)

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

	args = strings.TrimSpace(args)
	if args == "" {
		return Result{
			Handled: true,
			Picker:  NewPickerData(PickerPermissions, "Permission Mode").Context(session.ID).Popup().Items(permissionModeItems(session.PermissionMode)...).Ptr(),
		}, nil
	}

	mode, ok := parsePermissionMode(args)
	if !ok {
		return Result{Handled: true, Text: "Usage: /permissions default|accept_edits|full_auto"}, nil
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
