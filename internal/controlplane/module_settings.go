package controlplane

import (
	"context"
	"slices"
	"strings"

	"github.com/Suren878/matrixclaw/internal/modules"
	"github.com/Suren878/matrixclaw/internal/textutil"
)

// Module settings screens are drawn from the daemon's settings items:
//
//	/modules <id>                     the module's page
//	/modules <id> open <path> [group] a page, info, choice or prompt
//	/modules <id> confirm <path> [v]  ask, then set
//	/modules <id> set <path> [value]  change and show what the daemon opens
//
// A path is item keys joined by "/". Non-owners see the pages read-only.

func (d *Dispatcher) handleModuleSettings(ctx context.Context, moduleID string, args string) (Result, error) {
	step, rest := firstCommandStep(args)
	rawPath, value := firstCommandToken(rest)
	path := splitSettingPath(rawPath)
	switch step {
	case "":
		return d.moduleSettingsPage(ctx, moduleID, nil, "")
	case "open":
		return d.openModuleSetting(ctx, moduleID, path, strings.TrimSpace(value), "")
	case "confirm":
		return d.confirmModuleSetting(ctx, moduleID, path, strings.TrimSpace(value))
	case "set":
		return d.setModuleSetting(ctx, moduleID, path, strings.TrimSpace(value))
	default:
		return d.moduleSettingsPage(ctx, moduleID, nil, "")
	}
}

func (d *Dispatcher) moduleSettingsPage(ctx context.Context, moduleID string, path []string, message string) (Result, error) {
	settings, err := d.daemon.ModuleSettings(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	return d.settingsPage(moduleID, settings, path, message), nil
}

// settingsPage is the page at path (nil: the module's), one row per item.
func (d *Dispatcher) settingsPage(moduleID string, settings modules.Settings, path []string, message string) Result {
	title, items := settings.Status.Title, settings.Items
	meta := strings.Join(textutil.NonBlank(message, settings.Status.State, settings.Status.Detail), "\n")
	back := modulesCommand()
	if len(path) > 0 {
		page, ok := modules.Find(settings.Items, path)
		if !ok || page.Kind != modules.ItemPage {
			return d.settingsPage(moduleID, settings, nil, message)
		}
		title, items, meta = page.Label, page.Items, strings.Join(textutil.NonBlank(message, page.Display), "\n")
		back = settingsOpenCommand(moduleID, path[:len(path)-1])
	}
	picker := NewPickerData(PickerModule, title).Command(settingsOpenCommand(moduleID, path)).Meta(meta).Back(back)
	for _, item := range items {
		picker.Item(d.settingsRow(moduleID, append(append([]string(nil), path...), item.Key), item))
	}
	return Result{Handled: true, Picker: picker.Ptr()}
}

func (d *Dispatcher) settingsRow(moduleID string, path []string, item modules.Item) PickerItem {
	row := PickerItem{ID: item.Key, Title: item.Label, Info: settingDisplay(item), Disabled: item.Disabled}
	switch {
	case item.Kind == modules.ItemAction && item.Confirm != "":
		row.Command = settingsCommand(moduleID, "confirm", path, "")
	case item.Kind == modules.ItemAction:
		row.Command = settingsCommand(moduleID, "set", path, "")
	default:
		row.Command = settingsOpenCommand(moduleID, path)
	}
	switch {
	case item.Danger:
		row.Role = PickerItemRoleDanger
	case item.Kind == modules.ItemAction:
		row.Role = PickerItemRoleAction
	}
	if item.Disabled && item.Hint != "" {
		row.Info = item.Hint
	}
	if !d.owner() && item.Kind != modules.ItemPage && item.Kind != modules.ItemInfo {
		row.Command, row.Disabled = "", true
	}
	if row.Disabled {
		row.Command = ""
	}
	return row
}

// settingDisplay is the value shown next to an item's label.
func settingDisplay(item modules.Item) string {
	if item.Display != "" {
		return item.Display
	}
	switch item.Kind {
	case modules.ItemToggle:
		return formatEnabled(item.Value == "on")
	case modules.ItemChoice:
		for _, option := range item.Options {
			if option.Value == item.Value {
				return option.Label
			}
		}
	}
	return item.Value
}

func (d *Dispatcher) openModuleSetting(ctx context.Context, moduleID string, path []string, group string, message string) (Result, error) {
	settings, err := d.daemon.ModuleSettings(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	return d.openSetting(moduleID, settings, path, group, message), nil
}

// openSetting shows the item at path: its page, its facts, its choices or a
// prompt for its value.
func (d *Dispatcher) openSetting(moduleID string, settings modules.Settings, path []string, group string, message string) Result {
	item, ok := modules.Find(settings.Items, path)
	if !ok || len(path) == 0 {
		return d.settingsPage(moduleID, settings, nil, message)
	}
	parent := settingsOpenCommand(moduleID, path[:len(path)-1])
	editable := d.owner() && !item.Disabled
	switch item.Kind {
	case modules.ItemPage:
		return d.settingsPage(moduleID, settings, path, message)
	case modules.ItemInfo:
		rows := []InfoRow{{Label: "State", Value: item.Display}}
		for _, fact := range item.Facts {
			rows = append(rows, InfoRow{Label: fact.Label, Value: fact.Value})
		}
		return Result{Handled: true, Info: &InfoData{Title: item.Label, Rows: rows}}
	case modules.ItemToggle:
		if !editable {
			break
		}
		return Result{Handled: true, Picker: NewPickerData(PickerModule, item.Label).
			Meta(message).
			Select(parent).
			Item(PickerItem{ID: "on", Title: "On", Selected: item.Value == "on", Command: settingsCommand(moduleID, "set", path, "on")}).
			Item(PickerItem{ID: "off", Title: "Off", Selected: item.Value == "off", Command: settingsCommand(moduleID, "set", path, "off")}).
			Ptr()}
	case modules.ItemChoice:
		if !editable {
			break
		}
		return Result{Handled: true, Picker: d.choicePicker(moduleID, path, item, group, message, parent)}
	case modules.ItemText, modules.ItemSecret:
		if !editable {
			break
		}
		placeholder := item.Hint
		if item.Kind == modules.ItemSecret && item.Display != "" && item.Display != "Not set" {
			placeholder = item.Display
		}
		return Result{Handled: true, Prompt: &PromptData{
			Title:               strings.Join(textutil.NonBlank(message, item.Label), "\n"),
			Placeholder:         placeholder + " · - to clear",
			Value:               item.Value,
			SubmitCommandPrefix: settingsCommand(moduleID, "set", path, "") + " ",
			CancelCommand:       parent,
			Sensitive:           item.Kind == modules.ItemSecret,
		}}
	case modules.ItemAction:
		if editable && item.Confirm != "" {
			return d.confirmSetting(moduleID, path, item.Confirm, item.Danger, "")
		}
	}
	return d.settingsPage(moduleID, settings, path[:len(path)-1], message)
}

// choicePicker lists a choice's options; grouped options are picked group
// first.
func (d *Dispatcher) choicePicker(moduleID string, path []string, item modules.Item, group string, message string, parent string) *PickerData {
	grouped := false
	for _, option := range item.Options {
		grouped = grouped || option.Group != ""
	}
	title := item.Label
	if grouped && group != "" {
		title += ": " + group
	}
	picker := NewPickerData(PickerModule, title).Meta(message)
	if grouped && group == "" {
		picker.Command(settingsOpenCommand(moduleID, path)).Back(parent)
		var groups []string
		for _, option := range item.Options {
			if option.Group != "" && !slices.Contains(groups, option.Group) {
				groups = append(groups, option.Group)
				picker.Item(PickerItem{ID: option.Group, Title: option.Group, Command: settingsOpenCommand(moduleID, path) + " " + option.Group})
			}
		}
		return picker.Ptr()
	}
	if grouped {
		picker.Command(settingsOpenCommand(moduleID, path) + " " + group).Back(settingsOpenCommand(moduleID, path))
	} else {
		picker.Select(parent)
	}
	for _, option := range item.Options {
		if grouped && option.Group != group {
			continue
		}
		verb := "set"
		if option.Confirm != "" {
			verb = "confirm"
		}
		picker.Item(PickerItem{
			ID:       option.Value,
			Title:    option.Label,
			Info:     option.Info,
			Selected: option.Value == item.Value,
			Disabled: option.Disabled,
			Command:  settingsCommand(moduleID, verb, path, option.Value),
		})
	}
	return picker.Ptr()
}

func (d *Dispatcher) confirmModuleSetting(ctx context.Context, moduleID string, path []string, value string) (Result, error) {
	settings, err := d.daemon.ModuleSettings(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	item, ok := modules.Find(settings.Items, path)
	if !ok {
		return d.settingsPage(moduleID, settings, nil, ""), nil
	}
	question := item.Confirm
	for _, option := range item.Options {
		if option.Value == value && option.Confirm != "" {
			question = option.Confirm
		}
	}
	if question == "" {
		return d.setModuleSetting(ctx, moduleID, path, value)
	}
	return d.confirmSetting(moduleID, path, question, item.Danger, value), nil
}

func (d *Dispatcher) confirmSetting(moduleID string, path []string, question string, danger bool, value string) Result {
	label := "Continue"
	if danger {
		label = "Delete"
	}
	return Result{Handled: true, Confirm: &ConfirmData{
		Message:        question,
		ConfirmLabel:   label,
		CancelLabel:    "Close",
		ConfirmCommand: settingsCommand(moduleID, "set", path, value),
		CancelCommand:  settingsOpenCommand(moduleID, path[:len(path)-1]),
		ConfirmDanger:  danger,
	}}
}

// setModuleSetting changes the item at path and shows what the daemon opens
// next, else the item's page. Typed values: empty keeps, "-" clears.
func (d *Dispatcher) setModuleSetting(ctx context.Context, moduleID string, path []string, value string) (Result, error) {
	settings, err := d.daemon.ModuleSettings(ctx, moduleID)
	if err != nil {
		return Result{}, err
	}
	item, ok := modules.Find(settings.Items, path)
	if !ok || len(path) == 0 {
		return d.settingsPage(moduleID, settings, nil, ""), nil
	}
	if item.Kind == modules.ItemText || item.Kind == modules.ItemSecret {
		input := clearableInput(value)
		if input == nil {
			return d.settingsPage(moduleID, settings, path[:len(path)-1], ""), nil
		}
		value = *input
	}
	response, err := d.daemon.ChangeModuleSetting(ctx, moduleID, modules.ChangeRequest{Path: path, Value: value})
	if err != nil {
		return Result{}, err
	}
	if len(response.Open) > 0 {
		return d.openSetting(moduleID, response.Settings, response.Open, "", response.Message), nil
	}
	return d.settingsPage(moduleID, response.Settings, path[:len(path)-1], response.Message), nil
}

func splitSettingPath(raw string) []string {
	if raw = strings.Trim(strings.TrimSpace(raw), "/"); raw == "" {
		return nil
	}
	return strings.Split(raw, "/")
}

func settingsOpenCommand(moduleID string, path []string) string {
	if len(path) == 0 {
		return modulesCommand(moduleID)
	}
	return settingsCommand(moduleID, "open", path, "")
}

func settingsCommand(moduleID string, verb string, path []string, value string) string {
	return modulesCommand(moduleID, verb, strings.Join(path, "/"), value)
}
